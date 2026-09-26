import type { Timestamp } from "@bufbuild/protobuf/wkt";
import type {
  AuditLog,
  AuditLogSeverity,
} from "@/types/proto-es/v1/audit_log_service_pb";
import { fileTimestamp, toCsv } from "./csv";

/** The two display lookups the CSV columns need; the page owns their state. */
export interface AuditCsvContext {
  severityLabel: (severity: AuditLogSeverity) => string;
  identity: (name: string) => string;
}

const HEADERS = [
  "createTime",
  "parent",
  "severity",
  "method",
  "resource",
  "resourceIdentifier",
  "user",
  "userIdentifier",
  "statusCode",
  "statusMessage",
  "latencyMs",
  "ip",
  "userAgent",
  "request",
  "response",
  "serviceData",
];

/** An exported timestamp keeps its sub-second precision, unlike the table. */
export function timestampForExport(ts: Timestamp | undefined): string {
  if (!ts?.seconds) {
    return "";
  }
  const milliseconds =
    Number(ts.seconds) * 1000 + Number(ts.nanos ?? 0) / 1_000_000;
  const date = new Date(milliseconds);
  return Number.isNaN(date.getTime()) ? "" : date.toISOString();
}

export function buildAuditCsv(
  logs: AuditLog[],
  context: AuditCsvContext
): string {
  const rows = logs.map((log) => [
    timestampForExport(log.createTime),
    log.parent,
    context.severityLabel(log.severity),
    log.method,
    context.identity(log.resource),
    log.resource,
    context.identity(log.user),
    log.user,
    log.status?.code ?? "",
    log.status?.message ?? "",
    log.latencyMs,
    log.requestMetadata?.ip ?? "",
    log.requestMetadata?.userAgent ?? "",
    log.request,
    log.response,
    log.serviceData,
  ]);
  return toCsv(HEADERS, rows);
}

export function auditLogsCsvFilename(now = new Date()): string {
  return `audit-logs-${fileTimestamp(now)}.csv`;
}
