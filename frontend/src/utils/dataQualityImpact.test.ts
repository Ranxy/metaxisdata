import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  GetQualityTargetImpactResponseSchema,
  QualityConsumptionGapSchema,
  QualityImpactAssetSchema,
  QualityRootCauseCandidateSchema,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  consumptionGapRows,
  IMPACT_DEPTH,
  IMPACT_WINDOW_HOURS,
  impactDistanceText,
  impactDownstreamRows,
  impactExecutionRoute,
  impactFootnotes,
  impactGapWorkspaceRoute,
  impactMetadataRoute,
  impactMonitoringState,
  impactRootCauseRows,
  impactTargetPath,
  impactWindowHours,
  impactWindowText,
} from "./dataQualityImpact";

const START = new Date("2026-10-07T09:30:00Z");

function target(
  options: { guid?: string; object?: string; withPath?: boolean } = {}
) {
  return create(QualityTargetSchema, {
    metaGuid: options.guid ?? "inst1;shop;public;orders",
    metaType: MetaType.TABLE,
    path:
      options.withPath === false
        ? undefined
        : create(QualityTargetPathSchema, {
            instance: "prod",
            database: "shop",
            schema: "public",
            object: options.object ?? "orders",
          }),
  });
}

function summary(enabledRuleCount: number) {
  return create(QualityTargetSummarySchema, {
    target: target(),
    ruleCount: 2,
    enabledRuleCount,
  });
}

function downstreamAsset(options: {
  distance: number;
  object?: string;
  withSummary?: boolean;
  enabledRuleCount?: number;
}) {
  return create(QualityImpactAssetSchema, {
    target: target({ object: options.object }),
    distance: options.distance,
    summary:
      options.withSummary === false
        ? undefined
        : summary(options.enabledRuleCount ?? 1),
  });
}

function rootCause(options: {
  distance: number;
  object?: string;
  failingRuleCount?: number;
  failingExecutionCount?: number;
  executions?: string[];
  latestFailureTime?: Date;
  structureChangedTime?: Date;
}) {
  return create(QualityRootCauseCandidateSchema, {
    target: target({ object: options.object }),
    distance: options.distance,
    failingRuleCount: options.failingRuleCount ?? 0,
    failingExecutionCount: options.failingExecutionCount ?? 0,
    recentFailingExecutions: options.executions ?? [],
    latestFailureTime: options.latestFailureTime
      ? timestampFromDate(options.latestFailureTime)
      : undefined,
    structureChangedTime: options.structureChangedTime
      ? timestampFromDate(options.structureChangedTime)
      : undefined,
  });
}

describe("dataQualityImpact constants", () => {
  it("walks two levels and reads the 24 hours before the run", () => {
    expect(IMPACT_DEPTH).toBe(2);
    expect(IMPACT_WINDOW_HOURS).toBe(24);
  });
});

describe("impactTargetPath", () => {
  it("renders the scope and the object path", () => {
    expect(impactTargetPath(target())).toBe("prod · shop · public.orders");
  });

  it("falls back to the GUID when the path snapshot is empty", () => {
    expect(
      impactTargetPath(
        target({ guid: "inst1;shop;public;orders", withPath: false })
      )
    ).toBe("inst1;shop;public;orders");
  });

  it("renders '-' when there is neither a path nor a GUID", () => {
    expect(impactTargetPath(undefined)).toBe("-");
  });
});

