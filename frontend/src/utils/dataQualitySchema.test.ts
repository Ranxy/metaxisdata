import { describe, expect, it } from "vitest";
import {
  QualityAggregateFunction,
  QualityRuleType,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  EDITABLE_RULE_TYPES,
  emptyRuleForm,
  type RuleFormState,
  type TargetSelection,
  validateRuleForm,
} from "./dataQualityForm";
import {
  isFieldRequired,
  isParameterError,
  missingFieldElementId,
  missingRequiredFields,
  oneOfGroups,
  parseRuleError,
  ruleFieldSchema,
  ruleFields,
} from "./dataQualitySchema";

const TARGET: TargetSelection = {
  guid: "inst1;db1;public;orders",
  metaType: MetaType.TABLE,
  path: {
    instance: "inst1",
    database: "db1",
    schema: "public",
    object: "orders",
  },
  columns: [],
};

function completeForm(ruleType: QualityRuleType): RuleFormState {
  const form = emptyRuleForm(ruleType);
  form.title = "Rule";
  for (const spec of ruleFields(ruleType)) {
    const config = form.config as unknown as Record<string, unknown>;
    switch (spec.kind) {
      case "boolean":
        config[spec.field] = true;
        break;
      case "list":
        config[spec.field] = spec.field === "celColumns" ? "qty" : "amount";
        break;
      case "columns":
        if (spec.field === "expectedColumns") {
          form.config.expectedColumns = [
            { name: "id", type: "bigint", nullable: false },
          ];
        } else {
          config[spec.field] = "amount";
        }
        break;
      case "integer":
        config[spec.field] = spec.field === "maxLagSeconds" ? "3600" : "1";
        break;
      case "number":
        config[spec.field] = "1";
        break;
      case "sql":
        config[spec.field] = "SELECT 1";
        break;
      case "code":
        config[spec.field] = "qty > 0";
        break;
      default:
        config[spec.field] = "public.orders";
    }
  }
  // The one-of groups are filled by the loop above; the conditional arms need
  // the fields their sibling selects.
  if (ruleType === QualityRuleType.NULL_PERCENT) {
    form.config.maxNullPct = "1";
  }
  if (ruleType === QualityRuleType.UNIQUE_COMBINATION) {
    form.config.allColumns = false;
    form.config.columns = "amount";
  }
  if (ruleType === QualityRuleType.CROSS_TABLE) {
    form.config.leftAggFunction = QualityAggregateFunction.COUNT;
    form.config.rightAggFunction = QualityAggregateFunction.COUNT;
    // COUNT needs no column, and the validator rejects one.
    form.config.leftAggColumn = "";
    form.config.rightAggColumn = "";
  }
  return form;
}

describe("rule field schema", () => {
  it("covers every editable rule type with labeled metadata", () => {
    expect(EDITABLE_RULE_TYPES).toHaveLength(18);
    for (const ruleType of EDITABLE_RULE_TYPES) {
      const schema = ruleFieldSchema(ruleType);
      expect(schema, `schema for ${ruleType}`).toBeTruthy();
      expect(schema?.fields.length).toBeGreaterThan(0);
      for (const field of schema?.fields ?? []) {
        expect(field.labelKey.startsWith("dataQuality.editor.")).toBe(true);
        expect(field.field).not.toBe("");
      }
    }
  });

  it("marks the single required fields and the one-of groups", () => {
    expect(isFieldRequired(QualityRuleType.UNIQUE, "column")).toBe(true);
    expect(isFieldRequired(QualityRuleType.ROW_COUNT, "minRows")).toBe(false);
    expect(
      [...oneOfGroups(QualityRuleType.ROW_COUNT).values()][0]?.map(
        (field) => field.field
      )
    ).toEqual(["minRows", "maxRows", "exactRows"]);
    expect(
      [...oneOfGroups(QualityRuleType.AGGREGATE).values()][0]?.map(
        (field) => field.field
      )
    ).toEqual(["aggMin", "aggMax"]);
    expect(oneOfGroups(QualityRuleType.UNIQUE).size).toBe(0);
  });

  it("reports nothing missing for a complete form of every type", () => {
    for (const ruleType of EDITABLE_RULE_TYPES) {
      const form = completeForm(ruleType);
      expect(
        missingRequiredFields(form, TARGET),
        `complete ${ruleType}`
      ).toEqual([]);
      expect(validateRuleForm(form, TARGET), `valid ${ruleType}`).toEqual([]);
    }
  });

  it("reports the common fields and the type's required fields when empty", () => {
    const form = emptyRuleForm(QualityRuleType.UNIQUE);
    form.title = "Rule";
    const missing = missingRequiredFields(form, TARGET);
    expect(missing.map((field) => field.field)).toContain("column");
    expect(
      missingRequiredFields(form, undefined).map((field) => field.field)
    ).toContain("target");
    expect(
      missingRequiredFields(emptyRuleForm(), TARGET).map((field) => field.field)
    ).toContain("title");
  });

  it("reports an unfilled one-of group by its first field", () => {
    const form = completeForm(QualityRuleType.ROW_COUNT);
    form.config.minRows = "";
    form.config.maxRows = "";
    form.config.exactRows = "";
    const missing = missingRequiredFields(form, TARGET);
    expect(
      missing.find((field) => field.field === "minRows")?.oneOfKeys
    ).toEqual([
      "dataQuality.editor.minRows",
      "dataQuality.editor.maxRows",
      "dataQuality.editor.exactRows",
    ]);
  });

  it("follows the null metric and cross-table aggregate arms", () => {
    const nullForm = completeForm(QualityRuleType.NULL_PERCENT);
    nullForm.config.nullMetric = "count";
    nullForm.config.maxNullCount = "";
    expect(
      missingRequiredFields(nullForm, TARGET).map((field) => field.field)
    ).toContain("maxNullCount");

    const cross = completeForm(QualityRuleType.CROSS_TABLE);
    cross.config.leftAggFunction = QualityAggregateFunction.SUM;
    cross.config.leftAggColumn = "";
    expect(
      missingRequiredFields(cross, TARGET).map((field) => field.field)
    ).toContain("leftAggColumn");
  });
});

