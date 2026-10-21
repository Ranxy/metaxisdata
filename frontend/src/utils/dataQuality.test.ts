import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import enUS from "@/locales/en-US.json";
import zhCN from "@/locales/zh-CN.json";
import {
  QualityAggregateFunction,
  QualityComparison,
  QualityComparisonOperator,
  QualityDimension,
  QualityEmptyBehavior,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityFilterOperator,
  QualityIssueStatus,
  QualityObservedKind,
  QualityResolutionReason,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityStatus,
  QualityTargetPathSchema,
  QualityThresholdMetric,
  QualityThresholdsSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  describeThreshold,
  executionDurationSeconds,
  formatDuration,
  formatObservedValue,
  formatRowCount,
  formatScheduleInterval,
  formatSeconds,
  formatTargetPath,
  formatWarnThreshold,
  isExecutionInProgress,
  qualityAggregateFunctionKey,
  qualityComparisonOperatorKey,
  qualityDimensionKey,
  qualityEmptyBehaviorKey,
  qualityErrorCodeKey,
  qualityExecutionStateKey,
  qualityExecutionStateVariant,
  qualityExecutionTriggerKey,
  qualityFilterOperatorKey,
  qualityIssueStatusKey,
  qualityIssueStatusVariant,
  qualityResolutionReasonKey,
  qualityRuleStateKey,
  qualityRuleStateVariant,
  qualityRuleTypeKey,
  qualitySeverityKey,
  qualitySeverityVariant,
  qualityStatusKey,
  qualityStatusVariant,
  qualityThresholdMetricKey,
  targetScopeLabel,
} from "./dataQuality";

describe("qualityStatusKey / qualityStatusVariant", () => {
  it("names every outcome", () => {
    expect(qualityStatusKey(QualityStatus.PASS)).toBe(
      "dataQuality.status.pass"
    );
    expect(qualityStatusKey(QualityStatus.WARN)).toBe(
      "dataQuality.status.warn"
    );
    expect(qualityStatusKey(QualityStatus.FAIL)).toBe(
      "dataQuality.status.fail"
    );
    expect(qualityStatusKey(QualityStatus.ERROR)).toBe(
      "dataQuality.status.error"
    );
    expect(qualityStatusKey(QualityStatus.SKIPPED)).toBe(
      "dataQuality.status.skipped"
    );
    expect(qualityStatusKey(QualityStatus.NOT_EVALUATED)).toBe(
      "dataQuality.status.notEvaluated"
    );
    expect(qualityStatusKey(QualityStatus.UNSPECIFIED)).toBe(
      "dataQuality.status.unspecified"
    );
  });

  it("falls back for an outcome this build does not know", () => {
    expect(qualityStatusKey(99 as QualityStatus)).toBe(
      "dataQuality.status.unspecified"
    );
  });

  // ERROR is not FAIL: one means the data failed the check, the other means the
  // check could not be evaluated. They must not look identical.
  it("distinguishes fail from error and pass from warn", () => {
    expect(qualityStatusVariant(QualityStatus.PASS)).toBe("success");
    expect(qualityStatusVariant(QualityStatus.WARN)).toBe("warning");
    expect(qualityStatusVariant(QualityStatus.FAIL)).toBe("destructive");
    expect(qualityStatusVariant(QualityStatus.ERROR)).toBe("danger");
    expect(qualityStatusVariant(QualityStatus.SKIPPED)).toBe("neutral");
    expect(qualityStatusVariant(QualityStatus.NOT_EVALUATED)).toBe("outline");
    expect(qualityStatusVariant(QualityStatus.UNSPECIFIED)).toBe("secondary");
    expect(qualityStatusVariant(99 as QualityStatus)).toBe("secondary");
  });
});

describe("severity", () => {
  it("names and tones every severity", () => {
    expect(qualitySeverityKey(QualitySeverity.WARNING)).toBe(
      "dataQuality.severity.warning"
    );
    expect(qualitySeverityKey(QualitySeverity.ERROR)).toBe(
      "dataQuality.severity.error"
    );
    expect(qualitySeverityKey(QualitySeverity.CRITICAL)).toBe(
      "dataQuality.severity.critical"
    );
    expect(qualitySeverityKey(QualitySeverity.UNSPECIFIED)).toBe(
      "dataQuality.severity.unspecified"
    );
    expect(qualitySeverityKey(99 as QualitySeverity)).toBe(
      "dataQuality.severity.unspecified"
    );

    expect(qualitySeverityVariant(QualitySeverity.WARNING)).toBe("warning");
    expect(qualitySeverityVariant(QualitySeverity.ERROR)).toBe("destructive");
    expect(qualitySeverityVariant(QualitySeverity.CRITICAL)).toBe("danger");
    expect(qualitySeverityVariant(QualitySeverity.UNSPECIFIED)).toBe(
      "secondary"
    );
    expect(qualitySeverityVariant(99 as QualitySeverity)).toBe("secondary");
  });
});

