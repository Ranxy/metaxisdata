import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  DataQualitySettingSchema,
  EmailSettingSchema,
  SMTPAuthentication,
  SMTPConfigSchema,
  SMTPEncryption,
} from "@/types/proto-es/v1/setting_service_pb";

const mocks = vi.hoisted(() => ({
  getEmailSetting: vi.fn(),
  updateEmailSetting: vi.fn(),
  sendTestEmail: vi.fn(),
}));

vi.mock("./client", () => ({
  settingClient: {
    getEmailSetting: mocks.getEmailSetting,
    updateEmailSetting: mocks.updateEmailSetting,
    sendTestEmail: mocks.sendTestEmail,
  },
}));

import {
  buildUpdateDataQualitySettingRequest,
  buildUpdateEmailSettingRequest,
  getEmailSetting,
  sendTestEmail,
  updateEmailSetting,
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
