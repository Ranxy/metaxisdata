import type { QualityRuleType } from "@/types/proto-es/v1/data_quality_service_pb";
import type { TargetColumnDetail } from "@/utils/dataQualityForm";
import { isFieldRequired } from "@/utils/dataQualitySchema";

/** Props every rule-family config section shares. */
export interface RuleConfigSectionProps {
  ruleType: QualityRuleType;
  /** The target's columns; only the schema-drift snapshot reads them. */
  columnDetails?: TargetColumnDetail[];
  /** Inline errors keyed by config field, from the editor's pre-flight check. */
  fieldErrors?: Record<string, string>;
}

/** The id one config field's control gets, so the editor can focus it. */
export function fieldId(field: string): string {
  return `dq-field-${field}`;
}

/** Whether the schema marks this field required for the rule type. */
export function requiredFor(ruleType: QualityRuleType, field: string): boolean {
  return isFieldRequired(ruleType, field);
}

/** The editor's inline error for a field, or the empty string. */
export function errorFor(
  fieldErrors: Record<string, string> | undefined,
  field: string
): string {
  return fieldErrors?.[field] ?? "";
}

/** A CSV column field as the picker's array form. */
export function csvColumns(value: string | undefined): string[] {
  return (value ?? "")
    .split(",")
    .map((part) => part.trim())
    .filter(Boolean);
}

/** The picker's array back into the CSV field the config builder parses. */
export function joinCsvColumns(value: string | string[]): string {
  return Array.isArray(value) ? value.join(", ") : value;
}
