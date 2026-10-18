import { create } from "@bufbuild/protobuf";
import {
  ListConnectedAppsRequestSchema,
  RevokeConnectedAppRequestSchema,
} from "@/types/proto-es/v1/connected_app_service_pb";
import { connectedAppClient } from "./client";

/**
 * connectedAppName is the resource name a connected app is addressed by.
 * `id` is the grant row id, assigned by the server, so it is passed through
 * untouched.
 */
export function connectedAppName(id: string): string {
  return `connectedApps/${id}`;
}

export interface ListConnectedAppsOptions {
  pageSize?: number;
  pageToken?: string;
  /** users/{id}. Omitted, an administrator sees the workspace and anyone else
   *  only themselves; naming another user is refused without
   *  `metaxisdata.connectedApps.list`. */
  user?: string;
  /** CEL over user / state / client / client_id / user_email / create_time. */
  filter?: string;
  signal?: AbortSignal;
}

export async function listConnectedApps(
  options: ListConnectedAppsOptions = {}
) {
  const request = create(ListConnectedAppsRequestSchema, {
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    user: options.user ?? "",
    filter: options.filter ?? "",
  });
  return await connectedAppClient.listConnectedApps(request, {
    signal: options.signal,
  });
}

/**
 * Revokes one connection. The response is the revoked snapshot (state REVOKED
 * and revoke_time set), so the caller can update a row without refetching, but
 * the list is authoritative and refreshing is what keeps the page honest.
 */
export async function revokeConnectedApp(name: string) {
  const request = create(RevokeConnectedAppRequestSchema, { name });
  return await connectedAppClient.revokeConnectedApp(request);
}
