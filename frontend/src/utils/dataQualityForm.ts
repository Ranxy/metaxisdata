import { create } from "@bufbuild/protobuf";
import type { Duration } from "@bufbuild/protobuf/wkt";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import {
  AcceptedValuesConfigSchema,
  AggregateConfigSchema,
  CelExpressionConfigSchema,
  CrossColumnConfigSchema,
  CrossTableConfigSchema,
  CustomSQLConfigSchema,
  QualityFilterOperator as FilterOperator,
  FreshnessConfigSchema,
  GroupCheckConfigSchema,
  NullPercentConfigSchema,
  QualityAggregateFunction,
  QualityComparisonOperator,
  QualityDimension,
  QualityEmptyBehavior,
  QualityFilterClauseSchema,
  type QualityFilterOperator,
  QualityFilterSchema,
  type QualityRule,
  type QualityRuleConfig,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  type QualityTarget,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityThresholdMetric,
  RangeConfigSchema,
  ReferentialIntegrityConfigSchema,
  RegexConfigSchema,
  RowCountChangeConfigSchema,
  RowCountConfigSchema,
  SchemaDriftColumnSchema,
  SchemaDriftConfigSchema,
  StringLengthConfigSchema,
  UniqueCombinationConfigSchema,
  UniqueConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";

/**
 * The rule editor's pure layer: the form state, its conversion to and from the
 * stored `QualityRule`, the update mask, and validation.
 *
 * Everything here works on plain values so the conversions can be tested
 * without mounting the editor. The component keeps a `RuleFormState` and calls
 * `formToRule` at the request boundary; that keeps proto details (int64 as
 * bigint, oneof arms, optional thresholds) out of the template.
 */

// ---- limits ----

/** The schedule bounds the server accepts: 15 minutes to 7 days. */
export const MIN_SCHEDULE_MINUTES = 15;
export const MAX_SCHEDULE_MINUTES = 7 * 24 * 60;
/** The custom-SQL config is capped at 8 KiB by the service. */
export const MAX_CUSTOM_SQL_BYTES = 8192;
/** A rule's AND filter is capped at ten clauses; IN takes at most 100 values. */
export const MAX_FILTER_CLAUSES = 10;
export const MAX_FILTER_VALUES = 100;
/** accepted_values takes at most 200 values. */
export const MAX_ACCEPTED_VALUES = 200;
/** null_percent's extra missing values are capped at 20. */
export const MAX_NULL_VALUES = 20;
/** A grouped row-count rule groups by at most five columns. */
export const MAX_GROUP_COLUMNS = 5;
/** CEL expressions are capped at 4 KiB, 20 referenced columns and 100k rows. */
export const MAX_CEL_EXPRESSION_BYTES = 4096;
export const MAX_CEL_COLUMNS = 20;
export const MAX_CEL_SAMPLE_ROWS = 100_000;
export const DEFAULT_CEL_SAMPLE_ROWS = 10_000;

/** The aggregates the aggregate rule offers. COUNT is the run's total_rows. */
export const AGGREGATE_FUNCTIONS: QualityAggregateFunction[] = [
  QualityAggregateFunction.SUM,
  QualityAggregateFunction.AVG,
  QualityAggregateFunction.MIN,
  QualityAggregateFunction.MAX,
  QualityAggregateFunction.STDDEV,
  QualityAggregateFunction.PERCENTILE,
];

/** The aggregates one side of a cross-table rule may use. */
export const CROSS_TABLE_FUNCTIONS: QualityAggregateFunction[] = [
  QualityAggregateFunction.COUNT,
  QualityAggregateFunction.SUM,
  QualityAggregateFunction.AVG,
  QualityAggregateFunction.MIN,
  QualityAggregateFunction.MAX,
];

/**
 * The 18 rule types the editor offers. Every one has a config form;
 * CEL_EXPRESSION included now that the API accepts it.
 */
export const EDITABLE_RULE_TYPES: QualityRuleType[] = [
  QualityRuleType.ROW_COUNT,
  QualityRuleType.ROW_COUNT_CHANGE,
  QualityRuleType.NULL_PERCENT,
  QualityRuleType.UNIQUE,
  QualityRuleType.UNIQUE_COMBINATION,
  QualityRuleType.ACCEPTED_VALUES,
  QualityRuleType.RANGE,
  QualityRuleType.REGEX,
  QualityRuleType.STRING_LENGTH,
  QualityRuleType.REFERENTIAL_INTEGRITY,
  QualityRuleType.CROSS_COLUMN,
  QualityRuleType.FRESHNESS,
  QualityRuleType.CUSTOM_SQL,
  QualityRuleType.SCHEMA_DRIFT,
  QualityRuleType.GROUP_CHECK,
  QualityRuleType.AGGREGATE,
  QualityRuleType.CROSS_TABLE,
  QualityRuleType.CEL_EXPRESSION,
];

/**
 * The rule types whose shared `threshold_metric` applies: their config carries a
 * fail/warn threshold read as a count or a percentage. Mirrors
 * `backend/component/dataquality/validate.go` `ruleTypeAllowsPercent`:
 * row_count uses bounds, null_percent names its unit in its own config,
 * custom_sql returns failing rows only (no denominator), and
 * row_count_change/freshness/schema_drift/aggregate/cross_table measure something
 * other than a share of rows.
 */
export const THRESHOLD_METRIC_RULE_TYPES: QualityRuleType[] = [
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
];

/** One section of the rule-type select: an i18n group key and its types. */
export interface RuleTypeGroup {
  key: string;
  types: QualityRuleType[];
}

/**
 * The editable rule types by family, in the order the editor lists them. Each
 * type appears in exactly one group.
 */
export const RULE_TYPE_GROUPS: RuleTypeGroup[] = [
  {
    key: "dataQuality.ruleTypeGroup.count",
    types: [
      QualityRuleType.ROW_COUNT,
      QualityRuleType.ROW_COUNT_CHANGE,
      QualityRuleType.NULL_PERCENT,
      QualityRuleType.GROUP_CHECK,
      QualityRuleType.FRESHNESS,
    ],
  },
  {
    key: "dataQuality.ruleTypeGroup.value",
    types: [
      QualityRuleType.UNIQUE,
      QualityRuleType.UNIQUE_COMBINATION,
      QualityRuleType.ACCEPTED_VALUES,
      QualityRuleType.RANGE,
      QualityRuleType.REGEX,
      QualityRuleType.STRING_LENGTH,
      QualityRuleType.CROSS_COLUMN,
      QualityRuleType.REFERENTIAL_INTEGRITY,
    ],
  },
  {
    key: "dataQuality.ruleTypeGroup.aggregate",
    types: [QualityRuleType.AGGREGATE, QualityRuleType.CROSS_TABLE],
  },
  {
    key: "dataQuality.ruleTypeGroup.structure",
    types: [
      QualityRuleType.SCHEMA_DRIFT,
      QualityRuleType.CUSTOM_SQL,
      QualityRuleType.CEL_EXPRESSION,
    ],
  },
];

/**
 * Views and materialized views have no separate registry column rows, so only
 * table-level rules run against them. Mirrors
 * `backend/component/dataquality/support.go` `RuleTypeForTarget`.
 */
const VIEW_RULE_TYPES: QualityRuleType[] = [
  QualityRuleType.ROW_COUNT,
  QualityRuleType.ROW_COUNT_CHANGE,
  QualityRuleType.FRESHNESS,
  QualityRuleType.CUSTOM_SQL,
  QualityRuleType.SCHEMA_DRIFT,
];

/** The rule types each target MetaType may watch. */
export const RULE_TYPES_BY_META_TYPE: Partial<
  Record<MetaType, QualityRuleType[]>
> = {
  [MetaType.TABLE]: EDITABLE_RULE_TYPES,
  [MetaType.EXTERNAL_TABLE]: EDITABLE_RULE_TYPES,
  [MetaType.VIEW]: VIEW_RULE_TYPES,
  [MetaType.MATERIALIZED_VIEW]: VIEW_RULE_TYPES,
};

/**
 * The rule types the editor may offer for a target; every type when there is no
 * target yet. Never mutate the returned array.
 */
export function allowedRuleTypes(
  metaType: MetaType | undefined
): QualityRuleType[] {
  return (
    (metaType !== undefined && RULE_TYPES_BY_META_TYPE[metaType]) ||
    EDITABLE_RULE_TYPES
  );
}

/**
 * The rule types where on_empty never changes the verdict: row_count always
 * returns one row, schema_drift reads metadata, and a custom SQL rule with no
 * returned rows has no failing rows. The editor hides the select there.
 */
export const ON_EMPTY_IRRELEVANT_RULE_TYPES: QualityRuleType[] = [
  QualityRuleType.ROW_COUNT,
  QualityRuleType.CUSTOM_SQL,
  QualityRuleType.SCHEMA_DRIFT,
];

/** True when the on_empty behavior can change this rule type's verdict. */
export function isOnEmptyRelevant(ruleType: QualityRuleType): boolean {
  return !ON_EMPTY_IRRELEVANT_RULE_TYPES.includes(ruleType);
}

const DIMENSION_BY_RULE_TYPE: Partial<
  Record<QualityRuleType, QualityDimension>
> = {
  [QualityRuleType.ROW_COUNT]: QualityDimension.COMPLETENESS,
  [QualityRuleType.ROW_COUNT_CHANGE]: QualityDimension.COMPLETENESS,
  [QualityRuleType.NULL_PERCENT]: QualityDimension.COMPLETENESS,
  [QualityRuleType.UNIQUE]: QualityDimension.UNIQUENESS,
  [QualityRuleType.UNIQUE_COMBINATION]: QualityDimension.UNIQUENESS,
  [QualityRuleType.ACCEPTED_VALUES]: QualityDimension.VALIDITY,
  [QualityRuleType.RANGE]: QualityDimension.VALIDITY,
  [QualityRuleType.REGEX]: QualityDimension.VALIDITY,
  [QualityRuleType.STRING_LENGTH]: QualityDimension.VALIDITY,
  [QualityRuleType.REFERENTIAL_INTEGRITY]: QualityDimension.CONSISTENCY,
  [QualityRuleType.CROSS_COLUMN]: QualityDimension.CONSISTENCY,
  [QualityRuleType.FRESHNESS]: QualityDimension.TIMELINESS,
  [QualityRuleType.CUSTOM_SQL]: QualityDimension.ACCURACY,
  [QualityRuleType.SCHEMA_DRIFT]: QualityDimension.CONSISTENCY,
  [QualityRuleType.GROUP_CHECK]: QualityDimension.COMPLETENESS,
  [QualityRuleType.AGGREGATE]: QualityDimension.ACCURACY,
  [QualityRuleType.CROSS_TABLE]: QualityDimension.CONSISTENCY,
  [QualityRuleType.CEL_EXPRESSION]: QualityDimension.ACCURACY,
};

// ---- form state ----

export type ScheduleUnit = "minutes" | "hours" | "days";
export type NullMetric = "count" | "percent";

/** One clause of the rule's AND filter, with values kept as typed text. */
export interface FilterClauseForm {
  column: string;
  op: QualityFilterOperator;
  /** Comma- or newline-separated; unused by IS_NULL / IS_NOT_NULL. */
  values: string;
}

/**
 * The union of every config arm's fields, all as strings. Only the fields of
 * the selected `ruleType` are rendered and read; the rest keep their defaults so
 * switching the type does not have to preserve stale values.
 */
export interface RuleConfigForm {
  // row_count
  minRows?: string;
  maxRows?: string;
  exactRows?: string;
  warnMinRows?: string;
  warnMaxRows?: string;
  // row_count_change
  maxIncreasePct?: string;
  maxDecreasePct?: string;
  warnIncreasePct?: string;
  warnDecreasePct?: string;
  // null_percent
  nullMetric?: NullMetric;
  nullColumn?: string;
  maxNullCount?: string;
  warnNullCount?: string;
  maxNullPct?: string;
  warnNullPct?: string;
  // unique / unique_combination / accepted_values / range / regex / length
  column?: string;
  countNulls?: boolean;
  failThreshold?: string;
  warnThreshold?: string;
  columns?: string;
  allColumns?: boolean;
  values?: string;
  allowNull?: boolean;
  minValue?: string;
  maxValue?: string;
  inclusive?: boolean;
  pattern?: string;
  minLen?: string;
  maxLen?: string;
  // referential_integrity
  childColumn?: string;
  parentTable?: string;
  parentColumn?: string;
  // cross_column
  leftColumn?: string;
  rightColumn?: string;
  crossOperator?: QualityComparisonOperator;
  tolerance?: string;
  // freshness
  timestampColumn?: string;
  maxLagSeconds?: string;
  warnLagSeconds?: string;
  // custom_sql
  sql?: string;
  // cel_expression
  celExpression?: string;
  celColumns?: string;
  celSampleRows?: string;
  // null_percent: values that also count as missing
  nullValues?: string;
  // schema_drift
  expectedColumns?: SchemaDriftColumnForm[];
  checkTypes?: boolean;
  checkOrder?: boolean;
  allowExtraColumns?: boolean;
  // group_check
  groupColumns?: string;
  minRowsPerGroup?: string;
  maxRowsPerGroup?: string;
  // aggregate
  aggregateColumn?: string;
  aggregateFunction?: QualityAggregateFunction;
  percentile?: string;
  aggMin?: string;
  aggMax?: string;
  aggWarnMin?: string;
  aggWarnMax?: string;
  aggInclusive?: boolean;
  // cross_table
  rightTable?: string;
  leftAggFunction?: QualityAggregateFunction;
  leftAggColumn?: string;
  rightAggFunction?: QualityAggregateFunction;
  rightAggColumn?: string;
}

/** One expected column of a schema-drift snapshot. */
export interface SchemaDriftColumnForm {
  name: string;
  type: string;
  /**
   * The column's nullability in the snapshot. Unset means the rule does not
   * judge nullability for this column, which is what a snapshot saved before the
   * comparison existed means; an explicit value asserts the expected bit.
   */
  nullable?: boolean;
}

export interface RuleFormState {
  title: string;
  description: string;
  dimension: QualityDimension;
  ruleType: QualityRuleType;
  severity: QualitySeverity;
  state: QualityRuleState;
  /** Format: users/{user}; empty means no owner. */
  owner: string;
  /** Comma-separated. */
  tags: string;
  thresholdMetric: QualityThresholdMetric;
  onEmpty: QualityEmptyBehavior;
  filter: FilterClauseForm[];
  config: RuleConfigForm;
}

/** What the target is, as the editor and the request need it. */
export interface TargetSelection {
  /** The metadata GUID of the target table (`instance;database;schema;object`). */
  guid: string;
  metaType: MetaType;
  path: {
    instance: string;
    database: string;
    schema: string;
    object: string;
  };
  /** Every column the target's config reads; empty for table-level rules. */
  columns: string[];
}

/** What the target browser knows about one of the target's columns. */
export interface TargetColumnDetail {
  name: string;
  type: string;
  nullable: boolean;
}

/** A form plus its target: the unit the update mask compares. */
export interface RuleFormSnapshot {
  form: RuleFormState;
  target: TargetSelection | undefined;
}

/** A validation message, as an i18n key plus its parameters. */
export interface ValidationIssue {
  key: string;
  params?: Record<string, string | number>;
}

/** The target types the editor accepts. */
export const TARGET_META_TYPES: MetaType[] = [
  MetaType.TABLE,
  MetaType.VIEW,
  MetaType.MATERIALIZED_VIEW,
];

// ---- defaults ----

/** The dimension a rule type usually measures, offered as the default. */
export function suggestedDimension(
  ruleType: QualityRuleType
): QualityDimension {
  return DIMENSION_BY_RULE_TYPE[ruleType] ?? QualityDimension.UNSPECIFIED;
}

/** True when the shared threshold metric applies to this rule type. */
export function isThresholdMetricType(ruleType: QualityRuleType): boolean {
  return THRESHOLD_METRIC_RULE_TYPES.includes(ruleType);
}

/** A cleared filter clause, at the operator a clause usually starts with. */
export function emptyFilterClause(): FilterClauseForm {
  return { column: "", op: FilterOperator.EQ, values: "" };
}

/**
 * The empty config for a rule type; a new type never inherits old values. Every
 * field starts as a concrete blank (or its default) rather than `undefined`, so
 * the controls are controlled and a round trip through `ruleToForm` compares
 * equal without special-casing absent properties.
 */
export function emptyConfig(): RuleConfigForm {
  return {
    minRows: "",
    maxRows: "",
    exactRows: "",
    warnMinRows: "",
    warnMaxRows: "",
    maxIncreasePct: "",
    maxDecreasePct: "",
    warnIncreasePct: "",
    warnDecreasePct: "",
    nullMetric: "percent",
    nullColumn: "",
    maxNullCount: "",
    warnNullCount: "",
    maxNullPct: "",
    warnNullPct: "",
    column: "",
    countNulls: false,
    failThreshold: "",
    warnThreshold: "",
    columns: "",
    allColumns: false,
    values: "",
    allowNull: false,
    minValue: "",
    maxValue: "",
    inclusive: false,
    pattern: "",
    minLen: "",
    maxLen: "",
    childColumn: "",
    parentTable: "",
    parentColumn: "",
    leftColumn: "",
    rightColumn: "",
    crossOperator: QualityComparisonOperator.EQ,
    tolerance: "",
    timestampColumn: "",
    maxLagSeconds: "",
    warnLagSeconds: "",
    sql: "",
    celExpression: "",
    celColumns: "",
    celSampleRows: String(DEFAULT_CEL_SAMPLE_ROWS),
    nullValues: "",
    expectedColumns: [],
    checkTypes: true,
    checkOrder: false,
    allowExtraColumns: false,
    groupColumns: "",
    minRowsPerGroup: "",
    maxRowsPerGroup: "",
    aggregateColumn: "",
    aggregateFunction: QualityAggregateFunction.SUM,
    percentile: "",
    aggMin: "",
    aggMax: "",
    aggWarnMin: "",
    aggWarnMax: "",
    aggInclusive: false,
    rightTable: "",
    leftAggFunction: QualityAggregateFunction.COUNT,
    leftAggColumn: "",
    rightAggFunction: QualityAggregateFunction.COUNT,
    rightAggColumn: "",
  };
}

/** A new rule's form: enabled, weekly-ish defaults, no target yet. */
export function emptyRuleForm(
  ruleType: QualityRuleType = QualityRuleType.ROW_COUNT
): RuleFormState {
  return {
    title: "",
    description: "",
    dimension: suggestedDimension(ruleType),
    ruleType,
    severity: QualitySeverity.WARNING,
    state: QualityRuleState.ENABLED,
    owner: "",
    tags: "",
    thresholdMetric: QualityThresholdMetric.COUNT,
    onEmpty: QualityEmptyBehavior.NOT_EVALUATED,
    filter: [],
    config: emptyConfig(),
  };
}

// ---- schedule ----

/** The form's schedule as a protobuf Duration. */
export function scheduleToDuration(schedule: {
  value: string;
  unit: ScheduleUnit;
}): Duration {
  const minutes = scheduleMinutes(schedule);
  return create(DurationSchema, {
    seconds: BigInt(Math.max(0, minutes ?? 0) * 60),
    nanos: 0,
  });
}

/** The schedule's length in whole minutes, or undefined when unusable. */
export function scheduleMinutes(schedule: {
  value: string;
  unit: ScheduleUnit;
}): number | undefined {
  const amount = Number(schedule.value.trim());
  if (!Number.isFinite(amount) || !Number.isInteger(amount) || amount <= 0) {
    return undefined;
  }
  const factor =
    schedule.unit === "minutes" ? 1 : schedule.unit === "hours" ? 60 : 1440;
  return amount * factor;
}

/**
 * Client-side checks for a target's schedule interval, as catalog keys. The
 * target dialog owns this value now; rule forms carry no schedule.
 */
export function validateScheduleInterval(schedule: {
  value: string;
  unit: ScheduleUnit;
}): ValidationIssue[] {
  const minutes = scheduleMinutes(schedule);
  if (
    minutes === undefined ||
    minutes < MIN_SCHEDULE_MINUTES ||
    minutes > MAX_SCHEDULE_MINUTES
  ) {
    return [{ key: VALIDATION_PREFIX + "scheduleInvalid" }];
  }
  return [];
}

/** A stored Duration as the largest whole unit the form can show. */
export function durationToSchedule(duration: Duration | undefined): {
  value: string;
  unit: ScheduleUnit;
} {
  const seconds = duration
    ? Number(duration.seconds) + duration.nanos / 1_000_000_000
    : 0;
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return { value: "", unit: "minutes" };
  }
  for (const [unit, size] of [
    ["days", 86_400],
    ["hours", 3_600],
    ["minutes", 60],
  ] as const) {
    if (seconds % size === 0) {
      return { value: String(seconds / size), unit };
    }
  }
  return { value: String(Math.round(seconds / 60)), unit: "minutes" };
}