describe("qualityDimensionKey", () => {
  it("names every dimension", () => {
    expect(qualityDimensionKey(QualityDimension.COMPLETENESS)).toBe(
      "dataQuality.dimension.completeness"
    );
    expect(qualityDimensionKey(QualityDimension.VALIDITY)).toBe(
      "dataQuality.dimension.validity"
    );
    expect(qualityDimensionKey(QualityDimension.UNIQUENESS)).toBe(
      "dataQuality.dimension.uniqueness"
    );
    expect(qualityDimensionKey(QualityDimension.CONSISTENCY)).toBe(
      "dataQuality.dimension.consistency"
    );
    expect(qualityDimensionKey(QualityDimension.TIMELINESS)).toBe(
      "dataQuality.dimension.timeliness"
    );
    expect(qualityDimensionKey(QualityDimension.ACCURACY)).toBe(
      "dataQuality.dimension.accuracy"
    );
    expect(qualityDimensionKey(QualityDimension.UNSPECIFIED)).toBe(
      "dataQuality.dimension.unspecified"
    );
    expect(qualityDimensionKey(99 as QualityDimension)).toBe(
      "dataQuality.dimension.unspecified"
    );
  });
});

describe("qualityRuleTypeKey", () => {
  it("names every rule type", () => {
    expect(qualityRuleTypeKey(QualityRuleType.ROW_COUNT)).toBe(
      "dataQuality.ruleType.rowCount"
    );
    expect(qualityRuleTypeKey(QualityRuleType.ROW_COUNT_CHANGE)).toBe(
      "dataQuality.ruleType.rowCountChange"
    );
    expect(qualityRuleTypeKey(QualityRuleType.NULL_PERCENT)).toBe(
      "dataQuality.ruleType.nullPercent"
    );
    expect(qualityRuleTypeKey(QualityRuleType.UNIQUE)).toBe(
      "dataQuality.ruleType.unique"
    );
    expect(qualityRuleTypeKey(QualityRuleType.UNIQUE_COMBINATION)).toBe(
      "dataQuality.ruleType.uniqueCombination"
    );
    expect(qualityRuleTypeKey(QualityRuleType.ACCEPTED_VALUES)).toBe(
      "dataQuality.ruleType.acceptedValues"
    );
    expect(qualityRuleTypeKey(QualityRuleType.RANGE)).toBe(
      "dataQuality.ruleType.range"
    );
    expect(qualityRuleTypeKey(QualityRuleType.REGEX)).toBe(
      "dataQuality.ruleType.regex"
    );
    expect(qualityRuleTypeKey(QualityRuleType.STRING_LENGTH)).toBe(
      "dataQuality.ruleType.stringLength"
    );
    expect(qualityRuleTypeKey(QualityRuleType.REFERENTIAL_INTEGRITY)).toBe(
      "dataQuality.ruleType.referentialIntegrity"
    );
    expect(qualityRuleTypeKey(QualityRuleType.CROSS_COLUMN)).toBe(
      "dataQuality.ruleType.crossColumn"
    );
    expect(qualityRuleTypeKey(QualityRuleType.FRESHNESS)).toBe(
      "dataQuality.ruleType.freshness"
    );
    expect(qualityRuleTypeKey(QualityRuleType.CUSTOM_SQL)).toBe(
      "dataQuality.ruleType.customSql"
    );
    expect(qualityRuleTypeKey(QualityRuleType.CEL_EXPRESSION)).toBe(
      "dataQuality.ruleType.celExpression"
    );
    expect(qualityRuleTypeKey(QualityRuleType.UNSPECIFIED)).toBe(
      "dataQuality.ruleType.unspecified"
    );
    expect(qualityRuleTypeKey(99 as QualityRuleType)).toBe(
      "dataQuality.ruleType.unspecified"
    );
  });
});

