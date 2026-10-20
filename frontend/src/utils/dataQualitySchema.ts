import {
  QualityAggregateFunction,
  QualityRuleType,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  parseValueList,
  type RuleFormState,
  type TargetSelection,
} from "@/utils/dataQualityForm";

/**
 * The rule form's field metadata: which config fields exist for each rule type,
 * which are required on their own, and which belong to an "at least one of"
 * group. The editor renders the asterisks and runs the pre-flight check from
 * this catalog, so the required-field knowledge lives in one place instead of
 * being spread through the template and the server call.
 *
 * The groups mirror the server-side checks in
 * `backend/component/dataquality/validate.go`; the numbers/conditional arms
 * (null metric, percentile, cross-table aggregate columns) are handled in
 * `missingRequiredFields` because they depend on another field's value.
 */

export type FieldKind =
  | "text"
  | "number"
  | "integer"
  | "list"
  | "boolean"
  | "sql"
  | "code"
  | "columns";

export interface RuleFieldSpec {
  /** The `RuleConfigForm` field name the form binds. */
  field: string;
  /** The editor's existing label key. */
  labelKey: string;
  kind: FieldKind;
  /** This field alone is required. */
  required?: boolean;
  /** Group id: at least one field in the group must be filled. */
  requiredOneOf?: string;
}

export interface RuleFieldSchema {
  ruleType: QualityRuleType;
  fields: RuleFieldSpec[];
}

export interface CommonFieldSpec {
  /** "target", "title" or "schedule" — not a config field. */
  field: string;
  labelKey: string;
  required: true;
}

/** Fields every rule needs, whatever its type. */
export const COMMON_REQUIRED_FIELDS: CommonFieldSpec[] = [
  {
    field: "target",
    labelKey: "dataQuality.editor.targetTitle",
    required: true,
  },
  { field: "title", labelKey: "dataQuality.editor.title", required: true },
  {
    field: "schedule",
    labelKey: "dataQuality.editor.schedule",
    required: true,
  },
];

