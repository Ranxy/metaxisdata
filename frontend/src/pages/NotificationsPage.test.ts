import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useNotificationStore } from "@/store/modules/notification";
import {
  NotificationEventSchema,
  NotificationSchema,
  NotificationSeverity,
  NotificationType,
} from "@/types/proto-es/v1/notification_service_pb";
import NotificationsPage from "./NotificationsPage.vue";

const mocks = vi.hoisted(() => ({
  listNotificationsPage: vi.fn(),
  getNotificationCounts: vi.fn(),
  listRecentNotifications: vi.fn(),
  markNotificationsRead: vi.fn(),
  markAllNotificationsRead: vi.fn(),
  markNotificationsSeen: vi.fn(),
  deleteNotification: vi.fn(),
}));

vi.mock("@/api/notification", () => ({
  listNotificationsPage: mocks.listNotificationsPage,
  getNotificationCounts: mocks.getNotificationCounts,
  listRecentNotifications: mocks.listRecentNotifications,
  markNotificationsRead: mocks.markNotificationsRead,
  markAllNotificationsRead: mocks.markAllNotificationsRead,
  markNotificationsSeen: mocks.markNotificationsSeen,
  deleteNotification: mocks.deleteNotification,
  subscribeNotifications: vi.fn(),
}));

function message(id: number) {
  return create(NotificationSchema, {
    name: `workspaces/ws/notifications/${id}`,
    type: NotificationType.SCHEMA_SYNC,
    severity: NotificationSeverity.INFO,
  });
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/notifications", component: { template: "<div />" } }],
  });
  await router.push("/notifications");
  await router.isReady();

  const pinia = createPinia();
  setActivePinia(pinia);
  const wrapper = mount(NotificationsPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("notifications page", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 0,
      unseenCount: 0,
    });
    mocks.listRecentNotifications.mockResolvedValue([]);
    mocks.markNotificationsSeen.mockResolvedValue(undefined);
    mocks.listNotificationsPage.mockResolvedValue({
      items: [message(1)],
      nextPageToken: "",
    });
  });

  // Opening the inbox is the second half of the badge rule: the badge goes dark here,
  // and the messages keep their unread state, which is what the "mark all read" button
  // and the unread filter read. A count that arrives while the page is open must not
  // relight it, and leaving the page must let a new message light it again.
  it("clears the badge when it is opened, and leaves the messages unread", async () => {
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 2,
      unseenCount: 2,
    });
    const wrapper = await mountPage();
    const store = useNotificationStore();

    expect(mocks.markNotificationsSeen).toHaveBeenCalledTimes(1);

    await store.refreshCounts();
    expect(store.unreadCount).toBe(2);
    expect(store.hasUnread).toBe(true);
    expect(store.hasUnseen).toBe(false);

    wrapper.unmount();
    await store.refreshCounts();
    expect(store.hasUnseen).toBe(true);
  });

  // The pager loads when it is told to, so opening the page is what has to tell it: the
  // inbox otherwise shows its empty state until the reader finds the refresh button.
  it("loads the first page when it is opened", async () => {
    const wrapper = await mountPage();

    expect(mocks.listNotificationsPage).toHaveBeenCalledTimes(1);
    expect(mocks.listNotificationsPage.mock.calls[0][0]).toMatchObject({
      pageToken: "",
      unreadOnly: false,
    });
    expect(wrapper.findAll("ul > li")).toHaveLength(1);
  });

  // The stream's message is what the page exists to show without a reload.
  it("reloads the first page when a message arrives", async () => {
    await mountPage();
    expect(mocks.listNotificationsPage).toHaveBeenCalledTimes(1);

    useNotificationStore().applyArrival(
      create(NotificationEventSchema, {
        notification: message(2),
        unreadCount: 1,
      })
    );
    await flushPromises();

    expect(mocks.listNotificationsPage).toHaveBeenCalledTimes(2);
    expect(mocks.listNotificationsPage.mock.calls[1][0]).toMatchObject({
      pageToken: "",
    });
  });

  // A reader who walked further in must not be thrown back to the newest rows by a
  // message that belongs on the first page.
  it("leaves a reader on a later page alone", async () => {
    mocks.listNotificationsPage
      .mockResolvedValueOnce({ items: [message(1)], nextPageToken: "next" })
      .mockResolvedValueOnce({ items: [message(2)], nextPageToken: "" });
    const wrapper = await mountPage();

    const next = wrapper
      .findAll("button")
      .find((button) => button.text() === i18n.global.t("common.next"));
    expect(next).toBeDefined();
    await next?.trigger("click");
    await flushPromises();
    expect(mocks.listNotificationsPage).toHaveBeenCalledTimes(2);
    expect(mocks.listNotificationsPage.mock.calls[1][0]).toMatchObject({
      pageToken: "next",
    });

    useNotificationStore().applyArrival(
      create(NotificationEventSchema, {
        notification: message(3),
        unreadCount: 1,
      })
    );
    await flushPromises();

    expect(mocks.listNotificationsPage).toHaveBeenCalledTimes(2);
  });
});