describe("rule state", () => {
  it("names and tones every state", () => {
    expect(qualityRuleStateKey(QualityRuleState.ENABLED)).toBe(
      "dataQuality.ruleState.enabled"
    );
    expect(qualityRuleStateKey(QualityRuleState.PAUSED)).toBe(
      "dataQuality.ruleState.paused"
    );
    expect(qualityRuleStateKey(QualityRuleState.ARCHIVED)).toBe(
      "dataQuality.ruleState.archived"
    );
    expect(qualityRuleStateKey(QualityRuleState.UNSPECIFIED)).toBe(
      "dataQuality.ruleState.unspecified"
    );
    expect(qualityRuleStateKey(99 as QualityRuleState)).toBe(
      "dataQuality.ruleState.unspecified"
    );

    expect(qualityRuleStateVariant(QualityRuleState.ENABLED)).toBe("success");
    expect(qualityRuleStateVariant(QualityRuleState.PAUSED)).toBe("warning");
    expect(qualityRuleStateVariant(QualityRuleState.ARCHIVED)).toBe("neutral");
    expect(qualityRuleStateVariant(QualityRuleState.UNSPECIFIED)).toBe(
      "secondary"
    );
    expect(qualityRuleStateVariant(99 as QualityRuleState)).toBe("secondary");
  });
});

describe("execution lifecycle", () => {
  it("names and tones every execution state", () => {
    expect(qualityExecutionStateKey(QualityExecutionState.PENDING)).toBe(
      "dataQuality.executionState.pending"
    );
    expect(qualityExecutionStateKey(QualityExecutionState.RUNNING)).toBe(
      "dataQuality.executionState.running"
    );
    expect(qualityExecutionStateKey(QualityExecutionState.COMPLETED)).toBe(
      "dataQuality.executionState.completed"
    );
    expect(qualityExecutionStateKey(QualityExecutionState.ABANDONED)).toBe(
      "dataQuality.executionState.abandoned"
    );
    expect(qualityExecutionStateKey(QualityExecutionState.UNSPECIFIED)).toBe(
      "dataQuality.executionState.unspecified"
    );
    expect(qualityExecutionStateKey(99 as QualityExecutionState)).toBe(
      "dataQuality.executionState.unspecified"
    );

    expect(qualityExecutionStateVariant(QualityExecutionState.PENDING)).toBe(
      "info"
    );
    expect(qualityExecutionStateVariant(QualityExecutionState.RUNNING)).toBe(
      "info"
    );
    expect(qualityExecutionStateVariant(QualityExecutionState.COMPLETED)).toBe(
      "success"
    );
    expect(qualityExecutionStateVariant(QualityExecutionState.ABANDONED)).toBe(
      "warning"
    );
    expect(
      qualityExecutionStateVariant(QualityExecutionState.UNSPECIFIED)
    ).toBe("secondary");
    expect(qualityExecutionStateVariant(99 as QualityExecutionState)).toBe(
      "secondary"
    );
  });

  it("names every trigger", () => {
    expect(qualityExecutionTriggerKey(QualityExecutionTrigger.MANUAL)).toBe(
      "dataQuality.executionTrigger.manual"
    );
    expect(qualityExecutionTriggerKey(QualityExecutionTrigger.SCHEDULED)).toBe(
      "dataQuality.executionTrigger.scheduled"
    );
    expect(
      qualityExecutionTriggerKey(QualityExecutionTrigger.UNSPECIFIED)
    ).toBe("dataQuality.executionTrigger.unspecified");
    expect(qualityExecutionTriggerKey(99 as QualityExecutionTrigger)).toBe(
      "dataQuality.executionTrigger.unspecified"
    );
  });

  it("polls only while the execution can still change", () => {
    expect(isExecutionInProgress(QualityExecutionState.PENDING)).toBe(true);
    expect(isExecutionInProgress(QualityExecutionState.RUNNING)).toBe(true);
    expect(isExecutionInProgress(QualityExecutionState.COMPLETED)).toBe(false);
    expect(isExecutionInProgress(QualityExecutionState.ABANDONED)).toBe(false);
    expect(isExecutionInProgress(QualityExecutionState.UNSPECIFIED)).toBe(
      false
    );
  });

  it("measures a finished execution and leaves a running one undefined", () => {
    expect(
      executionDurationSeconds(
        create(QualityExecutionSchema, {
          startTime: { seconds: 1_700_000_000n, nanos: 0 },
          finishTime: { seconds: 1_700_000_125n, nanos: 0 },
        })
      )
    ).toBe(125);
    expect(
      executionDurationSeconds(
        create(QualityExecutionSchema, {
          startTime: { seconds: 1_700_000_000n, nanos: 0 },
        })
      )
    ).toBeUndefined();
    expect(
      executionDurationSeconds(
        create(QualityExecutionSchema, {
          startTime: { seconds: 1_700_000_200n, nanos: 0 },
          finishTime: { seconds: 1_700_000_000n, nanos: 0 },
        })
      )
    ).toBeUndefined();
    expect(
      executionDurationSeconds(
        create(QualityExecutionSchema, {
          startTime: { seconds: 0n, nanos: 0 },
          finishTime: { seconds: 1_700_000_000n, nanos: 0 },
        })
      )
    ).toBeUndefined();
  });
});

