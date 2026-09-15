import { create } from "@bufbuild/protobuf";
import {
  ApproveDeviceLoginRequestSchema,
  GetDeviceLoginRequestSchema,
} from "@/types/proto-es/v1/auth_service_pb";
import { authClient } from "./client";

/**
 * normalizeUserCode accepts what a person types: any case, with or without the
 * dash, and the characters Crockford decodes to a digit. The server normalizes
 * again, so this only keeps a stray separator from corrupting the resource name.
 */
export function normalizeUserCode(raw: string): string {
  return raw.toUpperCase().replace(/[^0-9A-Z]/g, "");
}

/** deviceLoginName is the resource name the RPCs address a request by. */
export function deviceLoginName(userCode: string): string {
  return `deviceLogins/${normalizeUserCode(userCode)}`;
}

export async function getDeviceLogin(userCode: string) {
  const request = create(GetDeviceLoginRequestSchema, {
    name: deviceLoginName(userCode),
  });
  return await authClient.getDeviceLogin(request);
}

export async function approveDeviceLogin(userCode: string, approve: boolean) {
  const request = create(ApproveDeviceLoginRequestSchema, {
    name: deviceLoginName(userCode),
    approve,
  });
  return await authClient.approveDeviceLogin(request);
}