// ---- target ----

/**
 * The stored target as the editor's selection. An incomplete target (no GUID or
 * no object segment) is undefined rather than a half-filled form.
 */
export function targetSelectionFromRule(
  rule: QualityRule
): TargetSelection | undefined {
  const target = rule.target;
  if (!target?.metaGuid) {
    return undefined;
  }
  return {
    guid: target.metaGuid,
    metaType: target.metaType,
    path: {
      instance: target.path?.instance ?? "",
      database: target.path?.database ?? "",
      schema: target.path?.schema ?? "",
      object: target.path?.object ?? "",
    },
    columns: [...target.columns],
  };
}

/**
 * The target a prefill query names: `?table_guid=inst;db;schema;table&meta_type=4`.
 * The path is derived from the GUID segments; the browser fills in display names
 * when the target is picked rather than prefilled.
 */
export function targetFromQuery(
  tableGuid: string | null | undefined,
  metaType: string | number | null | undefined
): TargetSelection | undefined {
  const guid = (tableGuid ?? "").trim();
  if (!guid) {
    return undefined;
  }
  const segments = guid.split(";");
  if (segments.length < 4 || !segments[3]) {
    return undefined;
  }
  const parsedMetaType = Number(metaType);
  return {
    guid,
    metaType: TARGET_META_TYPES.includes(parsedMetaType)
      ? parsedMetaType
      : MetaType.TABLE,
    path: {
      instance: segments[0],
      database: segments[1],
      schema: segments[2],
      object: segments[3],
    },
    columns: [],
  };
}