describe("describeThreshold", () => {
  it("returns nothing when the snapshot is absent", () => {
    expect(describeThreshold(undefined)).toBeUndefined();
  });

  it("renders a single-sided fail bound", () => {
    expect(
      describeThreshold(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.GREATER_THAN,
          failValue: 0,
        }),
        QualityObservedKind.COUNT
      )
    ).toEqual({
      key: "dataQuality.thresholdGreaterThan",
      params: { value: "0" },
    });
    expect(
      describeThreshold(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.LESS_THAN,
          failValue: 0.01,
        }),
        QualityObservedKind.RATIO
      )
    ).toEqual({
      key: "dataQuality.thresholdLessThan",
      params: { value: "1%" },
    });
  });

  it("renders a bound pair for the range comparisons", () => {
    expect(
      describeThreshold(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.OUTSIDE_RANGE,
          failValue: 10,
          failValueUpper: 20,
        }),
        QualityObservedKind.COUNT
      )
    ).toEqual({
      key: "dataQuality.thresholdOutside",
      params: { lower: "10", upper: "20" },
    });
    expect(
      describeThreshold(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.INSIDE_RANGE,
          failValue: 0,
          failValueUpper: 1,
        })
      )
    ).toEqual({
      key: "dataQuality.thresholdInside",
      params: { lower: "0", upper: "1" },
    });
  });

  it("leaves an incomplete snapshot unrendered", () => {
    expect(
      describeThreshold(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.OUTSIDE_RANGE,
          failValue: 10,
        })
      )
    ).toBeUndefined();
    expect(
      describeThreshold(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.GREATER_THAN,
        })
      )
    ).toBeUndefined();
    expect(
      describeThreshold(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.UNSPECIFIED,
        })
      )
    ).toBeUndefined();
  });

  it("falls back to a plain value for an unknown comparison", () => {
    expect(
      describeThreshold(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.UNSPECIFIED,
          failValue: 3,
        }),
        QualityObservedKind.NUMBER
      )
    ).toEqual({
      key: "dataQuality.thresholdValue",
      params: { value: "3" },
    });
  });
});

describe("formatWarnThreshold", () => {
  it("reads the warn bound from either side of a range", () => {
    expect(formatWarnThreshold(undefined)).toBeUndefined();
    expect(
      formatWarnThreshold(
        create(QualityThresholdsSchema, {
          warnValue: 5,
          comparison: QualityComparison.GREATER_THAN,
        }),
        QualityObservedKind.COUNT
      )
    ).toBe("5");
    expect(
      formatWarnThreshold(
        create(QualityThresholdsSchema, {
          warnValueUpper: 100,
          comparison: QualityComparison.OUTSIDE_RANGE,
        }),
        QualityObservedKind.COUNT
      )
    ).toBe("100");
    expect(
      formatWarnThreshold(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.GREATER_THAN,
        })
      )
    ).toBeUndefined();
  });
});

