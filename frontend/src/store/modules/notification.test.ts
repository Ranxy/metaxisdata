import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
  getUnreadNotificationCount: vi.fn(),
  listRecentNotifications: vi.fn(),
  markNotificationsRead: vi.fn(),
  markAllNotificationsRead: vi.fn(),
  deleteNotification: vi.fn(),
}));

vi.mock("@/api/notification", () => ({
  getUnreadNotificationCount: mocks.getUnreadNotificationCount,
  listRecentNotifications: mocks.listRecentNotifications,
  markNotificationsRead: mocks.markNotificationsRead,
  markAllNotificationsRead: mocks.markAllNotificationsRead,
  deleteNotification: mocks.deleteNotification,
  subscribeNotifications: (signal?: AbortSignal) => openStream(signal),
}));

function message(id: number, read = false) {
  return create(NotificationSchema, {
    name: `workspaces/ws/notifications/${id}`,
    type: NotificationType.SCHEMA_SYNC,
    severity: NotificationSeverity.INFO,
    readTime: read ? undefined : undefined,
  });
}

function arrival(id: number, unreadCount?: number) {
  return create(SubscribeNotificationsResponseSchema, {
    event: {
      case: "notification",
      value: create(NotificationEventSchema, {
        notification: message(id),
        unreadCount,
      }),
    },
  });
}

const keepAlive = create(SubscribeNotificationsResponseSchema, {
  event: { case: "keepAlive", value: create(KeepAliveSchema, {}) },
});

/**
 * One open stream, as the store sees it: a test delivers a message, ends the stream
 * the way a dropped connection does, and can see the signal the store aborted it with.
 */
interface FakeStream {
  signal?: AbortSignal;
  deliver: (response: SubscribeNotificationsResponse) => void;
  end: () => void;
}

let streams: FakeStream[] = [];

function openStream(
  signal?: AbortSignal
): AsyncIterable<SubscribeNotificationsResponse> {
  const queue: SubscribeNotificationsResponse[] = [];
  let ended = false;
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
  });

  return (async function* () {
    try {
      while (!ended && !signal?.aborted) {
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
    mocks.getUnreadNotificationCount.mockResolvedValue(0);
    mocks.listRecentNotifications.mockResolvedValue([]);
    mocks.markNotificationsRead.mockResolvedValue(undefined);
    mocks.markAllNotificationsRead.mockResolvedValue(undefined);
    mocks.deleteNotification.mockResolvedValue(undefined);
  });

  afterEach(() => {
    useNotificationStore().stopStreaming();
    vi.useRealTimers();
  });

  it("tracks the unread count the server reports", async () => {
    mocks.getUnreadNotificationCount.mockResolvedValue(3);
    const store = useNotificationStore();

    await expect(store.refreshUnreadCount()).resolves.toBe(3);
    expect(store.unreadCount).toBe(3);
    expect(store.hasUnread).toBe(true);
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
    mocks.getUnreadNotificationCount.mockResolvedValue(1);
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

  // The count is absent when the server could not read it, and a zero would read as
  // "everything is read": the store asks instead of showing it.
  it("asks for the count when the event carries none", async () => {
    const store = useNotificationStore();

    store.startStreaming();
    await settle();
    const afterConnect = mocks.getUnreadNotificationCount.mock.calls.length;

    mocks.getUnreadNotificationCount.mockResolvedValue(7);
    streams[0].deliver(arrival(1));
    await settle();

    expect(mocks.getUnreadNotificationCount.mock.calls.length).toBe(
      afterConnect + 1
    );
    expect(store.unreadCount).toBe(7);
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
    const refreshes = mocks.getUnreadNotificationCount.mock.calls.length;

    streams[0].end();
    await vi.advanceTimersByTimeAsync(1_000);

    expect(streams).toHaveLength(2);
    expect(mocks.getUnreadNotificationCount.mock.calls.length).toBeGreaterThan(
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
    const before = mocks.getUnreadNotificationCount.mock.calls.length;

    visibility = "visible";
    document.dispatchEvent(new Event("visibilitychange"));
    await settle();

    expect(mocks.getUnreadNotificationCount.mock.calls.length).toBeGreaterThan(
      before
    );
  });
});
