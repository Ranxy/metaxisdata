import { create } from "@bufbuild/protobuf";
import {
  QualityRuleSchema,
  QualityRuleState,
  type QualityRuleTemplate,
  QualityRuleTemplateSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  buildRuleConfig,
  type RuleConfigForm,
  type RuleFormState,
  ruleToForm,
} from "@/utils/dataQualityForm";
import type { RuleTemplate } from "@/utils/dataQualityTemplate";

/**
 * The workspace rule templates as the composer's template catalogue and check
 * editor consume them.
 *
 * A stored template is a check type plus its parameters, without a target. These
 * converters keep that contract in one place: applying one edits the form (it
 * never saves or dry-runs a check), and saving the form as a template sends a
 * template-shaped message that the server stores after clearing the
 * target-bound fields.
 */

/**
 * The config fields a template must not carry: they name a column (or the
 * target's whole column list) of the table the check watches, so a template
 * cannot prefill them. The filter and the schema-drift snapshot are cleared
 * alongside them.
 */
const TARGET_BOUND_COLUMN_FIELDS = [
  "column",
  "columns",
  "nullColumn",
  "timestampColumn",
  "childColumn",
  "leftColumn",
  "rightColumn",
  "aggregateColumn",
  "leftAggColumn",
  "rightAggColumn",
  "celColumns",
  "groupColumns",
] as const satisfies readonly (keyof RuleConfigForm)[];

/**
 * The check editor's form for a stored template: its type, parameters, grading
 * and copy, with every target-bound field cleared. Reading it is not a save; the
 * caller still has to save the check, on a target of its own.
 */
export function ruleTemplateToForm(
  template: QualityRuleTemplate
): RuleFormState {
  const form = ruleToForm(
    create(QualityRuleSchema, {
      title: template.title,
      description: template.description,
      dimension: template.dimension,
      ruleType: template.ruleType,
      severity: template.severity,
      state: QualityRuleState.ENABLED,
      config: template.config,
    })
  );
  form.filter = [];
  form.config.expectedColumns = [];
  for (const field of TARGET_BOUND_COLUMN_FIELDS) {
    (form.config as unknown as Record<string, unknown>)[field] = "";
  }
  return form;
}

/**
 * The catalogue entry for a stored template. It keeps the stored message so
 * applying it can use the complete config; built-ins, which only patch a few
 * fields, carry i18n keys instead.
 */
export function templateToCatalogueItem(
  template: QualityRuleTemplate
): RuleTemplate {
  return {
    id: template.name,
    titleKey: "",
    descriptionKey: "",
    title: template.title,
    description: template.description,
    ruleType: template.ruleType,
    fields: {},
    template,
  };
}

/** The template title to show: a stored one's copy, else its i18n key. */
export function templateTitle(
  template: RuleTemplate,
  translate: (key: string) => string
): string {
  return template.title?.trim() || translate(template.titleKey);
}

/** The template description to show: a stored one's copy, else its i18n key. */
export function templateDescription(
  template: RuleTemplate,
  translate: (key: string) => string
): string {
  return template.description?.trim() || translate(template.descriptionKey);
}

/**
 * The template message a "save as template" action sends for the current form.
 * It carries the check's type, parameters, severity and dimension; the server
 * clears the target-bound fields. The check itself is not saved.
 */
export function formToRuleTemplate(
  form: RuleFormState,
  options: { title: string; description?: string }
): QualityRuleTemplate {
  return create(QualityRuleTemplateSchema, {
    title: options.title.trim(),
    description: (options.description ?? "").trim(),
    ruleType: form.ruleType,
    config: buildRuleConfig(form),
    severity: form.severity,
    dimension: form.dimension,
  });
}
