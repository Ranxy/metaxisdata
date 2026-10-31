import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  QualityExecutionState,
  QualityRunSchema,
  QualityStatus,
  QualityStatusCountsSchema,
  QualityTargetPathSchema,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  countsSummary,
  failedChecks,
  latestRun,
  runCounts,
  runDurationLabel,
  runOutcome,
  runState,
  runStateHint,
  scanTime,
  scanTimeLabel,
  scanTimeTitle,
  targetGuid,
  targetName,
  targetPathLine,
  unfinishedChecks,
  verdictCount,
} from "./dataQualityExecutions";

const t = (key: string) => `[${key}]`;
const START = new Date("2026-10-07T09:30:00Z");
const FINISH = new Date(START.getTime() + 2_000);

function summaryWith(
  fields: {
    guid?: string;
    object?: string;
    counts?: {
      pass?: number;
      warn?: number;
      fail?: number;
      error?: number;
      skipped?: number;
      notEvaluated?: number;
      total?: number;
    };
    state?: QualityExecutionState;
    outcome?: QualityStatus;
    started?: boolean;
    finished?: boolean;
    enabledRuleCount?: number;
  } = {}
): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: fields.guid,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object: fields.object,
      }),
    }),
    latestRun: create(QualityRunSchema, {
      state: fields.state ?? QualityExecutionState.COMPLETED,
      outcome: fields.outcome ?? QualityStatus.PASS,
      startTime:
        fields.started === false ? undefined : timestampFromDate(START),
      finishTime:
        fields.finished === false ? undefined : timestampFromDate(FINISH),
      counts: fields.counts
        ? create(QualityStatusCountsSchema, {
            pass: fields.counts.pass ?? 0,
            warn: fields.counts.warn ?? 0,
            fail: fields.counts.fail ?? 0,
            error: fields.counts.error ?? 0,
            skipped: fields.counts.skipped ?? 0,
            notEvaluated: fields.counts.notEvaluated ?? 0,
            total: fields.counts.total ?? 0,
          })
        : undefined,
    }),
    enabledRuleCount: fields.enabledRuleCount,
  });
}

describe("target readings", () => {
  it("names a target without guessing from its path", () => {
    const summary = summaryWith({ guid: "guid-1", object: "orders" });

    expect(targetGuid(summary)).toBe("guid-1");
    expect(targetName(summary)).toBe("orders");
    expect(targetPathLine(summary)).toContain("orders");
  });

  it("falls back to the GUID and then a dash", () => {
    const noPath = create(QualityTargetSummarySchema, {
      target: create(QualityTargetSchema, { metaGuid: "guid-2" }),
    });
    expect(targetName(noPath)).toBe("guid-2");

    const empty = create(QualityTargetSummarySchema, {});
    expect(targetGuid(empty)).toBe("");
    expect(targetName(empty)).toBe("-");
    expect(latestRun(empty)).toBeUndefined();
    expect(runCounts(empty)).toBeUndefined();
  });
});

describe("latest batch readings", () => {
  it("reads the lifecycle state and verdict", () => {
    const summary = summaryWith({
      state: QualityExecutionState.RUNNING,
      outcome: QualityStatus.FAIL,
    });

    expect(runState(summary)).toBe(QualityExecutionState.RUNNING);
    expect(runOutcome(summary)).toBe(QualityStatus.FAIL);
  });

  it("counts one verdict, or nothing without counts", () => {
    const counts = create(QualityStatusCountsSchema, {
      pass: 3,
      fail: 1,
      total: 5,
    });

    expect(verdictCount(counts, QualityStatus.PASS)).toBe(3);
    expect(verdictCount(counts, QualityStatus.FAIL)).toBe(1);
    expect(verdictCount(counts, QualityStatus.ERROR)).toBe(0);
    expect(verdictCount(undefined, QualityStatus.PASS)).toBe(0);
    expect(verdictCount(counts, QualityStatus.UNSPECIFIED)).toBe(0);
  });

  it("summarizes only the verdicts that occurred, in reading order", () => {
    const summary = summaryWith({
      counts: { pass: 3, fail: 1, total: 4 },
    });

    expect(countsSummary(summary, t)).toBe(
      "3 [dataQuality.status.pass] · 1 [dataQuality.status.fail]"
    );
    expect(countsSummary(summaryWith(), t)).toBe("");
  });

  it("reports failed and unfinished checks", () => {
    const summary = summaryWith({
      counts: { pass: 2, fail: 1, error: 1, skipped: 1, total: 6 },
    });

    expect(failedChecks(summary)).toBe(2);
    expect(unfinishedChecks(summary)).toBe(1);
    expect(failedChecks(summaryWith())).toBe(0);
    expect(unfinishedChecks(summaryWith())).toBe(0);
  });

  it("reads the time the batch ran", () => {
    const finished = summaryWith();
    expect(scanTime(finished)?.seconds).toBe(
      BigInt(Math.floor(FINISH.getTime() / 1000))
    );
    expect(scanTimeLabel(finished, "en-US")).not.toBe("");
    expect(scanTimeTitle(finished, "en-US")).toContain("2026");

    const running = summaryWith({ finished: false });
    expect(scanTime(running)?.seconds).toBe(
      BigInt(Math.floor(START.getTime() / 1000))
    );

    expect(
      scanTime(summaryWith({ started: false, finished: false }))
    ).toBeUndefined();
  });

  it("formats how long the batch took", () => {
    expect(runDurationLabel(summaryWith())).toBe("2 s");
    expect(runDurationLabel(create(QualityTargetSummarySchema, {}))).toBe("-");
    expect(runDurationLabel(summaryWith({ finished: false }))).toBe("-");
    expect(
      runDurationLabel(summaryWith({ started: false, finished: false }))
    ).toBe("-");
  });

  it("hints only about queued, running or abandoned batches", () => {
    expect(
      runStateHint(summaryWith({ state: QualityExecutionState.RUNNING }), t)
    ).toBe("[dataQuality.executions.runRunningHint]");
    expect(
      runStateHint(summaryWith({ state: QualityExecutionState.ABANDONED }), t)
    ).toBe("[dataQuality.executions.runAbandonedHint]");
    expect(runStateHint(summaryWith(), t)).toBe("");
  });
});
