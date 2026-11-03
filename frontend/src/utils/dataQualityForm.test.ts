import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import { Engine } from "@/types/proto-es/v1/common_pb";
import {
  AcceptedValuesConfigSchema,
  AcceptedValuesQuerySchema,
  CelExpressionConfigSchema,
  FreshnessConfigSchema,
  QualityAggregateFunction,
  QualityComparison,
  QualityComparisonOperator,
  QualityDimension,
  QualityEmptyBehavior,
  QualityFilterOperator,
  QualityFrequencyTier,
  type QualityRule,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityStandardCode,
  QualityThresholdMetric,
  QualityThresholdSuggestionSchema,
  QualityThresholdsSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { RuleFormState, TargetSelection } from "./dataQualityForm";
import {
  allowedRuleTypes,
  applyThresholdSuggestion,
  buildFilter,
  buildRuleConfig,
  buildTarget,
  canSampleRuleType,
  columnsForConfig,
  computeUpdateMask,
  defaultOnEmptyForNewRule,
  durationToSchedule,
  EDITABLE_RULE_TYPES,
  emptyConfig,
  emptyFilterClause,
  emptyRuleForm,
  filterOperatorNeedsValues,
  formToRule,
  invalidInteger,
  invalidNumber,
  isOnEmptyRelevant,
  isThresholdMetricType,
  isValidCronExpression,
  MAX_ACCEPTED_VALUES,
  MAX_ACCEPTED_VALUES_QUERY_BYTES,
  MAX_CEL_COLUMNS,
  MAX_CEL_EXPRESSION_BYTES,
  MAX_CUSTOM_SQL_BYTES,
  MAX_FILTER_CLAUSES,
  MAX_FILTER_VALUES,
  MAX_NULL_VALUES,
  MAX_SAMPLE_MAX_ROWS,
  MAX_SAMPLE_PERCENT,
  MAX_TIMEOUT_SECONDS,
  MIN_SAMPLE_MAX_ROWS,
  MIN_SAMPLE_PERCENT,
  MIN_TIMEOUT_SECONDS,
  ON_EMPTY_IRRELEVANT_RULE_TYPES,
  parseValueList,
  percentSamplingSupported,
  RULE_TYPE_GROUPS,
  RULE_TYPES_BY_META_TYPE,
  resolveExpectedColumnNullability,
  ruleToForm,
  ruleTypeForConfigCase,
  SAMPLEABLE_RULE_TYPES,
  samplingMethodsFor,
  scheduleMinutes,
  scheduleToDuration,
  suggestedDimension,
  THRESHOLD_METRIC_RULE_TYPES,
  targetFromQuery,
  targetSelectionFromRule,
  trimmed,
  VALIDATION_PREFIX,
  validateRuleForm,
  validateScheduleInterval,
} from "./dataQualityForm";

// The representative values every config arm needs, so a round trip exercises
// the arm rather than only its defaults.
const CONFIG_FIXTURES: Partial<
  Record<QualityRuleType, Record<string, unknown>>
> = {
  [QualityRuleType.ROW_COUNT]: {
    minRows: "1",
    maxRows: "1000",
    exactRows: "500",
    warnMinRows: "2",
    warnMaxRows: "900",
  },
  [QualityRuleType.ROW_COUNT_CHANGE]: {
    maxIncreasePct: "10",
    maxDecreasePct: "20",
    warnIncreasePct: "5",
    warnDecreasePct: "8",
  },
  [QualityRuleType.NULL_PERCENT]: {
    nullMetric: "count",
    nullColumn: "id",
    maxNullCount: "0",
    warnNullCount: "5",
  },
  [QualityRuleType.UNIQUE]: {
    column: "id",
    countNulls: true,
    failThreshold: "0",
    warnThreshold: "5",
  },
  [QualityRuleType.UNIQUE_COMBINATION]: {
    columns: "id, email",
    allColumns: false,
    countNulls: true,
    failThreshold: "1",
    warnThreshold: "3",
  },
  [QualityRuleType.ACCEPTED_VALUES]: {
    column: "status",
    values: "open, closed",
    allowNull: true,
    failThreshold: "0",
    warnThreshold: "2",
  },
  [QualityRuleType.RANGE]: {
    column: "amount",
    minValue: "0",
    maxValue: "100",
    inclusive: true,
    failThreshold: "0",
    warnThreshold: "1",
  },
  [QualityRuleType.REGEX]: {
    column: "email",
    pattern: "^[^@]+@[^@]+$",
    failThreshold: "0",
    warnThreshold: "1",
  },
  [QualityRuleType.STRING_LENGTH]: {
    column: "name",
    minLen: "1",
    maxLen: "255",
    failThreshold: "0",
    warnThreshold: "1",
  },
  [QualityRuleType.REFERENTIAL_INTEGRITY]: {
    childColumn: "customer_id",
    parentTable: "public.customers",
    parentColumn: "id",
    allowNull: true,
    failThreshold: "0",
    warnThreshold: "1",
  },
  [QualityRuleType.CROSS_COLUMN]: {
    leftColumn: "shipped_at",
    rightColumn: "created_at",
    crossOperator: QualityComparisonOperator.GTE,
    tolerance: "0.5",
    failThreshold: "0",
    warnThreshold: "1",
  },
  [QualityRuleType.FRESHNESS]: {
    timestampColumn: "updated_at",
    maxLagSeconds: "86400",
    warnLagSeconds: "3600",
  },
  [QualityRuleType.CUSTOM_SQL]: {
    sql: "SELECT COUNT(*) FROM public.orders",
    failThreshold: "0",
    warnThreshold: "1",
  },
  [QualityRuleType.SCHEMA_DRIFT]: {
    expectedColumns: [
      { name: "id", type: "bigint", nullable: false },
      { name: "email", type: "text", nullable: true },
    ],
    checkTypes: false,
    checkOrder: true,
    allowExtraColumns: true,
    failThreshold: "0",
    warnThreshold: "1",
  },
  [QualityRuleType.GROUP_CHECK]: {
    groupColumns: "region, channel",
    minRowsPerGroup: "1",
    maxRowsPerGroup: "1000",
    failThreshold: "0",
    warnThreshold: "5",
  },
  [QualityRuleType.SEGMENT_CHECK]: {
    segmentColumns: "region, channel",
    segmentBaselineRuns: "5",
    segmentMinOccurrences: "3",
    failThreshold: "2",
    warnThreshold: "1",
  },
  [QualityRuleType.AGGREGATE]: {
    aggregateColumn: "amount",
    aggregateFunction: QualityAggregateFunction.AVG,
    percentile: "",
    aggMin: "0",
    aggMax: "100",
    aggWarnMin: "10",
    aggWarnMax: "90",
    aggInclusive: true,
  },
  [QualityRuleType.CEL_EXPRESSION]: {
    celExpression: "amount > 0.0",
    celColumns: "amount",
    celSampleRows: "5000",
    failThreshold: "0",
    warnThreshold: "1",
  },
  [QualityRuleType.STANDARD_CODE]: {
    column: "credit_code",
    standard: QualityStandardCode.UNIFIED_SOCIAL_CREDIT_CODE,
    allowNull: true,
    sampleRows: "5000",
    failThreshold: "2",
    warnThreshold: "1",
  },
  [QualityRuleType.CROSS_TABLE]: {
    referenceGuid: "inst2;db2;public;orders_ref",
    referenceMetaType: MetaType.TABLE,
    referencePath: {
      instance: "inst2",
      database: "db2",
      schema: "public",
      object: "orders_ref",
    },
    leftAggFunction: QualityAggregateFunction.SUM,
    leftAggColumn: "amount",
    rightAggFunction: QualityAggregateFunction.COUNT,
    rightAggColumn: "",
    rightFilter: [
      { column: "status", op: QualityFilterOperator.EQ, values: "paid" },
    ],
    tolerance: "0.5",
    failThreshold: "10",
    warnThreshold: "5",
  },
};

const CONFIG_CASES: Record<string, QualityRuleType> = {
  rowCount: QualityRuleType.ROW_COUNT,
  rowCountChange: QualityRuleType.ROW_COUNT_CHANGE,
  nullPercent: QualityRuleType.NULL_PERCENT,
  unique: QualityRuleType.UNIQUE,
  uniqueCombination: QualityRuleType.UNIQUE_COMBINATION,
  acceptedValues: QualityRuleType.ACCEPTED_VALUES,
  range: QualityRuleType.RANGE,
  regex: QualityRuleType.REGEX,
  stringLength: QualityRuleType.STRING_LENGTH,
  referentialIntegrity: QualityRuleType.REFERENTIAL_INTEGRITY,
  crossColumn: QualityRuleType.CROSS_COLUMN,
  freshness: QualityRuleType.FRESHNESS,
  customSql: QualityRuleType.CUSTOM_SQL,
  schemaDrift: QualityRuleType.SCHEMA_DRIFT,
  groupCheck: QualityRuleType.GROUP_CHECK,
  segmentCheck: QualityRuleType.SEGMENT_CHECK,
  aggregate: QualityRuleType.AGGREGATE,
  crossTable: QualityRuleType.CROSS_TABLE,
  celExpression: QualityRuleType.CEL_EXPRESSION,
  standardCode: QualityRuleType.STANDARD_CODE,
};

const TARGET: TargetSelection = {
  guid: "inst1;db1;public;orders",
  metaType: MetaType.TABLE,
  path: {
    instance: "inst1",
    database: "db1",
    schema: "public",
    object: "orders",
  },
  columns: ["id"],
};

function validForm(ruleType: QualityRuleType): RuleFormState {
  const form = emptyRuleForm(ruleType);
  form.title = `${ruleType} rule`;
  form.description = "Checks one thing.";
  form.owner = "users/7";
  form.tags = "orders, revenue";
  form.dimension = suggestedDimension(ruleType);
  form.filter = [
    { column: "region", op: QualityFilterOperator.EQ, values: "eu" },
  ];
  form.config = { ...emptyConfig(), ...CONFIG_FIXTURES[ruleType] };
  return form;
}

function ruleFor(ruleType: QualityRuleType): QualityRule {
  return formToRule(validForm(ruleType), { target: TARGET });
}

describe("rule type catalogs", () => {
  it("offers every editable type", () => {
    expect(EDITABLE_RULE_TYPES).toHaveLength(20);
    expect(new Set(EDITABLE_RULE_TYPES).size).toBe(20);
    for (const ruleType of [
      QualityRuleType.CEL_EXPRESSION,
      QualityRuleType.SCHEMA_DRIFT,
      QualityRuleType.GROUP_CHECK,
      QualityRuleType.AGGREGATE,
      QualityRuleType.CROSS_TABLE,
      QualityRuleType.SEGMENT_CHECK,
    ]) {
      expect(EDITABLE_RULE_TYPES).toContain(ruleType);
    }
  });

  it("lists exactly the percent-capable types for the shared threshold metric", () => {
    // Mirrors backend/component/dataquality/validate.go ruleTypeAllowsPercent.
    expect(new Set(THRESHOLD_METRIC_RULE_TYPES)).toEqual(
      new Set([
        QualityRuleType.UNIQUE,
        QualityRuleType.UNIQUE_COMBINATION,
        QualityRuleType.ACCEPTED_VALUES,
        QualityRuleType.RANGE,
        QualityRuleType.REGEX,
        QualityRuleType.STRING_LENGTH,
        QualityRuleType.REFERENTIAL_INTEGRITY,
        QualityRuleType.CROSS_COLUMN,
        QualityRuleType.GROUP_CHECK,
        QualityRuleType.CEL_EXPRESSION,
        QualityRuleType.STANDARD_CODE,
        QualityRuleType.SEGMENT_CHECK,
      ])
    );
    expect(THRESHOLD_METRIC_RULE_TYPES).not.toContain(
      QualityRuleType.CUSTOM_SQL
    );
    for (const ruleType of [
      QualityRuleType.ROW_COUNT,
      QualityRuleType.ROW_COUNT_CHANGE,
      QualityRuleType.NULL_PERCENT,
      QualityRuleType.FRESHNESS,
      QualityRuleType.SCHEMA_DRIFT,
      QualityRuleType.AGGREGATE,
      QualityRuleType.CROSS_TABLE,
      QualityRuleType.CUSTOM_SQL,
    ]) {
      expect(isThresholdMetricType(ruleType), String(ruleType)).toBe(false);
    }
  });

  it("suggests a dimension for every type and groups each type once", () => {
    for (const ruleType of EDITABLE_RULE_TYPES) {
      expect(suggestedDimension(ruleType)).not.toBe(
        QualityDimension.UNSPECIFIED
      );
    }
    expect(suggestedDimension(99 as QualityRuleType)).toBe(
      QualityDimension.UNSPECIFIED
    );
    const grouped = RULE_TYPE_GROUPS.flatMap((group) => group.types);
    expect(grouped).toHaveLength(EDITABLE_RULE_TYPES.length);
    expect(new Set(grouped)).toEqual(new Set(EDITABLE_RULE_TYPES));
    for (const group of RULE_TYPE_GROUPS) {
      expect(group.key.startsWith("dataQuality.ruleTypeGroup.")).toBe(true);
    }
  });

  it("limits the offered types by target MetaType", () => {
    expect(allowedRuleTypes(MetaType.TABLE)).toEqual(EDITABLE_RULE_TYPES);
    expect(allowedRuleTypes(MetaType.EXTERNAL_TABLE)).toEqual(
      EDITABLE_RULE_TYPES
    );
    expect(allowedRuleTypes(undefined)).toEqual(EDITABLE_RULE_TYPES);
    const viewTypes = [
      QualityRuleType.ROW_COUNT,
      QualityRuleType.ROW_COUNT_CHANGE,
      QualityRuleType.FRESHNESS,
      QualityRuleType.CUSTOM_SQL,
      QualityRuleType.SCHEMA_DRIFT,
    ];
    expect(allowedRuleTypes(MetaType.VIEW)).toEqual(viewTypes);
    expect(allowedRuleTypes(MetaType.MATERIALIZED_VIEW)).toEqual(viewTypes);
    expect(RULE_TYPES_BY_META_TYPE[MetaType.VIEW]).toEqual(viewTypes);
    expect(RULE_TYPES_BY_META_TYPE[MetaType.MATERIALIZED_VIEW]).toEqual(
      viewTypes
    );
    for (const ruleType of [
      QualityRuleType.UNIQUE,
      QualityRuleType.GROUP_CHECK,
      QualityRuleType.AGGREGATE,
      QualityRuleType.CROSS_TABLE,
      QualityRuleType.CEL_EXPRESSION,
      QualityRuleType.STANDARD_CODE,
      QualityRuleType.SEGMENT_CHECK,
    ]) {
      expect(allowedRuleTypes(MetaType.VIEW)).not.toContain(ruleType);
    }
  });

  it("keeps on_empty out of the types whose verdict it cannot change", () => {
    expect(ON_EMPTY_IRRELEVANT_RULE_TYPES).toEqual([
      QualityRuleType.ROW_COUNT,
      QualityRuleType.CUSTOM_SQL,
      QualityRuleType.SCHEMA_DRIFT,
    ]);
    expect(isOnEmptyRelevant(QualityRuleType.UNIQUE)).toBe(true);
    for (const ruleType of ON_EMPTY_IRRELEVANT_RULE_TYPES) {
      expect(isOnEmptyRelevant(ruleType), String(ruleType)).toBe(false);
    }
  });

  it("resets the config for a new type", () => {
    const form = emptyRuleForm(QualityRuleType.CUSTOM_SQL);
    form.config.sql = "SELECT 1";
    const reset = emptyRuleForm(QualityRuleType.ROW_COUNT);
    expect(reset.config.sql).toBe("");
    expect(reset.config).toEqual(emptyConfig());
  });
});

describe("schedule conversion", () => {
  it("parses whole minutes from every unit", () => {
    expect(scheduleMinutes({ value: "15", unit: "minutes" })).toBe(15);
    expect(scheduleMinutes({ value: "2", unit: "hours" })).toBe(120);
    expect(scheduleMinutes({ value: "7", unit: "days" })).toBe(10_080);
    expect(scheduleMinutes({ value: "", unit: "minutes" })).toBeUndefined();
    expect(scheduleMinutes({ value: "1.5", unit: "hours" })).toBeUndefined();
    expect(scheduleMinutes({ value: "-1", unit: "hours" })).toBeUndefined();
    expect(scheduleMinutes({ value: "soon", unit: "hours" })).toBeUndefined();
  });

  it("writes a Duration and reads the largest whole unit back", () => {
    expect(scheduleToDuration({ value: "2", unit: "hours" })).toEqual(
      create(DurationSchema, { seconds: 7_200n, nanos: 0 })
    );
    expect(scheduleToDuration({ value: "", unit: "hours" }).seconds).toBe(0n);
    expect(durationToSchedule(undefined)).toEqual({
      value: "",
      unit: "minutes",
    });
    expect(
      durationToSchedule(create(DurationSchema, { seconds: 900n, nanos: 0 }))
    ).toEqual({ value: "15", unit: "minutes" });
    expect(
      durationToSchedule(create(DurationSchema, { seconds: 3_600n, nanos: 0 }))
    ).toEqual({ value: "1", unit: "hours" });
    expect(
      durationToSchedule(
        create(DurationSchema, { seconds: 172_800n, nanos: 0 })
      )
    ).toEqual({ value: "2", unit: "days" });
    expect(
      durationToSchedule(create(DurationSchema, { seconds: 5_400n, nanos: 0 }))
    ).toEqual({ value: "90", unit: "minutes" });
    expect(
      durationToSchedule(create(DurationSchema, { seconds: 45n, nanos: 0 }))
    ).toEqual({ value: "1", unit: "minutes" });
    expect(
      durationToSchedule(create(DurationSchema, { seconds: 0n, nanos: 0 }))
    ).toEqual({ value: "", unit: "minutes" });
  });
});

describe("target selection", () => {
  it("accepts a prefilled GUID and a supported meta type", () => {
    expect(targetFromQuery("inst1;db1;public;orders", "4")).toEqual({
      guid: "inst1;db1;public;orders",
      metaType: MetaType.TABLE,
      path: {
        instance: "inst1",
        database: "db1",
        schema: "public",
        object: "orders",
      },
      columns: [],
    });
    expect(targetFromQuery("i;d;;t", String(MetaType.VIEW))?.metaType).toBe(
      MetaType.VIEW
    );
    expect(
      targetFromQuery("i;d;s;t", String(MetaType.MATERIALIZED_VIEW))?.metaType
    ).toBe(MetaType.MATERIALIZED_VIEW);
    expect(
      targetFromQuery("i;d;s;t", String(MetaType.EXTERNAL_TABLE))?.metaType
    ).toBe(MetaType.EXTERNAL_TABLE);
    // An unknown type falls back to TABLE rather than blocking the form.
    expect(targetFromQuery("i;d;s;t", "999")?.metaType).toBe(MetaType.TABLE);
    expect(targetFromQuery("i;d;s;t", undefined)?.metaType).toBe(
      MetaType.TABLE
    );
  });

  it("ignores a target it cannot address", () => {
    expect(targetFromQuery(undefined, "4")).toBeUndefined();
    expect(targetFromQuery("", "4")).toBeUndefined();
    expect(targetFromQuery("i;d;s", "4")).toBeUndefined();
    expect(targetFromQuery("i;d;s;", "4")).toBeUndefined();
  });

  it("reads a stored target and writes one back", () => {
    const rule = ruleFor(QualityRuleType.UNIQUE);
    expect(targetSelectionFromRule(rule)).toEqual({
      ...TARGET,
      columns: ["id"],
    });
    expect(
      targetSelectionFromRule(create(QualityRuleSchema, {}))
    ).toBeUndefined();

    const target = buildTarget({ ...TARGET, columns: ["id", "id"] });
    expect(target.columns).toEqual(["id"]);
    expect(target.path?.column).toBe("id");
    expect(buildTarget({ ...TARGET, columns: [] }).path?.column).toBe("");
  });

  it("derives the target's column snapshot from the config", () => {
    expect(columnsForConfig(validForm(QualityRuleType.ROW_COUNT))).toEqual([]);
    expect(columnsForConfig(validForm(QualityRuleType.NULL_PERCENT))).toEqual([
      "id",
    ]);
    expect(
      columnsForConfig(validForm(QualityRuleType.UNIQUE_COMBINATION))
    ).toEqual(["id", "email"]);
    const allColumns = validForm(QualityRuleType.UNIQUE_COMBINATION);
    allColumns.config.allColumns = true;
    expect(columnsForConfig(allColumns)).toEqual([]);
    expect(columnsForConfig(validForm(QualityRuleType.CROSS_COLUMN))).toEqual([
      "shipped_at",
      "created_at",
    ]);
    expect(columnsForConfig(validForm(QualityRuleType.FRESHNESS))).toEqual([
      "updated_at",
    ]);
    expect(columnsForConfig(validForm(QualityRuleType.CUSTOM_SQL))).toEqual([]);
    expect(columnsForConfig(validForm(QualityRuleType.SCHEMA_DRIFT))).toEqual([
      "id",
      "email",
    ]);
    expect(columnsForConfig(validForm(QualityRuleType.GROUP_CHECK))).toEqual([
      "region",
      "channel",
    ]);
    expect(columnsForConfig(validForm(QualityRuleType.AGGREGATE))).toEqual([
      "amount",
    ]);
    expect(columnsForConfig(validForm(QualityRuleType.CROSS_TABLE))).toEqual([
      "amount",
    ]);
    expect(columnsForConfig(validForm(QualityRuleType.CEL_EXPRESSION))).toEqual(
      ["amount"]
    );
    expect(columnsForConfig(validForm(QualityRuleType.STANDARD_CODE))).toEqual([
      "credit_code",
    ]);
    expect(columnsForConfig(validForm(QualityRuleType.SEGMENT_CHECK))).toEqual([
      "region",
      "channel",
    ]);
    // A field source reads the reference column as well; a query's value column
    // is the query's own result, not a column of the target.
    const fieldSource = validForm(QualityRuleType.ACCEPTED_VALUES);
    fieldSource.config.acceptedSource = "field";
    fieldSource.config.acceptedField = "status_code";
    expect(columnsForConfig(fieldSource)).toEqual(["status", "status_code"]);
    const querySource = validForm(QualityRuleType.ACCEPTED_VALUES);
    querySource.config.acceptedSource = "query";
    querySource.config.acceptedQuery = "SELECT code FROM codes";
    querySource.config.acceptedQueryColumn = "code";
    expect(columnsForConfig(querySource)).toEqual(["status"]);
  });
});

describe("config conversion", () => {
  it("builds the oneof arm that matches the rule type", () => {
    for (const ruleType of EDITABLE_RULE_TYPES) {
      const config = buildRuleConfig(validForm(ruleType));
      const expectedCase = Object.entries(CONFIG_CASES).find(
        ([, type]) => type === ruleType
      )?.[0];
      expect(config.config.case, String(ruleType)).toBe(expectedCase);
      expect(config.thresholdMetric).toBe(QualityThresholdMetric.COUNT);
      expect(config.onEmpty).toBe(QualityEmptyBehavior.NOT_EVALUATED);
    }
  });

  it("round-trips every config arm through the form", () => {
    for (const ruleType of EDITABLE_RULE_TYPES) {
      const form = validForm(ruleType);
      const rule = formToRule(form, { target: TARGET });
      const back = ruleToForm(rule);
      expect(back.ruleType, String(ruleType)).toBe(ruleType);
      expect(back.config, String(ruleType)).toEqual(form.config);
      expect(back.title).toBe(form.title);
      expect(back.description).toBe(form.description);
      expect(back.owner).toBe("users/7");
      expect(back.tags).toBe("orders, revenue");
      expect(back.filter).toEqual(form.filter);
      expect(rule.target?.columns).toEqual(columnsForConfig(form));
    }
  });

  it("sends and reads the cross-table failure and warning thresholds", () => {
    const form = validForm(QualityRuleType.CROSS_TABLE);
    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "crossTable") {
      throw new Error("expected a cross-table config arm");
    }
    expect(rule.config.config.value.failThreshold).toBe(10);
    expect(rule.config.config.value.warnThreshold).toBe(5);
    const back = ruleToForm(rule);
    expect(back.config.failThreshold).toBe("10");
    expect(back.config.warnThreshold).toBe("5");
  });

  it("sends and reads the cross-table reference relation and its filter", () => {
    const form = validForm(QualityRuleType.CROSS_TABLE);
    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "crossTable") {
      throw new Error("expected a cross-table config arm");
    }
    const cross = rule.config.config.value;
    expect(cross.reference?.metaGuid).toBe("inst2;db2;public;orders_ref");
    expect(cross.reference?.metaType).toBe(MetaType.TABLE);
    expect(cross.reference?.path).toMatchObject({
      instance: "inst2",
      database: "db2",
      schema: "public",
      object: "orders_ref",
    });
    expect(cross.rightFilter?.clauses).toMatchObject([
      { column: "status", op: QualityFilterOperator.EQ, values: ["paid"] },
    ]);

    const back = ruleToForm(rule);
    expect(back.config.referenceGuid).toBe("inst2;db2;public;orders_ref");
    expect(back.config.referenceMetaType).toBe(MetaType.TABLE);
    expect(back.config.referencePath).toEqual({
      instance: "inst2",
      database: "db2",
      schema: "public",
      object: "orders_ref",
    });
    expect(back.config.rightFilter).toEqual(form.config.rightFilter);
  });

  it("omits an unpicked reference and an empty reference filter", () => {
    const form = validForm(QualityRuleType.CROSS_TABLE);
    form.config = {
      ...form.config,
      referenceGuid: "",
      rightFilter: [],
    };
    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "crossTable") {
      throw new Error("expected a cross-table config arm");
    }
    expect(rule.config.config.value.reference).toBeUndefined();
    expect(rule.config.config.value.rightFilter).toBeUndefined();
  });

  it("keeps an empty config usable", () => {
    for (const ruleType of EDITABLE_RULE_TYPES) {
      const rule = formToRule(emptyRuleForm(ruleType));
      const back = ruleToForm(rule);
      expect(back.config, String(ruleType)).toEqual(emptyConfig());
    }
  });

  it("maps every config oneof case back to its rule type", () => {
    for (const [configCase, ruleType] of Object.entries(CONFIG_CASES)) {
      expect(ruleTypeForConfigCase(configCase), configCase).toBe(ruleType);
    }
    expect(ruleTypeForConfigCase("nope")).toBeUndefined();
  });

  it("builds the filter with values only where the operator takes them", () => {
    const filter = buildFilter([
      { column: " region ", op: QualityFilterOperator.IN, values: "eu, us" },
      { column: "deleted_at", op: QualityFilterOperator.IS_NULL, values: "x" },
      { column: "  ", op: QualityFilterOperator.EQ, values: "ignored" },
    ]);
    expect(filter.clauses).toHaveLength(2);
    expect(filter.clauses[0]).toMatchObject({
      column: "region",
      op: QualityFilterOperator.IN,
      values: ["eu", "us"],
    });
    expect(filter.clauses[1]?.values).toEqual([]);
    expect(filterOperatorNeedsValues(QualityFilterOperator.IS_NOT_NULL)).toBe(
      false
    );
    expect(filterOperatorNeedsValues(QualityFilterOperator.EQ)).toBe(true);
    expect(emptyFilterClause()).toEqual({
      column: "",
      op: QualityFilterOperator.EQ,
      values: "",
    });
  });

  it("does not attach an empty filter to the config", () => {
    const form = validForm(QualityRuleType.REGEX);
    form.filter = [];
    expect(buildRuleConfig(form).filter).toBeUndefined();
  });

  it("defaults the freshness lag to blank while reading a rule without one", () => {
    const rule = create(QualityRuleSchema, {
      ruleType: QualityRuleType.FRESHNESS,
      config: create(QualityRuleConfigSchema, {
        config: {
          case: "freshness",
          value: create(FreshnessConfigSchema, {
            timestampColumn: "updated_at",
            maxLagSeconds: 0,
          }),
        },
      }),
    });
    expect(ruleToForm(rule).config.maxLagSeconds).toBe("");
  });
});