describe("qualityErrorCodeKey", () => {
  it("maps every documented failure class", () => {
    expect(qualityErrorCodeKey("CONNECTION")).toBe(
      "dataQuality.errorCode.connection"
    );
    expect(qualityErrorCodeKey("AUTH")).toBe("dataQuality.errorCode.auth");
    expect(qualityErrorCodeKey("TIMEOUT")).toBe(
      "dataQuality.errorCode.timeout"
    );
    expect(qualityErrorCodeKey("PERMISSION")).toBe(
      "dataQuality.errorCode.permission"
    );
    expect(qualityErrorCodeKey("UNSUPPORTED")).toBe(
      "dataQuality.errorCode.unsupported"
    );
    expect(qualityErrorCodeKey("SQL_SYNTAX")).toBe(
      "dataQuality.errorCode.sqlSyntax"
    );
    expect(qualityErrorCodeKey("BAD_DATA")).toBe(
      "dataQuality.errorCode.badData"
    );
    expect(qualityErrorCodeKey("LIMIT_EXCEEDED")).toBe(
      "dataQuality.errorCode.limitExceeded"
    );
    expect(qualityErrorCodeKey("ORPHANED")).toBe(
      "dataQuality.errorCode.orphaned"
    );
  });

  it("is case- and whitespace-insensitive and returns nothing for an unknown code", () => {
    expect(qualityErrorCodeKey(" timeout ")).toBe(
      "dataQuality.errorCode.timeout"
    );
    expect(qualityErrorCodeKey("SOMETHING_NEW")).toBe("");
    expect(qualityErrorCodeKey("")).toBe("");
  });
});

describe("target formatting", () => {
  it("joins the path into schema.object.column", () => {
    expect(
      formatTargetPath(
        create(QualityTargetPathSchema, {
          instance: "prod",
          database: "shop",
          schema: "public",
          object: "orders",
          column: "id",
        })
      )
    ).toBe("public.orders.id");
    expect(
      formatTargetPath(
        create(QualityTargetPathSchema, {
          instance: "prod",
          database: "shop",
          schema: "public",
          object: "orders",
        })
      )
    ).toBe("public.orders");
    expect(
      formatTargetPath(
        create(QualityTargetPathSchema, {
          object: "orders",
          column: "id",
        })
      )
    ).toBe("orders.id");
  });

  it("returns a placeholder for a missing or empty path", () => {
    expect(formatTargetPath(undefined)).toBe("-");
    expect(formatTargetPath(create(QualityTargetPathSchema, {}))).toBe("-");
  });

  it("keeps the instance and database as the coarser scope", () => {
    expect(
      targetScopeLabel(
        create(QualityTargetPathSchema, {
          instance: "prod",
          database: "shop",
          schema: "public",
        })
      )
    ).toBe("prod · shop");
    expect(
      targetScopeLabel(create(QualityTargetPathSchema, { instance: "prod" }))
    ).toBe("prod");
    expect(targetScopeLabel(undefined)).toBe("-");
    expect(targetScopeLabel(create(QualityTargetPathSchema, {}))).toBe("-");
  });
});

describe("number and value formatting", () => {
  it("groups a row count in the reader's locale", () => {
    expect(formatRowCount(1204, "en-US")).toBe("1,204");
    expect(formatRowCount(1204n, "en-US")).toBe("1,204");
    expect(formatRowCount(0)).toBe("0");
  });

  it("formats the observed value in the unit the result recorded", () => {
    expect(formatObservedValue(1234.6, QualityObservedKind.COUNT)).toBe(
      "1,235"
    );
    expect(formatObservedValue(0.0125, QualityObservedKind.RATIO)).toBe(
      "1.25%"
    );
    expect(formatObservedValue(2.5, QualityObservedKind.SECONDS)).toBe("2.5 s");
    expect(
      formatObservedValue(1_700_000_000, QualityObservedKind.TIMESTAMP)
    ).toContain("2023");
    expect(formatObservedValue(12.345, QualityObservedKind.NUMBER)).toBe(
      "12.35"
    );
    expect(formatObservedValue(12.345)).toBe("12.35");
    expect(formatObservedValue(Number.NaN, QualityObservedKind.COUNT)).toBe(
      "-"
    );
  });

  it("renders seconds compactly across every magnitude", () => {
    expect(formatSeconds(-1)).toBe("-");
    expect(formatSeconds(Number.NaN)).toBe("-");
    expect(formatSeconds(0.85)).toBe("850 ms");
    expect(formatSeconds(2.5)).toBe("2.5 s");
    expect(formatSeconds(90)).toBe("1m 30s");
    expect(formatSeconds(120)).toBe("2m");
    expect(formatSeconds(3_660)).toBe("1h 1m");
    expect(formatSeconds(7_200)).toBe("2h");
  });

  it("renders a protobuf duration", () => {
    expect(formatDuration(undefined)).toBe("-");
    expect(
      formatDuration(create(DurationSchema, { seconds: 5n, nanos: 0 }))
    ).toBe("5 s");
    expect(
      formatDuration(
        create(DurationSchema, { seconds: 0n, nanos: 500_000_000 })
      )
    ).toBe("500 ms");
    expect(
      formatDuration(create(DurationSchema, { seconds: 0n, nanos: 0 }))
    ).toBe("-");
  });

  it("renders a schedule interval in clock notation", () => {
    expect(formatScheduleInterval(undefined)).toBe("-");
    expect(
      formatScheduleInterval(create(DurationSchema, { seconds: 0n, nanos: 0 }))
    ).toBe("-");
    expect(
      formatScheduleInterval(
        create(DurationSchema, { seconds: 900n, nanos: 0 })
      )
    ).toBe("15m");
    expect(
      formatScheduleInterval(
        create(DurationSchema, { seconds: 3_600n, nanos: 0 })
      )
    ).toBe("1h");
    expect(
      formatScheduleInterval(
        create(DurationSchema, { seconds: 5_400n, nanos: 0 })
      )
    ).toBe("90m");
    expect(
      formatScheduleInterval(
        create(DurationSchema, { seconds: 604_800n, nanos: 0 })
      )
    ).toBe("7d");
    expect(
      formatScheduleInterval(create(DurationSchema, { seconds: 90n, nanos: 0 }))
    ).toBe("90s");
  });
});

