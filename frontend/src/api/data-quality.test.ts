import { create } from "@bufbuild/protobuf";
import { DurationSchema, timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it, vi } from "vitest";
import {
  ExportQualityDataChunkSchema,
  ExportQualityReportResponseSchema,
  GetQualityConsumptionCoverageResponseSchema,
  GetQualityTargetImpactResponseSchema,
  QualityExecutionState,
  QualityIssueStatus,
  QualitySeverity,
  QualityStatus,
  QualityTargetOrder,
  QualityTargetScheduleSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  exportQualityExecutions,
  exportQualityIssues,
  exportQualityReport,
  exportQualityTargets,
  getQualityConsumptionCoverage,
  getQualityTargetImpact,
  listQualityTargets,
  updateQualityTargetSchedule,
} from "./data-quality";

const mocks = vi.hoisted(() => ({
  exportQualityExecutions: vi.fn(),
  exportQualityIssues: vi.fn(),
  exportQualityTargets: vi.fn(),
  exportQualityReport: vi.fn(),
  getQualityConsumptionCoverage: vi.fn(),
  getQualityTargetImpact: vi.fn(),
  listQualityTargets: vi.fn(),
  updateQualityTargetSchedule: vi.fn(),
}));

vi.mock("./client", () => ({
  dataQualityClient: {
    exportQualityExecutions: mocks.exportQualityExecutions,
    exportQualityIssues: mocks.exportQualityIssues,
    exportQualityTargets: mocks.exportQualityTargets,
    exportQualityReport: mocks.exportQualityReport,
    getQualityConsumptionCoverage: mocks.getQualityConsumptionCoverage,
    getQualityTargetImpact: mocks.getQualityTargetImpact,
    listQualityTargets: mocks.listQualityTargets,
    updateQualityTargetSchedule: mocks.updateQualityTargetSchedule,
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

describe("target selection", () => {
  it("asks for the exact objects it was handed, in request order", async () => {
    mocks.listQualityTargets.mockResolvedValue({
      qualityTargets: [],
      nextPageToken: "",
    });

    await listQualityTargets({
      targets: [
        { targetGuid: "inst1;shop;public;orders", metaType: MetaType.TABLE },
        {
          targetGuid: "inst1;shop;public;orders_view",
          metaType: MetaType.VIEW,
        },
      ],
    });

    const request = mocks.listQualityTargets.mock.calls[0]?.[0];
    expect(request.targets).toHaveLength(2);
    expect(request.targets[0]).toMatchObject({
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
    });
    expect(request.targets[1]).toMatchObject({
      targetGuid: "inst1;shop;public;orders_view",
      metaType: MetaType.VIEW,
    });
    // A selection is not a page: the caller's cap and order are the server's.
    expect(request.pageSize).toBe(50);
    expect(request.pageToken).toBe("");
  });

  it("sends no selection for a directory read", async () => {
    mocks.listQualityTargets.mockResolvedValue({
      qualityTargets: [],
      nextPageToken: "",
    });

    await listQualityTargets({ search: "orders" });

    expect(mocks.listQualityTargets.mock.calls[0]?.[0].targets).toEqual([]);
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

describe("target schedule wrapper", () => {
  it("sends the interval form alone", async () => {
    mocks.updateQualityTargetSchedule.mockResolvedValue(
      create(QualityTargetScheduleSchema, {})
    );

    await updateQualityTargetSchedule({
      targetGuid: "inst1;shop;public;orders",
      scheduleInterval: create(DurationSchema, { seconds: 3_600n, nanos: 0 }),
    });

    const request = mocks.updateQualityTargetSchedule.mock
      .calls[0][0] as Record<string, unknown>;
    expect(request).toMatchObject({
      parent: "workspaces/-",
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.UNSPECIFIED,
      cron: "",
      timezone: "",
    });
    expect(request.scheduleInterval).toMatchObject({ seconds: 3_600n });
  });

  it("sends the cron form with its zone and no interval", async () => {
    mocks.updateQualityTargetSchedule.mockResolvedValue(
      create(QualityTargetScheduleSchema, {})
    );

    await updateQualityTargetSchedule({
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
      cron: "0 2 * * *",
      timezone: "Asia/Shanghai",
    });

    const request = mocks.updateQualityTargetSchedule.mock
      .calls[0][0] as Record<string, unknown>;
    expect(request).toMatchObject({
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
      cron: "0 2 * * *",
      timezone: "Asia/Shanghai",
    });
    expect(request.scheduleInterval).toBeUndefined();
  });
});

describe("lineage impact wrapper", () => {
  it("names the target and asks for two levels over 24 hours", async () => {
    mocks.getQualityTargetImpact.mockResolvedValue(
      create(GetQualityTargetImpactResponseSchema, {})
    );

    await getQualityTargetImpact({
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
    });

    const request = requestOf(mocks.getQualityTargetImpact);
    expect(request).toMatchObject({
      parent: "workspaces/-",
      depth: 2,
      windowHours: 24,
    });
    expect(request.target).toMatchObject({
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
    });
    // An absent reference instant is omitted, not sent as the epoch.
    expect(request.asOf).toBeUndefined();
  });

  it("passes the reference instant and any explicit bounds", async () => {
    const asOf = timestampFromDate(new Date("2026-10-07T09:30:00Z"));
    const controller = new AbortController();
    mocks.getQualityTargetImpact.mockResolvedValue(
      create(GetQualityTargetImpactResponseSchema, {})
    );

    await getQualityTargetImpact({
      targetGuid: "inst1;shop;public;orders",
      metaType: MetaType.VIEW,
      depth: 3,
      windowHours: 6,
      asOf,
      signal: controller.signal,
    });

    expect(requestOf(mocks.getQualityTargetImpact)).toMatchObject({
      depth: 3,
      windowHours: 6,
      asOf,
    });
    expect(mocks.getQualityTargetImpact).toHaveBeenCalledWith(
      expect.anything(),
      { signal: controller.signal }
    );
  });
});

describe("consumption coverage wrapper", () => {
  it("uses the server default page size when none is given", async () => {
    mocks.getQualityConsumptionCoverage.mockResolvedValue(
      create(GetQualityConsumptionCoverageResponseSchema, {})
    );

    await getQualityConsumptionCoverage();

    expect(requestOf(mocks.getQualityConsumptionCoverage)).toMatchObject({
      parent: "workspaces/-",
      pageSize: 0,
    });
  });

  it("asks for the requested number of gaps and passes the signal", async () => {
    const controller = new AbortController();
    mocks.getQualityConsumptionCoverage.mockResolvedValue(
      create(GetQualityConsumptionCoverageResponseSchema, {
        consumedAssetCount: 40,
        monitoredAssetCount: 12,
        gapCount: 28,
      })
    );

    const response = await getQualityConsumptionCoverage({
      pageSize: 100,
      signal: controller.signal,
    });

    expect(response.gapCount).toBe(28);
    expect(requestOf(mocks.getQualityConsumptionCoverage)).toMatchObject({
      pageSize: 100,
    });
    expect(mocks.getQualityConsumptionCoverage).toHaveBeenCalledWith(
      expect.anything(),
      { signal: controller.signal }
    );
  });
});