describe("accepted-values sources", () => {
  function acceptedForm(): RuleFormState {
    return validForm(QualityRuleType.ACCEPTED_VALUES);
  }

  it("sends exactly one source and reads the same one back", () => {
    // The default is the fixed list.
    const list = acceptedForm();
    let rule = formToRule(list, { target: TARGET });
    if (rule.config?.config.case !== "acceptedValues") {
      throw new Error("expected an accepted-values config arm");
    }
    expect(rule.config.config.value.values).toEqual(["open", "closed"]);
    expect(rule.config.config.value.field).toBe("");
    expect(rule.config.config.value.query).toBeUndefined();
    expect(ruleToForm(rule).config.acceptedSource).toBe("list");

    // A reference column carries no values and no query.
    const field = acceptedForm();
    field.config.acceptedSource = "field";
    field.config.acceptedField = "status_code";
    rule = formToRule(field, { target: TARGET });
    if (rule.config?.config.case !== "acceptedValues") {
      throw new Error("expected an accepted-values config arm");
    }
    expect(rule.config.config.value.values).toEqual([]);
    expect(rule.config.config.value.field).toBe("status_code");
    expect(rule.config.config.value.query).toBeUndefined();
    expect(ruleToForm(rule).config.acceptedField).toBe("status_code");

    // A query carries its statement and the column it returns.
    const query = acceptedForm();
    query.config.acceptedSource = "query";
    query.config.acceptedQuery = "  SELECT DISTINCT code FROM public.codes  ";
    query.config.acceptedQueryColumn = " code ";
    rule = formToRule(query, { target: TARGET });
    if (rule.config?.config.case !== "acceptedValues") {
      throw new Error("expected an accepted-values config arm");
    }
    expect(rule.config.config.value.values).toEqual([]);
    expect(rule.config.config.value.field).toBe("");
    expect(rule.config.config.value.query?.sql).toBe(
      "SELECT DISTINCT code FROM public.codes"
    );
    expect(rule.config.config.value.query?.valueColumn).toBe("code");
    const back = ruleToForm(rule);
    expect(back.config.acceptedSource).toBe("query");
    expect(back.config.acceptedQuery).toBe(
      "SELECT DISTINCT code FROM public.codes"
    );
    expect(back.config.acceptedQueryColumn).toBe("code");
  });

  it("validates the selected source only", () => {
    // A field source does not need the fixed list, and vice versa.
    const field = acceptedForm();
    field.config.acceptedSource = "field";
    field.config.acceptedField = "status_code";
    field.config.values = "";
    expect(validateRuleForm(field, TARGET)).toEqual([]);

    const query = acceptedForm();
    query.config.acceptedSource = "query";
    query.config.acceptedQuery = "SELECT code FROM public.codes";
    query.config.acceptedQueryColumn = "code";
    query.config.values = "";
    expect(validateRuleForm(query, TARGET)).toEqual([]);

    // The bound is inclusive: exactly 8 KiB is allowed, one byte more is not.
    const atLimit = acceptedForm();
    atLimit.config.acceptedSource = "query";
    atLimit.config.acceptedQuery = "a".repeat(MAX_ACCEPTED_VALUES_QUERY_BYTES);
    atLimit.config.acceptedQueryColumn = "code";
    expect(validateRuleForm(atLimit, TARGET)).toEqual([]);

    const tooLong = acceptedForm();
    tooLong.config.acceptedSource = "query";
    tooLong.config.acceptedQuery = "a".repeat(
      MAX_ACCEPTED_VALUES_QUERY_BYTES + 1
    );
    tooLong.config.acceptedQueryColumn = "code";
    const issue = validateRuleForm(tooLong, TARGET).find(
      (candidate) =>
        candidate.key === `${VALIDATION_PREFIX}acceptedQueryTooLong`
    );
    expect(issue?.params).toEqual({ max: MAX_ACCEPTED_VALUES_QUERY_BYTES });
  });

  it("rebuilds a stored dynamic source unchanged", () => {
    // A rule written through the API carries the source on the wire; reading it
    // and building again must keep the same arm and the same source fields.
    const storedField = create(QualityRuleSchema, {
      ruleType: QualityRuleType.ACCEPTED_VALUES,
      config: create(QualityRuleConfigSchema, {
        config: {
          case: "acceptedValues",
          value: create(AcceptedValuesConfigSchema, {
            column: "status",
            field: "status_code",
            allowNull: true,
          }),
        },
      }),
    });
    const fieldForm = ruleToForm(storedField);
    expect(fieldForm.config.acceptedSource).toBe("field");
    expect(fieldForm.config.values).toBe("");
    const rebuiltField = buildRuleConfig(fieldForm);
    if (rebuiltField.config?.case !== "acceptedValues") {
      throw new Error("expected an accepted-values config arm");
    }
    expect(rebuiltField.config.value.column).toBe("status");
    expect(rebuiltField.config.value.values).toEqual([]);
    expect(rebuiltField.config.value.field).toBe("status_code");
    expect(rebuiltField.config.value.query).toBeUndefined();
    expect(rebuiltField.config.value.allowNull).toBe(true);

    const storedQuery = create(QualityRuleSchema, {
      ruleType: QualityRuleType.ACCEPTED_VALUES,
      config: create(QualityRuleConfigSchema, {
        config: {
          case: "acceptedValues",
          value: create(AcceptedValuesConfigSchema, {
            column: "status",
            query: create(AcceptedValuesQuerySchema, {
              sql: "SELECT code FROM public.codes",
              valueColumn: "code",
            }),
          }),
        },
      }),
    });
    const queryForm = ruleToForm(storedQuery);
    expect(queryForm.config.acceptedSource).toBe("query");
    const rebuiltQuery = buildRuleConfig(queryForm);
    if (rebuiltQuery.config?.case !== "acceptedValues") {
      throw new Error("expected an accepted-values config arm");
    }
    expect(rebuiltQuery.config.value.field).toBe("");
    expect(rebuiltQuery.config.value.values).toEqual([]);
    expect(rebuiltQuery.config.value.query?.sql).toBe(
      "SELECT code FROM public.codes"
    );
    expect(rebuiltQuery.config.value.query?.valueColumn).toBe("code");
  });
});