describe("compiled-SQL error parsing", () => {
  it("splits the message from the compiled query and parameters", () => {
    const parsed = parseRuleError(
      new Error(
        "the rule's compiled query failed the dry run: expected 2 arguments, got 0 (compiled SQL: SELECT COUNT(*) FROM orders WHERE created_at > $1 AND status = $2; parameters: [2026-01-01, paid])"
      )
    );
    expect(parsed.message).toBe(
      "the rule's compiled query failed the dry run: expected 2 arguments, got 0"
    );
    expect(parsed.sql).toBe(
      "SELECT COUNT(*) FROM orders WHERE created_at > $1 AND status = $2"
    );
    expect(parsed.parameters).toBe("[2026-01-01, paid]");
    expect(isParameterError(parsed.message)).toBe(true);
  });

  it("keeps a plain error as its message and handles empty parameters", () => {
    expect(parseRuleError(new Error("boom"))).toEqual({ message: "boom" });
    const empty = parseRuleError(
      "failed (compiled SQL: SELECT 1; parameters: [])"
    );
    expect(empty).toEqual({
      message: "failed",
      sql: "SELECT 1",
      parameters: "[]",
    });
    expect(isParameterError("the server is unavailable")).toBe(false);
  });
});

describe("schema catalog edge cases", () => {
  it("returns empty metadata for an unknown rule type", () => {
    const unknown = 999 as QualityRuleType;
    expect(ruleFieldSchema(unknown)).toBeUndefined();
    expect(ruleFields(unknown)).toEqual([]);
    expect(isFieldRequired(unknown, "anything")).toBe(false);
    expect(oneOfGroups(unknown).size).toBe(0);
    const form = emptyRuleForm();
    form.ruleType = unknown;
    form.title = "";
    expect(
      missingRequiredFields(form, TARGET).map((field) => field.field)
    ).toEqual(["title"]);
  });

  it("maps every missing field to the element that should receive focus", () => {
    expect(missingFieldElementId("target")).toBe("dq-target-select");
    expect(missingFieldElementId("title")).toBe("dq-rule-title");
    expect(missingFieldElementId("sql")).toBe("dq-config-sql");
    expect(missingFieldElementId("celExpression")).toBe("dq-config-cel");
    expect(missingFieldElementId("expectedColumns")).toBe(
      "dq-config-expected-columns"
    );
    expect(missingFieldElementId("minRows")).toBe("dq-field-minRows");
  });

  it("honours the all-columns escape and the percentile arm", () => {
    const combination = completeForm(QualityRuleType.UNIQUE_COMBINATION);
    combination.config.columns = "";
    combination.config.allColumns = true;
    expect(
      missingRequiredFields(combination, TARGET).map((field) => field.field)
    ).not.toContain("columns");

    const aggregate = completeForm(QualityRuleType.AGGREGATE);
    aggregate.config.aggregateFunction = QualityAggregateFunction.PERCENTILE;
    aggregate.config.percentile = "";
    expect(
      missingRequiredFields(aggregate, TARGET).map((field) => field.field)
    ).toContain("percentile");
  });

  it("reports both conditional cross-table columns", () => {
    const form = completeForm(QualityRuleType.CROSS_TABLE);
    form.config.leftAggFunction = QualityAggregateFunction.AVG;
    form.config.rightAggFunction = QualityAggregateFunction.SUM;
    form.config.leftAggColumn = "";
    form.config.rightAggColumn = "";
    expect(
      missingRequiredFields(form, TARGET).map((field) => field.field)
    ).toEqual(["leftAggColumn", "rightAggColumn"]);
  });

  it("treats a blank title as missing", () => {
    const form = completeForm(QualityRuleType.ROW_COUNT);
    form.title = "  ";
    expect(
      missingRequiredFields(form, TARGET).map((field) => field.field)
    ).toEqual(["title"]);
  });

  it("handles boolean and list values in complete forms", () => {
    const form = completeForm(QualityRuleType.UNIQUE_COMBINATION);
    expect(missingRequiredFields(form, TARGET)).toEqual([]);
    form.config.columns = "amount, id";
    expect(missingRequiredFields(form, TARGET)).toEqual([]);
  });
});

describe("rule error parsing edge cases", () => {
  it("accepts strings and empty errors", () => {
    expect(parseRuleError("plain string")).toEqual({ message: "plain string" });
    expect(parseRuleError(undefined)).toEqual({ message: "" });
    expect(parseRuleError(null)).toEqual({ message: "" });
  });

  it("recognises the parameter-problem wording", () => {
    for (const message of [
      "expected 2 arguments, got 0",
      "placeholder $1 has no binding",
      "parameter type mismatch",
      "bind parameter failed",
    ]) {
      expect(isParameterError(message)).toBe(true);
    }
    expect(isParameterError("permission denied")).toBe(false);
  });
});
