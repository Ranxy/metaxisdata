import { Code, ConnectError } from "@connectrpc/connect";
import { defineStore } from "pinia";
import {
  deleteNotification,
  getUnreadNotificationCount,
  listRecentNotifications,
  markAllNotificationsRead,
  markNotificationsRead,
  subscribeNotifications,
} from "@/api/notification";
import type {
  Notification,
  NotificationEvent,
} from "@/types/proto-es/v1/notification_service_pb";

/** How many messages the bell's dropdown shows. */
export const RECENT_NOTIFICATION_LIMIT = 8;

/**
 * How long the store waits before opening a stream again, and the ceiling that
 * wait grows to. The connection is the only channel the bell has, so it is retried
 * for as long as the session lives; the ceiling keeps a server that is down from
 * being hammered by every open tab.
 */
export const RECONNECT_MIN_MS = 1_000;
export const RECONNECT_MAX_MS = 30_000;

interface NotificationState {
  unreadCount: number;
  recent: Notification[];
  loadingRecent: boolean;
  /**
   * Increments for every message the stream delivers. A view that lists the inbox
   * watches this rather than the list itself, because it can be showing a later
   * page that a newly written message does not belong on.
   */
  arrivalSeq: number;
}

// The connection lives outside the store: it has to survive re-renders and be torn
// down exactly once, and a controller held in state would be reactive and
// serializable, which it is neither.
let controller: AbortController | undefined;
let running = false;
let wake: (() => void) | undefined;
let removeVisibilityListener: (() => void) | undefined;
// generation changes on every start and every stop. A connect loop has no other way to
// tell that it is the one that still owns the connection: an unmount and the mount that
// follows it can land in the same tick, and a loop that was stopped must not mistake the
// new start for its own and keep reconnecting beside it.
let generation = 0;

// Tickets for the two values the server also pushes. A fetch that was already in flight
// when a message arrived is older than that message, so it must not land on top of it:
// only the newest writer of each value may write it. This is the guard `usePagedFetch`
// uses for the same reason.
let countTicket = 0;
let recentTicket = 0;

/** A wait that stopStreaming can cut short, so teardown never waits out a backoff. */
function wait(ms: number): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      wake = undefined;
      resolve();
    }, ms);
    wake = () => {
      clearTimeout(timer);
      wake = undefined;
      resolve();
    };
  });
}

