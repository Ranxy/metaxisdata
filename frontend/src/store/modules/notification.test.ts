import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NotificationCounts } from "@/api/notification";
import {
  KeepAliveSchema,
  NotificationEventSchema,
  NotificationSchema,
  NotificationSeverity,
  NotificationType,
  type SubscribeNotificationsResponse,
  SubscribeNotificationsResponseSchema,
} from "@/types/proto-es/v1/notification_service_pb";
import {
  RECENT_NOTIFICATION_LIMIT,
  useNotificationStore,
} from "./notification";

const mocks = vi.hoisted(() => ({
  getNotificationCounts: vi.fn(),
  listRecentNotifications: vi.fn(),
  markNotificationsRead: vi.fn(),
  markAllNotificationsRead: vi.fn(),
  markNotificationsSeen: vi.fn(),
  deleteNotification: vi.fn(),
}));

vi.mock("@/api/notification", () => ({
  getNotificationCounts: mocks.getNotificationCounts,
  listRecentNotifications: mocks.listRecentNotifications,
  markNotificationsRead: mocks.markNotificationsRead,
  markAllNotificationsRead: mocks.markAllNotificationsRead,
  markNotificationsSeen: mocks.markNotificationsSeen,
  deleteNotification: mocks.deleteNotification,
  subscribeNotifications: (signal?: AbortSignal) => openStream(signal),
}));

function message(id: number) {
  return create(NotificationSchema, {
    name: `workspaces/ws/notifications/${id}`,
    type: NotificationType.SCHEMA_SYNC,
    severity: NotificationSeverity.INFO,
  });
}

/**
 * One streamed message with the counts the server computed alongside it. `unseenCount`
 * defaults to `unreadCount`, which is the state of an inbox nobody has opened yet, so a
 * test that does not care about the badge only has to name the unread count.
 */
function arrival(id: number, unreadCount?: number, unseenCount = unreadCount) {
  return create(SubscribeNotificationsResponseSchema, {
    event: {
      case: "notification",
      value: create(NotificationEventSchema, {
        notification: message(id),
        unreadCount,
        unseenCount,
      }),
    },
  });
}

const keepAlive = create(SubscribeNotificationsResponseSchema, {
  event: { case: "keepAlive", value: create(KeepAliveSchema, {}) },
});

/**
 * One open stream, as the store sees it: a test delivers a message, ends the stream the
 * way a dropped connection does, fails it the way an unsupported server does, and can see
 * the signal the store aborted it with.
 */
interface FakeStream {
  signal?: AbortSignal;
  deliver: (response: SubscribeNotificationsResponse) => void;
  end: () => void;
  fail: (error: unknown) => void;
}

let streams: FakeStream[] = [];

function openStream(
  signal?: AbortSignal
): AsyncIterable<SubscribeNotificationsResponse> {
  const queue: SubscribeNotificationsResponse[] = [];
  let ended = false;
  let failure: unknown;
  let notify: (() => void) | undefined;

  const wake = () => {
    notify?.();
    notify = undefined;
  };
  // A real transport rejects the pending read when the signal aborts, so the loop that was
  // stopped finds out. A generator parked on an unresolved promise would hide exactly the
  // leak these tests are about.
  signal?.addEventListener("abort", wake);
  streams.push({
    signal,
    deliver: (response) => {
      queue.push(response);
      wake();
    },
    end: () => {
      ended = true;
      wake();
    },
    fail: (error) => {
      failure = error;
      wake();
    },
  });

  return (async function* () {
    try {
      for (;;) {
        // A failure first: an unsupported-method answer that was already in flight when
        // the connection was aborted has to surface as that error rather than as the
        // cancellation, which is the race the generation check exists for.
        if (failure !== undefined) {
          throw failure;
        }
        if (ended || signal?.aborted) {
          return;
        }
        if (queue.length === 0) {
          await new Promise<void>((resolve) => {
            notify = resolve;
          });
          continue;
        }
        yield queue.shift()!;
      }
    } finally {
      signal?.removeEventListener("abort", wake);
    }
  })();
}

/** Lets the connect loop and the stream it iterates reach their next await. */
async function settle() {
  for (let i = 0; i < 8; i += 1) {
    await Promise.resolve();
  }
}