describe("segment check", () => {
  it("sends the grouping columns and the baseline window", () => {
    const form = validForm(QualityRuleType.SEGMENT_CHECK);
    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "segmentCheck") {
      throw new Error("expected a segment-check config arm");
    }
    const config = rule.config.config.value;
    expect(config.columns).toEqual(["region", "channel"]);
    expect(config.baselineRuns).toBe(5);
    expect(config.minOccurrences).toBe(3);
    expect(config.failThreshold).toBe(2);
    expect(config.warnThreshold).toBe(1);
    // The target snapshot records every grouping column.
    expect(rule.target?.columns).toEqual(["region", "channel"]);

    const back = ruleToForm(rule);
    expect(back.config.segmentColumns).toBe("region, channel");
    expect(back.config.segmentBaselineRuns).toBe("5");
    expect(back.config.segmentMinOccurrences).toBe("3");
    expect(back.thresholdMetric).toBe(QualityThresholdMetric.COUNT);
  });

  it("accepts PERCENT and blanks for the two numeric defaults", () => {
    const form = validForm(QualityRuleType.SEGMENT_CHECK);
    form.thresholdMetric = QualityThresholdMetric.PERCENT;
    form.config.segmentBaselineRuns = "";
    form.config.segmentMinOccurrences = "";
    expect(validateRuleForm(form, TARGET)).toEqual([]);

    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "segmentCheck") {
      throw new Error("expected a segment-check config arm");
    }
    // 0 asks the server for its own defaults (3 and 2).
    expect(rule.config.config.value.baselineRuns).toBe(0);
    expect(rule.config.config.value.minOccurrences).toBe(0);
    expect(ruleToForm(rule).config.segmentBaselineRuns).toBe("");
  });

  it("treats a typed zero as the server's default window", () => {
    // 0 on the wire means "use the default", so the form's own check must not
    // call the value the server would accept invalid.
    const form = validForm(QualityRuleType.SEGMENT_CHECK);
    form.config.segmentBaselineRuns = "0";
    form.config.segmentMinOccurrences = "0";
    expect(validateRuleForm(form, TARGET)).toEqual([]);

    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "segmentCheck") {
      throw new Error("expected a segment-check config arm");
    }
    expect(rule.config.config.value.baselineRuns).toBe(0);
    expect(rule.config.config.value.minOccurrences).toBe(0);
  });

  it("refuses more grouping columns than the server accepts", () => {
    const form = validForm(QualityRuleType.SEGMENT_CHECK);
    form.config.segmentColumns = "a, b, c, d, e, f";
    expect(validateRuleForm(form, TARGET).map((issue) => issue.key)).toContain(
      `${VALIDATION_PREFIX}segmentColumnsTooMany`
    );
  });

  it("cannot be sampled", () => {
    expect(canSampleRuleType(QualityRuleType.SEGMENT_CHECK)).toBe(false);
  });
});