/** The config fields of one rule type. */
export const RULE_SCHEMAS: Record<number, RuleFieldSchema> = {
  [QualityRuleType.ROW_COUNT]: {
    ruleType: QualityRuleType.ROW_COUNT,
    fields: [
      {
        field: "minRows",
        labelKey: "dataQuality.editor.minRows",
        kind: "integer",
        requiredOneOf: "rowCountBound",
      },
      {
        field: "maxRows",
        labelKey: "dataQuality.editor.maxRows",
        kind: "integer",
        requiredOneOf: "rowCountBound",
      },
      {
        field: "exactRows",
        labelKey: "dataQuality.editor.exactRows",
        kind: "integer",
        requiredOneOf: "rowCountBound",
      },
      {
        field: "warnMinRows",
        labelKey: "dataQuality.editor.warnMinRows",
        kind: "integer",
      },
      {
        field: "warnMaxRows",
        labelKey: "dataQuality.editor.warnMaxRows",
        kind: "integer",
      },
    ],
  },
  [QualityRuleType.ROW_COUNT_CHANGE]: {
    ruleType: QualityRuleType.ROW_COUNT_CHANGE,
    fields: [
      {
        field: "maxIncreasePct",
        labelKey: "dataQuality.editor.maxIncreasePct",
        kind: "number",
        requiredOneOf: "rowCountChangeBound",
      },
      {
        field: "maxDecreasePct",
        labelKey: "dataQuality.editor.maxDecreasePct",
        kind: "number",
        requiredOneOf: "rowCountChangeBound",
      },
      {
        field: "warnIncreasePct",
        labelKey: "dataQuality.editor.warnIncreasePct",
        kind: "number",
        requiredOneOf: "rowCountChangeBound",
      },
      {
        field: "warnDecreasePct",
        labelKey: "dataQuality.editor.warnDecreasePct",
        kind: "number",
        requiredOneOf: "rowCountChangeBound",
      },
    ],
  },
  [QualityRuleType.NULL_PERCENT]: {
    ruleType: QualityRuleType.NULL_PERCENT,
    fields: [
      {
        field: "nullColumn",
        labelKey: "dataQuality.editor.column",
        kind: "columns",
        required: true,
      },
      {
        field: "maxNullCount",
        labelKey: "dataQuality.editor.maxNullCount",
        kind: "integer",
      },
      {
        field: "maxNullPct",
        labelKey: "dataQuality.editor.maxNullPct",
        kind: "number",
      },
    ],
  },
  [QualityRuleType.UNIQUE]: {
    ruleType: QualityRuleType.UNIQUE,
    fields: [
      {
        field: "column",
        labelKey: "dataQuality.editor.column",
        kind: "columns",
        required: true,
      },
    ],
  },
  [QualityRuleType.UNIQUE_COMBINATION]: {
    ruleType: QualityRuleType.UNIQUE_COMBINATION,
    fields: [
      {
        field: "columns",
        labelKey: "dataQuality.editor.columns",
        kind: "list",
        required: true,
      },
      {
        field: "allColumns",
        labelKey: "dataQuality.editor.allColumns",
        kind: "boolean",
      },
    ],
  },
  [QualityRuleType.ACCEPTED_VALUES]: {
    ruleType: QualityRuleType.ACCEPTED_VALUES,
    fields: [
      {
        field: "column",
        labelKey: "dataQuality.editor.column",
        kind: "columns",
        required: true,
      },
      {
        field: "values",
        labelKey: "dataQuality.editor.values",
        kind: "list",
        required: true,
      },
    ],
  },
  [QualityRuleType.RANGE]: {
    ruleType: QualityRuleType.RANGE,
    fields: [
      {
        field: "column",
        labelKey: "dataQuality.editor.column",
        kind: "columns",
        required: true,
      },
      {
        field: "minValue",
        labelKey: "dataQuality.editor.minValue",
        kind: "text",
        requiredOneOf: "rangeBound",
      },
      {
        field: "maxValue",
        labelKey: "dataQuality.editor.maxValue",
        kind: "text",
        requiredOneOf: "rangeBound",
      },
    ],
  },
  [QualityRuleType.REGEX]: {
    ruleType: QualityRuleType.REGEX,
    fields: [
      {
        field: "column",
        labelKey: "dataQuality.editor.column",
        kind: "columns",
        required: true,
      },
      {
        field: "pattern",
        labelKey: "dataQuality.editor.pattern",
        kind: "text",
        required: true,
      },
    ],
  },
  [QualityRuleType.STRING_LENGTH]: {
    ruleType: QualityRuleType.STRING_LENGTH,
    fields: [
      {
        field: "column",
        labelKey: "dataQuality.editor.column",
        kind: "columns",
        required: true,
      },
      {
        field: "minLen",
        labelKey: "dataQuality.editor.minLen",
        kind: "integer",
        requiredOneOf: "lengthBound",
      },
      {
        field: "maxLen",
        labelKey: "dataQuality.editor.maxLen",
        kind: "integer",
        requiredOneOf: "lengthBound",
      },
    ],
  },
  [QualityRuleType.REFERENTIAL_INTEGRITY]: {
    ruleType: QualityRuleType.REFERENTIAL_INTEGRITY,
    fields: [
      {
        field: "childColumn",
        labelKey: "dataQuality.editor.childColumn",
        kind: "columns",
        required: true,
      },
      {
        field: "parentTable",
        labelKey: "dataQuality.editor.parentTable",
        kind: "text",
        required: true,
      },
      {
        field: "parentColumn",
        labelKey: "dataQuality.editor.parentColumn",
        kind: "columns",
        required: true,
      },
    ],
  },
  [QualityRuleType.CROSS_COLUMN]: {
    ruleType: QualityRuleType.CROSS_COLUMN,
    fields: [
      {
        field: "leftColumn",
        labelKey: "dataQuality.editor.leftColumn",
        kind: "columns",
        required: true,
      },
      {
        field: "rightColumn",
        labelKey: "dataQuality.editor.rightColumn",
        kind: "columns",
        required: true,
      },
    ],
  },
  [QualityRuleType.FRESHNESS]: {
    ruleType: QualityRuleType.FRESHNESS,
    fields: [
      {
        field: "timestampColumn",
        labelKey: "dataQuality.editor.timestampColumn",
        kind: "columns",
        required: true,
      },
      {
        field: "maxLagSeconds",
        labelKey: "dataQuality.editor.maxLagSeconds",
        kind: "integer",
        required: true,
      },
    ],
  },
  [QualityRuleType.CUSTOM_SQL]: {
    ruleType: QualityRuleType.CUSTOM_SQL,
    fields: [
      {
        field: "sql",
        labelKey: "dataQuality.editor.sql",
        kind: "sql",
        required: true,
      },
    ],
  },
  [QualityRuleType.CEL_EXPRESSION]: {
    ruleType: QualityRuleType.CEL_EXPRESSION,
    fields: [
      {
        field: "celExpression",
        labelKey: "dataQuality.editor.celExpression",
        kind: "code",
        required: true,
      },
      {
        field: "celColumns",
        labelKey: "dataQuality.editor.celColumns",
        kind: "list",
        required: true,
      },
    ],
  },
  [QualityRuleType.SCHEMA_DRIFT]: {
    ruleType: QualityRuleType.SCHEMA_DRIFT,
    fields: [
      {
        field: "expectedColumns",
        labelKey: "dataQuality.editor.expectedColumns",
        kind: "columns",
        required: true,
      },
    ],
  },
  [QualityRuleType.GROUP_CHECK]: {
    ruleType: QualityRuleType.GROUP_CHECK,
    fields: [
      {
        field: "groupColumns",
        labelKey: "dataQuality.editor.groupColumns",
        kind: "list",
        required: true,
      },
      {
        field: "minRowsPerGroup",
        labelKey: "dataQuality.editor.minRowsPerGroup",
        kind: "integer",
        requiredOneOf: "groupBound",
      },
      {
        field: "maxRowsPerGroup",
        labelKey: "dataQuality.editor.maxRowsPerGroup",
        kind: "integer",
        requiredOneOf: "groupBound",
      },
    ],
  },
  [QualityRuleType.AGGREGATE]: {
    ruleType: QualityRuleType.AGGREGATE,
    fields: [
      {
        field: "aggregateColumn",
        labelKey: "dataQuality.editor.column",
        kind: "columns",
        required: true,
      },
      {
        field: "aggMin",
        labelKey: "dataQuality.editor.minValue",
        kind: "number",
        requiredOneOf: "aggregateBound",
      },
      {
        field: "aggMax",
        labelKey: "dataQuality.editor.maxValue",
        kind: "number",
        requiredOneOf: "aggregateBound",
      },
    ],
  },
  [QualityRuleType.CROSS_TABLE]: {
    ruleType: QualityRuleType.CROSS_TABLE,
    fields: [
      {
        field: "rightTable",
        labelKey: "dataQuality.editor.rightTable",
        kind: "text",
        required: true,
      },
      {
        field: "leftAggColumn",
        labelKey: "dataQuality.editor.leftColumn",
        kind: "columns",
      },
      {
        field: "rightAggColumn",
        labelKey: "dataQuality.editor.rightColumn",
        kind: "columns",
      },
    ],
  },
};

