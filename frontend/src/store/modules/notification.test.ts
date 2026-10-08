import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  NotificationSchema,
  NotificationSeverity,
  NotificationType,
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
}));

function message(id: number, read = false) {
  return create(NotificationSchema, {
    name: `workspaces/ws/notifications/${id}`,
    type: NotificationType.SCHEMA_SYNC,
    severity: NotificationSeverity.INFO,
    readTime: read ? undefined : undefined,
  });
}

describe("notification store", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    mocks.getUnreadNotificationCount.mockResolvedValue(0);
    mocks.listRecentNotifications.mockResolvedValue([]);
    mocks.markNotificationsRead.mockResolvedValue(undefined);
    mocks.markAllNotificationsRead.mockResolvedValue(undefined);
    mocks.deleteNotification.mockResolvedValue(undefined);
  });

  afterEach(() => {
    useNotificationStore().stopPolling();
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

  it("polls while the tab is visible and stops when asked", async () => {
    vi.useFakeTimers();
    const store = useNotificationStore();

    store.startPolling();
    expect(mocks.getUnreadNotificationCount).toHaveBeenCalledTimes(1);

    await vi.advanceTimersByTimeAsync(3 * 30_000);
    expect(mocks.getUnreadNotificationCount).toHaveBeenCalledTimes(4);

    // A second start must not add a second timer.
    store.startPolling();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(mocks.getUnreadNotificationCount).toHaveBeenCalledTimes(5);

    store.stopPolling();
    await vi.advanceTimersByTimeAsync(2 * 30_000);
    expect(mocks.getUnreadNotificationCount).toHaveBeenCalledTimes(5);
  });

  it("swallows a failed poll instead of leaving an unhandled rejection", async () => {
    vi.useFakeTimers();
    mocks.getUnreadNotificationCount.mockRejectedValue(new Error("offline"));
    const store = useNotificationStore();

    store.startPolling();
    await vi.advanceTimersByTimeAsync(2 * 30_000);

    expect(store.unreadCount).toBe(0);
  });
});
