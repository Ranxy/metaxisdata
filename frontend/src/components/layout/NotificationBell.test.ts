import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useNotificationStore } from "@/store/modules/notification";
import {
  NotificationSchema,
  NotificationSeverity,
  NotificationType,
} from "@/types/proto-es/v1/notification_service_pb";
import NotificationBell from "./NotificationBell.vue";

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
  // The bell owns the stream, so mounting it opens one. A stream that never says anything
  // is enough: what these tests are about is the click.
  subscribeNotifications: () => (async function* () {})(),
}));

function message(id: number) {
  return create(NotificationSchema, {
    name: `workspaces/ws/notifications/${id}`,
    type: NotificationType.SCHEMA_SYNC,
    severity: NotificationSeverity.INFO,
    detail: {
      case: "schemaSync",
      value: { instance: "instances/inst1", instanceTitle: "prod" },
    },
  });
}

// The dropdown's content is teleported out of the component and only rendered while the
// menu is open, so the assertions read the document, not the wrapper.
async function mountBell(pinia: ReturnType<typeof createPinia>) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: "/:pathMatch(.*)*", component: { template: "<div />" } }],
  });
  await router.push("/");
  await router.isReady();

  const wrapper = mount(NotificationBell, {
    attachTo: document.body,
    global: { plugins: [router, i18n, pinia] },
  });
  return wrapper;
}

/** Lets the mount's stream, its connect-time refresh and the click settle. */
async function settle() {
  for (let i = 0; i < 8; i += 1) {
    await Promise.resolve();
  }
  await new Promise((resolve) => setTimeout(resolve, 0));
}

describe("NotificationBell", () => {
  let pinia: ReturnType<typeof createPinia>;

  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    pinia = createPinia();
    setActivePinia(pinia);
    mocks.getNotificationCounts.mockResolvedValue({
      unreadCount: 2,
      unseenCount: 2,
    });
    mocks.listRecentNotifications.mockResolvedValue([]);
    mocks.markNotificationsRead.mockResolvedValue(undefined);
    mocks.markNotificationsSeen.mockResolvedValue(undefined);
  });

  afterEach(() => {
    useNotificationStore().stopStreaming();
    document.body.innerHTML = "";
  });

  // Opening the bell is what clears the badge, and it must not read anything: the messages
  // keep their unread state, so the inbox page still shows them as unread.
  it("clears the badge when it is opened, and marks nothing read", async () => {
    const wrapper = await mountBell(pinia);
    const store = useNotificationStore();
    await settle();
    expect(store.hasUnseen).toBe(true);

    await wrapper.find("button").trigger("click");
    await settle();

    expect(mocks.markNotificationsSeen).toHaveBeenCalledTimes(1);
    expect(store.unseenCount).toBe(0);
    expect(store.hasUnseen).toBe(false);
    expect(store.unreadCount).toBe(2);
    expect(store.hasUnread).toBe(true);
    expect(mocks.markNotificationsRead).not.toHaveBeenCalled();
    expect(mocks.listRecentNotifications).toHaveBeenCalled();
    wrapper.unmount();
  });

  // The other half of the rule: a message is read when the message itself is clicked, and
  // the notification surface closes behind it.
  it("marks one message read when that message is clicked", async () => {
    mocks.listRecentNotifications.mockResolvedValue([message(7)]);
    const wrapper = await mountBell(pinia);
    const store = useNotificationStore();
    await settle();

    await wrapper.find("button").trigger("click");
    await settle();
    expect(store.openInboxCount).toBe(1);

    const item = document.body.querySelector('[role="menuitem"]');
    expect(item).not.toBeNull();
    (item as HTMLElement).click();
    await settle();

    expect(mocks.markNotificationsRead).toHaveBeenCalledWith([
      "workspaces/ws/notifications/7",
    ]);
    expect(store.openInboxCount).toBe(0);
    wrapper.unmount();
  });
});