export interface MissingField {
  /** The form field to focus; a group reports its first field. */
  field: string;
  /** The message key when this is a single field. */
  labelKey?: string;
  /** The fields of an "at least one of" group. */
  oneOfKeys?: string[];
}

/** The schema of a rule type, if this build knows the type. */
export function ruleFieldSchema(
  ruleType: QualityRuleType
): RuleFieldSchema | undefined {
  return RULE_SCHEMAS[ruleType];
}

/** The config fields of a rule type, empty for an unknown one. */
export function ruleFields(ruleType: QualityRuleType): RuleFieldSpec[] {
  return RULE_SCHEMAS[ruleType]?.fields ?? [];
}

/** Whether one config field is a single required field. */
export function isFieldRequired(
  ruleType: QualityRuleType,
  field: string
): boolean {
  return ruleFields(ruleType).some(
    (spec) => spec.field === field && spec.required === true
  );
}

/** The "at least one of" groups of a rule type, each with its field specs. */
export function oneOfGroups(
  ruleType: QualityRuleType
): Map<string, RuleFieldSpec[]> {
  const groups = new Map<string, RuleFieldSpec[]>();
  for (const spec of ruleFields(ruleType)) {
    if (!spec.requiredOneOf) {
      continue;
    }
    const group = groups.get(spec.requiredOneOf) ?? [];
    group.push(spec);
    groups.set(spec.requiredOneOf, group);
  }
  return groups;
}

function filled(value: string | undefined): boolean {
  return (value ?? "").trim() !== "";
}

function fieldIsFilled(form: RuleFormState, spec: RuleFieldSpec): boolean {
  const config = form.config as unknown as Record<string, unknown>;
  const value = config[spec.field];
  switch (spec.kind) {
    case "boolean":
      return value === true;
    case "list":
      return parseValueList(String(value ?? "")).length > 0;
    case "columns": {
      if (spec.field === "expectedColumns") {
        return (form.config.expectedColumns ?? []).some((column) =>
          column.name.trim()
        );
      }
      return filled(String(value ?? ""));
    }
    case "sql":
    case "code":
    case "text":
    case "number":
    case "integer":
    default:
      return filled(String(value ?? ""));
  }
}

/**
 * The fields a dry run or save needs but the form has not filled. Common fields
 * (target, title, schedule) come first, then the rule-type catalog, then the
 * conditional arms the catalog cannot express on its own.
 */