describe("impactDistanceText", () => {
  it("names a direct dependent and a direct source", () => {
    expect(impactDistanceText(1)).toEqual({
      key: "dataQuality.impact.distanceDirectDependent",
      params: {},
    });
    expect(impactDistanceText(-1)).toEqual({
      key: "dataQuality.impact.distanceDirectSource",
      params: {},
    });
  });

  it("counts the hops in either direction", () => {
    expect(impactDistanceText(2)).toEqual({
      key: "dataQuality.impact.distanceHops",
      params: { count: 2 },
    });
    expect(impactDistanceText(5)).toEqual({
      key: "dataQuality.impact.distanceHops",
      params: { count: 5 },
    });
    expect(impactDistanceText(-2)).toEqual({
      key: "dataQuality.impact.distanceHopsUpstream",
      params: { count: 2 },
    });
    expect(impactDistanceText(-4)).toEqual({
      key: "dataQuality.impact.distanceHopsUpstream",
      params: { count: 4 },
    });
  });

  it("does not invent a hop count for zero", () => {
    expect(impactDistanceText(0)).toEqual({
      key: "dataQuality.impact.distanceUnknown",
      params: {},
    });
  });
});

describe("impactMonitoringState", () => {
  it("is unmonitored when the server sent no summary", () => {
    expect(impactMonitoringState(undefined)).toBe("unmonitored");
  });

  it("is allPaused when the target has no enabled rule", () => {
    expect(impactMonitoringState(summary(0))).toBe("allPaused");
  });

  it("is monitored when at least one rule is enabled", () => {
    expect(impactMonitoringState(summary(1))).toBe("monitored");
  });
});

describe("impactWindowHours", () => {
  it("reads the window length back from both bounds", () => {
    const start = timestampFromDate(new Date(START.getTime() - 24 * 3_600_000));
    expect(impactWindowHours(start, timestampFromDate(START))).toBe(24);
  });

  it("rounds a shorter window to whole hours", () => {
    const start = timestampFromDate(new Date(START.getTime() - 6 * 3_600_000));
    expect(impactWindowHours(start, timestampFromDate(START))).toBe(6);
  });

  it("is undefined without both bounds or with them reversed", () => {
    expect(impactWindowHours(undefined, timestampFromDate(START))).toBe(
      undefined
    );
    expect(
      impactWindowHours(timestampFromDate(START), timestampFromDate(START))
    ).toBe(undefined);
    expect(
      impactWindowHours(
        timestampFromDate(START),
        timestampFromDate(new Date(START.getTime() - 3_600_000))
      )
    ).toBe(undefined);
  });
});

describe("impactWindowText", () => {
  it("uses the response window when it carries one", () => {
    const response = create(GetQualityTargetImpactResponseSchema, {
      windowStart: timestampFromDate(
        new Date(START.getTime() - 24 * 3_600_000)
      ),
      windowEnd: timestampFromDate(START),
    });
    expect(impactWindowText(response)).toEqual({
      key: "dataQuality.impact.window",
      params: { hours: 24 },
    });
  });

  it("falls back to the requested 24 hours without a window", () => {
    const response = create(GetQualityTargetImpactResponseSchema, {});
    expect(impactWindowText(response)).toEqual({
      key: "dataQuality.impact.window",
      params: { hours: 24 },
    });
  });
});

describe("impactDownstreamRows", () => {
  it("keeps the server's nearest-first order", () => {
    const response = create(GetQualityTargetImpactResponseSchema, {
      downstreamAssets: [
        downstreamAsset({ distance: 1, object: "orders" }),
        downstreamAsset({ distance: 2, object: "revenue" }),
        downstreamAsset({ distance: 3, object: "dashboard" }),
      ],
    });
    const rows = impactDownstreamRows(response);

    expect(rows.map((row) => row.path)).toEqual([
      "prod · shop · public.orders",
      "prod · shop · public.revenue",
      "prod · shop · public.dashboard",
    ]);
    expect(rows.map((row) => row.distance)).toEqual([
      { key: "dataQuality.impact.distanceDirectDependent", params: {} },
      { key: "dataQuality.impact.distanceHops", params: { count: 2 } },
      { key: "dataQuality.impact.distanceHops", params: { count: 3 } },
    ]);
  });

  it("carries the monitoring state and the identity of each asset", () => {
    const response = create(GetQualityTargetImpactResponseSchema, {
      downstreamAssets: [
        downstreamAsset({ distance: 1, enabledRuleCount: 3 }),
        downstreamAsset({
          distance: 2,
          object: "revenue",
          enabledRuleCount: 0,
        }),
        downstreamAsset({
          distance: 3,
          object: "dashboard",
          withSummary: false,
        }),
      ],
    });
    const rows = impactDownstreamRows(response);

    expect(rows.map((row) => row.monitoring)).toEqual([
      "monitored",
      "allPaused",
      "unmonitored",
    ]);
    expect(rows[0]?.summary?.enabledRuleCount).toBe(3);
    expect(rows[2]?.summary).toBeUndefined();
    expect(rows[0]?.key).toBe("4:inst1;shop;public;orders");
  });

  it("keeps an asset with no target and keys it by position", () => {
    const response = create(GetQualityTargetImpactResponseSchema, {
      downstreamAssets: [create(QualityImpactAssetSchema, { distance: 1 })],
    });
    const [row] = impactDownstreamRows(response);

    expect(row?.path).toBe("-");
    expect(row?.key).toBe("downstream:0");
    expect(row?.metadataRoute).toBeNull();
  });
});