describe("aggregate function vocabulary", () => {
  it("names every aggregate", () => {
    expect(qualityAggregateFunctionKey(QualityAggregateFunction.COUNT)).toBe(
      "dataQuality.aggregateFunction.count"
    );
    expect(qualityAggregateFunctionKey(QualityAggregateFunction.SUM)).toBe(
      "dataQuality.aggregateFunction.sum"
    );
    expect(qualityAggregateFunctionKey(QualityAggregateFunction.AVG)).toBe(
      "dataQuality.aggregateFunction.avg"
    );
    expect(qualityAggregateFunctionKey(QualityAggregateFunction.MIN)).toBe(
      "dataQuality.aggregateFunction.min"
    );
    expect(qualityAggregateFunctionKey(QualityAggregateFunction.MAX)).toBe(
      "dataQuality.aggregateFunction.max"
    );
    expect(qualityAggregateFunctionKey(QualityAggregateFunction.STDDEV)).toBe(
      "dataQuality.aggregateFunction.stddev"
    );
    expect(
      qualityAggregateFunctionKey(QualityAggregateFunction.PERCENTILE)
    ).toBe("dataQuality.aggregateFunction.percentile");
    expect(
      qualityAggregateFunctionKey(QualityAggregateFunction.UNSPECIFIED)
    ).toBe("dataQuality.aggregateFunction.unspecified");
    expect(qualityAggregateFunctionKey(99 as QualityAggregateFunction)).toBe(
      "dataQuality.aggregateFunction.unspecified"
    );
  });
});

describe("issue vocabulary", () => {
  it("names and tones every issue state", () => {
    expect(qualityIssueStatusKey(QualityIssueStatus.OPEN)).toBe(
      "dataQuality.issueStatus.open"
    );
    expect(qualityIssueStatusKey(QualityIssueStatus.ACKNOWLEDGED)).toBe(
      "dataQuality.issueStatus.acknowledged"
    );
    expect(qualityIssueStatusKey(QualityIssueStatus.RESOLVED)).toBe(
      "dataQuality.issueStatus.resolved"
    );
    expect(qualityIssueStatusKey(QualityIssueStatus.UNSPECIFIED)).toBe(
      "dataQuality.issueStatus.unspecified"
    );
    expect(qualityIssueStatusKey(99 as QualityIssueStatus)).toBe(
      "dataQuality.issueStatus.unspecified"
    );

    expect(qualityIssueStatusVariant(QualityIssueStatus.OPEN)).toBe(
      "destructive"
    );
    expect(qualityIssueStatusVariant(QualityIssueStatus.ACKNOWLEDGED)).toBe(
      "warning"
    );
    expect(qualityIssueStatusVariant(QualityIssueStatus.RESOLVED)).toBe(
      "success"
    );
    expect(qualityIssueStatusVariant(QualityIssueStatus.UNSPECIFIED)).toBe(
      "secondary"
    );
    expect(qualityIssueStatusVariant(99 as QualityIssueStatus)).toBe(
      "secondary"
    );
  });

  it("names every resolution reason", () => {
    expect(qualityResolutionReasonKey(QualityResolutionReason.FIXED)).toBe(
      "dataQuality.resolutionReason.fixed"
    );
    expect(qualityResolutionReasonKey(QualityResolutionReason.EXPECTED)).toBe(
      "dataQuality.resolutionReason.expected"
    );
    expect(
      qualityResolutionReasonKey(QualityResolutionReason.NO_ACTION_NEEDED)
    ).toBe("dataQuality.resolutionReason.noActionNeeded");
    expect(qualityResolutionReasonKey(QualityResolutionReason.DUPLICATE)).toBe(
      "dataQuality.resolutionReason.duplicate"
    );
    expect(
      qualityResolutionReasonKey(QualityResolutionReason.AUTO_RECOVERED)
    ).toBe("dataQuality.resolutionReason.autoRecovered");
    expect(
      qualityResolutionReasonKey(QualityResolutionReason.UNSPECIFIED)
    ).toBe("dataQuality.resolutionReason.unspecified");
    expect(qualityResolutionReasonKey(99 as QualityResolutionReason)).toBe(
      "dataQuality.resolutionReason.unspecified"
    );
  });
});