describe("standard code checks", () => {
  it("sends the standard, sample bound, null policy and thresholds", () => {
    const form = validForm(QualityRuleType.STANDARD_CODE);
    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "standardCode") {
      throw new Error("expected a standard-code config arm");
    }
    const config = rule.config.config.value;
    expect(config.column).toBe("credit_code");
    expect(config.standard).toBe(
      QualityStandardCode.UNIFIED_SOCIAL_CREDIT_CODE
    );
    expect(config.allowNull).toBe(true);
    expect(config.sampleRows).toBe(5000);
    expect(config.failThreshold).toBe(2);
    expect(config.warnThreshold).toBe(1);
    // The rule is filed under the column it validates.
    expect(rule.target?.columns).toEqual(["credit_code"]);
    expect(rule.target?.path?.column).toBe("credit_code");

    const back = ruleToForm(rule);
    expect(back.config.standard).toBe(
      QualityStandardCode.UNIFIED_SOCIAL_CREDIT_CODE
    );
    expect(back.config.sampleRows).toBe("5000");
  });

  it("starts an unset standard at the server's default sample size", () => {
    const form = validForm(QualityRuleType.STANDARD_CODE);
    form.config.standard = QualityStandardCode.UNSPECIFIED;
    form.config.sampleRows = "";
    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "standardCode") {
      throw new Error("expected a standard-code config arm");
    }
    // 0 asks the server for its own default (10000).
    expect(rule.config.config.value.sampleRows).toBe(0);
    expect(ruleToForm(rule).config.sampleRows).toBe("0");
  });

  it("requires a column and a standard", () => {
    const form = validForm(QualityRuleType.STANDARD_CODE);
    form.config.column = " ";
    form.config.standard = QualityStandardCode.UNSPECIFIED;
    const keys = validateRuleForm(form, TARGET).map((issue) => issue.key);
    expect(keys).toContain(`${VALIDATION_PREFIX}columnRequired`);
    expect(keys).toContain(`${VALIDATION_PREFIX}standardRequired`);
  });

  it("sends and re-reads the workspace table a custom standard validates against", () => {
    const form = validForm(QualityRuleType.STANDARD_CODE);
    form.config.standard = QualityStandardCode.CUSTOM_CODE_TABLE;
    form.config.codeTable = "workspaces/-/qualityCodeTables/municipal";
    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "standardCode") {
      throw new Error("expected a standard-code config arm");
    }
    expect(rule.config.config.value.codeTable).toBe(
      "workspaces/-/qualityCodeTables/municipal"
    );
    expect(ruleToForm(rule).config.codeTable).toBe(
      "workspaces/-/qualityCodeTables/municipal"
    );
  });

  it("requires a workspace table only for a custom standard", () => {
    const form = validForm(QualityRuleType.STANDARD_CODE);
    form.config.standard = QualityStandardCode.CUSTOM_CODE_TABLE;
    form.config.codeTable = "";
    expect(validateRuleForm(form, TARGET).map((issue) => issue.key)).toContain(
      `${VALIDATION_PREFIX}codeTableRequired`
    );

    // A built-in standard brings its own table; a workspace one only narrows it.
    form.config.standard = QualityStandardCode.ORGANIZATION_CODE;
    expect(validateRuleForm(form, TARGET)).toEqual([]);

    form.config.standard = QualityStandardCode.CUSTOM_CODE_TABLE;
    form.config.codeTable = "workspaces/-/qualityCodeTables/municipal";
    expect(validateRuleForm(form, TARGET)).toEqual([]);
  });

  it("accepts the server's sample row range and refuses the rest", () => {
    const form = validForm(QualityRuleType.STANDARD_CODE);
    for (const sampleRows of ["0", "1", "100000", ""]) {
      form.config.sampleRows = sampleRows;
      expect(validateRuleForm(form, TARGET), sampleRows).toEqual([]);
    }
    for (const sampleRows of ["-1", "100001", "12.5", "many"]) {
      form.config.sampleRows = sampleRows;
      expect(
        validateRuleForm(form, TARGET).map((issue) => issue.key),
        sampleRows
      ).toContain(`${VALIDATION_PREFIX}sampleRowsInvalid`);
    }
  });

  it("refuses a warning threshold above the failure threshold", () => {
    const form = validForm(QualityRuleType.STANDARD_CODE);
    form.config.failThreshold = "2";
    form.config.warnThreshold = "2";
    expect(validateRuleForm(form, TARGET)).toEqual([]);
    form.config.warnThreshold = "3";
    expect(validateRuleForm(form, TARGET).map((issue) => issue.key)).toContain(
      `${VALIDATION_PREFIX}thresholdOrder`
    );
  });

  it("reads a suggested threshold pair into the form", () => {
    const form = validForm(QualityRuleType.STANDARD_CODE);
    const next = applyThresholdSuggestion(
      form,
      create(QualityThresholdSuggestionSchema, {
        thresholds: create(QualityThresholdsSchema, {
          failValue: 0.2,
          warnValue: 0.1,
        }),
      })
    );
    expect(next.config.failThreshold).toBe("0.2");
    expect(next.config.warnThreshold).toBe("0.1");
  });
});

describe("schema drift nullability", () => {
  it("keeps an unset nullability bit unset through a round trip", () => {
    const form = validForm(QualityRuleType.SCHEMA_DRIFT);
    form.config.expectedColumns = [
      { name: "id", type: "bigint" },
      { name: "email", type: "text", nullable: true },
    ];
    const rule = formToRule(form, { target: TARGET });
    if (rule.config?.config.case !== "schemaDrift") {
      throw new Error("expected a schema-drift config arm");
    }
    const sent = rule.config.config.value.expectedColumns;
    expect(sent[0]?.nullable).toBeUndefined();
    expect(sent[1]?.nullable).toBe(true);

    const back = ruleToForm(rule);
    expect(back.config.expectedColumns?.[0]?.nullable).toBeUndefined();
    expect(back.config.expectedColumns?.[1]?.nullable).toBe(true);
  });

  it("resolves unset rows from the target and leaves unknown columns alone", () => {
    const targetColumns = [
      { name: "id", type: "bigint", nullable: false },
      { name: "email", type: "text", nullable: true },
    ];
    const resolved = resolveExpectedColumnNullability(
      [
        { name: "id", type: "bigint" },
        { name: "email", type: "text", nullable: false },
        { name: "gone", type: "text" },
      ],
      targetColumns
    );
    expect(resolved?.[0]?.nullable).toBe(false);
    // An explicit snapshot bit wins over the live column.
    expect(resolved?.[1]?.nullable).toBe(false);
    // A column the target no longer has stays unjudged.
    expect(resolved?.[2]?.nullable).toBeUndefined();
    expect(
      resolveExpectedColumnNullability(undefined, targetColumns)
    ).toBeUndefined();
  });
});

describe("target schedule helpers", () => {
  it("validates the interval bounds the server accepts", () => {
    expect(validateScheduleInterval({ value: "14", unit: "minutes" })).toEqual([
      { key: `${VALIDATION_PREFIX}scheduleInvalid` },
    ]);
    expect(validateScheduleInterval({ value: "15", unit: "minutes" })).toEqual(
      []
    );
    expect(validateScheduleInterval({ value: "7", unit: "days" })).toEqual([]);
    expect(validateScheduleInterval({ value: "8", unit: "days" })).toHaveLength(
      1
    );
  });

  it("accepts only a well-shaped five-field cron expression", () => {
    expect(isValidCronExpression("0 2 * * *")).toBe(true);
    expect(isValidCronExpression("*/30 9-17 * * 1-5")).toBe(true);
    expect(isValidCronExpression("0 2 * * MON")).toBe(true);
    // The shape check is deliberately shallow: the server parses the expression.
    expect(isValidCronExpression("  0 2 * * *  ")).toBe(true);
    expect(isValidCronExpression("0 2 * *")).toBe(false);
    expect(isValidCronExpression("0 2 * * * *")).toBe(false);
    expect(isValidCronExpression("0 2 * * @daily")).toBe(false);
    expect(isValidCronExpression("0 2 * * 1; DROP TABLE orders")).toBe(false);
    expect(isValidCronExpression("")).toBe(false);
  });
});

describe("run cadence", () => {
  it("starts a new rule on the target's own schedule", () => {
    expect(emptyRuleForm().frequencyTier).toBe(
      QualityFrequencyTier.UNSPECIFIED
    );
  });

  it("round-trips the tier through the rule", () => {
    const form = validForm(QualityRuleType.ROW_COUNT);
    form.frequencyTier = QualityFrequencyTier.DAILY;
    const rule = formToRule(form, { target: TARGET });
    expect(rule.frequencyTier).toBe(QualityFrequencyTier.DAILY);
    expect(ruleToForm(rule).frequencyTier).toBe(QualityFrequencyTier.DAILY);
  });
});

