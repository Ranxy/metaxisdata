import { create } from "@bufbuild/protobuf";
import {
  GetDebugConfigRequestSchema,
  GetWorkspaceProfileSettingRequestSchema,
  UpdateDebugConfigRequestSchema,
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
    disallowSignup?: boolean;
    disallowPasswordSignin?: boolean;
  },
  updateMask: string[]
) {
  const request = create(UpdateWorkspaceProfileSettingRequestSchema, {
    setting: create(WorkspaceProfileSettingSchema, {
      disallowSignup: setting.disallowSignup ?? false,
      disallowPasswordSignin: setting.disallowPasswordSignin ?? false,
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