export function missingRequiredFields(
  form: RuleFormState,
  target: TargetSelection | undefined
): MissingField[] {
  const missing: MissingField[] = [];
  if (!target) {
    missing.push({
      field: "target",
      labelKey: "dataQuality.editor.targetTitle",
    });
  }
  if (!filled(form.title)) {
    missing.push({ field: "title", labelKey: "dataQuality.editor.title" });
  }
  if (form.schedule.value.trim() === "") {
    missing.push({
      field: "schedule",
      labelKey: "dataQuality.editor.schedule",
    });
  }

  const schema = ruleFieldSchema(form.ruleType);
  if (!schema) {
    return missing;
  }

  for (const spec of schema.fields) {
    if (spec.required && !fieldIsFilled(form, spec)) {
      // "Every column" is the valid alternative to listing them.
      if (
        form.ruleType === QualityRuleType.UNIQUE_COMBINATION &&
        spec.field === "columns" &&
        form.config.allColumns
      ) {
        continue;
      }
      missing.push({ field: spec.field, labelKey: spec.labelKey });
    }
  }

  for (const [group, specs] of oneOfGroups(form.ruleType)) {
    if (specs.every((spec) => !fieldIsFilled(form, spec))) {
      missing.push({
        field: specs[0]?.field ?? group,
        oneOfKeys: specs.map((spec) => spec.labelKey),
      });
    }
  }

  // Arms that depend on another field's value.
  const config = form.config;
  switch (form.ruleType) {
    case QualityRuleType.NULL_PERCENT: {
      const wanted =
        config.nullMetric === "count" ? config.maxNullCount : config.maxNullPct;
      const label =
        config.nullMetric === "count"
          ? "dataQuality.editor.maxNullCount"
          : "dataQuality.editor.maxNullPct";
      if (!filled(wanted)) {
        missing.push({
          field: config.nullMetric === "count" ? "maxNullCount" : "maxNullPct",
          labelKey: label,
        });
      }
      break;
    }
    case QualityRuleType.UNIQUE_COMBINATION:
      if (!config.allColumns && (config.columns ?? "").trim() === "") {
        // Already required above; this branch documents the all-columns escape.
        break;
      }
      break;
    case QualityRuleType.AGGREGATE:
      if (
        config.aggregateFunction === QualityAggregateFunction.PERCENTILE &&
        !filled(config.percentile)
      ) {
        missing.push({
          field: "percentile",
          labelKey: "dataQuality.editor.percentile",
        });
      }
      break;
    case QualityRuleType.CROSS_TABLE: {
      const leftCount =
        (config.leftAggFunction ?? QualityAggregateFunction.COUNT) ===
        QualityAggregateFunction.COUNT;
      const rightCount =
        (config.rightAggFunction ?? QualityAggregateFunction.COUNT) ===
        QualityAggregateFunction.COUNT;
      if (!leftCount && !filled(config.leftAggColumn)) {
        missing.push({
          field: "leftAggColumn",
          labelKey: "dataQuality.editor.leftColumn",
        });
      }
      if (!rightCount && !filled(config.rightAggColumn)) {
        missing.push({
          field: "rightAggColumn",
          labelKey: "dataQuality.editor.rightColumn",
        });
      }
      break;
    }
    default:
      break;
  }
  return missing;
}

/** The first DOM id to focus for a missing top-level field. */
export function missingFieldElementId(field: string): string {
  if (field === "target") {
    return "dq-target-select";
  }
  if (field === "title") {
    return "dq-rule-title";
  }
  // The long-form fields keep their original ids (the editor tests and the
  // browser focus path agree on them).
  if (field === "sql") {
    return "dq-config-sql";
  }
  if (field === "celExpression") {
    return "dq-config-cel";
  }
  if (field === "expectedColumns") {
    return "dq-config-expected-columns";
  }
  return `dq-field-${field}`;
}

// ---- compiled-SQL errors ----

export interface ParsedRuleError {
  /** The sentence without the appended compiled query. */
  message: string;
  /** The compiled SQL when the server attached it. */
  sql?: string;
  /** The bound parameters, as the server rendered them. */
  parameters?: string;
}

const COMPILED_SQL_PATTERN =
  /\(compiled SQL:\s*([\s\S]*?);\s*parameters:\s*(\[[\s\S]*?\])\s*\)\s*$/;

/**
 * Split a server rule error into its message and the compiled query it carries.
 * The server appends `(compiled SQL: …; parameters: […])`; showing that blob in
 * one line is unreadable, so the editor renders the parts as blocks instead.
 */
export function parseRuleError(error: unknown): ParsedRuleError {
  const text = error instanceof Error ? error.message : String(error ?? "");
  const match = text.match(COMPILED_SQL_PATTERN);
  if (!match) {
    return { message: text };
  }
  return {
    message: text.slice(0, match.index).trim() || text,
    sql: match[1]?.trim(),
    parameters: match[2]?.trim(),
  };
}

/** Whether the message smells like a bound-parameter problem. */
export function isParameterError(message: string): boolean {
  return /argument|parameter|placeholder|bind|type mismatch|expected \d+/i.test(
    message
  );
}