describe("editor vocabulary", () => {
  it("names every threshold metric, empty behaviour and comparison operator", () => {
    expect(qualityThresholdMetricKey(QualityThresholdMetric.COUNT)).toBe(
      "dataQuality.thresholdMetric.count"
    );
    expect(qualityThresholdMetricKey(QualityThresholdMetric.PERCENT)).toBe(
      "dataQuality.thresholdMetric.percent"
    );
    expect(qualityThresholdMetricKey(QualityThresholdMetric.UNSPECIFIED)).toBe(
      "dataQuality.thresholdMetric.unspecified"
    );
    expect(qualityThresholdMetricKey(99 as QualityThresholdMetric)).toBe(
      "dataQuality.thresholdMetric.unspecified"
    );

    expect(qualityEmptyBehaviorKey(QualityEmptyBehavior.PASS)).toBe(
      "dataQuality.emptyBehavior.pass"
    );
    expect(qualityEmptyBehaviorKey(QualityEmptyBehavior.FAIL)).toBe(
      "dataQuality.emptyBehavior.fail"
    );
    expect(qualityEmptyBehaviorKey(QualityEmptyBehavior.NOT_EVALUATED)).toBe(
      "dataQuality.emptyBehavior.notEvaluated"
    );
    expect(qualityEmptyBehaviorKey(QualityEmptyBehavior.UNSPECIFIED)).toBe(
      "dataQuality.emptyBehavior.unspecified"
    );
    expect(qualityEmptyBehaviorKey(99 as QualityEmptyBehavior)).toBe(
      "dataQuality.emptyBehavior.unspecified"
    );

    expect(qualityComparisonOperatorKey(QualityComparisonOperator.EQ)).toBe(
      "dataQuality.comparisonOperator.eq"
    );
    expect(qualityComparisonOperatorKey(QualityComparisonOperator.NE)).toBe(
      "dataQuality.comparisonOperator.ne"
    );
    expect(qualityComparisonOperatorKey(QualityComparisonOperator.LT)).toBe(
      "dataQuality.comparisonOperator.lt"
    );
    expect(qualityComparisonOperatorKey(QualityComparisonOperator.LTE)).toBe(
      "dataQuality.comparisonOperator.lte"
    );
    expect(qualityComparisonOperatorKey(QualityComparisonOperator.GT)).toBe(
      "dataQuality.comparisonOperator.gt"
    );
    expect(qualityComparisonOperatorKey(QualityComparisonOperator.GTE)).toBe(
      "dataQuality.comparisonOperator.gte"
    );
    expect(
      qualityComparisonOperatorKey(QualityComparisonOperator.UNSPECIFIED)
    ).toBe("dataQuality.comparisonOperator.unspecified");
    expect(qualityComparisonOperatorKey(99 as QualityComparisonOperator)).toBe(
      "dataQuality.comparisonOperator.unspecified"
    );
  });

  it("names every filter operator", () => {
    expect(qualityFilterOperatorKey(QualityFilterOperator.EQ)).toBe(
      "dataQuality.filterOperator.eq"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.NE)).toBe(
      "dataQuality.filterOperator.ne"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.LT)).toBe(
      "dataQuality.filterOperator.lt"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.LTE)).toBe(
      "dataQuality.filterOperator.lte"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.GT)).toBe(
      "dataQuality.filterOperator.gt"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.GTE)).toBe(
      "dataQuality.filterOperator.gte"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.IN)).toBe(
      "dataQuality.filterOperator.in"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.NOT_IN)).toBe(
      "dataQuality.filterOperator.notIn"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.IS_NULL)).toBe(
      "dataQuality.filterOperator.isNull"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.IS_NOT_NULL)).toBe(
      "dataQuality.filterOperator.isNotNull"
    );
    expect(qualityFilterOperatorKey(QualityFilterOperator.UNSPECIFIED)).toBe(
      "dataQuality.filterOperator.unspecified"
    );
    expect(qualityFilterOperatorKey(99 as QualityFilterOperator)).toBe(
      "dataQuality.filterOperator.unspecified"
    );
  });
});

