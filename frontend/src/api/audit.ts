import { create } from "@bufbuild/protobuf";
import { ListAuditLogsRequestSchema } from "@/types/proto-es/v1/audit_log_service_pb";
import { auditLogClient } from "./client";

export async function listAuditLogs(options?: {
  parent?: string;
  pageSize?: number;
  pageToken?: string;
  filter?: string;
}) {
  const request = create(ListAuditLogsRequestSchema, {
    parent: options?.parent ?? "workspaces/-",
    pageSize: options?.pageSize ?? 50,
    pageToken: options?.pageToken ?? "",
    filter: options?.filter ?? "",
  });
  return await auditLogClient.listAuditLogs(request);
}
