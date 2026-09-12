import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import type { Group, GroupMember } from "@/types/proto-es/v1/group_service_pb";
import {
  CreateGroupRequestSchema,
  DeleteGroupRequestSchema,
  GetGroupRequestSchema,
  GroupSchema,
  ListGroupsRequestSchema,
  UpdateGroupRequestSchema,
} from "@/types/proto-es/v1/group_service_pb";
import { groupClient } from "./client";

export async function listGroups(options?: {
  pageSize?: number;
  pageToken?: string;
}) {
  const request = create(ListGroupsRequestSchema, {
    pageSize: options?.pageSize ?? 1000,
    pageToken: options?.pageToken ?? "",
  });
  return await groupClient.listGroups(request);
}

export async function getGroup(name: string) {
  const request = create(GetGroupRequestSchema, { name });
  return await groupClient.getGroup(request);
}

export async function createGroup(
  group: Pick<Group, "name" | "title" | "description"> & {
    members: GroupMember[];
  }
) {
  const request = create(CreateGroupRequestSchema, {
    group: create(GroupSchema, group),
  });
  return await groupClient.createGroup(request);
}

export async function updateGroup(
  group: Pick<Group, "name" | "title" | "description"> & {
    members: GroupMember[];
  },
  updateMask: string[]
) {
  const request = create(UpdateGroupRequestSchema, {
    group: create(GroupSchema, group),
    updateMask: create(FieldMaskSchema, { paths: updateMask }),
  });
  return await groupClient.updateGroup(request);
}

export async function deleteGroup(name: string) {
  const request = create(DeleteGroupRequestSchema, { name });
  return await groupClient.deleteGroup(request);
}
