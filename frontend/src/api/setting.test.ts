import { create } from "@bufbuild/protobuf";
import { DurationSchema, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  DataQualitySettingSchema,
  DayOfWeek,
  EmailSettingSchema,
  MaintenanceWindowSchema,
  QuietHoursSettingSchema,
  QuietWindowSchema,
  SMTPAuthentication,
  SMTPConfigSchema,
  SMTPEncryption,
} from "@/types/proto-es/v1/setting_service_pb";

const mocks = vi.hoisted(() => ({
  getEmailSetting: vi.fn(),
  updateEmailSetting: vi.fn(),
  sendTestEmail: vi.fn(),
  getQuietHoursSetting: vi.fn(),
  updateQuietHoursSetting: vi.fn(),
}));

vi.mock("./client", () => ({
  settingClient: {
    getEmailSetting: mocks.getEmailSetting,
    updateEmailSetting: mocks.updateEmailSetting,
    sendTestEmail: mocks.sendTestEmail,
    getQuietHoursSetting: mocks.getQuietHoursSetting,
    updateQuietHoursSetting: mocks.updateQuietHoursSetting,
  },
}));

import {
  buildUpdateDataQualitySettingRequest,
  buildUpdateEmailSettingRequest,
  buildUpdateQuietHoursSettingRequest,
  getEmailSetting,
  getQuietHoursSetting,
  sendTestEmail,
  updateEmailSetting,
  updateQuietHoursSetting,
} from "./setting";

beforeEach(() => {
  vi.clearAllMocks();
});

function emailSetting() {
  return create(EmailSettingSchema, {
    from: "notifications@example.com",
    fromName: "Metaxisdata",
    smtp: create(SMTPConfigSchema, {
      host: "smtp.example.com",
      port: 587,
      encryption: SMTPEncryption.SMTP_ENCRYPTION_STARTTLS,
      authentication: SMTPAuthentication.SMTP_AUTHENTICATION_PLAIN,
      username: "mailer",
      password: "s3cret",
    }),
  });
}

function quietHoursSetting() {
  return create(QuietHoursSettingSchema, {
    enabled: true,
    timezone: "Asia/Shanghai",
    windows: [
      create(QuietWindowSchema, {
        days: [DayOfWeek.MONDAY, DayOfWeek.FRIDAY],
        start: "18:00",
        end: "09:00",
      }),
    ],
    maintenanceWindows: [
      create(MaintenanceWindowSchema, {
        start: timestampFromDate(new Date("2026-11-01T01:00:00Z")),
        end: timestampFromDate(new Date("2026-11-01T03:00:00Z")),
      }),
    ],
  });
}

describe("buildUpdateDataQualitySettingRequest", () => {
  it("writes only the data_quality field", () => {
    const request = buildUpdateDataQualitySettingRequest(
      create(DataQualitySettingSchema, {
        enableLlmAnalysis: true,
        failureSampleLimit: 25,
        defaultScheduleInterval: create(DurationSchema, {
          seconds: 900n,
          nanos: 0,
        }),
      })
    );

    expect(request.updateMask?.paths).toEqual(["data_quality"]);
    expect(request.setting?.dataQuality).toMatchObject({
      enableLlmAnalysis: true,
      failureSampleLimit: 25,
    });
    expect(request.setting?.dataQuality?.defaultScheduleInterval?.seconds).toBe(
      900n
    );
    // Nothing else in the profile is sent, so the mask has nothing else to apply.
    expect(request.setting?.externalUrl).toBe("");
    expect(request.setting?.allowedLlmProviderProfiles).toEqual([]);
  });
});

describe("buildUpdateEmailSettingRequest", () => {
  it("writes from, from_name and the whole smtp message", () => {
    const request = buildUpdateEmailSettingRequest(emailSetting());

    expect(request.updateMask?.paths).toEqual(["from", "from_name", "smtp"]);
    // The password is write-only and travels inside `smtp`; the mask never
    // names it, so an empty value keeps the stored password.
    expect(request.updateMask?.paths).not.toContain("smtp.password");
    expect(request.setting?.from).toBe("notifications@example.com");
    expect(request.setting?.fromName).toBe("Metaxisdata");
    expect(request.setting?.smtp).toMatchObject({
      host: "smtp.example.com",
      port: 587,
      encryption: SMTPEncryption.SMTP_ENCRYPTION_STARTTLS,
      authentication: SMTPAuthentication.SMTP_AUTHENTICATION_PLAIN,
      username: "mailer",
      password: "s3cret",
    });
    // password_set is an output-only field and is not written back.
    expect(request.setting?.passwordSet).toBe(false);
  });
});

