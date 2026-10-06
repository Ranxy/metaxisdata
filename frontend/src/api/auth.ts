import { create } from "@bufbuild/protobuf";
import { LoginRequestSchema } from "@/types/proto-es/v1/auth_service_pb";
import { authClient } from "./client";

export async function login(email: string, password: string) {
  const request = create(LoginRequestSchema, {
    email,
    password,
    web: true,
  });
  return await authClient.login(request);
}

export async function logout() {
  return await authClient.logout({});
}

/**
 * Renews the session from the refresh cookie. The transport calls it when a
 * request is refused with `Unauthenticated`; it needs no arguments because both
 * credentials travel in HttpOnly cookies.
 */
export async function refresh(): Promise<void> {
  await authClient.refresh({});
}
