import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  QualityDimension,
  QualityFilterClauseSchema,
  QualityFilterOperator,
  QualityFilterSchema,
  QualityRuleConfigSchema,
  QualityRuleState,
  QualityRuleTemplateSchema,
  QualityRuleType,
  QualitySeverity,
  QualityStandardCode,
  SchemaDriftColumnSchema,
  SchemaDriftConfigSchema,
  StandardCodeConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { emptyRuleForm, type RuleFormState } from "@/utils/dataQualityForm";
import {
  formToRuleTemplate,
  ruleTemplateToForm,
  templateDescription,
  templateTitle,
  templateToCatalogueItem,
} from "@/utils/dataQualityRuleTemplate";
import { RULE_TEMPLATES } from "@/utils/dataQualityTemplate";

function standardTemplate() {
  return create(QualityRuleTemplateSchema, {
    name: "workspaces/ws/qualityRuleTemplates/t1",
    title: "Credit code",
    description: "Validates a municipal credit code.",
    ruleType: QualityRuleType.STANDARD_CODE,
    severity: QualitySeverity.CRITICAL,
    dimension: QualityDimension.VALIDITY,
    config: create(QualityRuleConfigSchema, {
      config: {
        case: "standardCode",
        value: create(StandardCodeConfigSchema, {
          column: "credit_code",
          standard: QualityStandardCode.CUSTOM_CODE_TABLE,
          codeTable: "workspaces/ws/qualityCodeTables/municipal",
          allowNull: true,
          sampleRows: 500,
        }),
      },
      filter: create(QualityFilterSchema, {
        clauses: [
          create(QualityFilterClauseSchema, {
            column: "region",
            op: QualityFilterOperator.EQ,
            values: ["eu"],
          }),
        ],
      }),
    }),
  });
}

function driftTemplate() {
  return create(QualityRuleTemplateSchema, {
    name: "workspaces/ws/qualityRuleTemplates/t2",
    title: "Orders shape",
    ruleType: QualityRuleType.SCHEMA_DRIFT,
    config: create(QualityRuleConfigSchema, {
      config: {
        case: "schemaDrift",
        value: create(SchemaDriftConfigSchema, {
          expectedColumns: [
            create(SchemaDriftColumnSchema, {
              name: "id",
              type: "bigint",
              nullable: false,
            }),
          ],
          checkTypes: false,
        }),
      },
    }),
  });
}

describe("ruleTemplateToForm", () => {
  it("fills the editor with the stored type, parameters and grading", () => {
    const form = ruleTemplateToForm(standardTemplate());

    expect(form.title).toBe("Credit code");
    expect(form.description).toBe("Validates a municipal credit code.");
    expect(form.ruleType).toBe(QualityRuleType.STANDARD_CODE);
    expect(form.severity).toBe(QualitySeverity.CRITICAL);
    expect(form.dimension).toBe(QualityDimension.VALIDITY);
    // A template is applied to an enabled check; state is not stored on it.
    expect(form.state).toBe(QualityRuleState.ENABLED);
    expect(form.config.standard).toBe(QualityStandardCode.CUSTOM_CODE_TABLE);
    expect(form.config.codeTable).toBe(
      "workspaces/ws/qualityCodeTables/municipal"
    );
    expect(form.config.allowNull).toBe(true);
    expect(form.config.sampleRows).toBe("500");
  });

  it("clears the target-bound column and filter a template never carries", () => {
    const form = ruleTemplateToForm(standardTemplate());
    expect(form.config.column).toBe("");
    expect(form.filter).toEqual([]);
  });

  it("clears a schema-drift snapshot, which names the target's columns", () => {
    const form = ruleTemplateToForm(driftTemplate());
    expect(form.config.expectedColumns).toEqual([]);
    // The non-target-bound checks survive.
    expect(form.config.checkTypes).toBe(false);
  });

  it("survives a template whose config is missing", () => {
    const form = ruleTemplateToForm(
      create(QualityRuleTemplateSchema, {
        name: "workspaces/ws/qualityRuleTemplates/t3",
        title: "Empty",
        ruleType: QualityRuleType.ROW_COUNT,
      })
    );
    expect(form.ruleType).toBe(QualityRuleType.ROW_COUNT);
    expect(form.config.minRows).toBe("");
  });
});

