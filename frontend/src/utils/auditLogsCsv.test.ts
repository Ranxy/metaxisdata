import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  type AuditLog,
  AuditLogSeverity,
} from "@/types/proto-es/v1/audit_log_service_pb";
import {
  auditLogsCsvFilename,
  buildAuditCsv,
  timestampForExport,
} from "./auditLogsCsv";

function log(overrides: Partial<AuditLog> = {}): AuditLog {
  return {
    createTime: { seconds: 1_758_800_000n, nanos: 0 },
    parent: "workspaces/-",
    severity: AuditLogSeverity.INFO,
    method: "/v1/instances",
    resource: "instances/a",
    user: "users/1",
    status: { code: 0, message: "ok" },
    latencyMs: 12,
    requestMetadata: { ip: "127.0.0.1", userAgent: "probe" },
    ...overrides,
  } as AuditLog;
}

const context = {
  severityLabel: (severity: AuditLogSeverity) =>
    severity === AuditLogSeverity.INFO ? "Info" : "Other",
  identity: (name: string) => (name ? `${name} (resolved)` : ""),
};

describe("buildAuditCsv", () => {
  it("writes one header and one row per log", () => {
    const csv = buildAuditCsv([log(), log({ method: "/v1/users" })], context);
    const lines = csv.split("\n");
    expect(lines).toHaveLength(3);
    expect(lines[0]).toContain('"createTime"');
    expect(lines[0]).toContain('"serviceData"');
    expect(lines[1]).toContain('"Info"');
    expect(lines[1]).toContain('"instances/a (resolved)"');
    expect(lines[2]).toContain('"/v1/users"');
  });

  it("keeps the exported timestamp more precise than the table", () => {
    const csv = buildAuditCsv(
      [
        log({
          createTime: {
            seconds: 1_758_800_000n,
            nanos: 500_000_000,
          } as Timestamp,
        }),
      ],
      context
    );
    expect(csv).toContain('"2025-09-25T11:33:20.500Z"');
    // The same instant without the nanos column, as the table would show it.
    expect(new Date(1_758_800_000_000).toISOString()).toBe(
      "2025-09-25T11:33:20.000Z"
    );
  });

  it("serializes nested request data as JSON", () => {
    const csv = buildAuditCsv([log({ request: { pageSize: 50 } })], context);
    expect(csv).toContain('"{""pageSize"":50}"');
  });

  it("leaves absent optional columns empty and prints an int64", () => {
    const csv = buildAuditCsv(
      [
        log({
          createTime: undefined,
          status: undefined,
          latencyMs: 12n,
          requestMetadata: undefined,
          resource: "",
          user: "",
        }),
      ],
      context
    );

    expect(csv.split("\n")[1]).toBe(
      '"","workspaces/-","Info","/v1/instances","","","","","","","12","","","","",""'
    );
  });
});

describe("timestampForExport", () => {
  it("exports nothing for a missing or unrepresentable instant", () => {
    expect(timestampForExport(undefined)).toBe("");
    // Zero seconds is the proto's "unset" default, not 1970.
    expect(timestampForExport({ seconds: 0n, nanos: 0 } as Timestamp)).toBe("");
    // An absurd int64 overflows Date, which would otherwise render "Invalid Date".
    expect(
      timestampForExport({ seconds: 10n ** 30n, nanos: 0 } as Timestamp)
    ).toBe("");
  });
});

describe("auditLogsCsvFilename", () => {
  it("names the file after the export time", () => {
    expect(auditLogsCsvFilename(new Date(2026, 8, 26, 12, 8, 30))).toBe(
      "audit-logs-20260926-120830.csv"
    );
  });
});
