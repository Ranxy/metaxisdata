import { create } from "@bufbuild/protobuf";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  NotificationChannelKind,
  NotificationChannelSchema,
  NotificationDeliverySchema,
  NotificationDeliveryState,
  NotificationSeverity,
  NotificationType,
} from "@/types/proto-es/v1/notification_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import NotificationSettingsPage from "./NotificationSettingsPage.vue";

const mocks = vi.hoisted(() => ({
  listNotificationChannels: vi.fn(),
  listNotificationDeliveriesPage: vi.fn(),
  createNotificationChannel: vi.fn(),
  updateNotificationChannel: vi.fn(),
  deleteNotificationChannel: vi.fn(),
  testNotificationChannel: vi.fn(),
  retryNotificationDelivery: vi.fn(),
}));

vi.mock("@/api/notification", () => mocks);

const notifyMocks = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("@/lib/notify", () => ({ notify: notifyMocks }));

const PERMISSIONS = ["metaxisdata.settings.get", "metaxisdata.settings.update"];

function channel() {
  return create(NotificationChannelSchema, {
    name: "workspaces/ws-1/notificationChannels/abc123",
    displayName: "on-call",
    kind: NotificationChannelKind.WEBHOOK,
    enabled: true,
    eventTypes: [NotificationType.DATA_QUALITY],
    minSeverity: NotificationSeverity.WARNING,
    language: "en-US",
    secretSet: true,
    config: { config: { case: "webhook", value: { sign: false } } },
  });
}

function deadDelivery() {
  return create(NotificationDeliverySchema, {
    name: "workspaces/ws-1/notificationDeliveries/9",
    channel: "workspaces/ws-1/notificationChannels/abc123",
    channelName: "on-call",
    eventType: NotificationType.DATA_QUALITY,
    severity: NotificationSeverity.ERROR,
    state: NotificationDeliveryState.DEAD,
    attempts: 6,
    lastError: "the webhook endpoint answered 500",
  });
}

let current: VueWrapper | undefined;

function mountPage(permissions = PERMISSIONS) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  current = mount(NotificationSettingsPage, {
    global: { plugins: [i18n, pinia] },
    attachTo: document.body,
  });
  return current;
}

function buttonsByText(text: string): HTMLButtonElement[] {
  return Array.from(document.querySelectorAll("button")).filter(
    (button) => button.textContent?.trim() === text
  );
}

beforeEach(() => {
  mocks.listNotificationChannels.mockResolvedValue([channel()]);
  mocks.listNotificationDeliveriesPage.mockResolvedValue({
    items: [deadDelivery()],
    nextPageToken: "",
  });
  mocks.createNotificationChannel.mockResolvedValue(channel());
  mocks.retryNotificationDelivery.mockResolvedValue(deadDelivery());
});

afterEach(() => {
  // Unmount before clearing: a mounted page keeps its teleported dialog in the
  // document, and the next case would then find this case's inputs.
  current?.unmount();
  current = undefined;
  document.body.innerHTML = "";
  vi.clearAllMocks();
});

describe("NotificationSettingsPage", () => {
  it("lists channels and deliveries", async () => {
    const wrapper = mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain("on-call");
    expect(wrapper.text()).toContain("Dead");
    expect(wrapper.text()).toContain("the webhook endpoint answered 500");
    expect(mocks.listNotificationChannels).toHaveBeenCalledTimes(1);
    expect(mocks.listNotificationDeliveriesPage).toHaveBeenCalledTimes(1);
  });

  it("gates the mutating actions on settings.update", async () => {
    mountPage(["metaxisdata.settings.get"]);
    await flushPromises();

    expect(buttonsByText("Test")[0]?.disabled).toBe(true);
    expect(buttonsByText("Edit")[0]?.disabled).toBe(true);
    expect(buttonsByText("Delete")[0]?.disabled).toBe(true);
    // The retry action is destructive too: a dead delivery is not requeued by a
    // read-only caller.
    expect(buttonsByText("Retry")[0]?.disabled).toBe(true);
  });

  it("opens the create dialog and refuses an unnamed channel", async () => {
    mountPage();
    await flushPromises();

    buttonsByText("New channel")[0]?.click();
    await flushPromises();
    expect(document.body.textContent).toContain(
      "A channel receives each notification event once"
    );
    expect(document.body.textContent).toContain("Endpoint URL");

    buttonsByText("Save")[0]?.click();
    await flushPromises();
    expect(mocks.createNotificationChannel).not.toHaveBeenCalled();
    expect(notifyMocks.error).toHaveBeenCalled();
  });

  it("shows the stored channel in the edit dialog without leaking the endpoint", async () => {
    mountPage();
    await flushPromises();

    buttonsByText("Edit")[0]?.click();
    await flushPromises();
    const textInputs = Array.from(
      document.querySelectorAll('input[type="text"], input:not([type])')
    ) as HTMLInputElement[];
    expect(textInputs.length).toBeGreaterThanOrEqual(2);
    expect(textInputs[0].value).toBe("on-call");
    expect(textInputs[1].value).toBe("");
    expect(document.body.textContent).toContain(
      "Leave empty to keep the stored endpoint"
    );
  });

  it("retries a dead delivery", async () => {
    mountPage();
    await flushPromises();

    buttonsByText("Retry")[0]?.click();
    await flushPromises();
    expect(mocks.retryNotificationDelivery).toHaveBeenCalledWith(
      "workspaces/ws-1/notificationDeliveries/9"
    );
  });
});