describe("notification store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    streams = [];
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 0,
      unseenCount: 0,
    });
    mocks.listRecentNotifications.mockResolvedValue([]);
    mocks.markNotificationsRead.mockResolvedValue(undefined);
    mocks.markAllNotificationsRead.mockResolvedValue(undefined);
    mocks.markNotificationsSeen.mockResolvedValue(undefined);
    mocks.deleteNotification.mockResolvedValue(undefined);
  });

  afterEach(() => {
    useNotificationStore().stopStreaming();
    vi.useRealTimers();
  });

  it("tracks the two counts the server reports", async () => {
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 3,
      unseenCount: 1,
    });
    const store = useNotificationStore();

    await expect(store.refreshCounts()).resolves.toEqual({
      unreadCount: 3,
      unseenCount: 1,
    });
    expect(store.unreadCount).toBe(3);
    expect(store.hasUnread).toBe(true);
    expect(store.unseenCount).toBe(1);
    expect(store.hasUnseen).toBe(true);
  });

  // Opening the inbox is what clears the badge. It must not mark anything read: the
  // messages keep their unread state, and the page's "mark all read" stays enabled.
  it("clears the badge when the inbox is opened, and leaves every message unread", async () => {
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 3,
      unseenCount: 3,
    });
    const store = useNotificationStore();
    await store.refreshCounts();
    expect(store.hasUnseen).toBe(true);

    store.openInbox();
    expect(store.unseenCount).toBe(0);
    expect(store.hasUnseen).toBe(false);
    expect(store.unreadCount).toBe(3);
    expect(store.hasUnread).toBe(true);
    expect(mocks.markNotificationsSeen).toHaveBeenCalledTimes(1);

    // Closing it is symmetric, and the count never goes negative.
    store.closeInbox();
    store.closeInbox();
    expect(store.openInboxCount).toBe(0);
  });

  // A count that comes back while the inbox is open must not relight the badge, and the
  // watermark has to move over what the count called new: this refresh is the path a
  // message the stream missed arrives by (a reconnect or a visible tab reloads the list
  // with the counts), and the user can see it. Without the second half, closing the inbox
  // would light the badge for a message that was just on screen.
  it("keeps the badge dark, and advances the watermark, when a refresh lands with the inbox open", async () => {
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 5,
      unseenCount: 2,
    });
    const store = useNotificationStore();

    store.openInbox();
    mocks.markNotificationsSeen.mockClear();
    await store.refreshCounts();

    expect(store.unreadCount).toBe(5);
    expect(store.unseenCount).toBe(0);
    expect(mocks.markNotificationsSeen).toHaveBeenCalledTimes(1);

    // Nothing new to move over: the refresh is silent.
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 5,
      unseenCount: 0,
    });
    await store.refreshCounts();
    expect(mocks.markNotificationsSeen).toHaveBeenCalledTimes(1);
  });

  // What is on screen has been seen: a message that arrives while the list is open must
  // not light the badge, and the server's watermark is moved over it so a later refresh
  // does not bring it back. An inbox opened while empty is still on screen, so the message
  // has to land in that empty list — otherwise the watermark moves past a message nobody
  // was ever shown.
  it("keeps the badge dark for a message that arrives while the inbox is open, and shows it", async () => {
    const store = useNotificationStore();

    store.startStreaming();
    await settle();
    expect(store.recent).toEqual([]);
    store.openInbox();
    mocks.markNotificationsSeen.mockClear();

    streams[0].deliver(arrival(1, 4, 3));
    await settle();

    expect(store.unreadCount).toBe(4);
    expect(store.unseenCount).toBe(0);
    expect(mocks.markNotificationsSeen).toHaveBeenCalledTimes(1);
    expect(store.recent.map((item) => item.name)).toEqual([
      "workspaces/ws/notifications/1",
    ]);
  });

  it("lights the badge again once the inbox is closed", async () => {
    const store = useNotificationStore();

    store.startStreaming();
    await settle();
    store.openInbox();
    store.closeInbox();

    streams[0].deliver(arrival(1, 2, 1));
    await settle();

    expect(store.unseenCount).toBe(1);
  });

  // A failed mark-seen is a badge that comes back on the next refresh, not a rejected
  // open: the store clears the badge either way and never throws at its caller.
  it("clears the badge even when the server cannot record it", async () => {
    mocks.markNotificationsSeen.mockRejectedValue(new Error("offline"));
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 1,
      unseenCount: 1,
    });
    const store = useNotificationStore();
    await store.refreshCounts();

    expect(() => store.openInbox()).not.toThrow();
    await settle();
    expect(store.unseenCount).toBe(0);
  });

  it("asks for the bell's page size", async () => {
    const store = useNotificationStore();
    await store.refreshRecent();

    expect(mocks.listRecentNotifications).toHaveBeenCalledWith(
      RECENT_NOTIFICATION_LIMIT
    );
    expect(store.loadingRecent).toBe(false);
  });

  // The count comes from the server: a name that was already read changes
  // nothing, so subtracting the names locally would drift.
  it("refreshes the count after marking messages read", async () => {
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 1,
      unseenCount: 0,
    });
    const store = useNotificationStore();

    await store.markRead(["workspaces/ws/notifications/1"]);
    expect(mocks.markNotificationsRead).toHaveBeenCalledWith([
      "workspaces/ws/notifications/1",
    ]);
    expect(store.unreadCount).toBe(1);

    await store.markAllRead();
    expect(mocks.markAllNotificationsRead).toHaveBeenCalled();

    await store.remove("workspaces/ws/notifications/2");
    expect(mocks.deleteNotification).toHaveBeenCalledWith(
      "workspaces/ws/notifications/2"
    );
  });

  it("reloads the visible list together with the count, and the count alone when it is not shown", async () => {
    const store = useNotificationStore();

    await store.refresh();
    expect(mocks.listRecentNotifications).not.toHaveBeenCalled();

    store.recent = [message(1)];
    await store.refresh();
    expect(mocks.listRecentNotifications).toHaveBeenCalledTimes(1);
  });

  // The stream is the push channel: a message written while the bell is open reaches
  // it without a reload, and brings the count the inbox would answer with.
  it("applies the message the stream delivers, with the count it came with", async () => {
    mocks.listRecentNotifications.mockResolvedValue([message(9)]);
    const store = useNotificationStore();

    store.startStreaming();
    expect(streams).toHaveLength(1);
    await settle();

    // Opening the dropdown is what puts the list on screen.
    await store.refreshRecent();
    expect(store.recent).toHaveLength(1);

    streams[0].deliver(arrival(1, 4));
    await settle();

    expect(store.unreadCount).toBe(4);
    expect(store.recent.map((item) => item.name)).toEqual([
      "workspaces/ws/notifications/1",
      "workspaces/ws/notifications/9",
    ]);
    expect(store.arrivalSeq).toBe(1);
  });

  it("keeps the list closed until it has been opened, but still counts", async () => {
    const store = useNotificationStore();

    store.startStreaming();
    await settle();
    streams[0].deliver(arrival(1, 2));
    await settle();

    expect(store.unreadCount).toBe(2);
    expect(store.recent).toEqual([]);
  });

  // A keepalive exists so a proxy does not close an idle connection. It carries
  // nothing, so it must change nothing.
  it("ignores the keepalive that holds the connection open", async () => {
    const store = useNotificationStore();

    store.startStreaming();
    await settle();
    streams[0].deliver(keepAlive);
    await settle();

    expect(store.arrivalSeq).toBe(0);
    expect(store.unreadCount).toBe(0);
  });

  // The counts are absent when the server could not read them, and a zero would read as
  // "everything is read": the store asks instead of showing it.
  it("asks for the counts when the event carries none", async () => {
    const store = useNotificationStore();

    store.startStreaming();
    await settle();
    const afterConnect = mocks.getNotificationCounts.mock.calls.length;

    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 7,
      unseenCount: 7,
    });
    streams[0].deliver(arrival(1));
    await settle();

    expect(mocks.getNotificationCounts.mock.calls.length).toBe(
      afterConnect + 1
    );
    expect(store.unreadCount).toBe(7);
    expect(store.unseenCount).toBe(7);
  });

  // Every connect refreshes: that one call is what covers a message written while the
  // stream was down, and it is why the reconnect does not have to be gap-free.
  it("refreshes on every connect and retries after the stream ends", async () => {
    vi.useFakeTimers();
    const store = useNotificationStore();

    store.startStreaming();
    await vi.advanceTimersByTimeAsync(0);
    expect(streams).toHaveLength(1);
    await settle();
    const refreshes = mocks.getNotificationCounts.mock.calls.length;

    streams[0].end();
    await vi.advanceTimersByTimeAsync(1_000);

    expect(streams).toHaveLength(2);
    expect(mocks.getNotificationCounts.mock.calls.length).toBeGreaterThan(
      refreshes
    );
  });

  it("stops when asked: the stream is aborted and no reconnect follows", async () => {
    vi.useFakeTimers();
    const store = useNotificationStore();

    store.startStreaming();
    await vi.advanceTimersByTimeAsync(0);
    const signal = streams[0].signal;

    store.stopStreaming();
    expect(signal?.aborted).toBe(true);

    await vi.advanceTimersByTimeAsync(5 * 60_000);
    expect(streams).toHaveLength(1);
  });

  // An unmount and the mount that follows it can land in the same tick, as a layout swap
  // does. The loop that was stopped must not mistake the new start for its own and keep
  // reconnecting beside it: that opens one more stream per transition, until the server's
  // per-principal bound refuses every new connection.
  it("does not leave the previous loop reconnecting when it is restarted", async () => {
    vi.useFakeTimers();
    const store = useNotificationStore();

    store.startStreaming();
    await vi.advanceTimersByTimeAsync(0);
    expect(streams).toHaveLength(1);
    const first = streams[0].signal;

    store.stopStreaming();
    store.startStreaming();
    await vi.advanceTimersByTimeAsync(5 * 60_000);

    expect(first?.aborted).toBe(true);
    expect(streams).toHaveLength(2);
    expect(streams[1].signal?.aborted).toBe(false);
  });

  it("resynchronizes when the tab becomes visible again", async () => {
    const store = useNotificationStore();
    let visibility: DocumentVisibilityState = "hidden";
    Object.defineProperty(document, "visibilityState", {
      configurable: true,
      get: () => visibility,
    });

    store.startStreaming();
    await settle();
    const before = mocks.getNotificationCounts.mock.calls.length;

    visibility = "visible";
    document.dispatchEvent(new Event("visibilitychange"));
    await settle();

    expect(mocks.getNotificationCounts.mock.calls.length).toBeGreaterThan(
      before
    );
  });

  // A count that was already being fetched when a message arrived is older than that
  // message: applying it would drop the badge back to a number the message superseded.
  it("drops a count that a pushed message already superseded", async () => {
    let release: ((counts: NotificationCounts) => void) | undefined;
    mocks.getNotificationCounts.mockImplementation(
      () =>
        new Promise<NotificationCounts>((resolve) => {
          release = resolve;
        })
    );
    const store = useNotificationStore();

    store.startStreaming();
    await settle();
    streams[0].deliver(arrival(1, 1));
    await settle();
    expect(store.unreadCount).toBe(1);

    release?.({ unreadCount: 0, unseenCount: 0 });
    await settle();
    expect(store.unreadCount).toBe(1);
  });

  // The same rule for the list: a list that was already being fetched does not know about
  // a message delivered while it was in flight.
  it("keeps a delivered message when a slower list arrives", async () => {
    let release: ((items: ReturnType<typeof message>[]) => void) | undefined;
    mocks.listRecentNotifications.mockImplementation(
      () =>
        new Promise<ReturnType<typeof message>[]>((resolve) => {
          release = resolve;
        })
    );
    const store = useNotificationStore();

    store.startStreaming();
    await settle();
    // The list is on screen. The connect-time refresh has already run, so nothing
    // overwrites this while the slow one below is in flight.
    store.recent = [message(8)];

    const listing = store.refreshRecent();
    await settle();

    streams[0].deliver(arrival(1, 1));
    await settle();
    expect(store.recent.map((item) => item.name)).toEqual([
      "workspaces/ws/notifications/1",
      "workspaces/ws/notifications/8",
    ]);

    release?.([message(9)]);
    await listing;
    expect(store.recent.map((item) => item.name)).toEqual([
      "workspaces/ws/notifications/1",
      "workspaces/ws/notifications/8",
    ]);
  });

  // A quiet inbox is a healthy one: the server ends every stream after half an hour on
  // purpose, and the wait before reconnecting must not have grown in the meantime.
  it("keeps the reconnect at its floor while the connection is healthy", async () => {
    vi.useFakeTimers();
    const store = useNotificationStore();

    store.startStreaming();
    await vi.advanceTimersByTimeAsync(0);
    expect(streams).toHaveLength(1);

    streams[0].deliver(keepAlive);
    await vi.advanceTimersByTimeAsync(0);
    streams[0].end();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(streams).toHaveLength(2);

    streams[1].deliver(keepAlive);
    await vi.advanceTimersByTimeAsync(0);
    streams[1].end();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(streams).toHaveLength(3);
  });

  // A refused handshake — the server answering the stream opening with Unauthenticated — is
  // not fatal for the loop. The session interceptor renews the cookie, and the next attempt
  // is a fresh call with a fresh request, which is what makes the loop's plain retry the
  // recovery path. src/api/notification.test.ts pins the transport half of this.
  it("reconnects after a refused handshake and applies what it then delivers", async () => {
    vi.useFakeTimers();
    const store = useNotificationStore();

    store.startStreaming();
    await vi.advanceTimersByTimeAsync(0);
    expect(streams).toHaveLength(1);

    streams[0].fail(
      new ConnectError("unauthenticated", Code.Unauthenticated) as unknown
    );
    await vi.advanceTimersByTimeAsync(1_000);
    expect(streams).toHaveLength(2);

    streams[1].deliver(arrival(1, 1));
    await vi.advanceTimersByTimeAsync(0);

    expect(store.unreadCount).toBe(1);
    expect(store.arrivalSeq).toBe(1);
  });

  // A failure that belongs to a superseded generation must not end the stream a later
  // start opened. It is the race an in-flight "method not supported" answer runs against
  // the abort that a restart sends: both land before the old loop resumes.
  it("ignores an old generation's failure after a restart", async () => {
    vi.useFakeTimers();
    const store = useNotificationStore();

    store.startStreaming();
    await vi.advanceTimersByTimeAsync(0);
    const superseded = streams[0];

    store.stopStreaming();
    superseded.fail(
      new ConnectError("not implemented", Code.Unimplemented) as unknown
    );
    store.startStreaming();
    await vi.advanceTimersByTimeAsync(0);
    expect(streams).toHaveLength(2);

    await vi.advanceTimersByTimeAsync(60_000);

    expect(streams[1].signal?.aborted).toBe(false);
    expect(streams).toHaveLength(2);
  });

  it("ignores a message it already holds and caps the list", async () => {
    const store = useNotificationStore();
    store.startStreaming();
    await settle();
    store.recent = Array.from(
      { length: RECENT_NOTIFICATION_LIMIT },
      (_, index) => message(index + 1)
    );

    streams[0].deliver(arrival(1, 1));
    await settle();
    expect(store.recent).toHaveLength(RECENT_NOTIFICATION_LIMIT);
    expect(store.recent[0].name).toBe("workspaces/ws/notifications/1");

    streams[0].deliver(arrival(99, 2));
    await settle();
    expect(store.recent).toHaveLength(RECENT_NOTIFICATION_LIMIT);
    expect(store.recent[0].name).toBe("workspaces/ws/notifications/99");
    expect(store.recent.map((item) => item.name)).not.toContain(
      `workspaces/ws/notifications/${RECENT_NOTIFICATION_LIMIT}`
    );
  });

  // An event that carries no message at all must not be able to break the badge or the
  // list; the counts are still applied.
  it("survives an event without a message", async () => {
    const store = useNotificationStore();
    store.startStreaming();
    await settle();
    store.recent = [message(1)];

    streams[0].deliver(
      create(SubscribeNotificationsResponseSchema, {
        event: {
          case: "notification",
          value: create(NotificationEventSchema, {
            unreadCount: 3,
            unseenCount: 2,
          }),
        },
      })
    );
    await settle();

    expect(store.unreadCount).toBe(3);
    expect(store.unseenCount).toBe(2);
    expect(store.recent.map((item) => item.name)).toEqual([
      "workspaces/ws/notifications/1",
    ]);
  });
});
