import { create } from "@bufbuild/protobuf";
import type { IamPolicy } from "@/types/proto-es/v1/iam_service_pb";
import {
  GetWorkspaceIamPolicyRequestSchema,
  SetWorkspaceIamPolicyRequestSchema,
} from "@/types/proto-es/v1/iam_service_pb";
import { iamClient } from "./client";

export async function getWorkspaceIamPolicy() {
  const request = create(GetWorkspaceIamPolicyRequestSchema, {});
  return await iamClient.getWorkspaceIamPolicy(request);
}

export async function setWorkspaceIamPolicy(policy: IamPolicy, etag: string) {
  const request = create(SetWorkspaceIamPolicyRequestSchema, {
    policy,
    etag,
  });
  return await iamClient.setWorkspaceIamPolicy(request);
}