export const useNotificationStore = defineStore("notification", {
  state: (): NotificationState => ({
    unreadCount: 0,
    recent: [],
    loadingRecent: false,
    arrivalSeq: 0,
  }),

  getters: {
    hasUnread: (state) => state.unreadCount > 0,
  },

  actions: {
    /**
     * Counts the unread messages. The stream carries the count with every message it
     * delivers, so this is what answers when it could not, and what draws the badge
     * before the first message arrives. A pushed count supersedes a fetch that was
     * already in flight, so the fetched one is dropped rather than applied late.
     */
    async refreshUnreadCount() {
      const mine = ++countTicket;
      const count = await getUnreadNotificationCount();
      if (mine === countTicket) {
        this.unreadCount = count;
      }
      return count;
    },

    /**
     * Loads the messages the bell shows. Like the count, a message the stream delivered
     * while this was in flight is newer than the list, so the list does not overwrite it.
     */
    async refreshRecent() {
      const mine = ++recentTicket;
      this.loadingRecent = true;
      try {
        const recent = await listRecentNotifications(RECENT_NOTIFICATION_LIMIT);
        if (mine === recentTicket) {
          this.recent = recent;
        }
      } finally {
        if (mine === recentTicket) {
          this.loadingRecent = false;
        }
      }
    },

    /**
     * Marks the named messages read and refreshes the ones on screen. The count
     * comes from the server rather than from the number of names, because a name
     * that was already read changes nothing.
     */
    async markRead(names: string[]) {
      await markNotificationsRead(names);
      await this.refresh();
    },

    async markAllRead() {
      await markAllNotificationsRead();
      await this.refresh();
    },

    async remove(name: string) {
      await deleteNotification(name);
      await this.refresh();
    },

    /** Reloads the count and, when the messages are on screen, the list. */
    async refresh() {
      await Promise.all([
        this.refreshUnreadCount(),
        this.recent.length > 0 ? this.refreshRecent() : Promise.resolve(),
      ]);
    },

    /**
     * Takes in one message the stream delivered: the count the server computed
     * alongside it, and the message itself when the bell has a list to put it in.
     * Until the dropdown has been opened there is nothing on screen to update, which
     * is the same reason `refresh` only reloads the list once it is showing.
     */
    applyArrival(event: NotificationEvent) {
      if (event.unreadCount !== undefined) {
        // The count the server computed with this message is newer than any fetch that
        // was already running, so it takes the ticket and the fetch stands down.
        countTicket += 1;
        this.unreadCount = event.unreadCount;
      } else {
        // The server could not read the count, so it sent none rather than a zero
        // that would read as "everything is read": ask for it here.
        void this.refreshUnreadCount().catch(() => {});
      }

      const message = event.notification;
      if (
        message &&
        this.recent.length > 0 &&
        !this.recent.some((item) => item.name === message.name)
      ) {
        recentTicket += 1;
        this.recent = [message, ...this.recent].slice(
          0,
          RECENT_NOTIFICATION_LIMIT
        );
      }
      this.arrivalSeq += 1;
    },

    /**
     * Opens the caller's notification stream and keeps it open until stopStreaming.
     *
     * Every (re)connect refreshes once before it starts listening. That one call is
     * what makes the gaps harmless: a message written while the stream was down, a
     * tab the browser froze in the background, and a subscription the server dropped
     * because it fell behind all end up covered, without any of them needing to be
     * detected.
     */
    startStreaming() {
      if (running) {
        return;
      }
      running = true;
      generation += 1;
      const mine = generation;

      const onVisibilityChange = () => {
        if (document.visibilityState === "visible") {
          // A background tab's connection may have been frozen or closed. Becoming
          // visible is the moment to resynchronize, and it is nearly free.
          void this.refresh().catch(() => {});
        }
      };
      document.addEventListener("visibilitychange", onVisibilityChange);
      removeVisibilityListener = () =>
        document.removeEventListener("visibilitychange", onVisibilityChange);

      void run(this, mine);
    },

    stopStreaming() {
      running = false;
      generation += 1;
      controller?.abort();
      controller = undefined;
      wake?.();
      removeVisibilityListener?.();
      removeVisibilityListener = undefined;
    },
  },
});

type NotificationStore = ReturnType<typeof useNotificationStore>;

/**
 * The connect loop. A failed attempt is not reported: the handshake runs through the
 * session interceptor, so an expired session is refreshed or ended there, and a
 * network failure is retried here with a backoff that doubles up to
 * RECONNECT_MAX_MS.
 */
async function run(store: NotificationStore, mine: number) {
  const live = () => running && generation === mine;
  let delay = RECONNECT_MIN_MS;
  while (live()) {
    controller = new AbortController();
    try {
      // See startStreaming: one refresh per attempt.
      void store.refresh().catch(() => {});
      for await (const message of subscribeNotifications(controller.signal)) {
        // Any frame at all — a keepalive included — means the connection is healthy, so
        // the wait before the next one stays at its floor. Without this a quiet inbox
        // would climb to the ceiling and then lose up to RECONNECT_MAX_MS after every
        // stream the server ends on its own.
        delay = RECONNECT_MIN_MS;
        if (message.event.case === "notification") {
          store.applyArrival(message.event.value);
        }
        // A keepAlive carries nothing else. It exists so an idle connection is not closed
        // by a proxy, which is why the loop has nothing to do with it.
      }
    } catch (error) {
      // A server that does not have this method cannot be talked into it. A failure that
      // belongs to a superseded generation is ignored: it must not end the stream a later
      // start opened.
      if (
        live() &&
        error instanceof ConnectError &&
        error.code === Code.Unimplemented
      ) {
        store.stopStreaming();
        return;
      }
    }
    // Nothing outside this loop may be touched once a later start owns the connection.
    if (!live()) {
      return;
    }
    controller = undefined;
    await wait(delay);
    delay = Math.min(delay * 2, RECONNECT_MAX_MS);
  }
}
