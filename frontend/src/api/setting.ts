import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import {
  type DataQualitySetting,
  DataQualitySettingSchema,
  type EmailSetting,
  EmailSettingSchema,
  GetDebugConfigRequestSchema,
  GetEmailSettingRequestSchema,
  GetWorkspaceProfileSettingRequestSchema,
  SendTestEmailRequestSchema,
  SMTPConfigSchema,
  UpdateDebugConfigRequestSchema,
  UpdateEmailSettingRequestSchema,
  UpdateWorkspaceProfileSettingRequestSchema,
  WorkspaceProfileSettingSchema,
} from "@/types/proto-es/v1/setting_service_pb";
import { settingClient } from "./client";

export async function getWorkspaceProfileSetting() {
  const request = create(GetWorkspaceProfileSettingRequestSchema, {});
  return await settingClient.getWorkspaceProfileSetting(request);
}

export async function updateWorkspaceProfileSetting(
  setting: {
    externalUrl?: string;
    disallowSignup?: boolean;
    disallowPasswordSignin?: boolean;
    openlineageRetentionDays?: number;
    domains?: string[];
    enforceIdentityDomain?: boolean;
    allowedLlmProviderProfiles?: string[];
    mcpEnabled?: boolean;
    ssoEmailIdentityIdps?: string[];
  },
  updateMask: string[]
) {
  const request = create(UpdateWorkspaceProfileSettingRequestSchema, {
    setting: create(WorkspaceProfileSettingSchema, {
      externalUrl: setting.externalUrl ?? "",
      disallowSignup: setting.disallowSignup ?? false,
      disallowPasswordSignin: setting.disallowPasswordSignin ?? false,
      openlineageRetentionDays: setting.openlineageRetentionDays ?? 0,
      domains: setting.domains ?? [],
      enforceIdentityDomain: setting.enforceIdentityDomain ?? false,
      allowedLlmProviderProfiles: setting.allowedLlmProviderProfiles ?? [],
      mcpEnabled: setting.mcpEnabled ?? false,
      ssoEmailIdentityIdps: setting.ssoEmailIdentityIdps ?? [],
    }),
    updateMask: { paths: updateMask },
  });
  return await settingClient.updateWorkspaceProfileSetting(request);
}

export async function getDebugConfig() {
  const request = create(GetDebugConfigRequestSchema, {});
  return await settingClient.getDebugConfig(request);
}

export async function updateDebugConfig(enabled: boolean) {
  const request = create(UpdateDebugConfigRequestSchema, { enabled });
  return await settingClient.updateDebugConfig(request);
}

/**
 * Write the data quality section of the workspace profile. Only that field is
 * named in the update mask, so a page saving it cannot touch the rest of the
 * profile.
 */
/**
 * The update request for the data quality section. Exported so its mask can be
 * asserted without a transport: only `data_quality` may be written, so a page
 * saving this section cannot clear the rest of the profile.
 */
export function buildUpdateDataQualitySettingRequest(
  setting: DataQualitySetting
) {
  return create(UpdateWorkspaceProfileSettingRequestSchema, {
    setting: create(WorkspaceProfileSettingSchema, {
      dataQuality: create(DataQualitySettingSchema, setting),
    }),
    updateMask: create(FieldMaskSchema, { paths: ["data_quality"] }),
  });
}

export async function updateDataQualitySetting(setting: DataQualitySetting) {
  return await settingClient.updateWorkspaceProfileSetting(
    buildUpdateDataQualitySettingRequest(setting)
  );
}

/**
 * The workspace SMTP setting the EMAIL channels deliver through. A workspace
 * that never configured one reads back as an empty setting, not a 404.
 */
export async function getEmailSetting(): Promise<EmailSetting> {
  return await settingClient.getEmailSetting(
    create(GetEmailSettingRequestSchema, {})
  );
}

/**
 * The update request for the email setting. Exported so its mask can be
 * asserted without a transport: the whole `smtp` message is written, so the
 * password is not named as a path — an empty one keeps the stored password.
 */
export function buildUpdateEmailSettingRequest(setting: EmailSetting) {
  return create(UpdateEmailSettingRequestSchema, {
    setting: create(EmailSettingSchema, {
      from: setting.from,
      fromName: setting.fromName,
      smtp: create(SMTPConfigSchema, setting.smtp ?? {}),
    }),
    updateMask: create(FieldMaskSchema, {
      paths: ["from", "from_name", "smtp"],
    }),
  });
}

export async function updateEmailSetting(
  setting: EmailSetting
): Promise<EmailSetting> {
  return await settingClient.updateEmailSetting(
    buildUpdateEmailSettingRequest(setting)
  );
}

/** Send one test message through the stored SMTP setting. */
export async function sendTestEmail(): Promise<void> {
  await settingClient.sendTestEmail(create(SendTestEmailRequestSchema, {}));
}