/**
 * The protobuf target for a selection. `path.column` is filled only for a
 * single-column rule; the config's column is authoritative either way.
 */
export function buildTarget(selection: TargetSelection): QualityTarget {
  const columns = uniqueValues(selection.columns);
  return create(QualityTargetSchema, {
    metaGuid: selection.guid,
    metaType: selection.metaType,
    path: create(QualityTargetPathSchema, {
      instance: selection.path.instance,
      database: selection.path.database,
      schema: selection.path.schema,
      object: selection.path.object,
      column: columns.length === 1 ? columns[0] : "",
    }),
    columns,
  });
}

/**
 * The columns a config reads, in the order the target's snapshot should carry
 * them. Table-level rules return none.
 */
export function columnsForConfig(form: RuleFormState): string[] {
  const c = form.config;
  switch (form.ruleType) {
    case QualityRuleType.NULL_PERCENT:
      return uniqueValues([c.nullColumn ?? ""]);
    case QualityRuleType.UNIQUE:
    case QualityRuleType.ACCEPTED_VALUES:
    case QualityRuleType.RANGE:
    case QualityRuleType.REGEX:
    case QualityRuleType.STRING_LENGTH:
      return uniqueValues([c.column ?? ""]);
    case QualityRuleType.UNIQUE_COMBINATION:
      return c.allColumns ? [] : uniqueValues(parseValueList(c.columns ?? ""));
    case QualityRuleType.REFERENTIAL_INTEGRITY:
      return uniqueValues([c.childColumn ?? ""]);
    case QualityRuleType.CROSS_COLUMN:
      return uniqueValues([c.leftColumn ?? "", c.rightColumn ?? ""]);
    case QualityRuleType.FRESHNESS:
      return uniqueValues([c.timestampColumn ?? ""]);
    case QualityRuleType.SCHEMA_DRIFT:
      return uniqueValues(
        (c.expectedColumns ?? []).map((column) => column.name)
      );
    case QualityRuleType.GROUP_CHECK:
      return uniqueValues(parseValueList(c.groupColumns ?? ""));
    case QualityRuleType.AGGREGATE:
      return uniqueValues([c.aggregateColumn ?? ""]);
    case QualityRuleType.CEL_EXPRESSION:
      return uniqueValues(parseValueList(c.celColumns ?? ""));
    case QualityRuleType.CROSS_TABLE:
      // Only the target's own columns belong in the snapshot; the right side
      // reads another table and is validated against that table at the API.
      return uniqueValues([c.leftAggColumn ?? ""]);
    default:
      return [];
  }
}

