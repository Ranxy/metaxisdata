import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  CelExpressionConfigSchema,
  FreshnessConfigSchema,
  QualityAggregateFunction,
  QualityComparisonOperator,
  QualityDimension,
  QualityEmptyBehavior,
  QualityFilterOperator,
  type QualityRule,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityThresholdMetric,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import type { RuleFormState, TargetSelection } from "./dataQualityForm";
import {
  buildFilter,
  buildRuleConfig,
  buildTarget,
  columnsForConfig,
  computeUpdateMask,
  durationToSchedule,
  EDITABLE_RULE_TYPES,
  emptyConfig,
  emptyFilterClause,
  emptyRuleForm,
  filterOperatorNeedsValues,
  formToRule,
  invalidInteger,
  invalidNumber,
  isThresholdMetricType,
  MAX_ACCEPTED_VALUES,
  MAX_CEL_COLUMNS,
  MAX_CEL_EXPRESSION_BYTES,
  MAX_CUSTOM_SQL_BYTES,
  MAX_FILTER_CLAUSES,
  MAX_FILTER_VALUES,
  MAX_NULL_VALUES,
  MAX_SCHEDULE_MINUTES,
  MIN_SCHEDULE_MINUTES,
  parseValueList,
  ruleToForm,
  scheduleMinutes,
  scheduleToDuration,
  suggestedDimension,
  THRESHOLD_METRIC_RULE_TYPES,
  targetFromQuery,
  targetSelectionFromRule,
  trimmed,
  VALIDATION_PREFIX,
  validateRuleForm,
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
  [QualityRuleType.CROSS_TABLE]: {
    rightTable: "public.orders_ref",
    leftAggFunction: QualityAggregateFunction.SUM,
    leftAggColumn: "amount",
    rightAggFunction: QualityAggregateFunction.COUNT,
    rightAggColumn: "",
    tolerance: "0.5",
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
  aggregate: QualityRuleType.AGGREGATE,
  crossTable: QualityRuleType.CROSS_TABLE,
  celExpression: QualityRuleType.CEL_EXPRESSION,
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
  it("offers every implemented type and not the reserved one", () => {
    expect(EDITABLE_RULE_TYPES).toHaveLength(18);
    expect(new Set(EDITABLE_RULE_TYPES).size).toBe(18);
    expect(EDITABLE_RULE_TYPES).toContain(QualityRuleType.CEL_EXPRESSION);
    for (const ruleType of [
      QualityRuleType.SCHEMA_DRIFT,
      QualityRuleType.GROUP_CHECK,
      QualityRuleType.AGGREGATE,
      QualityRuleType.CROSS_TABLE,
    ]) {
      expect(EDITABLE_RULE_TYPES).toContain(ruleType);
    }
  });

  it("suggests a dimension for every type and keeps the metric types inside the editable set", () => {
    for (const ruleType of EDITABLE_RULE_TYPES) {
      expect(suggestedDimension(ruleType)).not.toBe(
        QualityDimension.UNSPECIFIED
      );
    }
    expect(suggestedDimension(99 as QualityRuleType)).toBe(
      QualityDimension.UNSPECIFIED
    );
    expect(THRESHOLD_METRIC_RULE_TYPES).toHaveLength(11);
    for (const ruleType of THRESHOLD_METRIC_RULE_TYPES) {
      expect(EDITABLE_RULE_TYPES).toContain(ruleType);
      expect(isThresholdMetricType(ruleType)).toBe(true);
    }
    expect(isThresholdMetricType(QualityRuleType.ROW_COUNT)).toBe(false);
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
      expect(back.schedule).toEqual({ value: "1", unit: "hours" });
      expect(back.filter).toEqual(form.filter);
      expect(rule.target?.columns).toEqual(columnsForConfig(form));
    }
  });

  it("keeps an empty config usable", () => {
    for (const ruleType of EDITABLE_RULE_TYPES) {
      const rule = formToRule(emptyRuleForm(ruleType));
      const back = ruleToForm(rule);
      expect(back.config, String(ruleType)).toEqual(emptyConfig());
      expect(back.schedule).toEqual({ value: "1", unit: "hours" });
    }
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

describe("schedule defaults", () => {
  it("keeps a new rule's default of one hour", () => {
    expect(emptyRuleForm().schedule).toEqual({ value: "1", unit: "hours" });
    expect(scheduleToDuration(emptyRuleForm().schedule).seconds).toBe(3_600n);
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
    expect(
      changed((form) => {
        form.schedule = { value: "2", unit: "hours" };
      })
    ).toEqual(["schedule_interval"]);
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

  it("requires a title, a target and a usable schedule", () => {
    const form = validForm(QualityRuleType.ROW_COUNT);
    form.title = "  ";
    const issues = validateRuleForm(form, undefined).map((issue) => issue.key);
    expect(issues).toContain(`${VALIDATION_PREFIX}titleRequired`);
    expect(issues).toContain(`${VALIDATION_PREFIX}targetRequired`);

    form.schedule = { value: "14", unit: "minutes" };
    expect(validateRuleForm(form, TARGET).map((issue) => issue.key)).toContain(
      `${VALIDATION_PREFIX}scheduleInvalid`
    );
    form.schedule = {
      value: String(MAX_SCHEDULE_MINUTES + 1),
      unit: "minutes",
    };
    expect(validateRuleForm(form, TARGET).map((issue) => issue.key)).toContain(
      `${VALIDATION_PREFIX}scheduleInvalid`
    );
    form.schedule = { value: String(MIN_SCHEDULE_MINUTES), unit: "minutes" };
    expect(
      validateRuleForm(form, TARGET).map((issue) => issue.key)
    ).not.toContain(`${VALIDATION_PREFIX}scheduleInvalid`);
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
          form.config = { ...form.config, rightTable: "" };
        },
        "crossTableRightTableRequired",
      ],
      [
        QualityRuleType.CROSS_TABLE,
        (form) => {
          form.config = { ...form.config, rightTable: "a.b.c" };
        },
        "crossTableRightTableShape",
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

  it("checks both sides of a cross-table rule and its tolerance", () => {
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
