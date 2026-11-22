import { create } from "@bufbuild/protobuf";
import type { IamGrant, IamPolicy } from "@/types/proto-es/v1/iam_service_pb";
import {
  CreateIamGrantRequestSchema,
  DeleteIamGrantRequestSchema,
  GetWorkspaceIamPolicyRequestSchema,
  IamGrantSchema,
  ListIamGrantsRequestSchema,
  SetWorkspaceIamPolicyRequestSchema,
} from "@/types/proto-es/v1/iam_service_pb";
import { iamClient } from "./client";
import { listAll } from "./list";

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

/**
 * Every scoped grant, or the grants of one scope. Grants are additive to the
 * workspace policy; the page renders the whole set.
 */
export async function listIamGrants(resource?: string): Promise<IamGrant[]> {
  return await listAll(async (pageToken) => {
    const request = create(ListIamGrantsRequestSchema, {
      resource: resource ?? "",
      pageSize: 100,
      pageToken,
    });
    const response = await iamClient.listIamGrants(request);
    return {
      items: response.grants,
      nextPageToken: response.nextPageToken,
    };
  });
}

/**
 * Add one scoped grant. `resource` is the scope resource name:
 * `instances/{instance}` or `instances/{instance}/databases/{database}`.
 */
export async function createIamGrant(input: {
  resource: string;
  role: string;
  member: string;
}) {
  const request = create(CreateIamGrantRequestSchema, {
    grant: create(IamGrantSchema, {
      resource: input.resource,
      role: input.role,
      member: input.member,
    }),
  });
  return await iamClient.createIamGrant(request);
}

export async function deleteIamGrant(name: string) {
  const request = create(DeleteIamGrantRequestSchema, { name });
  return await iamClient.deleteIamGrant(request);
}