/**
 * Fills in an expected column's nullability from the target's current columns,
 * by exact column name. A row whose nullability is already explicit is left
 * alone; an unmatched name keeps it unset, so the rule does not judge a column
 * it cannot see. The editor runs this after loading a rule so a save writes
 * explicit values instead of a snapshot that re-judges as drift.
 */
export function resolveExpectedColumnNullability(
  expectedColumns: SchemaDriftColumnForm[] | undefined,
  targetColumns: readonly TargetColumnDetail[]
): SchemaDriftColumnForm[] | undefined {
  if (expectedColumns === undefined) {
    return undefined;
  }
  const byName = new Map(
    targetColumns.map((column) => [column.name, column.nullable])
  );
  return expectedColumns.map((column) => {
    if (column.nullable !== undefined) {
      return column;
    }
    const nullable = byName.get(column.name);
    return nullable === undefined ? column : { ...column, nullable };
  });
}

// ---- form → proto ----

/** The rule request built from the form, its target and (on edit) its base. */
export function formToRule(
  form: RuleFormState,
  options: { base?: QualityRule; target?: TargetSelection } = {}
): QualityRule {
  const rule = create(QualityRuleSchema, options.base);
  rule.title = form.title.trim();
  rule.description = form.description.trim();
  rule.dimension = form.dimension;
  rule.ruleType = form.ruleType;
  rule.severity = form.severity;
  rule.state = form.state;
  rule.owner = form.owner.trim();
  rule.tags = uniqueValues(parseValueList(form.tags));
  rule.config = buildRuleConfig(form);
  if (options.target) {
    rule.target = buildTarget({
      ...options.target,
      columns: columnsForConfig(form),
    });
  }
  return rule;
}

/**
 * The rule type whose config arm a oneof case names. Used to rebuild a form from
 * a message that was not built here (a suggested draft's config).
 */
export function ruleTypeForConfigCase(
  configCase: string
): QualityRuleType | undefined {
  switch (configCase) {
    case "rowCount":
      return QualityRuleType.ROW_COUNT;
    case "rowCountChange":
      return QualityRuleType.ROW_COUNT_CHANGE;
    case "nullPercent":
      return QualityRuleType.NULL_PERCENT;
    case "unique":
      return QualityRuleType.UNIQUE;
    case "uniqueCombination":
      return QualityRuleType.UNIQUE_COMBINATION;
    case "acceptedValues":
      return QualityRuleType.ACCEPTED_VALUES;
    case "range":
      return QualityRuleType.RANGE;
    case "regex":
      return QualityRuleType.REGEX;
    case "stringLength":
      return QualityRuleType.STRING_LENGTH;
    case "referentialIntegrity":
      return QualityRuleType.REFERENTIAL_INTEGRITY;
    case "crossColumn":
      return QualityRuleType.CROSS_COLUMN;
    case "freshness":
      return QualityRuleType.FRESHNESS;
    case "customSql":
      return QualityRuleType.CUSTOM_SQL;
    case "celExpression":
      return QualityRuleType.CEL_EXPRESSION;
    case "schemaDrift":
      return QualityRuleType.SCHEMA_DRIFT;
    case "groupCheck":
      return QualityRuleType.GROUP_CHECK;
    case "aggregate":
      return QualityRuleType.AGGREGATE;
    case "crossTable":
      return QualityRuleType.CROSS_TABLE;
    default:
      return undefined;
  }
}

