import { create } from "@bufbuild/protobuf";
import { describe, expect, it, vi } from "vitest";
import {
  ExportQualityDataChunkSchema,
  ExportQualityReportResponseSchema,
  QualityExecutionState,
  QualityIssueStatus,
  QualitySeverity,
  QualityStatus,
  QualityTargetOrder,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  exportQualityExecutions,
  exportQualityIssues,
  exportQualityReport,
  exportQualityTargets,
} from "./data-quality";

const mocks = vi.hoisted(() => ({
  exportQualityExecutions: vi.fn(),
  exportQualityIssues: vi.fn(),
  exportQualityTargets: vi.fn(),
  exportQualityReport: vi.fn(),
}));

vi.mock("./client", () => ({
  dataQualityClient: {
    exportQualityExecutions: mocks.exportQualityExecutions,
    exportQualityIssues: mocks.exportQualityIssues,
    exportQualityTargets: mocks.exportQualityTargets,
    exportQualityReport: mocks.exportQualityReport,
  },
}));

const encoder = new TextEncoder();

/** Two slices of one CSV document, the first carrying the server's UTF-8 BOM. */
function chunkStream() {
  return (async function* () {
    yield create(ExportQualityDataChunkSchema, {
      data: encoder.encode('\uFEFF"id","target"\n'),
      rowCount: 0n,
    });
    yield create(ExportQualityDataChunkSchema, {
      data: encoder.encode('"1","shop.orders"\n'),
      rowCount: 1n,
    });
  })();
}

/** The request the wrapper handed the client, for one mocked export call. */
function requestOf(mock: typeof mocks.exportQualityExecutions) {
  return mock.mock.calls[0]?.[0] as Record<string, unknown>;
}

describe("CSV export wrappers", () => {
  it("concatenates the streamed slices into one CSV file", async () => {
    mocks.exportQualityExecutions.mockReturnValue(chunkStream());

    const file = await exportQualityExecutions();

    expect(file.rowCount).toBe(1n);
    expect(file.truncated).toBe(false);
    expect(file.blob.type).toBe("text/csv;charset=utf-8;");
    // The server's BOM arrives in the first slice; the client keeps it.
    const bytes = new Uint8Array(await file.blob.arrayBuffer());
    expect([...bytes.slice(0, 3)]).toEqual([0xef, 0xbb, 0xbf]);
    expect(await file.blob.text()).toBe('"id","target"\n"1","shop.orders"\n');
  });

  it("exports the executions the filters select, without paging", async () => {
    mocks.exportQualityExecutions.mockReturnValue(chunkStream());
    const controller = new AbortController();

    await exportQualityExecutions({
      state: QualityExecutionState.COMPLETED,
      outcome: QualityStatus.FAIL,
      rule: "workspaces/ws/qualityRules/rule-1",
      targetGuid: "inst1;shop;public;orders",
      run: "workspaces/ws/qualityRuns/run-1",
      maxRows: 5000,
      signal: controller.signal,
    });

    expect(requestOf(mocks.exportQualityExecutions)).toMatchObject({
      parent: "workspaces/-",
      state: QualityExecutionState.COMPLETED,
      outcome: QualityStatus.FAIL,
      rule: "workspaces/ws/qualityRules/rule-1",
      targetGuid: "inst1;shop;public;orders",
      run: "workspaces/ws/qualityRuns/run-1",
      maxRows: 5000,
    });
    expect(mocks.exportQualityExecutions).toHaveBeenCalledWith(
      expect.anything(),
      {
        signal: controller.signal,
      }
    );
  });

  it("defaults every execution filter to unspecified", async () => {
    mocks.exportQualityExecutions.mockReturnValue(chunkStream());

    await exportQualityExecutions();

    expect(requestOf(mocks.exportQualityExecutions)).toMatchObject({
      state: QualityExecutionState.UNSPECIFIED,
      outcome: QualityStatus.UNSPECIFIED,
      rule: "",
      targetGuid: "",
      maxRows: 0,
    });
  });

  it("exports the issues the filters select", async () => {
    mocks.exportQualityIssues.mockReturnValue(chunkStream());

    const file = await exportQualityIssues({
      status: QualityIssueStatus.OPEN,
      severity: QualitySeverity.CRITICAL,
    });

    expect(file.rowCount).toBe(1n);
    expect(requestOf(mocks.exportQualityIssues)).toMatchObject({
      parent: "workspaces/-",
      status: QualityIssueStatus.OPEN,
      severity: QualitySeverity.CRITICAL,
      rule: "",
      owner: "",
      maxRows: 0,
    });
  });

  it("exports the targets the directory filters select", async () => {
    mocks.exportQualityTargets.mockReturnValue(chunkStream());

    await exportQualityTargets({
      search: "orders",
      failingOnly: true,
      hasOpenIssue: true,
      hasPendingDraft: true,
      orderBy: QualityTargetOrder.ATTENTION,
    });

    expect(requestOf(mocks.exportQualityTargets)).toMatchObject({
      parent: "workspaces/-",
      search: "orders",
      // The failing switch means FAIL or ERROR, so it must not ride on worstStatus.
      failingOnly: true,
      worstStatus: QualityStatus.UNSPECIFIED,
      hasOpenIssue: true,
      hasPendingDraft: true,
      orderBy: QualityTargetOrder.ATTENTION,
      maxRows: 0,
    });
  });
});

describe("report export", () => {
  it("asks for one target's report in the UI language", async () => {
    const response = create(ExportQualityReportResponseSchema, {
      fileName: "quality-report-2026-10-31T120000Z.html",
      htmlContent: "<html><body>report</body></html>",
    });
    mocks.exportQualityReport.mockResolvedValue(response);

    const result = await exportQualityReport({
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
      language: "zh-CN",
    });

    expect(result.fileName).toBe("quality-report-2026-10-31T120000Z.html");
    expect(result.htmlContent).toContain("report");
    expect(requestOf(mocks.exportQualityReport)).toMatchObject({
      parent: "workspaces/-",
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
      trendDays: 0,
      language: "zh-CN",
    });
  });
});
