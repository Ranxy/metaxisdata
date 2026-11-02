import {
  QualityRuleType,
  QualityStandardCode,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  emptyConfig,
  emptyRuleForm,
  type RuleConfigForm,
  type RuleFormState,
} from "@/utils/dataQualityForm";

/**
 * The built-in rule templates: ready-made checks a reader can start from
 * instead of filling an empty form. A template is a check type plus the config
 * fields it pre-fills; applying one edits the form and never saves it.
 *
 * The catalog is plain data (no Vue, no Pinia, no request) so its completeness
 * can be asserted without mounting the composer. Custom templates are a later
 * phase; this build ships the read-only built-ins only.
 */

export interface RuleTemplate {
  /** Stable id; the composer marks the button with it for tests. */
  id: string;
  /** The template's name, as an i18n key. */
  titleKey: string;
  /** One line on what the template checks, as an i18n key. */
  descriptionKey: string;
  ruleType: QualityRuleType;
  /** The config fields the template fills; the rest keep their defaults. */
  fields: Partial<RuleConfigForm>;
}

/** One section of the template catalogue: an i18n heading and its templates. */
export interface RuleTemplateGroup {
  id: string;
  labelKey: string;
  templates: RuleTemplate[];
}

/** One standard-code template per standard the check understands. */
const STANDARD_CODE_TEMPLATES: RuleTemplate[] = [
  {
    id: "unifiedSocialCreditCode",
    titleKey: "dataQuality.ruleTemplate.unifiedSocialCreditCode.title",
    descriptionKey:
      "dataQuality.ruleTemplate.unifiedSocialCreditCode.description",
    ruleType: QualityRuleType.STANDARD_CODE,
    fields: {
      standard: QualityStandardCode.UNIFIED_SOCIAL_CREDIT_CODE,
      sampleRows: "0",
    },
  },
  {
    id: "organizationCode",
    titleKey: "dataQuality.ruleTemplate.organizationCode.title",
    descriptionKey: "dataQuality.ruleTemplate.organizationCode.description",
    ruleType: QualityRuleType.STANDARD_CODE,
    fields: {
      standard: QualityStandardCode.ORGANIZATION_CODE,
      sampleRows: "0",
    },
  },
  {
    id: "residentIdCard",
    titleKey: "dataQuality.ruleTemplate.residentIdCard.title",
    descriptionKey: "dataQuality.ruleTemplate.residentIdCard.description",
    ruleType: QualityRuleType.STANDARD_CODE,
    fields: {
      standard: QualityStandardCode.RESIDENT_ID_CARD,
      sampleRows: "0",
    },
  },
  {
    id: "administrativeDivision",
    titleKey: "dataQuality.ruleTemplate.administrativeDivision.title",
    descriptionKey:
      "dataQuality.ruleTemplate.administrativeDivision.description",
    ruleType: QualityRuleType.STANDARD_CODE,
    fields: {
      standard: QualityStandardCode.ADMINISTRATIVE_DIVISION,
      sampleRows: "0",
    },
  },
  {
    id: "industryClassification",
    titleKey: "dataQuality.ruleTemplate.industryClassification.title",
    descriptionKey:
      "dataQuality.ruleTemplate.industryClassification.description",
    ruleType: QualityRuleType.STANDARD_CODE,
    fields: {
      standard: QualityStandardCode.INDUSTRY_CLASSIFICATION,
      sampleRows: "0",
    },
  },
];

/**
 * The generic presets that fit the same shape. They only set bounds or a shape
 * the reader would otherwise have to type; none of them guesses a column.
 */
const GENERIC_TEMPLATES: RuleTemplate[] = [
  {
    id: "primaryKeyUniqueness",
    titleKey: "dataQuality.ruleTemplate.primaryKeyUniqueness.title",
    descriptionKey: "dataQuality.ruleTemplate.primaryKeyUniqueness.description",
    ruleType: QualityRuleType.UNIQUE,
    fields: { countNulls: false, failThreshold: "0" },
  },
  {
    id: "timestampFreshness",
    titleKey: "dataQuality.ruleTemplate.timestampFreshness.title",
    descriptionKey: "dataQuality.ruleTemplate.timestampFreshness.description",
    ruleType: QualityRuleType.FRESHNESS,
    fields: { maxLagSeconds: "86400" },
  },
  {
    id: "emailFormat",
    titleKey: "dataQuality.ruleTemplate.emailFormat.title",
    descriptionKey: "dataQuality.ruleTemplate.emailFormat.description",
    ruleType: QualityRuleType.REGEX,
    fields: { pattern: "^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$", failThreshold: "0" },
  },
];

/** The template catalogue, in the order the composer lists it. */
export const RULE_TEMPLATE_GROUPS: RuleTemplateGroup[] = [
  {
    id: "standardCode",
    labelKey: "dataQuality.composer.templateGroup.standardCode",
    templates: STANDARD_CODE_TEMPLATES,
  },
  {
    id: "preset",
    labelKey: "dataQuality.composer.templateGroup.preset",
    templates: GENERIC_TEMPLATES,
  },
];

/** Every built-in template, flattened across the groups. */
export const RULE_TEMPLATES: RuleTemplate[] = RULE_TEMPLATE_GROUPS.flatMap(
  (group) => group.templates
);

/**
 * The form a template prefills: the check type's own defaults with the
 * template's fields written over them. It is a form edit, not a save.
 */
export function templateForm(template: RuleTemplate): RuleFormState {
  const form = emptyRuleForm(template.ruleType);
  form.config = { ...emptyConfig(), ...template.fields };
  return form;
}
