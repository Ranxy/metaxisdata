import { defineStore } from "pinia";
import {
  deleteNotification,
  getUnreadNotificationCount,
  listRecentNotifications,
  markAllNotificationsRead,
  markNotificationsRead,
} from "@/api/notification";
import type { Notification } from "@/types/proto-es/v1/notification_service_pb";

/**
 * How often the bell refreshes while the tab is visible. There is no push
 * channel in this app — SSE is only used by the LLM client and MCP — so a
 * message written by a background runner reaches the user on the next poll.
 */
const POLL_INTERVAL_MS = 30_000;

/** How many messages the bell's dropdown shows. */
export const RECENT_NOTIFICATION_LIMIT = 8;

interface NotificationState {
  unreadCount: number;
  recent: Notification[];
  loadingRecent: boolean;
}

// The poll lives outside the store because it must survive re-renders and be
// cleared exactly once; a store action holding an interval id in state would
// make it reactive and serializable, which it is neither.
let pollTimer: ReturnType<typeof setInterval> | undefined;
let removeVisibilityListener: (() => void) | undefined;

export const useNotificationStore = defineStore("notification", {
  state: (): NotificationState => ({
    unreadCount: 0,
    recent: [],
    loadingRecent: false,
  }),

  getters: {
    hasUnread: (state) => state.unreadCount > 0,
  },

  actions: {
    /** Counts the unread messages. Polled, so it must stay one cheap query. */
    async refreshUnreadCount() {
      this.unreadCount = await getUnreadNotificationCount();
      return this.unreadCount;
    },

    /** Loads the messages the bell shows. */
    async refreshRecent() {
      this.loadingRecent = true;
      try {
        this.recent = await listRecentNotifications(RECENT_NOTIFICATION_LIMIT);
      } finally {
        this.loadingRecent = false;
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
     * Starts the visibility-aware poll. A hidden tab stops asking: a user who
     * left the app open overnight should not generate a request every 30 seconds,
     * and the refresh on becoming visible again is what they actually see.
     */
    startPolling() {
      if (pollTimer !== undefined) {
        return;
      }
      const tick = () => {
        if (document.visibilityState !== "visible") {
          return;
        }
        // A failed poll is not worth a toast: the badge simply keeps its last
        // value and the next tick tries again.
        void this.refreshUnreadCount().catch(() => {});
      };
      pollTimer = setInterval(tick, POLL_INTERVAL_MS);
      const onVisibilityChange = () => {
        if (document.visibilityState === "visible") {
          tick();
        }
      };
      document.addEventListener("visibilitychange", onVisibilityChange);
      removeVisibilityListener = () =>
        document.removeEventListener("visibilitychange", onVisibilityChange);
      tick();
    },

    stopPolling() {
      if (pollTimer !== undefined) {
        clearInterval(pollTimer);
        pollTimer = undefined;
      }
      removeVisibilityListener?.();
      removeVisibilityListener = undefined;
    },
  },
});
