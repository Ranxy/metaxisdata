import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  QualityStatus,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  isQualitySignalMetaType,
  qualitySignalAllPaused,
  qualitySignalCoverage,
  qualitySignalKey,
  qualitySignalTone,
  summarySignalKey,
} from "./dataQualitySignal";

const GUID = "inst1;shop;public;orders";

function summary(
  fields: {
    worstStatus?: QualityStatus;
    enabledRuleCount?: number;
    coveredColumnCount?: number;
    totalColumnCount?: number;
  } = {}
): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: GUID,
      metaType: MetaType.TABLE,
    }),
    worstStatus: fields.worstStatus ?? QualityStatus.UNSPECIFIED,
    enabledRuleCount: fields.enabledRuleCount ?? 1,
    coveredColumnCount: fields.coveredColumnCount ?? 0,
    totalColumnCount: fields.totalColumnCount ?? 0,
  });
}

describe("isQualitySignalMetaType", () => {
  it("accepts the four object types a check can target", () => {
    for (const metaType of [
      MetaType.TABLE,
      MetaType.VIEW,
      MetaType.MATERIALIZED_VIEW,
      MetaType.EXTERNAL_TABLE,
    ]) {
      expect(isQualitySignalMetaType(metaType)).toBe(true);
    }
  });

  it("rejects every other registry type", () => {
    for (const metaType of [
      MetaType.UNSPECIFIED,
      MetaType.DATABASE,
      MetaType.SCHEMA,
      MetaType.COLUMN,
      MetaType.FUNCTION,
      MetaType.MANUAL_SQL,
    ]) {
      expect(isQualitySignalMetaType(metaType)).toBe(false);
    }
  });
});

describe("qualitySignalKey", () => {
  it("names the meta type, because a GUID alone is ambiguous", () => {
    expect(qualitySignalKey(GUID, MetaType.TABLE)).not.toBe(
      qualitySignalKey(GUID, MetaType.VIEW)
    );
  });
});

describe("summarySignalKey", () => {
  it("reads the key back out of a returned summary", () => {
    expect(summarySignalKey(summary())).toBe(
      qualitySignalKey(GUID, MetaType.TABLE)
    );
  });

  it("is empty for a summary that names no target", () => {
    expect(summarySignalKey(create(QualityTargetSummarySchema, {}))).toBe("");
  });

  it("is empty for a type that cannot carry checks", () => {
    const column = create(QualityTargetSummarySchema, {
      target: create(QualityTargetSchema, {
        metaGuid: GUID,
        metaType: MetaType.COLUMN,
      }),
    });
    expect(summarySignalKey(column)).toBe("");
  });
});

describe("qualitySignalAllPaused", () => {
  it("is true only for a target that exists with no enabled check", () => {
    expect(qualitySignalAllPaused(undefined)).toBe(false);
    expect(qualitySignalAllPaused(summary({ enabledRuleCount: 0 }))).toBe(true);
    expect(qualitySignalAllPaused(summary({ enabledRuleCount: 1 }))).toBe(
      false
    );
  });
});

describe("qualitySignalTone", () => {
  it("is neutral when there is no signal at all", () => {
    expect(qualitySignalTone(undefined)).toBe("neutral");
  });

  it("is neutral for a paused target whatever its last verdict was", () => {
    expect(
      qualitySignalTone(
        summary({ enabledRuleCount: 0, worstStatus: QualityStatus.PASS })
      )
    ).toBe("neutral");
  });

  it("maps every verdict to its semantic tone", () => {
    expect(
      qualitySignalTone(summary({ worstStatus: QualityStatus.PASS }))
    ).toBe("success");
    expect(
      qualitySignalTone(summary({ worstStatus: QualityStatus.WARN }))
    ).toBe("warning");
    expect(
      qualitySignalTone(summary({ worstStatus: QualityStatus.FAIL }))
    ).toBe("destructive");
    expect(
      qualitySignalTone(summary({ worstStatus: QualityStatus.ERROR }))
    ).toBe("danger");
  });

  it("is neutral for a target whose checks never ran", () => {
    expect(
      qualitySignalTone(summary({ worstStatus: QualityStatus.UNSPECIFIED }))
    ).toBe("neutral");
  });
});

describe("qualitySignalCoverage", () => {
  it("is null without a signal or without a denominator", () => {
    expect(qualitySignalCoverage(undefined)).toBeNull();
    expect(qualitySignalCoverage(summary({ totalColumnCount: 0 }))).toBeNull();
  });

  it("reports covered and total for a resolved target", () => {
    expect(
      qualitySignalCoverage(
        summary({ coveredColumnCount: 4, totalColumnCount: 9 })
      )
    ).toEqual({ covered: 4, total: 9 });
  });

  it("never reports more covered columns than the target has", () => {
    expect(
      qualitySignalCoverage(
        summary({ coveredColumnCount: 12, totalColumnCount: 9 })
      )
    ).toEqual({ covered: 9, total: 9 });
    expect(
      qualitySignalCoverage(
        summary({ coveredColumnCount: -2, totalColumnCount: 9 })
      )
    ).toEqual({ covered: 0, total: 9 });
  });
});