describe("impactRootCauseRows", () => {
  it("keeps the server's strongest-first order and its evidence", () => {
    const failedAt = new Date(START.getTime() - 3_600_000);
    const changedAt = new Date(START.getTime() - 7_200_000);
    const response = create(GetQualityTargetImpactResponseSchema, {
      rootCauses: [
        rootCause({
          distance: -1,
          object: "raw_orders",
          failingRuleCount: 2,
          failingExecutionCount: 5,
          executions: [
            "workspaces/ws/qualityExecutions/9",
            "workspaces/ws/qualityExecutions/8",
          ],
          latestFailureTime: failedAt,
          structureChangedTime: changedAt,
        }),
        rootCause({
          distance: -2,
          object: "extract",
          structureChangedTime: changedAt,
        }),
      ],
    });
    const rows = impactRootCauseRows(response);

    expect(rows.map((row) => row.target?.path?.object)).toEqual([
      "raw_orders",
      "extract",
    ]);
    expect(rows[0]?.failingRuleCount).toBe(2);
    expect(rows[0]?.failingExecutionCount).toBe(5);
    expect(rows[0]?.latestFailureTime).toEqual(timestampFromDate(failedAt));
    expect(rows[0]?.structureChangedTime).toEqual(timestampFromDate(changedAt));
    expect(rows[0]?.latestFailureExecution).toBe(
      "workspaces/ws/qualityExecutions/9"
    );
    expect(rows[0]?.executionRoute).toEqual({
      name: "DataQualityExecutionDetail",
      params: {
        executionId: ["workspaces", "ws", "qualityExecutions", "9"],
      },
    });
    expect(rows[1]?.failingExecutionCount).toBe(0);
    expect(rows[1]?.latestFailureExecution).toBe("");
    expect(rows[1]?.executionRoute).toBeNull();
  });

  it("keeps a target-less candidate honest", () => {
    const response = create(GetQualityTargetImpactResponseSchema, {
      rootCauses: [create(QualityRootCauseCandidateSchema, { distance: -1 })],
    });
    const [row] = impactRootCauseRows(response);

    expect(row?.path).toBe("-");
    expect(row?.key).toBe("rootCause:0");
    expect(row?.metadataRoute).toBeNull();
  });
});

describe("impactFootnotes", () => {
  it("lists each truncation and omission, downstream first", () => {
    const response = create(GetQualityTargetImpactResponseSchema, {
      downstreamTruncated: true,
      omittedDownstreamCount: 4,
      upstreamTruncated: true,
      omittedRootCauseCount: 2,
    });

    expect(impactFootnotes(response)).toEqual([
      { key: "dataQuality.impact.footnoteDownstreamTruncated", params: {} },
      {
        key: "dataQuality.impact.footnoteOmittedDownstream",
        params: { count: 4 },
      },
      { key: "dataQuality.impact.footnoteUpstreamTruncated", params: {} },
      {
        key: "dataQuality.impact.footnoteOmittedRootCauses",
        params: { count: 2 },
      },
    ]);
  });

  it("says nothing when no list was cut short", () => {
    expect(
      impactFootnotes(create(GetQualityTargetImpactResponseSchema, {}))
    ).toEqual([]);
    expect(
      impactFootnotes(
        create(GetQualityTargetImpactResponseSchema, {
          omittedDownstreamCount: 0,
          omittedRootCauseCount: 0,
        })
      )
    ).toEqual([]);
  });
});

