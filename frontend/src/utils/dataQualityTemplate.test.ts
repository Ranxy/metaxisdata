import { describe, expect, it } from "vitest";
import enUS from "@/locales/en-US.json";
import zhCN from "@/locales/zh-CN.json";
import {
  QualityRuleType,
  QualityStandardCode,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  emptyConfig,
  emptyRuleForm,
  suggestedDimension,
} from "@/utils/dataQualityForm";
import {
  RULE_TEMPLATE_GROUPS,
  RULE_TEMPLATES,
  type RuleTemplate,
  templateForm,
} from "./dataQualityTemplate";

const STANDARD_TEMPLATES = RULE_TEMPLATES.filter(
  (template) => template.ruleType === QualityRuleType.STANDARD_CODE
);

const STANDARDS = [
  QualityStandardCode.UNIFIED_SOCIAL_CREDIT_CODE,
  QualityStandardCode.ORGANIZATION_CODE,
  QualityStandardCode.RESIDENT_ID_CARD,
  QualityStandardCode.ADMINISTRATIVE_DIVISION,
  QualityStandardCode.INDUSTRY_CLASSIFICATION,
];

function label(catalog: unknown, key: string): unknown {
  return key.split(".").reduce<unknown>((node, part) => {
    if (typeof node !== "object" || node === null) {
      return undefined;
    }
    return (node as Record<string, unknown>)[part];
  }, catalog);
}

describe("rule template catalog", () => {
  it("lists each template once under a labeled group", () => {
    expect(RULE_TEMPLATE_GROUPS.length).toBeGreaterThan(0);
    for (const group of RULE_TEMPLATE_GROUPS) {
      expect(group.id).not.toBe("");
      expect(group.labelKey.startsWith("dataQuality.composer.")).toBe(true);
      expect(group.templates.length).toBeGreaterThan(0);
    }
    const ids = RULE_TEMPLATES.map((template) => template.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(new Set(RULE_TEMPLATES)).toEqual(
      new Set(RULE_TEMPLATE_GROUPS.flatMap((group) => group.templates))
    );
    for (const template of RULE_TEMPLATES) {
      expect(template.titleKey).toMatch(
        /^dataQuality\.ruleTemplate\..+\.title$/
      );
      expect(template.descriptionKey).toMatch(
        /^dataQuality\.ruleTemplate\..+\.description$/
      );
    }
  });

  it("ships one standard-code template per standard", () => {
    expect(STANDARD_TEMPLATES).toHaveLength(STANDARDS.length);
    expect(
      STANDARD_TEMPLATES.map((template) => template.fields.standard)
    ).toEqual(STANDARDS);
    for (const template of STANDARD_TEMPLATES) {
      // 0 asks the server for its default sample size.
      expect(template.fields.sampleRows).toBe("0");
    }
  });

  it("covers the generic presets the editor can offer", () => {
    const types = new Set(RULE_TEMPLATES.map((template) => template.ruleType));
    expect(types).toEqual(
      new Set([
        QualityRuleType.STANDARD_CODE,
        QualityRuleType.UNIQUE,
        QualityRuleType.FRESHNESS,
        QualityRuleType.REGEX,
      ])
    );
  });

  it("defines every template's title and description in both locales", () => {
    for (const [locale, catalog] of Object.entries({
      "en-US": enUS,
      "zh-CN": zhCN,
    })) {
      for (const template of RULE_TEMPLATES) {
        expect(
          label(catalog, template.titleKey),
          `${locale} ${template.id}`
        ).toEqual(expect.any(String));
        expect(
          label(catalog, template.descriptionKey),
          `${locale} ${template.id}`
        ).toEqual(expect.any(String));
      }
    }
  });
});

describe("templateForm", () => {
  it("writes the template's fields over the check type's defaults", () => {
    const template: RuleTemplate = {
      id: "sample",
      titleKey: "dataQuality.ruleTemplate.primaryKeyUniqueness.title",
      descriptionKey:
        "dataQuality.ruleTemplate.primaryKeyUniqueness.description",
      ruleType: QualityRuleType.STANDARD_CODE,
      fields: {
        standard: QualityStandardCode.ORGANIZATION_CODE,
        sampleRows: "250",
      },
    };
    const form = templateForm(template);
    expect(form.ruleType).toBe(QualityRuleType.STANDARD_CODE);
    expect(form.dimension).toBe(
      suggestedDimension(QualityRuleType.STANDARD_CODE)
    );
    expect(form.config).toEqual({
      ...emptyConfig(),
      standard: QualityStandardCode.ORGANIZATION_CODE,
      sampleRows: "250",
    });
  });

  it("leaves the template untouched and every other check type at its defaults", () => {
    const template = RULE_TEMPLATES.find(
      (candidate) => candidate.ruleType === QualityRuleType.FRESHNESS
    );
    if (!template) {
      throw new Error("expected a freshness preset");
    }
    const before = { ...template.fields };
    const form = templateForm(template);
    expect(template.fields).toEqual(before);
    expect(form.config.maxLagSeconds).toBe("86400");
    expect(form.config.column).toBe(emptyConfig().column);
    expect(form.title).toBe(emptyRuleForm(QualityRuleType.FRESHNESS).title);
  });
});
