import { create } from "@bufbuild/protobuf";
import {
  CreateUserRequestSchema,
  DeleteUserRequestSchema,
  GetUserRequestSchema,
  ListUsersRequestSchema,
  UndeleteUserRequestSchema,
  UpdateUserRequestSchema,
  UserSchema,
  UserType,
} from "@/types/proto-es/v1/user_service_pb";
import { userClient } from "./client";

export async function getCurrentUser() {
  return await userClient.getCurrentUser({});
}

export async function listUsers(options?: {
  pageSize?: number;
  pageToken?: string;
  showDeleted?: boolean;
  filter?: string;
}) {
  const request = create(ListUsersRequestSchema, {
    pageSize: options?.pageSize ?? 50,
    pageToken: options?.pageToken ?? "",
    showDeleted: options?.showDeleted ?? false,
    filter: options?.filter ?? "",
  });
  return await userClient.listUsers(request);
}

export async function getUser(name: string) {
  const request = create(GetUserRequestSchema, { name });
  return await userClient.getUser(request);
}

export async function createUser(
  email: string,
  password: string,
  title?: string
) {
  const user = create(UserSchema, {
    email,
    password,
    title: title || "",
    userType: UserType.USER,
  });
  const request = create(CreateUserRequestSchema, {
    user,
  });
  return await userClient.createUser(request);
}

export async function updateUser(
  user: {
    name: string;
    email?: string;
    title?: string;
    phone?: string;
    password?: string;
  },
  updateMask?: string[]
) {
  const request = create(UpdateUserRequestSchema, {
    user: create(UserSchema, {
      name: user.name,
      email: user.email ?? "",
      title: user.title ?? "",
      phone: user.phone ?? "",
      password: user.password ?? "",
    }),
    updateMask: updateMask
      ? { paths: updateMask }
      : { paths: ["email", "title", "phone"] },
  });
  return await userClient.updateUser(request);
}

export async function deleteUser(name: string) {
  const request = create(DeleteUserRequestSchema, { name });
  return await userClient.deleteUser(request);
}

export async function undeleteUser(name: string) {
  const request = create(UndeleteUserRequestSchema, { name });
  return await userClient.undeleteUser(request);
}