describe("impactMetadataRoute", () => {
  it("opens a target's metadata detail page", () => {
    expect(impactMetadataRoute(target())).toEqual({
      name: "MetadataDetail",
      params: {
        guid: ["inst1", "shop", "public", "orders"],
      },
      query: { metaType: String(MetaType.TABLE) },
    });
  });

  it("is null without a GUID or a meta type", () => {
    expect(
      impactMetadataRoute({ metaGuid: "", metaType: MetaType.TABLE })
    ).toBeNull();
    expect(
      impactMetadataRoute({
        metaGuid: "inst1;shop;public;orders",
        metaType: MetaType.UNSPECIFIED,
      })
    ).toBeNull();
    expect(impactMetadataRoute(undefined)).toBeNull();
  });
});

describe("impactExecutionRoute", () => {
  it("opens the execution as one path segment per resource segment", () => {
    expect(
      impactExecutionRoute("workspaces/ws/qualityExecutions/exec-1")
    ).toEqual({
      name: "DataQualityExecutionDetail",
      params: {
        executionId: ["workspaces", "ws", "qualityExecutions", "exec-1"],
      },
    });
  });

  it("is null when there is no execution name", () => {
    expect(impactExecutionRoute(undefined)).toBeNull();
    expect(impactExecutionRoute("")).toBeNull();
    expect(impactExecutionRoute("   ")).toBeNull();
  });
});

describe("impactGapWorkspaceRoute", () => {
  it("opens the gap's target workspace", () => {
    expect(impactGapWorkspaceRoute(target())).toEqual({
      name: "DataQualityTarget",
      params: { targetGuid: ["inst1", "shop", "public", "orders"] },
      query: { metaType: String(MetaType.TABLE) },
    });
  });

  it("is null when the gap names no usable target", () => {
    expect(impactGapWorkspaceRoute(undefined)).toBeNull();
    expect(
      impactGapWorkspaceRoute(
        create(QualityTargetSchema, {
          metaGuid: "",
          metaType: MetaType.TABLE,
        })
      )
    ).toBeNull();
  });
});

describe("consumptionGapRows", () => {
  it("keeps the server's ranking and links each gap's workspace", () => {
    const rows = consumptionGapRows([
      create(QualityConsumptionGapSchema, {
        target: target({ object: "orders" }),
        downstreamAssetCount: 9,
      }),
      create(QualityConsumptionGapSchema, {
        target: target({ object: "customers" }),
        downstreamAssetCount: 3,
      }),
    ]);

    expect(rows.map((row) => row.path)).toEqual([
      "prod · shop · public.orders",
      "prod · shop · public.customers",
    ]);
    expect(rows.map((row) => row.downstreamAssetCount)).toEqual([9, 3]);
    expect(rows[0]?.key).toBe("4:inst1;shop;public;orders");
    expect(rows[0]?.workspaceRoute).toEqual({
      name: "DataQualityTarget",
      params: { targetGuid: ["inst1", "shop", "public", "orders"] },
      query: { metaType: String(MetaType.TABLE) },
    });
  });

  it("keys a target-less gap by position and gives it no link", () => {
    const rows = consumptionGapRows([
      create(QualityConsumptionGapSchema, { downstreamAssetCount: 1 }),
    ]);

    expect(rows[0]?.path).toBe("-");
    expect(rows[0]?.key).toBe("gap:0");
    expect(rows[0]?.workspaceRoute).toBeNull();
  });
});
