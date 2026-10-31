import { create } from "@bufbuild/protobuf";
import { flushPromises, mount, type VueWrapper } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { Select } from "@/components/ui/select";
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
import {
  EmailSettingSchema,
  SMTPAuthentication,
  SMTPConfigSchema,
  SMTPEncryption,
} from "@/types/proto-es/v1/setting_service_pb";
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

const settingMocks = vi.hoisted(() => ({
  getEmailSetting: vi.fn(),
  updateEmailSetting: vi.fn(),
  sendTestEmail: vi.fn(),
}));

vi.mock("@/api/setting", () => settingMocks);

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

function dingtalkChannel() {
  return create(NotificationChannelSchema, {
    name: "workspaces/ws-1/notificationChannels/ding123",
    displayName: "ops robot",
    kind: NotificationChannelKind.DINGTALK,
    enabled: true,
    eventTypes: [NotificationType.DATA_QUALITY],
    minSeverity: NotificationSeverity.WARNING,
    language: "en-US",
    secretSet: true,
    config: { config: { case: "dingtalk", value: { sign: true } } },
  });
}

function emailSetting() {
  return create(EmailSettingSchema, {
    from: "notifications@example.com",
    fromName: "Metaxisdata",
    passwordSet: true,
    smtp: create(SMTPConfigSchema, {
      host: "smtp.example.com",
      port: 587,
      encryption: SMTPEncryption.SMTP_ENCRYPTION_STARTTLS,
      authentication: SMTPAuthentication.SMTP_AUTHENTICATION_PLAIN,
      username: "mailer",
    }),
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

function buttonsByText(
  text: string,
  root: ParentNode = document
): HTMLButtonElement[] {
  return Array.from(root.querySelectorAll("button")).filter(
    (button) => button.textContent?.trim() === text
  );
}

/** The dialog is teleported out of the wrapper, so its buttons are queried in the document. */
function dialogButtonsByText(text: string): HTMLButtonElement[] {
  const dialog = document.querySelector('[role="dialog"]');
  return dialog ? buttonsByText(text, dialog) : [];
}

/**
 * A FormField label carries the `for` of its input, which is how a field is
 * found without depending on the generated id.
 */
function inputByLabel(label: string): HTMLInputElement | null {
  const field = Array.from(document.querySelectorAll("label")).find(
    (element) =>
      element.getAttribute("for") &&
      element.textContent?.trim().startsWith(label)
  );
  const id = field?.getAttribute("for");
  return id ? (document.getElementById(id) as HTMLInputElement | null) : null;
}

async function setInput(input: HTMLInputElement | null, value: string) {
  if (!input) {
    throw new Error("input not found");
  }
  input.value = value;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  await flushPromises();
}

/** The create dialog's kind select is the only Select whose value is a channel kind. */
function kindSelect(wrapper: VueWrapper): VueWrapper {
  const select = wrapper
    .findAllComponents(Select)
    .find(
      (candidate) =>
        candidate.props("modelValue") ===
        String(NotificationChannelKind.WEBHOOK)
    );
  if (!select) {
    throw new Error("kind select not found");
  }
  return select;
}

beforeEach(() => {
  mocks.listNotificationChannels.mockResolvedValue([channel()]);
  mocks.listNotificationDeliveriesPage.mockResolvedValue({
    items: [deadDelivery()],
    nextPageToken: "",
  });
  mocks.createNotificationChannel.mockResolvedValue(channel());
  mocks.retryNotificationDelivery.mockResolvedValue(deadDelivery());
  settingMocks.getEmailSetting.mockResolvedValue(emailSetting());
  settingMocks.updateEmailSetting.mockResolvedValue(emailSetting());
  settingMocks.sendTestEmail.mockResolvedValue({});
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
    // The SMTP card saves and sends through settings.update as well.
    expect(buttonsByText("Send test email")[0]?.disabled).toBe(true);
    expect(buttonsByText("Save")[0]?.disabled).toBe(true);
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

    dialogButtonsByText("Save")[0]?.click();
    await flushPromises();
    expect(mocks.createNotificationChannel).not.toHaveBeenCalled();
    expect(notifyMocks.error).toHaveBeenCalled();
  });

  it("shows the stored channel in the edit dialog without leaking the endpoint", async () => {
    mountPage();
    await flushPromises();

    buttonsByText("Edit")[0]?.click();
    await flushPromises();
    const dialog = document.querySelector('[role="dialog"]');
    const textInputs = Array.from(
      dialog?.querySelectorAll('input[type="text"], input:not([type])') ?? []
    ) as HTMLInputElement[];
    expect(textInputs.length).toBeGreaterThanOrEqual(2);
    expect(textInputs[0].value).toBe("on-call");
    expect(textInputs[1].value).toBe("");
    expect(document.body.textContent).toContain(
      "Leave empty to keep the stored endpoint"
    );
  });

  it("creates an email channel with recipients and no secret", async () => {
    const wrapper = mountPage();
    await flushPromises();

    buttonsByText("New channel")[0]?.click();
    await flushPromises();
    await kindSelect(wrapper).vm.$emit("update:modelValue", "5");
    await flushPromises();

    // The endpoint/secret fields belong to the webhook-like kinds only.
    expect(document.body.textContent).toContain("Recipients");
    expect(document.body.textContent).not.toContain("Endpoint URL");

    await setInput(inputByLabel("Name"), "digest");
    await setInput(
      inputByLabel("Recipients"),
      "oncall@example.com, data@example.com"
    );
    dialogButtonsByText("Save")[0]?.click();
    await flushPromises();

    expect(mocks.createNotificationChannel).toHaveBeenCalledTimes(1);
    const [created, secret] = mocks.createNotificationChannel.mock.calls[0];
    expect(created.kind).toBe(NotificationChannelKind.EMAIL);
    expect(created.config?.config?.case).toBe("email");
    expect(created.config?.config?.value).toMatchObject({
      addresses: ["oncall@example.com", "data@example.com"],
      includeAudience: false,
    });
    expect(secret).toBeUndefined();
  });

  it("refuses an email channel with no recipient source", async () => {
    const wrapper = mountPage();
    await flushPromises();

    buttonsByText("New channel")[0]?.click();
    await flushPromises();
    await kindSelect(wrapper).vm.$emit("update:modelValue", "5");
    await flushPromises();
    await setInput(inputByLabel("Name"), "digest");

    dialogButtonsByText("Save")[0]?.click();
    await flushPromises();

    expect(mocks.createNotificationChannel).not.toHaveBeenCalled();
    expect(notifyMocks.error).toHaveBeenCalled();
  });

  it("shows the robot signing fields for a DingTalk channel", async () => {
    mocks.listNotificationChannels.mockResolvedValue([dingtalkChannel()]);
    mountPage();
    await flushPromises();

    expect(document.body.textContent).toContain("DingTalk");

    buttonsByText("Edit")[0]?.click();
    await flushPromises();

    expect(document.body.textContent).toContain("Sign the request");
    expect(document.body.textContent).toContain("Signing secret");
    // The webhook-only HMAC copy must not leak into the robot dialog.
    expect(document.body.textContent).not.toContain("HMAC-SHA256");
  });

  it("loads and saves the SMTP setting, sending a typed password only", async () => {
    mountPage();
    await flushPromises();

    expect(settingMocks.getEmailSetting).toHaveBeenCalledTimes(1);
    expect(inputByLabel("From address")?.value).toBe(
      "notifications@example.com"
    );
    expect(inputByLabel("SMTP host")?.value).toBe("smtp.example.com");
    expect(inputByLabel("Password")?.type).toBe("password");
    expect(document.body.textContent).toContain(
      "Leave empty to keep the stored password"
    );

    await setInput(inputByLabel("Password"), "hunter2");
    buttonsByText("Save")[0]?.click();
    await flushPromises();

    expect(settingMocks.updateEmailSetting).toHaveBeenCalledTimes(1);
    const saved = settingMocks.updateEmailSetting.mock.calls[0][0];
    expect(saved.from).toBe("notifications@example.com");
    expect(saved.smtp).toMatchObject({
      host: "smtp.example.com",
      port: 587,
      encryption: SMTPEncryption.SMTP_ENCRYPTION_STARTTLS,
      authentication: SMTPAuthentication.SMTP_AUTHENTICATION_PLAIN,
      username: "mailer",
      password: "hunter2",
    });
    expect(notifyMocks.success).toHaveBeenCalledWith("Email setting saved");
    // The reload happens after the save, and the write-only field is cleared.
    expect(settingMocks.getEmailSetting).toHaveBeenCalledTimes(2);
    expect(inputByLabel("Password")?.value).toBe("");

    // An empty password is sent as empty: the server keeps the stored one.
    buttonsByText("Save")[0]?.click();
    await flushPromises();
    expect(settingMocks.updateEmailSetting.mock.calls[1][0].smtp.password).toBe(
      ""
    );
  });

  it("sends a test email", async () => {
    mountPage();
    await flushPromises();

    buttonsByText("Send test email")[0]?.click();
    await flushPromises();

    expect(settingMocks.sendTestEmail).toHaveBeenCalledTimes(1);
    expect(notifyMocks.success).toHaveBeenCalledWith("Test email sent");
  });

  it("keeps the SMTP card read-only without settings.get", async () => {
    mountPage(["metaxisdata.settings.update"]);
    await flushPromises();

    expect(settingMocks.getEmailSetting).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      "You do not have permission to view or change the email setting"
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