describe("update mask", () => {
  function snapshot(): {
    form: RuleFormState;
    target: TargetSelection | undefined;
  } {
    return { form: validForm(QualityRuleType.UNIQUE), target: { ...TARGET } };
  }

  function changed(mutate: (form: RuleFormState) => void) {
    const next = snapshot();
    mutate(next.form);
    return computeUpdateMask(snapshot(), next);
  }

  it("is empty when nothing changed", () => {
    expect(computeUpdateMask(snapshot(), snapshot())).toEqual([]);
  });

  it("names each scalar field that changed", () => {
    expect(
      changed((form) => {
        form.title = "another";
      })
    ).toEqual(["title"]);
    expect(
      changed((form) => {
        form.description = "more";
      })
    ).toEqual(["description"]);
    expect(
      changed((form) => {
        form.dimension = QualityDimension.VALIDITY;
      })
    ).toEqual(["dimension"]);
    expect(
      changed((form) => {
        form.ruleType = QualityRuleType.REGEX;
      })
    ).toEqual(["rule_type", "config"]);
    expect(
      changed((form) => {
        form.severity = QualitySeverity.CRITICAL;
      })
    ).toEqual(["severity"]);
    expect(
      changed((form) => {
        form.state = QualityRuleState.PAUSED;
      })
    ).toEqual(["state"]);
    expect(
      changed((form) => {
        form.owner = "users/9";
      })
    ).toEqual(["owner"]);
    expect(
      changed((form) => {
        form.tags = "only";
      })
    ).toEqual(["tags"]);
  });

  it("notices a standard-only change in the update mask", () => {
    // The stored target snapshot records the columns the config reads, so it has
    // to carry the same column for the target itself not to count as changed.
    const target: TargetSelection = { ...TARGET, columns: ["credit_code"] };
    const base = { form: validForm(QualityRuleType.STANDARD_CODE), target };
    const next = {
      form: {
        ...base.form,
        config: {
          ...base.form.config,
          standard: QualityStandardCode.ORGANIZATION_CODE,
        },
      },
      target,
    };
    expect(computeUpdateMask(base, next)).toEqual(["config"]);
  });

  it("treats a reference or reference-filter edit as a config change", () => {
    const base = {
      form: validForm(QualityRuleType.CROSS_TABLE),
      target: { ...TARGET, columns: ["amount"] },
    };
    const next = {
      form: {
        ...base.form,
        config: {
          ...base.form.config,
          referenceGuid: "inst3;db3;public;other",
        },
      },
      target: base.target,
    };
    expect(computeUpdateMask(base, next)).toEqual(["config"]);

    const refiltered = {
      form: {
        ...base.form,
        config: {
          ...base.form.config,
          rightFilter: [
            {
              column: "status",
              op: QualityFilterOperator.EQ,
              values: "refunded",
            },
          ],
        },
      },
      target: base.target,
    };
    expect(computeUpdateMask(base, refiltered)).toEqual(["config"]);

    // The stored path is display metadata: a relation that moved does not
    // change the rule's reference identity.
    const moved = {
      form: {
        ...base.form,
        config: {
          ...base.form.config,
          referencePath: {
            instance: "inst2",
            database: "db2",
            schema: "public",
            object: "renamed",
          },
        },
      },
      target: base.target,
    };
    expect(computeUpdateMask(base, moved)).toEqual([]);
  });

  it("treats a config, filter, metric or empty-behaviour edit as a config change", () => {
    // The target snapshot records the columns the config reads, so changing the
    // column changes both the config and the target's column list.
    expect(
      changed((form) => {
        form.config.column = "email";
      })
    ).toEqual(["target", "config"]);
    expect(
      changed((form) => {
        form.config.failThreshold = "5";
      })
    ).toEqual(["config"]);
    expect(
      changed((form) => {
        form.thresholdMetric = QualityThresholdMetric.PERCENT;
      })
    ).toEqual(["config"]);
    expect(
      changed((form) => {
        form.onEmpty = QualityEmptyBehavior.PASS;
      })
    ).toEqual(["config"]);
    expect(
      changed((form) => {
        form.filter = [
          { column: "region", op: QualityFilterOperator.EQ, values: "us" },
        ];
      })
    ).toEqual(["config"]);
  });

  // The cadence is scheduling metadata: it travels under its own path and never
  // as a config change, so the server does not append a rule revision for it.
  it("names a changed run cadence as frequency_tier alone", () => {
    expect(
      changed((form) => {
        form.frequencyTier = QualityFrequencyTier.WEEKLY;
      })
    ).toEqual(["frequency_tier"]);
  });

  it("treats an edited schema-drift snapshot as a config change", () => {
    const base = {
      form: validForm(QualityRuleType.SCHEMA_DRIFT),
      target: { ...TARGET, columns: ["id", "email"] },
    };
    const next = {
      form: validForm(QualityRuleType.SCHEMA_DRIFT),
      target: { ...TARGET, columns: ["id", "email"] },
    };
    expect(computeUpdateMask(base, next)).toEqual([]);
    next.form.config.expectedColumns = [
      { name: "id", type: "varchar", nullable: false },
      { name: "email", type: "text", nullable: true },
    ];
    expect(computeUpdateMask(base, next)).toEqual(["config"]);
  });

  // Unset means "do not judge nullability"; an explicit false asserts NOT NULL.
  // Toggling a legacy row must therefore be a change, not a no-op.
  it("treats an unset nullability bit as different from an explicit false", () => {
    const snapshot = (form: RuleFormState) => ({
      form,
      target: { ...TARGET, columns: ["id"] },
    });
    const base = validForm(QualityRuleType.SCHEMA_DRIFT);
    base.config.expectedColumns = [{ name: "id", type: "bigint" }];
    const toggled = validForm(QualityRuleType.SCHEMA_DRIFT);
    toggled.config.expectedColumns = [
      { name: "id", type: "bigint", nullable: false },
    ];
    expect(computeUpdateMask(snapshot(base), snapshot(base))).toEqual([]);
    expect(computeUpdateMask(snapshot(base), snapshot(toggled))).toEqual([
      "config",
    ]);
  });

  it("ignores whitespace and formatting-only differences", () => {
    const next = snapshot();
    next.form.title = `  ${next.form.title}  `;
    next.form.description = ` ${next.form.description} `;
    next.form.tags = " orders ,  revenue ";
    next.form.config.column = " id ";
    expect(computeUpdateMask(snapshot(), next)).toEqual([]);
  });

  it("treats a changed target or its column snapshot as a target change", () => {
    const moved = snapshot();
    moved.target = { ...TARGET, guid: "inst1;db1;public;customers" };
    expect(computeUpdateMask(snapshot(), moved)).toEqual(["target"]);

    const columns = snapshot();
    const form = validForm(QualityRuleType.UNIQUE);
    form.config.column = "email";
    columns.form = form;
    expect(computeUpdateMask(snapshot(), columns)).toEqual([
      "target",
      "config",
    ]);

    const removed = snapshot();
    removed.target = undefined;
    expect(computeUpdateMask(snapshot(), removed)).toEqual(["target"]);
    const none = snapshot();
    none.target = undefined;
    expect(computeUpdateMask(none, removed)).toEqual([]);
  });
});

