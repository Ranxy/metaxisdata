import { create } from "@bufbuild/protobuf";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import type { Role } from "@/types/proto-es/v1/role_service_pb";
import {
  CreateRoleRequestSchema,
  DeleteRoleRequestSchema,
  GetRoleRequestSchema,
  ListRolesRequestSchema,
  RoleSchema,
  UpdateRoleRequestSchema,
} from "@/types/proto-es/v1/role_service_pb";
import { roleClient } from "./client";

export async function listRoles(options?: {
  pageSize?: number;
  pageToken?: string;
}) {
  const request = create(ListRolesRequestSchema, {
    pageSize: options?.pageSize ?? 1000,
    pageToken: options?.pageToken ?? "",
  });
  return await roleClient.listRoles(request);
}

export async function getRole(name: string) {
  const request = create(GetRoleRequestSchema, { name });
  return await roleClient.getRole(request);
}

export async function createRole(
  role: Pick<Role, "name" | "title" | "description" | "permissions">
) {
  const request = create(CreateRoleRequestSchema, {
    role: create(RoleSchema, role),
  });
  return await roleClient.createRole(request);
}

export async function updateRole(
  role: Pick<Role, "name" | "title" | "description" | "permissions">,
  updateMask: string[]
) {
  const request = create(UpdateRoleRequestSchema, {
    role: create(RoleSchema, role),
    updateMask: create(FieldMaskSchema, { paths: updateMask }),
  });
  return await roleClient.updateRole(request);
}

export async function deleteRole(name: string) {
  const request = create(DeleteRoleRequestSchema, { name });
  return await roleClient.deleteRole(request);
}