/** The typed config message for the selected rule type. */
export function buildRuleConfig(form: RuleFormState): QualityRuleConfig {
  const c = form.config;
  const shared = {
    thresholdMetric: form.thresholdMetric,
    onEmpty: form.onEmpty,
    ...(form.filter.some((clause) => clause.column.trim())
      ? { filter: buildFilter(form.filter) }
      : {}),
  };
  const configArm = (() => {
    switch (form.ruleType) {
      case QualityRuleType.ROW_COUNT:
        return {
          case: "rowCount" as const,
          value: create(RowCountConfigSchema, {
            ...optionalInt64("minRows", c.minRows),
            ...optionalInt64("maxRows", c.maxRows),
            ...optionalInt64("exactRows", c.exactRows),
            ...optionalInt64("warnMinRows", c.warnMinRows),
            ...optionalInt64("warnMaxRows", c.warnMaxRows),
          }),
        };
      case QualityRuleType.ROW_COUNT_CHANGE:
        return {
          case: "rowCountChange" as const,
          value: create(RowCountChangeConfigSchema, {
            ...optionalDouble("maxIncreasePct", c.maxIncreasePct),
            ...optionalDouble("maxDecreasePct", c.maxDecreasePct),
            ...optionalDouble("warnIncreasePct", c.warnIncreasePct),
            ...optionalDouble("warnDecreasePct", c.warnDecreasePct),
          }),
        };
      case QualityRuleType.NULL_PERCENT:
        return {
          case: "nullPercent" as const,
          value: create(NullPercentConfigSchema, {
            column: trimmed(c.nullColumn),
            nullValues: parseValueList(c.nullValues ?? ""),
            ...(c.nullMetric === "count"
              ? {
                  ...optionalInt64("maxNullCount", c.maxNullCount),
                  ...optionalInt64("warnNullCount", c.warnNullCount),
                }
              : {
                  ...optionalDouble("maxNullPct", c.maxNullPct),
                  ...optionalDouble("warnNullPct", c.warnNullPct),
                }),
          }),
        };
      case QualityRuleType.UNIQUE:
        return {
          case: "unique" as const,
          value: create(UniqueConfigSchema, {
            column: trimmed(c.column),
            countNulls: c.countNulls ?? false,
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.UNIQUE_COMBINATION:
        return {
          case: "uniqueCombination" as const,
          value: create(UniqueCombinationConfigSchema, {
            columns: parseValueList(c.columns ?? ""),
            allColumns: c.allColumns ?? false,
            countNulls: c.countNulls ?? false,
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.ACCEPTED_VALUES:
        return {
          case: "acceptedValues" as const,
          value: create(AcceptedValuesConfigSchema, {
            column: trimmed(c.column),
            values: parseValueList(c.values ?? ""),
            allowNull: c.allowNull ?? false,
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.RANGE:
        return {
          case: "range" as const,
          value: create(RangeConfigSchema, {
            column: trimmed(c.column),
            ...optionalString("min", c.minValue),
            ...optionalString("max", c.maxValue),
            inclusive: c.inclusive ?? false,
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.REGEX:
        return {
          case: "regex" as const,
          value: create(RegexConfigSchema, {
            column: trimmed(c.column),
            pattern: trimmed(c.pattern),
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.STRING_LENGTH:
        return {
          case: "stringLength" as const,
          value: create(StringLengthConfigSchema, {
            column: trimmed(c.column),
            ...optionalInt64("minLen", c.minLen),
            ...optionalInt64("maxLen", c.maxLen),
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.REFERENTIAL_INTEGRITY:
        return {
          case: "referentialIntegrity" as const,
          value: create(ReferentialIntegrityConfigSchema, {
            childColumn: trimmed(c.childColumn),
            parentTable: trimmed(c.parentTable),
            parentColumn: trimmed(c.parentColumn),
            allowNull: c.allowNull ?? false,
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.CROSS_COLUMN:
        return {
          case: "crossColumn" as const,
          value: create(CrossColumnConfigSchema, {
            leftColumn: trimmed(c.leftColumn),
            rightColumn: trimmed(c.rightColumn),
            op: c.crossOperator ?? QualityComparisonOperator.EQ,
            ...optionalDouble("tolerance", c.tolerance),
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.FRESHNESS:
        return {
          case: "freshness" as const,
          value: create(FreshnessConfigSchema, {
            timestampColumn: trimmed(c.timestampColumn),
            maxLagSeconds: optionalInt32(c.maxLagSeconds) ?? 0,
            ...optionalInt32Prop("warnLagSeconds", c.warnLagSeconds),
          }),
        };
      case QualityRuleType.CUSTOM_SQL:
        return {
          case: "customSql" as const,
          value: create(CustomSQLConfigSchema, {
            sql: (c.sql ?? "").trim(),
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.CEL_EXPRESSION:
        return {
          case: "celExpression" as const,
          value: create(CelExpressionConfigSchema, {
            expression: (c.celExpression ?? "").trim(),
            columns: parseValueList(c.celColumns ?? ""),
            sampleRows:
              optionalIntegerValue(c.celSampleRows) ?? DEFAULT_CEL_SAMPLE_ROWS,
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.SCHEMA_DRIFT:
        return {
          case: "schemaDrift" as const,
          value: create(SchemaDriftConfigSchema, {
            expectedColumns: (c.expectedColumns ?? [])
              .filter((column) => column.name.trim())
              .map((column) =>
                create(SchemaDriftColumnSchema, {
                  name: column.name.trim(),
                  type: column.type.trim(),
                  // Unset means "do not judge nullability": leave the field off
                  // the wire rather than asserting a false it never recorded.
                  ...(column.nullable === undefined
                    ? {}
                    : { nullable: column.nullable }),
                })
              ),
            // Unset means true; only an explicit false needs to travel.
            ...(c.checkTypes === false ? { checkTypes: false } : {}),
            checkOrder: c.checkOrder ?? false,
            allowExtraColumns: c.allowExtraColumns ?? false,
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.GROUP_CHECK:
        return {
          case: "groupCheck" as const,
          value: create(GroupCheckConfigSchema, {
            columns: parseValueList(c.groupColumns ?? ""),
            ...optionalInt64("minRowsPerGroup", c.minRowsPerGroup),
            ...optionalInt64("maxRowsPerGroup", c.maxRowsPerGroup),
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      case QualityRuleType.AGGREGATE:
        return {
          case: "aggregate" as const,
          value: create(AggregateConfigSchema, {
            column: trimmed(c.aggregateColumn),
            function: c.aggregateFunction ?? QualityAggregateFunction.SUM,
            ...optionalDouble("percentile", c.percentile),
            ...optionalDouble("min", c.aggMin),
            ...optionalDouble("max", c.aggMax),
            ...optionalDouble("warnMin", c.aggWarnMin),
            ...optionalDouble("warnMax", c.aggWarnMax),
            inclusive: c.aggInclusive ?? false,
          }),
        };
      case QualityRuleType.CROSS_TABLE:
        return {
          case: "crossTable" as const,
          value: create(CrossTableConfigSchema, {
            rightTable: trimmed(c.rightTable),
            leftFunction: c.leftAggFunction ?? QualityAggregateFunction.COUNT,
            leftColumn:
              (c.leftAggFunction ?? QualityAggregateFunction.COUNT) ===
              QualityAggregateFunction.COUNT
                ? ""
                : trimmed(c.leftAggColumn),
            rightFunction: c.rightAggFunction ?? QualityAggregateFunction.COUNT,
            rightColumn:
              (c.rightAggFunction ?? QualityAggregateFunction.COUNT) ===
              QualityAggregateFunction.COUNT
                ? ""
                : trimmed(c.rightAggColumn),
            ...optionalDouble("tolerance", c.tolerance),
            ...optionalDouble("failThreshold", c.failThreshold),
            ...optionalDouble("warnThreshold", c.warnThreshold),
          }),
        };
      default:
        return {
          case: "rowCount" as const,
          value: create(RowCountConfigSchema, {}),
        };
    }
  })();
  return create(QualityRuleConfigSchema, { config: configArm, ...shared });
}

/** The AND filter message for the given clauses. */
export function buildFilter(clauses: FilterClauseForm[]) {
  return create(QualityFilterSchema, {
    clauses: clauses
      .filter((clause) => clause.column.trim())
      .map((clause) =>
        create(QualityFilterClauseSchema, {
          column: clause.column.trim(),
          op: clause.op,
          values: filterOperatorNeedsValues(clause.op)
            ? parseValueList(clause.values)
            : [],
        })
      ),
  });
}

/** True when a filter operator takes a value list. */
export function filterOperatorNeedsValues(op: QualityFilterOperator): boolean {
  return op !== FilterOperator.IS_NULL && op !== FilterOperator.IS_NOT_NULL;
}

// ---- proto → form ----

/** The editor's form for a stored rule. */
export function ruleToForm(rule: QualityRule): RuleFormState {
  const form = emptyRuleForm(rule.ruleType);
  form.title = rule.title;
  form.description = rule.description;
  form.dimension = rule.dimension;
  form.ruleType = rule.ruleType;
  form.severity = rule.severity;
  form.state = rule.state;
  form.owner = rule.owner;
  form.tags = rule.tags.join(", ");
  // UNSPECIFIED means COUNT on the wire; normalize it so the select never opens
  // blank for a legacy rule or a draft that did not set the metric. A PERCENT
  // value survives only where the rule type actually reads the shared metric; a
  // stored PERCENT elsewhere would be refused on the next save.
  form.thresholdMetric =
    rule.config?.thresholdMetric === QualityThresholdMetric.PERCENT &&
    isThresholdMetricType(rule.ruleType)
      ? QualityThresholdMetric.PERCENT
      : QualityThresholdMetric.COUNT;
  form.onEmpty = rule.config?.onEmpty ?? form.onEmpty;
  form.filter = filterToForm(rule.config?.filter?.clauses ?? []);
  form.config = configToForm(rule);
  return form;
}

function filterToForm(
  clauses: {
    column: string;
    op: QualityFilterOperator;
    values: string[];
  }[]
): FilterClauseForm[] {
  return clauses.map((clause) => ({
    column: clause.column,
    op: clause.op,
    values: clause.values.join(", "),
  }));
}

function configToForm(rule: QualityRule): RuleConfigForm {
  const form = emptyConfig();
  const config = rule.config?.config;
  if (!config) {
    return form;
  }
  switch (config.case) {
    case "rowCount": {
      const value = config.value;
      form.minRows = optionalStringFromInt64(value.minRows);
      form.maxRows = optionalStringFromInt64(value.maxRows);
      form.exactRows = optionalStringFromInt64(value.exactRows);
      form.warnMinRows = optionalStringFromInt64(value.warnMinRows);
      form.warnMaxRows = optionalStringFromInt64(value.warnMaxRows);
      break;
    }
    case "rowCountChange": {
      const value = config.value;
      form.maxIncreasePct = optionalStringFromDouble(value.maxIncreasePct);
      form.maxDecreasePct = optionalStringFromDouble(value.maxDecreasePct);
      form.warnIncreasePct = optionalStringFromDouble(value.warnIncreasePct);
      form.warnDecreasePct = optionalStringFromDouble(value.warnDecreasePct);
      break;
    }
    case "nullPercent": {
      const value = config.value;
      form.nullColumn = value.column;
      form.nullValues = value.nullValues.join(", ");
      form.maxNullCount = optionalStringFromInt64(value.maxNullCount);
      form.warnNullCount = optionalStringFromInt64(value.warnNullCount);
      form.maxNullPct = optionalStringFromDouble(value.maxNullPct);
      form.warnNullPct = optionalStringFromDouble(value.warnNullPct);
      form.nullMetric =
        value.maxNullCount !== undefined || value.warnNullCount !== undefined
          ? "count"
          : "percent";
      break;
    }
    case "unique": {
      const value = config.value;
      form.column = value.column;
      form.countNulls = value.countNulls;
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "uniqueCombination": {
      const value = config.value;
      form.columns = value.columns.join(", ");
      form.allColumns = value.allColumns;
      form.countNulls = value.countNulls;
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "acceptedValues": {
      const value = config.value;
      form.column = value.column;
      form.values = value.values.join(", ");
      form.allowNull = value.allowNull;
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "range": {
      const value = config.value;
      form.column = value.column;
      form.minValue = value.min ?? "";
      form.maxValue = value.max ?? "";
      form.inclusive = value.inclusive;
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "regex": {
      const value = config.value;
      form.column = value.column;
      form.pattern = value.pattern;
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "stringLength": {
      const value = config.value;
      form.column = value.column;
      form.minLen = optionalStringFromInt64(value.minLen);
      form.maxLen = optionalStringFromInt64(value.maxLen);
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "referentialIntegrity": {
      const value = config.value;
      form.childColumn = value.childColumn;
      form.parentTable = value.parentTable;
      form.parentColumn = value.parentColumn;
      form.allowNull = value.allowNull;
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "crossColumn": {
      const value = config.value;
      form.leftColumn = value.leftColumn;
      form.rightColumn = value.rightColumn;
      form.crossOperator = value.op;
      form.tolerance = optionalStringFromDouble(value.tolerance);
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "freshness": {
      const value = config.value;
      form.timestampColumn = value.timestampColumn;
      form.maxLagSeconds =
        value.maxLagSeconds > 0 ? String(value.maxLagSeconds) : "";
      form.warnLagSeconds = optionalStringFromInt32(value.warnLagSeconds);
      break;
    }
    case "customSql": {
      const value = config.value;
      form.sql = value.sql;
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "celExpression": {
      const value = config.value;
      form.celExpression = value.expression;
      form.celColumns = value.columns.join(", ");
      form.celSampleRows = value.sampleRows > 0 ? String(value.sampleRows) : "";
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "schemaDrift": {
      const value = config.value;
      form.expectedColumns = value.expectedColumns.map((column) => ({
        name: column.name,
        type: column.type,
        nullable: column.nullable,
      }));
      form.checkTypes = value.checkTypes ?? true;
      form.checkOrder = value.checkOrder;
      form.allowExtraColumns = value.allowExtraColumns;
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "groupCheck": {
      const value = config.value;
      form.groupColumns = value.columns.join(", ");
      form.minRowsPerGroup = optionalStringFromInt64(value.minRowsPerGroup);
      form.maxRowsPerGroup = optionalStringFromInt64(value.maxRowsPerGroup);
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    case "aggregate": {
      const value = config.value;
      form.aggregateColumn = value.column;
      form.aggregateFunction = value.function;
      form.percentile = optionalStringFromDouble(value.percentile);
      form.aggMin = optionalStringFromDouble(value.min);
      form.aggMax = optionalStringFromDouble(value.max);
      form.aggWarnMin = optionalStringFromDouble(value.warnMin);
      form.aggWarnMax = optionalStringFromDouble(value.warnMax);
      form.aggInclusive = value.inclusive;
      break;
    }
    case "crossTable": {
      const value = config.value;
      form.rightTable = value.rightTable;
      form.leftAggFunction = value.leftFunction;
      form.leftAggColumn = value.leftColumn;
      form.rightAggFunction = value.rightFunction;
      form.rightAggColumn = value.rightColumn;
      form.tolerance = optionalStringFromDouble(value.tolerance);
      form.failThreshold = optionalStringFromDouble(value.failThreshold);
      form.warnThreshold = optionalStringFromDouble(value.warnThreshold);
      break;
    }
    default:
      break;
  }
  return form;
}

// ---- update mask ----

/**
 * The minimal update mask between the rule as loaded and as edited. Only the
 * fields that actually differ are named, so an edit that touches one label does
 * not rewrite the config, and a semantic edit names config/target (which is what
 * appends a revision server-side).
 */
export function computeUpdateMask(
  original: RuleFormSnapshot,
  next: RuleFormSnapshot
): string[] {
  const mask: string[] = [];
  const before = original.form;
  const after = next.form;
  if (trimmed(before.title) !== trimmed(after.title)) {
    mask.push("title");
  }
  if (trimmed(before.description) !== trimmed(after.description)) {
    mask.push("description");
  }
  if (before.dimension !== after.dimension) {
    mask.push("dimension");
  }
  if (before.ruleType !== after.ruleType) {
    mask.push("rule_type");
  }
  if (!sameTarget(original.target, next.target, next)) {
    mask.push("target");
  }
  // A type change implies a different config arm, so the mask has to carry the
  // new config with it; the server validates the two together.
  if (before.ruleType !== after.ruleType || !sameConfig(before, after)) {
    mask.push("config");
  }
  if (before.severity !== after.severity) {
    mask.push("severity");
  }
  if (before.state !== after.state) {
    mask.push("state");
  }
  if (trimmed(before.owner) !== trimmed(after.owner)) {
    mask.push("owner");
  }
  if (tagsKey(before.tags) !== tagsKey(after.tags)) {
    mask.push("tags");
  }
  return mask;
}

function sameTarget(
  before: TargetSelection | undefined,
  after: TargetSelection | undefined,
  afterSnapshot: RuleFormSnapshot
): boolean {
  if (!before || !after) {
    return before === after;
  }
  return (
    before.guid === after.guid &&
    before.metaType === after.metaType &&
    // The snapshot records the columns the config reads, so compare the new
    // config's columns with what the stored target actually carried.
    sameStringList(before.columns, columnsForConfig(afterSnapshot.form)) &&
    before.path.instance === after.path.instance &&
    before.path.database === after.path.database &&
    before.path.schema === after.path.schema &&
    before.path.object === after.path.object
  );
}

function sameConfig(before: RuleFormState, after: RuleFormState): boolean {
  return JSON.stringify(configKey(before)) === JSON.stringify(configKey(after));
}

/** The comparable form of everything the config message carries. */
function configKey(form: RuleFormState): unknown {
  return {
    thresholdMetric: form.thresholdMetric,
    onEmpty: form.onEmpty,
    filter: form.filter.map((clause) => ({
      column: clause.column.trim(),
      op: clause.op,
      values: filterOperatorNeedsValues(clause.op)
        ? parseValueList(clause.values)
        : [],
    })),
    config: normalizeConfigForm(form),
  };
}

/** Trimmed config values, with blanks dropped, so whitespace is not a change. */
function normalizeConfigForm(form: RuleFormState): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(form.config)) {
    if (typeof value === "string") {
      const text = value.trim();
      if (text !== "") {
        out[key] = text;
      }
    } else if (typeof value === "boolean") {
      out[key] = value;
    } else if (Array.isArray(value)) {
      // The schema-drift snapshot is the only list of rows; a row with no name
      // is not part of the rule, so it is not part of the comparison either.
      out[key] = value
        .map((row) => normalizeConfigRow(row))
        .filter((row) => row.name !== "");
    }
  }
  return out;
}

function normalizeConfigRow(row: unknown): Record<string, unknown> {
  if (!row || typeof row !== "object") {
    return { name: "" };
  }
  const record = row as Record<string, unknown>;
  return {
    name: String(record.name ?? "").trim(),
    type: String(record.type ?? "").trim(),
    // Unset and an explicit false assert different things about the column, so
    // they compare differently; otherwise toggling a previously-unset row to
    // "not nullable" would look like no change.
    nullable:
      record.nullable === undefined ? "unset" : record.nullable === true,
  };
}

function tagsKey(tags: string): string {
  return uniqueValues(parseValueList(tags)).join("\u0000");
}

// ---- validation ----

/**
 * Client-side checks, as catalog keys. The server validates the same rule again
 * (including the compiled SQL), so this is the fast, inline pass: it only
 * catches what the form can see.
 */
export function validateRuleForm(
  form: RuleFormState,
  target: TargetSelection | undefined,
  knownColumns: readonly string[] = []
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];
  const invalid = () =>
    issues.push({ key: VALIDATION_PREFIX + "numberInvalid" });
  if (!trimmed(form.title)) {
    issues.push({ key: VALIDATION_PREFIX + "titleRequired" });
  }
  if (!target) {
    issues.push({ key: VALIDATION_PREFIX + "targetRequired" });
  }
  const c = form.config;
  for (const value of [
    c.minRows,
    c.maxRows,
    c.exactRows,
    c.warnMinRows,
    c.warnMaxRows,
    c.maxNullCount,
    c.warnNullCount,
    c.minLen,
    c.maxLen,
    c.maxLagSeconds,
    c.warnLagSeconds,
    c.minRowsPerGroup,
    c.maxRowsPerGroup,
  ]) {
    if (invalidInteger(value)) {
      invalid();
    }
  }
  for (const value of [
    c.maxIncreasePct,
    c.maxDecreasePct,
    c.warnIncreasePct,
    c.warnDecreasePct,
    c.maxNullPct,
    c.warnNullPct,
    c.failThreshold,
    c.warnThreshold,
    c.tolerance,
    c.percentile,
    c.aggMin,
    c.aggMax,
    c.aggWarnMin,
    c.aggWarnMax,
  ]) {
    if (invalidNumber(value)) {
      invalid();
    }
  }
  switch (form.ruleType) {
    case QualityRuleType.ROW_COUNT:
      if (!hasAny([c.minRows, c.maxRows, c.exactRows])) {
        issues.push({ key: VALIDATION_PREFIX + "rowCountBoundRequired" });
      }
      break;
    case QualityRuleType.ROW_COUNT_CHANGE:
      if (
        !hasAny([
          c.maxIncreasePct,
          c.maxDecreasePct,
          c.warnIncreasePct,
          c.warnDecreasePct,
        ])
      ) {
        issues.push({ key: VALIDATION_PREFIX + "rowCountChangeBoundRequired" });
      }
      break;
    case QualityRuleType.NULL_PERCENT:
      if (!trimmed(c.nullColumn)) {
        issues.push({ key: VALIDATION_PREFIX + "columnRequired" });
      }
      if (
        c.nullMetric === "count"
          ? !trimmed(c.maxNullCount)
          : !trimmed(c.maxNullPct)
      ) {
        issues.push({
          key: VALIDATION_PREFIX + "nullPercentThresholdRequired",
        });
      }
      if (parseValueList(c.nullValues ?? "").length > MAX_NULL_VALUES) {
        issues.push({
          key: VALIDATION_PREFIX + "nullValuesTooMany",
          params: { max: MAX_NULL_VALUES },
        });
      }
      break;
    case QualityRuleType.UNIQUE:
      if (!trimmed(c.column)) {
        issues.push({ key: VALIDATION_PREFIX + "columnRequired" });
      }
      break;
    case QualityRuleType.UNIQUE_COMBINATION:
      if (!c.allColumns && parseValueList(c.columns ?? "").length === 0) {
        issues.push({ key: VALIDATION_PREFIX + "columnsRequired" });
      }
      break;
    case QualityRuleType.ACCEPTED_VALUES: {
      if (!trimmed(c.column)) {
        issues.push({ key: VALIDATION_PREFIX + "columnRequired" });
      }
      const values = parseValueList(c.values ?? "");
      if (values.length === 0) {
        issues.push({ key: VALIDATION_PREFIX + "valuesRequired" });
      } else if (values.length > MAX_ACCEPTED_VALUES) {
        issues.push({
          key: VALIDATION_PREFIX + "valuesTooMany",
          params: { max: MAX_ACCEPTED_VALUES },
        });
      }
      break;
    }
    case QualityRuleType.RANGE:
      if (!trimmed(c.column)) {
        issues.push({ key: VALIDATION_PREFIX + "columnRequired" });
      }
      if (!hasAny([c.minValue, c.maxValue])) {
        issues.push({ key: VALIDATION_PREFIX + "rangeBoundRequired" });
      }
      break;
    case QualityRuleType.REGEX:
      if (!trimmed(c.column)) {
        issues.push({ key: VALIDATION_PREFIX + "columnRequired" });
      }
      if (!trimmed(c.pattern)) {
        issues.push({ key: VALIDATION_PREFIX + "patternRequired" });
      }
      break;
    case QualityRuleType.STRING_LENGTH:
      if (!trimmed(c.column)) {
        issues.push({ key: VALIDATION_PREFIX + "columnRequired" });
      }
      if (!hasAny([c.minLen, c.maxLen])) {
        issues.push({ key: VALIDATION_PREFIX + "lengthBoundRequired" });
      }
      break;
    case QualityRuleType.REFERENTIAL_INTEGRITY:
      if (
        !trimmed(c.childColumn) ||
        !trimmed(c.parentTable) ||
        !trimmed(c.parentColumn)
      ) {
        issues.push({ key: VALIDATION_PREFIX + "referentialFieldsRequired" });
      }
      break;
    case QualityRuleType.CROSS_COLUMN:
      if (!trimmed(c.leftColumn) || !trimmed(c.rightColumn)) {
        issues.push({ key: VALIDATION_PREFIX + "crossColumnFieldsRequired" });
      }
      break;
    case QualityRuleType.FRESHNESS:
      if (!trimmed(c.timestampColumn)) {
        issues.push({ key: VALIDATION_PREFIX + "columnRequired" });
      }
      if (invalidInteger(c.maxLagSeconds) || !trimmed(c.maxLagSeconds)) {
        issues.push({ key: VALIDATION_PREFIX + "freshnessLagRequired" });
      }
      break;
    case QualityRuleType.CUSTOM_SQL: {
      const sql = (c.sql ?? "").trim();
      if (!sql) {
        issues.push({ key: VALIDATION_PREFIX + "sqlRequired" });
      } else if (byteLength(sql) > MAX_CUSTOM_SQL_BYTES) {
        issues.push({
          key: VALIDATION_PREFIX + "sqlTooLong",
          params: { max: MAX_CUSTOM_SQL_BYTES },
        });
      }
      break;
    }
    case QualityRuleType.CEL_EXPRESSION: {
      const expression = (c.celExpression ?? "").trim();
      if (!expression) {
        issues.push({ key: VALIDATION_PREFIX + "celExpressionRequired" });
      } else if (byteLength(expression) > MAX_CEL_EXPRESSION_BYTES) {
        issues.push({
          key: VALIDATION_PREFIX + "celExpressionTooLong",
          params: { max: MAX_CEL_EXPRESSION_BYTES },
        });
      }
      const columns = parseValueList(c.celColumns ?? "");
      if (columns.length === 0) {
        issues.push({ key: VALIDATION_PREFIX + "celColumnsRequired" });
      } else if (columns.length > MAX_CEL_COLUMNS) {
        issues.push({
          key: VALIDATION_PREFIX + "celColumnsTooMany",
          params: { max: MAX_CEL_COLUMNS },
        });
      }
      for (const column of columns) {
        if (knownColumns.length > 0 && !knownColumns.includes(column)) {
          issues.push({
            key: VALIDATION_PREFIX + "celColumnUnknown",
            params: { column },
          });
        }
      }
      if (c.celSampleRows !== undefined && c.celSampleRows.trim() !== "") {
        const sampleRows = optionalIntegerValue(c.celSampleRows);
        if (
          sampleRows === undefined ||
          sampleRows < 0 ||
          sampleRows > MAX_CEL_SAMPLE_ROWS
        ) {
          issues.push({
            key: VALIDATION_PREFIX + "celSampleRowsInvalid",
            params: { max: MAX_CEL_SAMPLE_ROWS },
          });
        }
      }
      break;
    }
    case QualityRuleType.SCHEMA_DRIFT: {
      const columns = c.expectedColumns ?? [];
      if (!columns.some((column) => column.name.trim())) {
        issues.push({ key: VALIDATION_PREFIX + "schemaDriftColumnRequired" });
      }
      const seen = new Set<string>();
      for (const column of columns) {
        const name = column.name.trim();
        if (!name) {
          continue;
        }
        if (seen.has(name.toLowerCase())) {
          issues.push({
            key: VALIDATION_PREFIX + "schemaDriftDuplicateColumn",
            params: { name },
          });
        }
        seen.add(name.toLowerCase());
      }
      break;
    }
    case QualityRuleType.GROUP_CHECK: {
      const columns = parseValueList(c.groupColumns ?? "");
      if (columns.length === 0) {
        issues.push({ key: VALIDATION_PREFIX + "groupColumnsRequired" });
      } else if (columns.length > MAX_GROUP_COLUMNS) {
        issues.push({
          key: VALIDATION_PREFIX + "groupColumnsTooMany",
          params: { max: MAX_GROUP_COLUMNS },
        });
      }
      if (!hasAny([c.minRowsPerGroup, c.maxRowsPerGroup])) {
        issues.push({ key: VALIDATION_PREFIX + "groupBoundRequired" });
      }
      break;
    }
    case QualityRuleType.AGGREGATE: {
      if (!trimmed(c.aggregateColumn)) {
        issues.push({ key: VALIDATION_PREFIX + "aggregateColumnRequired" });
      }
      if (!hasAny([c.aggMin, c.aggMax])) {
        issues.push({ key: VALIDATION_PREFIX + "aggregateBoundRequired" });
      }
      const min = optionalNumberValue(c.aggMin);
      const max = optionalNumberValue(c.aggMax);
      if (min !== undefined && max !== undefined && min > max) {
        issues.push({ key: VALIDATION_PREFIX + "aggregateRangeOrder" });
      }
      const warnMin = optionalNumberValue(c.aggWarnMin);
      const warnMax = optionalNumberValue(c.aggWarnMax);
      if (
        (warnMin !== undefined && min !== undefined && warnMin < min) ||
        (warnMax !== undefined && max !== undefined && warnMax > max) ||
        (warnMin !== undefined && warnMax !== undefined && warnMin > warnMax)
      ) {
        issues.push({ key: VALIDATION_PREFIX + "aggregateWarnRange" });
      }
      if (c.aggregateFunction === QualityAggregateFunction.PERCENTILE) {
        const percentile = optionalNumberValue(c.percentile);
        if (percentile === undefined || percentile < 0 || percentile > 100) {
          issues.push({ key: VALIDATION_PREFIX + "aggregatePercentileRange" });
        }
      }
      break;
    }
    case QualityRuleType.CROSS_TABLE: {
      const right = trimmed(c.rightTable);
      if (!right) {
        issues.push({
          key: VALIDATION_PREFIX + "crossTableRightTableRequired",
        });
      } else if (!isRightTableShape(right)) {
        issues.push({ key: VALIDATION_PREFIX + "crossTableRightTableShape" });
      }
      const leftCount =
        (c.leftAggFunction ?? QualityAggregateFunction.COUNT) ===
        QualityAggregateFunction.COUNT;
      const rightCount =
        (c.rightAggFunction ?? QualityAggregateFunction.COUNT) ===
        QualityAggregateFunction.COUNT;
      if (leftCount && trimmed(c.leftAggColumn)) {
        issues.push({ key: VALIDATION_PREFIX + "crossTableColumnForbidden" });
      }
      if (rightCount && trimmed(c.rightAggColumn)) {
        issues.push({ key: VALIDATION_PREFIX + "crossTableColumnForbidden" });
      }
      if (!leftCount && !trimmed(c.leftAggColumn)) {
        issues.push({ key: VALIDATION_PREFIX + "crossTableColumnRequired" });
      }
      if (!rightCount && !trimmed(c.rightAggColumn)) {
        issues.push({ key: VALIDATION_PREFIX + "crossTableColumnRequired" });
      }
      const tolerance = optionalNumberValue(c.tolerance);
      if (tolerance !== undefined && tolerance < 0) {
        issues.push({ key: VALIDATION_PREFIX + "crossTableToleranceInvalid" });
      }
      break;
    }
    default:
      break;
  }
  const activeClauses = form.filter.filter((clause) => clause.column.trim());
  if (activeClauses.length > MAX_FILTER_CLAUSES) {
    issues.push({
      key: VALIDATION_PREFIX + "filterTooMany",
      params: { max: MAX_FILTER_CLAUSES },
    });
  }
  for (const clause of activeClauses) {
    if (
      !filterOperatorNeedsValues(clause.op) &&
      parseValueList(clause.values).length === 0
    ) {
      continue;
    }
    if (
      filterOperatorNeedsValues(clause.op) &&
      parseValueList(clause.values).length > MAX_FILTER_VALUES
    ) {
      issues.push({
        key: VALIDATION_PREFIX + "filterValuesTooMany",
        params: { max: MAX_FILTER_VALUES },
      });
    }
  }
  return issues;
}

/** Every validation key lives under this prefix (see DYNAMIC_PREFIXES). */
export const VALIDATION_PREFIX = "dataQuality.editor.validation.";

/** A right table is `table` or `schema.table` (one dot, no empty segment). */
function isRightTableShape(value: string): boolean {
  const parts = value.split(".");
  return parts.length <= 2 && parts.every((part) => part.trim().length > 0);
}

// ---- shared helpers ----

/** A comma- or newline-separated list, trimmed, blanks dropped, duplicates kept in order. */
export function parseValueList(text: string): string[] {
  return uniqueValues(
    text
      .split(/[,\n]/)
      .map((item) => item.trim())
      .filter(Boolean)
  );
}

/** Trimmed text. */
export function trimmed(value: string | undefined): string {
  return (value ?? "").trim();
}

/** True when the text is non-empty and not a finite number. */
export function invalidNumber(value: string | undefined): boolean {
  const text = trimmed(value);
  return text !== "" && !Number.isFinite(Number(text));
}

/** True when the text is non-empty and not a whole number. */
export function invalidInteger(value: string | undefined): boolean {
  const text = trimmed(value);
  return text !== "" && !isIntegerText(text);
}

function isIntegerText(text: string): boolean {
  return /^-?\d+$/.test(text);
}

function uniqueValues(values: string[]): string[] {
  return [...new Set(values.map((value) => value.trim()).filter(Boolean))];
}

function hasAny(values: Array<string | undefined>): boolean {
  return values.some((value) => trimmed(value) !== "");
}

function sameStringList(left: string[], right: string[]): boolean {
  return (
    left.length === right.length &&
    left.every((value, index) => value === right[index])
  );
}

/** UTF-8 byte length without allocating a TextEncoder per call site. */
function byteLength(text: string): number {
  return new TextEncoder().encode(text).length;
}

function optionalString(key: string, value: string | undefined) {
  const text = trimmed(value);
  return text === "" ? {} : { [key]: text };
}

function optionalDouble(key: string, value: string | undefined) {
  const number = optionalNumberValue(value);
  return number === undefined ? {} : { [key]: number };
}

function optionalInt64(key: string, value: string | undefined) {
  const number = optionalIntegerValue(value);
  return number === undefined ? {} : { [key]: BigInt(number) };
}

function optionalInt32Prop(key: string, value: string | undefined) {
  const number = optionalIntegerValue(value);
  return number === undefined ? {} : { [key]: number };
}

function optionalNumberValue(value: string | undefined): number | undefined {
  const text = trimmed(value);
  if (text === "") {
    return undefined;
  }
  const number = Number(text);
  return Number.isFinite(number) ? number : undefined;
}

function optionalIntegerValue(value: string | undefined): number | undefined {
  const text = trimmed(value);
  if (!isIntegerText(text)) {
    return undefined;
  }
  return Number(text);
}

function optionalInt32(value: string | undefined): number | undefined {
  return optionalIntegerValue(value);
}

function optionalStringFromInt64(value: bigint | undefined): string {
  return value === undefined ? "" : String(value);
}

function optionalStringFromInt32(value: number | undefined): string {
  return value === undefined ? "" : String(value);
}

function optionalStringFromDouble(value: number | undefined): string {
  return value === undefined ? "" : String(value);
}