describe("templateToCatalogueItem", () => {
  it("maps a stored template onto the catalogue shape", () => {
    const template = standardTemplate();
    const item = templateToCatalogueItem(template);
    expect(item.id).toBe(template.name);
    expect(item.ruleType).toBe(QualityRuleType.STANDARD_CODE);
    expect(item.title).toBe("Credit code");
    expect(item.description).toBe("Validates a municipal credit code.");
    expect(item.fields).toEqual({});
    expect(item.titleKey).toBe("");
    expect(item.descriptionKey).toBe("");
    expect(item.template).toBe(template);
  });
});

describe("templateTitle and templateDescription", () => {
  const translate = (key: string) => `t:${key}`;

  it("prefers a stored template's own copy", () => {
    const item = templateToCatalogueItem(standardTemplate());
    expect(templateTitle(item, translate)).toBe("Credit code");
    expect(templateDescription(item, translate)).toBe(
      "Validates a municipal credit code."
    );
  });

  it("falls back to the i18n keys of a built-in", () => {
    const builtIn = RULE_TEMPLATES[0];
    if (!builtIn) {
      throw new Error("expected a built-in template");
    }
    expect(templateTitle(builtIn, translate)).toBe(`t:${builtIn.titleKey}`);
    expect(templateDescription(builtIn, translate)).toBe(
      `t:${builtIn.descriptionKey}`
    );
  });

  it("treats blank stored copy as absent", () => {
    const item = templateToCatalogueItem(
      create(QualityRuleTemplateSchema, {
        name: "workspaces/ws/qualityRuleTemplates/t4",
        title: "  ",
        description: "  ",
        ruleType: QualityRuleType.UNIQUE,
      })
    );
    expect(templateTitle(item, translate)).toBe("t:");
    expect(templateDescription(item, translate)).toBe("t:");
  });
});

describe("formToRuleTemplate", () => {
  function form(): RuleFormState {
    const value = emptyRuleForm(QualityRuleType.UNIQUE);
    value.title = "Unique email";
    value.description = "One account per address.";
    value.severity = QualitySeverity.ERROR;
    value.dimension = QualityDimension.UNIQUENESS;
    value.config.column = "email";
    value.config.countNulls = true;
    value.config.failThreshold = "0";
    return value;
  }

  it("carries the form's type, config, severity and dimension", () => {
    const template = formToRuleTemplate(form(), {
      title: "  Email uniqueness  ",
      description: "  One account per address.  ",
    });

    expect(template.title).toBe("Email uniqueness");
    expect(template.description).toBe("One account per address.");
    expect(template.ruleType).toBe(QualityRuleType.UNIQUE);
    expect(template.severity).toBe(QualitySeverity.ERROR);
    expect(template.dimension).toBe(QualityDimension.UNIQUENESS);
    if (template.config?.config.case !== "unique") {
      throw new Error("expected a unique config arm");
    }
    expect(template.config.config.value.column).toBe("email");
    expect(template.config.config.value.countNulls).toBe(true);
    expect(template.config.config.value.failThreshold).toBe(0);
  });

  it("defaults the description to empty", () => {
    const template = formToRuleTemplate(form(), { title: "Email uniqueness" });
    expect(template.description).toBe("");
    // No target is attached: a template names no table.
    expect(template.name).toBe("");
  });

  it("round-trips through the editor form", () => {
    const template = formToRuleTemplate(form(), { title: "Email uniqueness" });
    const reopened = ruleTemplateToForm(template);
    expect(reopened.ruleType).toBe(QualityRuleType.UNIQUE);
    expect(reopened.severity).toBe(QualitySeverity.ERROR);
    // The column is target-bound, so a template never carries it back.
    expect(reopened.config.column).toBe("");
    expect(reopened.config.countNulls).toBe(true);
    expect(reopened.title).toBe("Email uniqueness");
  });
});