describe("email setting API", () => {
  it("reads the workspace email setting with an empty request", async () => {
    mocks.getEmailSetting.mockResolvedValue(emailSetting());

    const setting = await getEmailSetting();

    expect(setting.from).toBe("notifications@example.com");
    expect(mocks.getEmailSetting).toHaveBeenCalledTimes(1);
    expect(mocks.getEmailSetting.mock.calls[0][0].$typeName).toBe(
      "metaxisdata.v1.GetEmailSettingRequest"
    );
  });

  it("updates the email setting through the request builder", async () => {
    mocks.updateEmailSetting.mockResolvedValue(emailSetting());

    await updateEmailSetting(emailSetting());

    expect(mocks.updateEmailSetting).toHaveBeenCalledTimes(1);
    const request = mocks.updateEmailSetting.mock.calls[0][0];
    expect(request.$typeName).toBe("metaxisdata.v1.UpdateEmailSettingRequest");
    expect(request.updateMask?.paths).toEqual(["from", "from_name", "smtp"]);
    expect(request.setting?.smtp?.password).toBe("s3cret");
  });

  it("sends a test email with an empty request", async () => {
    mocks.sendTestEmail.mockResolvedValue({});

    await sendTestEmail();

    expect(mocks.sendTestEmail).toHaveBeenCalledTimes(1);
    expect(mocks.sendTestEmail.mock.calls[0][0].$typeName).toBe(
      "metaxisdata.v1.SendTestEmailRequest"
    );
  });
});

describe("buildUpdateQuietHoursSettingRequest", () => {
  it("writes the whole schedule behind its four mask paths", () => {
    const request = buildUpdateQuietHoursSettingRequest(quietHoursSetting());

    expect(request.updateMask?.paths).toEqual([
      "enabled",
      "timezone",
      "windows",
      "maintenance_windows",
    ]);
    expect(request.setting).toMatchObject({
      enabled: true,
      timezone: "Asia/Shanghai",
    });
    expect(request.setting?.windows[0]).toMatchObject({
      days: [DayOfWeek.MONDAY, DayOfWeek.FRIDAY],
      start: "18:00",
      end: "09:00",
    });
    expect(request.setting?.maintenanceWindows[0].start?.seconds).toBe(
      BigInt(new Date("2026-11-01T01:00:00Z").getTime() / 1000)
    );
    expect(request.setting?.maintenanceWindows[0].end?.seconds).toBe(
      BigInt(new Date("2026-11-01T03:00:00Z").getTime() / 1000)
    );
  });
});

describe("quiet-hours API", () => {
  it("reads the schedule with an empty request", async () => {
    mocks.getQuietHoursSetting.mockResolvedValue(quietHoursSetting());

    const setting = await getQuietHoursSetting();

    expect(setting.timezone).toBe("Asia/Shanghai");
    expect(mocks.getQuietHoursSetting).toHaveBeenCalledTimes(1);
    expect(mocks.getQuietHoursSetting.mock.calls[0][0].$typeName).toBe(
      "metaxisdata.v1.GetQuietHoursSettingRequest"
    );
  });

  it("updates the schedule through the request builder", async () => {
    mocks.updateQuietHoursSetting.mockResolvedValue(quietHoursSetting());

    await updateQuietHoursSetting(quietHoursSetting());

    expect(mocks.updateQuietHoursSetting).toHaveBeenCalledTimes(1);
    const request = mocks.updateQuietHoursSetting.mock.calls[0][0];
    expect(request.$typeName).toBe(
      "metaxisdata.v1.UpdateQuietHoursSettingRequest"
    );
    expect(request.updateMask?.paths).toEqual([
      "enabled",
      "timezone",
      "windows",
      "maintenance_windows",
    ]);
    expect(request.setting?.windows).toHaveLength(1);
  });
});