describe("validation", () => {
  it("accepts a complete form for every rule type", () => {
    for (const ruleType of EDITABLE_RULE_TYPES) {
      expect(
        validateRuleForm(validForm(ruleType), TARGET),
        String(ruleType)
      ).toEqual([]);
    }
  });

  it("requires a title and a target", () => {
    const form = validForm(QualityRuleType.ROW_COUNT);
    form.title = "  ";
    const issues = validateRuleForm(form, undefined).map((issue) => issue.key);
    expect(issues).toContain(`${VALIDATION_PREFIX}titleRequired`);
    expect(issues).toContain(`${VALIDATION_PREFIX}targetRequired`);
  });

  it("requires each rule type's own parameters", () => {
    const cases: Array<
      [QualityRuleType, (form: RuleFormState) => void, string]
    > = [
      [
        QualityRuleType.ROW_COUNT,
        (form) => {
          form.config = {
            ...form.config,
            minRows: "",
            maxRows: "",
            exactRows: "",
          };
        },
        "rowCountBoundRequired",
      ],
      [
        QualityRuleType.ROW_COUNT_CHANGE,
        (form) => {
          form.config = {
            ...form.config,
            maxIncreasePct: "",
            maxDecreasePct: "",
            warnIncreasePct: "",
            warnDecreasePct: "",
          };
        },
        "rowCountChangeBoundRequired",
      ],
      [
        QualityRuleType.NULL_PERCENT,
        (form) => {
          form.config = { ...form.config, nullColumn: "" };
        },
        "columnRequired",
      ],
      [
        QualityRuleType.NULL_PERCENT,
        (form) => {
          form.config = { ...form.config, maxNullCount: "" };
        },
        "nullPercentThresholdRequired",
      ],
      [
        QualityRuleType.UNIQUE,
        (form) => {
          form.config = { ...form.config, column: "" };
        },
        "columnRequired",
      ],
      [
        QualityRuleType.UNIQUE_COMBINATION,
        (form) => {
          form.config = { ...form.config, columns: "" };
        },
        "columnsRequired",
      ],
      [
        QualityRuleType.ACCEPTED_VALUES,
        (form) => {
          form.config = { ...form.config, values: "" };
        },
        "valuesRequired",
      ],
      [
        QualityRuleType.ACCEPTED_VALUES,
        (form) => {
          form.config = {
            ...form.config,
            column: "",
          };
        },
        "columnRequired",
      ],
      [
        QualityRuleType.RANGE,
        (form) => {
          form.config = { ...form.config, minValue: "", maxValue: "" };
        },
        "rangeBoundRequired",
      ],
      [
        QualityRuleType.REGEX,
        (form) => {
          form.config = { ...form.config, pattern: "" };
        },
        "patternRequired",
      ],
      [
        QualityRuleType.STRING_LENGTH,
        (form) => {
          form.config = { ...form.config, minLen: "", maxLen: "" };
        },
        "lengthBoundRequired",
      ],
      [
        QualityRuleType.REFERENTIAL_INTEGRITY,
        (form) => {
          form.config = { ...form.config, parentColumn: "" };
        },
        "referentialFieldsRequired",
      ],
      [
        QualityRuleType.CROSS_COLUMN,
        (form) => {
          form.config = { ...form.config, rightColumn: "" };
        },
        "crossColumnFieldsRequired",
      ],
      [
        QualityRuleType.FRESHNESS,
        (form) => {
          form.config = { ...form.config, maxLagSeconds: "" };
        },
        "freshnessLagRequired",
      ],
      [
        QualityRuleType.CUSTOM_SQL,
        (form) => {
          form.config = { ...form.config, sql: "  " };
        },
        "sqlRequired",
      ],
      [
        QualityRuleType.SCHEMA_DRIFT,
        (form) => {
          form.config = { ...form.config, expectedColumns: [] };
        },
        "schemaDriftColumnRequired",
      ],
      [
        QualityRuleType.GROUP_CHECK,
        (form) => {
          form.config = { ...form.config, groupColumns: "" };
        },
        "groupColumnsRequired",
      ],
      [
        QualityRuleType.GROUP_CHECK,
        (form) => {
          form.config = {
            ...form.config,
            minRowsPerGroup: "",
            maxRowsPerGroup: "",
          };
        },
        "groupBoundRequired",
      ],
      [
        QualityRuleType.SEGMENT_CHECK,
        (form) => {
          form.config = { ...form.config, segmentColumns: "" };
        },
        "segmentColumnsRequired",
      ],
      [
        QualityRuleType.SEGMENT_CHECK,
        (form) => {
          form.config = { ...form.config, segmentBaselineRuns: "11" };
        },
        "segmentBaselineRunsInvalid",
      ],
      [
        QualityRuleType.SEGMENT_CHECK,
        (form) => {
          form.config = { ...form.config, segmentMinOccurrences: "6" };
        },
        "segmentMinOccurrencesInvalid",
      ],
      [
        QualityRuleType.ACCEPTED_VALUES,
        (form) => {
          form.config = { ...form.config, acceptedSource: "field" };
        },
        "acceptedFieldRequired",
      ],
      [
        QualityRuleType.ACCEPTED_VALUES,
        (form) => {
          form.config = {
            ...form.config,
            acceptedSource: "field",
            acceptedField: "status",
          };
        },
        "acceptedFieldSameColumn",
      ],
      [
        QualityRuleType.ACCEPTED_VALUES,
        (form) => {
          form.config = { ...form.config, acceptedSource: "query" };
        },
        "acceptedQueryRequired",
      ],
      [
        QualityRuleType.ACCEPTED_VALUES,
        (form) => {
          form.config = {
            ...form.config,
            acceptedSource: "query",
            acceptedQuery: "SELECT 1",
          };
        },
        "acceptedQueryColumnRequired",
      ],
      [
        QualityRuleType.AGGREGATE,
        (form) => {
          form.config = { ...form.config, aggMin: "", aggMax: "" };
        },
        "aggregateBoundRequired",
      ],
      [
        QualityRuleType.AGGREGATE,
        (form) => {
          form.config = { ...form.config, aggregateColumn: "" };
        },
        "aggregateColumnRequired",
      ],
      [
        QualityRuleType.CROSS_TABLE,
        (form) => {
          form.config = { ...form.config, referenceGuid: "" };
        },
        "crossTableReferenceRequired",
      ],
      [
        QualityRuleType.CROSS_TABLE,
        (form) => {
          form.config = {
            ...form.config,
            rightFilter: [
              {
                column: "status",
                op: QualityFilterOperator.EQ,
                values: "",
              },
            ],
          };
        },
        "crossTableRightFilterValueRequired",
      ],
      [
        QualityRuleType.CROSS_TABLE,
        (form) => {
          form.config = {
            ...form.config,
            leftAggFunction: QualityAggregateFunction.SUM,
            leftAggColumn: "",
          };
        },
        "crossTableColumnRequired",
      ],
      [
        QualityRuleType.CROSS_TABLE,
        (form) => {
          form.config = {
            ...form.config,
            rightAggFunction: QualityAggregateFunction.COUNT,
            rightAggColumn: "id",
          };
        },
        "crossTableColumnForbidden",
      ],
      [
        QualityRuleType.CEL_EXPRESSION,
        (form) => {
          form.config = { ...form.config, celExpression: "  " };
        },
        "celExpressionRequired",
      ],
      [
        QualityRuleType.CEL_EXPRESSION,
        (form) => {
          form.config = { ...form.config, celColumns: "" };
        },
        "celColumnsRequired",
      ],
      [
        QualityRuleType.CEL_EXPRESSION,
        (form) => {
          form.config = { ...form.config, celSampleRows: "200000" };
        },
        "celSampleRowsInvalid",
      ],
    ];
    for (const [ruleType, mutate, expected] of cases) {
      const form = validForm(ruleType);
      mutate(form);
      const keys = validateRuleForm(form, TARGET).map((issue) => issue.key);
      expect(keys, `${ruleType} -> ${expected}`).toContain(
        `${VALIDATION_PREFIX}${expected}`
      );
    }
  });

  it("rejects values past the server's limits", () => {
    const accepted = validForm(QualityRuleType.ACCEPTED_VALUES);
    accepted.config.values = Array.from(
      { length: MAX_ACCEPTED_VALUES + 1 },
      (_, index) => `v${index}`
    ).join(",");
    expect(
      validateRuleForm(accepted, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}valuesTooMany`);

    const sql = validForm(QualityRuleType.CUSTOM_SQL);
    sql.config.sql = "a".repeat(MAX_CUSTOM_SQL_BYTES + 1);
    const sqlIssue = validateRuleForm(sql, TARGET).find(
      (issue) => issue.key === `${VALIDATION_PREFIX}sqlTooLong`
    );
    expect(sqlIssue?.params).toEqual({ max: MAX_CUSTOM_SQL_BYTES });

    const filter = validForm(QualityRuleType.UNIQUE);
    filter.filter = Array.from({ length: MAX_FILTER_CLAUSES + 1 }, () => ({
      column: "region",
      op: QualityFilterOperator.EQ,
      values: "eu",
    }));
    expect(
      validateRuleForm(filter, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}filterTooMany`);

    const manyValues = validForm(QualityRuleType.UNIQUE);
    manyValues.filter = [
      {
        column: "id",
        op: QualityFilterOperator.IN,
        values: Array.from({ length: MAX_FILTER_VALUES + 1 }, (_, i) =>
          String(i)
        ).join(","),
      },
    ];
    const valueIssue = validateRuleForm(manyValues, TARGET).find(
      (issue) => issue.key === `${VALIDATION_PREFIX}filterValuesTooMany`
    );
    expect(valueIssue?.params).toEqual({ max: MAX_FILTER_VALUES });
  });

  it("checks the aggregate range, percentile and null-value limits", () => {
    const aggregate = validForm(QualityRuleType.AGGREGATE);
    aggregate.config.aggMin = "10";
    aggregate.config.aggMax = "5";
    expect(
      validateRuleForm(aggregate, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}aggregateRangeOrder`);

    aggregate.config.aggMin = "0";
    aggregate.config.aggMax = "100";
    aggregate.config.aggWarnMin = "120";
    expect(
      validateRuleForm(aggregate, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}aggregateWarnRange`);

    const percentile = validForm(QualityRuleType.AGGREGATE);
    percentile.config.aggregateFunction = QualityAggregateFunction.PERCENTILE;
    percentile.config.percentile = "120";
    expect(
      validateRuleForm(percentile, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}aggregatePercentileRange`);

    const nulls = validForm(QualityRuleType.NULL_PERCENT);
    nulls.config.nullValues = Array.from(
      { length: MAX_NULL_VALUES + 1 },
      (_, index) => `v${index}`
    ).join(",");
    const nullIssue = validateRuleForm(nulls, TARGET).find(
      (issue) => issue.key === `${VALIDATION_PREFIX}nullValuesTooMany`
    );
    expect(nullIssue?.params).toEqual({ max: MAX_NULL_VALUES });
  });

  it("checks the CEL expression's size and its columns against the target", () => {
    const tooLong = validForm(QualityRuleType.CEL_EXPRESSION);
    tooLong.config.celExpression = "a".repeat(MAX_CEL_EXPRESSION_BYTES + 1);
    expect(
      validateRuleForm(tooLong, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}celExpressionTooLong`);

    const unknown = validForm(QualityRuleType.CEL_EXPRESSION);
    unknown.config.celColumns = "amount, nope";
    const unknownIssue = validateRuleForm(unknown, TARGET, [
      "id",
      "amount",
    ]).find((issue) => issue.key === `${VALIDATION_PREFIX}celColumnUnknown`);
    expect(unknownIssue?.params).toEqual({ column: "nope" });

    // Without a known-column list the form accepts any column; the server checks it.
    expect(validateRuleForm(unknown, TARGET)).toEqual([]);

    const tooMany = validForm(QualityRuleType.CEL_EXPRESSION);
    tooMany.config.celColumns = Array.from(
      { length: MAX_CEL_COLUMNS + 1 },
      (_, index) => `c${index}`
    ).join(",");
    expect(
      validateRuleForm(tooMany, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}celColumnsTooMany`);
  });

  it("reports a number that is not a number and a lag that is not whole", () => {
    const form = validForm(QualityRuleType.FRESHNESS);
    form.config.maxLagSeconds = "3.5";
    expect(validateRuleForm(form, TARGET).map((issue) => issue.key)).toContain(
      `${VALIDATION_PREFIX}numberInvalid`
    );
    const threshold = validForm(QualityRuleType.UNIQUE);
    threshold.config.failThreshold = "lots";
    expect(
      validateRuleForm(threshold, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}numberInvalid`);
  });
});

describe("degenerate inputs", () => {
  it("falls back to a row-count arm for a type without a form", () => {
    const form = emptyRuleForm(QualityRuleType.UNSPECIFIED);
    expect(buildRuleConfig(form).config.case).toBe("rowCount");
  });

  it("reads a rule that has no config or an unsupported arm", () => {
    const withoutConfig = create(QualityRuleSchema, {
      ruleType: QualityRuleType.RANGE,
    });
    expect(ruleToForm(withoutConfig).config.minValue).toBe("");

    const unsupported = create(QualityRuleSchema, {
      ruleType: QualityRuleType.CEL_EXPRESSION,
      config: create(QualityRuleConfigSchema, {
        config: {
          case: "celExpression",
          value: create(CelExpressionConfigSchema, { expression: "x > 0" }),
        },
      }),
    });
    expect(ruleToForm(unsupported).config.aggregateColumn).toBe("");
  });

  it("reads an unset threshold metric as count", () => {
    const rule = ruleFor(QualityRuleType.UNIQUE);
    if (!rule.config) {
      throw new Error("expected a rule config");
    }
    rule.config.thresholdMetric = QualityThresholdMetric.UNSPECIFIED;
    expect(ruleToForm(rule).thresholdMetric).toBe(QualityThresholdMetric.COUNT);
    rule.config.thresholdMetric = QualityThresholdMetric.PERCENT;
    expect(ruleToForm(rule).thresholdMetric).toBe(
      QualityThresholdMetric.PERCENT
    );
  });

  it("drops a stored percentage threshold on a type that does not read it", () => {
    const rule = ruleFor(QualityRuleType.ROW_COUNT);
    if (!rule.config) {
      throw new Error("expected a rule config");
    }
    rule.config.thresholdMetric = QualityThresholdMetric.PERCENT;
    expect(ruleToForm(rule).thresholdMetric).toBe(QualityThresholdMetric.COUNT);
  });

  it("accepts a valid percentile and rejects a missing one", () => {
    const form = validForm(QualityRuleType.AGGREGATE);
    form.config.aggregateFunction = QualityAggregateFunction.PERCENTILE;
    form.config.percentile = "95";
    expect(validateRuleForm(form, TARGET)).toEqual([]);
    form.config.percentile = "";
    expect(validateRuleForm(form, TARGET).map((issue) => issue.key)).toContain(
      `${VALIDATION_PREFIX}aggregatePercentileRange`
    );
  });
});