// The static i18n checks cannot follow a key a helper returns, so this is what
// keeps the vocabulary and the catalogs from drifting apart: every key the
// display helpers can produce must exist in both locales. Adding an enum member
// without its copy fails here, at the same place the reader would see a raw key.
describe("data quality vocabulary catalogs", () => {
  const CATALOGS: Record<string, unknown> = {
    "en-US": enUS,
    "zh-CN": zhCN,
  };

  function label(catalog: unknown, key: string): unknown {
    return key.split(".").reduce<unknown>((node, part) => {
      if (typeof node !== "object" || node === null) {
        return undefined;
      }
      return (node as Record<string, unknown>)[part];
    }, catalog);
  }

  function enumValues<T extends Record<string, string | number>>(
    values: T
  ): number[] {
    return Object.values(values).filter(
      (value): value is number => typeof value === "number"
    );
  }

  function helperKeys(): string[] {
    const keys: string[] = [];
    for (const status of enumValues(QualityStatus)) {
      keys.push(qualityStatusKey(status));
    }
    for (const severity of enumValues(QualitySeverity)) {
      keys.push(qualitySeverityKey(severity));
    }
    for (const dimension of enumValues(QualityDimension)) {
      keys.push(qualityDimensionKey(dimension));
    }
    for (const ruleType of enumValues(QualityRuleType)) {
      keys.push(qualityRuleTypeKey(ruleType));
    }
    for (const state of enumValues(QualityRuleState)) {
      keys.push(qualityRuleStateKey(state));
    }
    for (const state of enumValues(QualityExecutionState)) {
      keys.push(qualityExecutionStateKey(state));
    }
    for (const trigger of enumValues(QualityExecutionTrigger)) {
      keys.push(qualityExecutionTriggerKey(trigger));
    }
    for (const fn of enumValues(QualityAggregateFunction)) {
      keys.push(qualityAggregateFunctionKey(fn));
    }
    for (const status of enumValues(QualityIssueStatus)) {
      keys.push(qualityIssueStatusKey(status));
    }
    for (const reason of enumValues(QualityResolutionReason)) {
      keys.push(qualityResolutionReasonKey(reason));
    }
    for (const metric of enumValues(QualityThresholdMetric)) {
      keys.push(qualityThresholdMetricKey(metric));
    }
    for (const behavior of enumValues(QualityEmptyBehavior)) {
      keys.push(qualityEmptyBehaviorKey(behavior));
    }
    for (const operator of enumValues(QualityComparisonOperator)) {
      keys.push(qualityComparisonOperatorKey(operator));
    }
    for (const operator of enumValues(QualityFilterOperator)) {
      keys.push(qualityFilterOperatorKey(operator));
    }
    for (const code of [
      "CONNECTION",
      "AUTH",
      "TIMEOUT",
      "PERMISSION",
      "UNSUPPORTED",
      "SQL_SYNTAX",
      "BAD_DATA",
      "LIMIT_EXCEEDED",
      "ORPHANED",
    ]) {
      keys.push(qualityErrorCodeKey(code));
    }
    for (const comparison of [
      QualityComparison.GREATER_THAN,
      QualityComparison.LESS_THAN,
      QualityComparison.OUTSIDE_RANGE,
      QualityComparison.INSIDE_RANGE,
      QualityComparison.UNSPECIFIED,
    ]) {
      const threshold = describeThreshold(
        create(QualityThresholdsSchema, {
          comparison,
          failValue: 1,
          failValueUpper: 2,
        })
      );
      if (threshold) {
        keys.push(threshold.key);
      }
    }
    return keys;
  }

  it("defines every key the helpers can return in both locales", () => {
    const keys = helperKeys();
    expect(keys.length).toBeGreaterThan(0);
    for (const key of keys) {
      for (const [locale, catalog] of Object.entries(CATALOGS)) {
        expect(label(catalog, key), `${locale} is missing ${key}`).toEqual(
          expect.any(String)
        );
      }
    }
  });
});