describe("more validation branches", () => {
  const cases: Array<[QualityRuleType, (form: RuleFormState) => void, string]> =
    [
      [
        QualityRuleType.RANGE,
        (form) => {
          form.config.column = "";
        },
        "columnRequired",
      ],
      [
        QualityRuleType.REGEX,
        (form) => {
          form.config.column = "";
        },
        "columnRequired",
      ],
      [
        QualityRuleType.STRING_LENGTH,
        (form) => {
          form.config.column = "";
        },
        "columnRequired",
      ],
      [
        QualityRuleType.FRESHNESS,
        (form) => {
          form.config.timestampColumn = "";
        },
        "columnRequired",
      ],
      [
        QualityRuleType.NULL_PERCENT,
        (form) => {
          form.config.nullColumn = "";
        },
        "columnRequired",
      ],
    ];
  for (const [ruleType, mutate, expected] of cases) {
    it(`requires the checked column for ${ruleType}`, () => {
      const form = validForm(ruleType);
      mutate(form);
      expect(
        validateRuleForm(form, TARGET).map((issue) => issue.key)
      ).toContain(`${VALIDATION_PREFIX}${expected}`);
    });
  }

  it("flags duplicate schema-drift columns and ignores blank rows", () => {
    const form = validForm(QualityRuleType.SCHEMA_DRIFT);
    form.config.expectedColumns = [
      { name: "id", type: "bigint", nullable: false },
      { name: "ID", type: "bigint", nullable: false },
      { name: "  ", type: "", nullable: true },
    ];
    const keys = validateRuleForm(form, TARGET).map((issue) => issue.key);
    expect(keys).toContain(`${VALIDATION_PREFIX}schemaDriftDuplicateColumn`);
  });

  it("caps the grouping columns", () => {
    const form = validForm(QualityRuleType.GROUP_CHECK);
    form.config.groupColumns = "a, b, c, d, e, f";
    expect(validateRuleForm(form, TARGET).map((issue) => issue.key)).toContain(
      `${VALIDATION_PREFIX}groupColumnsTooMany`
    );
  });

  it("checks the cross-table reference, both sides and the tolerance", () => {
    const missingReference = validForm(QualityRuleType.CROSS_TABLE);
    missingReference.config.referenceGuid = "";
    expect(
      validateRuleForm(missingReference, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}crossTableReferenceRequired`);

    const forbidden = validForm(QualityRuleType.CROSS_TABLE);
    forbidden.config.leftAggFunction = QualityAggregateFunction.COUNT;
    forbidden.config.leftAggColumn = "amount";
    expect(
      validateRuleForm(forbidden, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}crossTableColumnForbidden`);

    const required = validForm(QualityRuleType.CROSS_TABLE);
    required.config.rightAggFunction = QualityAggregateFunction.SUM;
    required.config.rightAggColumn = "";
    expect(
      validateRuleForm(required, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}crossTableColumnRequired`);

    const tolerance = validForm(QualityRuleType.CROSS_TABLE);
    tolerance.config.tolerance = "-1";
    expect(
      validateRuleForm(tolerance, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}crossTableToleranceInvalid`);
  });

  it("mirrors the filter bounds on the reference side too", () => {
    const cases: Array<{
      clauses: {
        column: string;
        op: QualityFilterOperator;
        values: string;
      }[];
      key: string;
    }> = [
      {
        clauses: Array.from({ length: MAX_FILTER_CLAUSES + 1 }, () => ({
          column: "status",
          op: QualityFilterOperator.EQ,
          values: "paid",
        })),
        key: "crossTableRightFilterTooMany",
      },
      {
        clauses: [
          {
            column: "status",
            op: QualityFilterOperator.EQ,
            values: "",
          },
        ],
        key: "crossTableRightFilterValueRequired",
      },
      {
        clauses: [
          {
            column: "status",
            op: QualityFilterOperator.IN,
            values: Array.from({ length: MAX_FILTER_VALUES + 1 }, (_, i) =>
              String(i)
            ).join(","),
          },
        ],
        key: "crossTableRightFilterValuesTooMany",
      },
    ];
    for (const { clauses, key } of cases) {
      const form = validForm(QualityRuleType.CROSS_TABLE);
      form.config.rightFilter = clauses;
      expect(
        validateRuleForm(form, TARGET).map((issue) => issue.key),
        key
      ).toContain(`${VALIDATION_PREFIX}${key}`);
    }
  });

  it("requires a value for every clause of the check's own filter", () => {
    const form = validForm(QualityRuleType.UNIQUE);
    form.filter = [
      { column: "region", op: QualityFilterOperator.EQ, values: "" },
    ];
    expect(validateRuleForm(form, TARGET).map((issue) => issue.key)).toContain(
      `${VALIDATION_PREFIX}filterValueRequired`
    );
    const multiValue = validForm(QualityRuleType.UNIQUE);
    multiValue.filter = [
      { column: "region", op: QualityFilterOperator.EQ, values: "eu, us" },
    ];
    expect(
      validateRuleForm(multiValue, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}filterValuesTooMany`);
  });

  it("skips the value count for an operator that takes none", () => {
    const form = validForm(QualityRuleType.UNIQUE);
    form.filter = [
      {
        column: "deleted_at",
        op: QualityFilterOperator.IS_NULL,
        values: "ignored",
      },
    ];
    expect(validateRuleForm(form, TARGET)).toEqual([]);
  });
});

describe("shared helpers", () => {
  it("parses value lists deterministically", () => {
    expect(parseValueList(" a, b\nc ,, a ")).toEqual(["a", "b", "c"]);
    expect(parseValueList("")).toEqual([]);
    expect(trimmed(" x ")).toBe("x");
    expect(trimmed(undefined)).toBe("");
  });

  it("distinguishes blank from invalid numbers", () => {
    expect(invalidNumber("")).toBe(false);
    expect(invalidNumber("  ")).toBe(false);
    expect(invalidNumber("1.5")).toBe(false);
    expect(invalidNumber("x")).toBe(true);
    expect(invalidInteger("")).toBe(false);
    expect(invalidInteger("10")).toBe(false);
    expect(invalidInteger("1.5")).toBe(true);
    expect(invalidInteger("-2")).toBe(false);
  });
});

describe("threshold suggestions", () => {
  function suggestion(thresholds: Record<string, unknown>) {
    return create(QualityThresholdSuggestionSchema, {
      thresholds: create(QualityThresholdsSchema, thresholds),
    });
  }

  function formFor(ruleType: QualityRuleType): RuleFormState {
    return emptyRuleForm(ruleType);
  }

  it("defaults a new count-change check's empty behaviour to pass", () => {
    // Its first run only records the baseline, and "pass" is the setting that
    // says the run has no verdict.
    expect(defaultOnEmptyForNewRule(QualityRuleType.ROW_COUNT_CHANGE)).toBe(
      QualityEmptyBehavior.PASS
    );
    // Every other type keeps the editor's own default.
    expect(defaultOnEmptyForNewRule(QualityRuleType.ROW_COUNT)).toBeUndefined();
    expect(
      defaultOnEmptyForNewRule(QualityRuleType.NULL_PERCENT)
    ).toBeUndefined();
  });

  it("fills a row count's two-sided range, only where a bound exists", () => {
    const form = formFor(QualityRuleType.ROW_COUNT);
    const applied = applyThresholdSuggestion(
      form,
      suggestion({
        comparison: QualityComparison.OUTSIDE_RANGE,
        failValue: 10,
        failValueUpper: 100,
        warnValue: 20,
        warnValueUpper: 80,
      })
    );
    expect(applied.config.minRows).toBe("10");
    expect(applied.config.maxRows).toBe("100");
    expect(applied.config.warnMinRows).toBe("20");
    expect(applied.config.warnMaxRows).toBe("80");

    const oneSided = applyThresholdSuggestion(
      formFor(QualityRuleType.ROW_COUNT),
      suggestion({ comparison: QualityComparison.OUTSIDE_RANGE, failValue: 10 })
    );
    expect(oneSided.config.minRows).toBe("10");
    // A bound the suggestion did not carry is not invented.
    expect(oneSided.config.maxRows).toBe("");
    expect(oneSided.config.warnMinRows).toBe("");
  });

  it("fills the shared fail/warn fields of the single-bound family", () => {
    for (const ruleType of [
      QualityRuleType.UNIQUE,
      QualityRuleType.UNIQUE_COMBINATION,
      QualityRuleType.ACCEPTED_VALUES,
      QualityRuleType.RANGE,
      QualityRuleType.REGEX,
      QualityRuleType.STRING_LENGTH,
      QualityRuleType.REFERENTIAL_INTEGRITY,
      QualityRuleType.CROSS_COLUMN,
      QualityRuleType.GROUP_CHECK,
      QualityRuleType.CEL_EXPRESSION,
      QualityRuleType.CUSTOM_SQL,
      QualityRuleType.CROSS_TABLE,
    ]) {
      const applied = applyThresholdSuggestion(
        formFor(ruleType),
        suggestion({ failValue: 5.5, warnValue: 3 })
      );
      expect(applied.config.failThreshold).toBe("5.5");
      expect(applied.config.warnThreshold).toBe("3");
    }
  });

  it("clears a warning bound the suggestion does not carry", () => {
    // A flat history suggests fail=0 and no warning zone; keeping the old warn=5
    // would make the form invalid (warn above fail) and the server would reject
    // the save.
    const form = formFor(QualityRuleType.UNIQUE);
    form.config.failThreshold = "10";
    form.config.warnThreshold = "5";
    const applied = applyThresholdSuggestion(
      form,
      suggestion({ failValue: 0 })
    );
    expect(applied.config.failThreshold).toBe("0");
    expect(applied.config.warnThreshold).toBe("");
  });

  it("clears a freshness warning lag the suggestion does not carry", () => {
    const form = formFor(QualityRuleType.FRESHNESS);
    form.config.maxLagSeconds = "3600";
    form.config.warnLagSeconds = "1800";
    const applied = applyThresholdSuggestion(
      form,
      suggestion({ failValue: 900 })
    );
    expect(applied.config.maxLagSeconds).toBe("900");
    expect(applied.config.warnLagSeconds).toBe("");
  });

  it("clears a null check's warning bound in the unit it measures in", () => {
    const form = formFor(QualityRuleType.NULL_PERCENT);
    form.config.nullMetric = "percent";
    form.config.maxNullPct = "5";
    form.config.warnNullPct = "2";
    const applied = applyThresholdSuggestion(
      form,
      suggestion({ failValue: 1 })
    );
    expect(applied.config.maxNullPct).toBe("1");
    expect(applied.config.warnNullPct).toBe("");
  });

  it("clears a row count's warning bound when only the failure bound is suggested", () => {
    const form = formFor(QualityRuleType.ROW_COUNT);
    form.config.minRows = "5";
    form.config.warnMinRows = "8";
    const applied = applyThresholdSuggestion(
      form,
      suggestion({ failValue: 10 })
    );
    expect(applied.config.minRows).toBe("10");
    expect(applied.config.warnMinRows).toBe("");
  });

  it("writes a null check's suggestion into the unit it measures in", () => {
    const countForm = formFor(QualityRuleType.NULL_PERCENT);
    countForm.config.nullMetric = "count";
    const count = applyThresholdSuggestion(
      countForm,
      suggestion({ failValue: 5, warnValue: 2 })
    );
    expect(count.config.maxNullCount).toBe("5");
    expect(count.config.warnNullCount).toBe("2");
    expect(count.config.maxNullPct).toBe("");

    const percentForm = formFor(QualityRuleType.NULL_PERCENT);
    percentForm.config.nullMetric = "percent";
    const percent = applyThresholdSuggestion(
      percentForm,
      suggestion({ failValue: 5.5, warnValue: 2 })
    );
    expect(percent.config.maxNullPct).toBe("5.5");
    expect(percent.config.warnNullPct).toBe("2");
    expect(percent.config.maxNullCount).toBe("");
  });

  it("rounds a lag to whole seconds", () => {
    const fresh = applyThresholdSuggestion(
      formFor(QualityRuleType.FRESHNESS),
      suggestion({ failValue: 86_400.4, warnValue: 3_600 })
    );
    expect(fresh.config.maxLagSeconds).toBe("86400");
    expect(fresh.config.warnLagSeconds).toBe("3600");
  });

  it("leaves the form alone (and unmutated) when nothing applies", () => {
    const form = formFor(QualityRuleType.UNIQUE);
    form.config.failThreshold = "1";
    expect(
      applyThresholdSuggestion(
        form,
        create(QualityThresholdSuggestionSchema, {})
      )
    ).toBe(form);

    for (const ruleType of [
      QualityRuleType.ROW_COUNT_CHANGE,
      QualityRuleType.SCHEMA_DRIFT,
    ]) {
      const untouched = formFor(ruleType);
      expect(
        applyThresholdSuggestion(untouched, suggestion({ failValue: 5 }))
      ).toBe(untouched);
    }

    const applied = applyThresholdSuggestion(
      form,
      suggestion({ failValue: 5 })
    );
    // The helper returns a new form; the one the dialog is holding keeps its
    // values until the caller replaces it.
    expect(applied).not.toBe(form);
    expect(form.config.failThreshold).toBe("1");
    expect(applied.config.failThreshold).toBe("5");
  });
});

describe("execution settings", () => {
  it("offers a percentage only where the engine and target can express it", () => {
    for (const engine of [Engine.POSTGRES, Engine.MSSQL]) {
      expect(
        samplingMethodsFor(engine, MetaType.TABLE),
        String(engine)
      ).toEqual(["none", "percent", "maxRows"]);
      expect(percentSamplingSupported(engine, MetaType.TABLE)).toBe(true);
    }
    for (const engine of [
      Engine.MYSQL,
      Engine.TIDB,
      Engine.MARIADB,
      Engine.OCEANBASE,
      Engine.STARROCKS,
      Engine.DORIS,
      Engine.ENGINE_UNSPECIFIED,
    ]) {
      expect(
        samplingMethodsFor(engine, MetaType.TABLE),
        String(engine)
      ).toEqual(["none", "maxRows"]);
      expect(percentSamplingSupported(engine, MetaType.TABLE)).toBe(false);
    }
    for (const metaType of [
      MetaType.VIEW,
      MetaType.MATERIALIZED_VIEW,
      MetaType.EXTERNAL_TABLE,
      MetaType.COLUMN,
    ]) {
      expect(
        samplingMethodsFor(Engine.POSTGRES, metaType),
        String(metaType)
      ).toEqual(["none", "maxRows"]);
      expect(percentSamplingSupported(Engine.POSTGRES, metaType)).toBe(false);
    }
    // An engine or type that has not loaded yet is not a refusal: a stored
    // method must not disappear because the instance cache is still empty.
    expect(samplingMethodsFor(undefined, undefined)).toEqual([
      "none",
      "percent",
      "maxRows",
    ]);
    expect(percentSamplingSupported(undefined, MetaType.TABLE)).toBe(true);
    expect(percentSamplingSupported(Engine.POSTGRES, undefined)).toBe(true);
  });

  it("converts a timeout and a row limit to the config message", () => {
    const form = validForm(QualityRuleType.UNIQUE);
    form.config.timeoutSeconds = "120";
    form.config.sampleMethod = "maxRows";
    form.config.sampleMaxRows = "1000";

    const rule = formToRule(form, { target: TARGET });
    expect(rule.config?.timeoutSeconds).toBe(120);
    expect(rule.config?.sampling?.method.case).toBe("maxRows");
    expect(rule.config?.sampling?.method.value).toBe(1000n);

    const back = ruleToForm(rule);
    expect(back.config.timeoutSeconds).toBe("120");
    expect(back.config.sampleMethod).toBe("maxRows");
    expect(back.config.sampleMaxRows).toBe("1000");
    expect(back.config.samplePercent).toBe("");
  });

  it("converts a percentage to the config message", () => {
    const form = validForm(QualityRuleType.UNIQUE);
    form.config.sampleMethod = "percent";
    form.config.samplePercent = "12.5";

    const rule = formToRule(form, { target: TARGET });
    expect(rule.config?.timeoutSeconds).toBe(0);
    expect(rule.config?.sampling?.method.case).toBe("percent");
    expect(rule.config?.sampling?.method.value).toBe(12.5);

    const back = ruleToForm(rule);
    expect(back.config.sampleMethod).toBe("percent");
    expect(back.config.samplePercent).toBe("12.5");
    expect(back.config.sampleMaxRows).toBe("");
  });

  it("keeps no sampling and the default timeout out of the message", () => {
    const rule = formToRule(validForm(QualityRuleType.UNIQUE), {
      target: TARGET,
    });
    expect(rule.config?.timeoutSeconds).toBe(0);
    expect(rule.config?.sampling).toBeUndefined();

    const back = ruleToForm(rule);
    expect(back.config.sampleMethod).toBe("none");
    expect(back.config.timeoutSeconds).toBe("");
  });

  it("builds no sample for a chosen method with no usable amount", () => {
    const percent = validForm(QualityRuleType.UNIQUE);
    percent.config.sampleMethod = "percent";
    expect(
      formToRule(percent, { target: TARGET }).config?.sampling
    ).toBeUndefined();
    const maxRows = validForm(QualityRuleType.UNIQUE);
    maxRows.config.sampleMethod = "maxRows";
    expect(
      formToRule(maxRows, { target: TARGET }).config?.sampling
    ).toBeUndefined();
  });

  it("never turns a stored percentage into a row limit", () => {
    // Even on a target the server will refuse, the client sends the intent it
    // was given; the verdict about availability belongs to the save.
    const form = validForm(QualityRuleType.UNIQUE);
    form.config.sampleMethod = "percent";
    form.config.samplePercent = "25";
    const rule = formToRule(form, { target: TARGET });
    expect(rule.config?.sampling?.method.case).toBe("percent");
    expect(rule.config?.sampling?.method.value).toBe(25);
  });

  it("notices a timeout, a method or an amount change in the update mask", () => {
    function snapshot(): {
      form: RuleFormState;
      target: TargetSelection | undefined;
    } {
      return { form: validForm(QualityRuleType.UNIQUE), target: { ...TARGET } };
    }
    function changed(mutate: (form: RuleFormState) => void) {
      const next = snapshot();
      mutate(next.form);
      return computeUpdateMask(snapshot(), next);
    }

    expect(
      changed((form) => {
        form.config.timeoutSeconds = String(MIN_TIMEOUT_SECONDS);
      })
    ).toEqual(["config"]);
    expect(
      changed((form) => {
        form.config.sampleMethod = "maxRows";
        form.config.sampleMaxRows = "10";
      })
    ).toEqual(["config"]);
    expect(
      changed((form) => {
        form.config.sampleMethod = "percent";
        form.config.samplePercent = "10";
      })
    ).toEqual(["config"]);
    // Whitespace around a value is not a change; a new amount is.
    expect(
      changed((form) => {
        form.config.timeoutSeconds = "  ";
      })
    ).toEqual([]);
    const base = snapshot();
    base.form.config.sampleMethod = "maxRows";
    base.form.config.sampleMaxRows = "10";
    const next = {
      form: validForm(QualityRuleType.UNIQUE),
      target: { ...TARGET } as TargetSelection | undefined,
    };
    next.form.config.sampleMethod = "maxRows";
    next.form.config.sampleMaxRows = "20";
    expect(computeUpdateMask(base, next)).toEqual(["config"]);
  });

  it("keeps the timeout on 1-300 or blank", () => {
    const cases: Array<[string, boolean]> = [
      ["", false],
      [String(MIN_TIMEOUT_SECONDS), false],
      [String(MAX_TIMEOUT_SECONDS), false],
      ["0", true],
      [String(MAX_TIMEOUT_SECONDS + 1), true],
      ["1.5", true],
      ["soon", true],
    ];
    for (const [value, invalid] of cases) {
      const form = validForm(QualityRuleType.UNIQUE);
      form.config.timeoutSeconds = value;
      const keys = validateRuleForm(form, TARGET).map((issue) => issue.key);
      expect(keys.includes(`${VALIDATION_PREFIX}timeoutInvalid`), value).toBe(
        invalid
      );
    }
  });

  it("keeps the percentage on 1-99 with an integer or decimal value", () => {
    const cases: Array<[string, boolean]> = [
      ["", true],
      [String(MIN_SAMPLE_PERCENT), false],
      ["50.5", false],
      [String(MAX_SAMPLE_PERCENT), false],
      ["0", true],
      [String(MAX_SAMPLE_PERCENT + 1), true],
      ["some", true],
    ];
    for (const [value, invalid] of cases) {
      const form = validForm(QualityRuleType.UNIQUE);
      form.config.sampleMethod = "percent";
      form.config.samplePercent = value;
      const keys = validateRuleForm(form, TARGET).map((issue) => issue.key);
      expect(
        keys.includes(`${VALIDATION_PREFIX}samplePercentInvalid`),
        value
      ).toBe(invalid);
      expect(keys).not.toContain(
        `${VALIDATION_PREFIX}samplePercentUnsupported`
      );
    }
  });

  it("refuses a percentage the target cannot express", () => {
    const form = validForm(QualityRuleType.UNIQUE);
    form.config.sampleMethod = "percent";
    form.config.samplePercent = "10";

    for (const engine of [Engine.MYSQL, Engine.STARROCKS]) {
      expect(
        validateRuleForm(form, TARGET, [], engine).map((issue) => issue.key)
      ).toContain(`${VALIDATION_PREFIX}samplePercentUnsupported`);
    }
    const view: TargetSelection = { ...TARGET, metaType: MetaType.VIEW };
    expect(
      validateRuleForm(form, view, [], Engine.POSTGRES).map(
        (issue) => issue.key
      )
    ).toContain(`${VALIDATION_PREFIX}samplePercentUnsupported`);
    expect(
      validateRuleForm(form, TARGET, [], Engine.POSTGRES).map(
        (issue) => issue.key
      )
    ).not.toContain(`${VALIDATION_PREFIX}samplePercentUnsupported`);
  });

  it("keeps the row limit on 1-10,000,000", () => {
    const cases: Array<[string, boolean]> = [
      ["", true],
      [String(MIN_SAMPLE_MAX_ROWS), false],
      [String(MAX_SAMPLE_MAX_ROWS), false],
      ["0", true],
      [String(MAX_SAMPLE_MAX_ROWS + 1), true],
      ["1.5", true],
      ["many", true],
    ];
    for (const [value, invalid] of cases) {
      const form = validForm(QualityRuleType.UNIQUE);
      form.config.sampleMethod = "maxRows";
      form.config.sampleMaxRows = value;
      const issue = validateRuleForm(form, TARGET).find(
        (candidate) =>
          candidate.key === `${VALIDATION_PREFIX}sampleMaxRowsInvalid`
      );
      expect(Boolean(issue), value).toBe(invalid);
    }
    const form = validForm(QualityRuleType.UNIQUE);
    form.config.sampleMethod = "maxRows";
    form.config.sampleMaxRows = "0";
    expect(
      validateRuleForm(form, TARGET).find(
        (candidate) =>
          candidate.key === `${VALIDATION_PREFIX}sampleMaxRowsInvalid`
      )?.params
    ).toEqual({ max: MAX_SAMPLE_MAX_ROWS });
  });
});

describe("execution semantics sampling gating", () => {
  it("refuses a sampling method the rule type cannot use", () => {
    const rowCount = validForm(QualityRuleType.ROW_COUNT);
    rowCount.config.sampleMethod = "maxRows";
    rowCount.config.sampleMaxRows = "100";
    expect(
      validateRuleForm(rowCount, TARGET).map((issue) => issue.key)
    ).toContain(`${VALIDATION_PREFIX}samplingUnsupportedRuleType`);

    // The same method on a type that can be sampled raises no such issue.
    const unique = validForm(QualityRuleType.UNIQUE);
    unique.config.sampleMethod = "maxRows";
    unique.config.sampleMaxRows = "100";
    expect(
      validateRuleForm(unique, TARGET).map((issue) => issue.key)
    ).not.toContain(`${VALIDATION_PREFIX}samplingUnsupportedRuleType`);
  });

  it("keeps the sampleable rule types in step with the server contract", () => {
    // Mirrors ruleTypeSpecs[].Sampling in backend/component/dataquality.
    expect(SAMPLEABLE_RULE_TYPES).toEqual([
      QualityRuleType.NULL_PERCENT,
      QualityRuleType.UNIQUE,
      QualityRuleType.UNIQUE_COMBINATION,
      QualityRuleType.ACCEPTED_VALUES,
      QualityRuleType.RANGE,
      QualityRuleType.REGEX,
      QualityRuleType.STRING_LENGTH,
      QualityRuleType.CROSS_COLUMN,
      QualityRuleType.GROUP_CHECK,
      QualityRuleType.AGGREGATE,
    ]);
    expect(canSampleRuleType(QualityRuleType.AGGREGATE)).toBe(true);
    expect(canSampleRuleType(QualityRuleType.ROW_COUNT)).toBe(false);
    expect(canSampleRuleType(QualityRuleType.FRESHNESS)).toBe(false);
    expect(canSampleRuleType(QualityRuleType.CUSTOM_SQL)).toBe(false);
    expect(canSampleRuleType(QualityRuleType.SCHEMA_DRIFT)).toBe(false);
    expect(canSampleRuleType(QualityRuleType.CEL_EXPRESSION)).toBe(false);
    expect(canSampleRuleType(QualityRuleType.REFERENTIAL_INTEGRITY)).toBe(
      false
    );
    expect(canSampleRuleType(QualityRuleType.CROSS_TABLE)).toBe(false);
  });
});
