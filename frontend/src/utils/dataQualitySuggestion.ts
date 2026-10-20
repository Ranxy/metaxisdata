import { create } from "@bufbuild/protobuf";
import {
  type ColumnProfile,
  type ColumnProfileStats,
  type ColumnSemanticLabel,
  QualityDimension,
  QualityLabelSource,
  QualityLabelStatus,
  type QualityRuleDraftConfig,
  QualityRuleSchema,
  QualityRuleState,
  type QualityRuleType,
  QualitySemanticType,
  QualitySeverity,
  type QualitySuggestion,
  QualitySuggestionKind,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  type SemanticLabelEvidence,
  type SemanticLabelSuggestion,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatRowCount } from "@/utils/dataQuality";
import {
  emptyConfig,
  emptyRuleForm,
  type RuleConfigForm,
  type RuleFormState,
  ruleToForm,
  ruleTypeForConfigCase,
  suggestedDimension,
  type TargetSelection,
  targetSelectionFromRule,
} from "@/utils/dataQualityForm";
import { formatPercent } from "@/utils/dataQualityOverview";

/**
 * The suggestion queue's pure layer: the vocabulary its badges need, the
 * conversion of a suggested rule draft into an editor form, and the display of
 * column profiles and semantic labels in the asset page's quality section.
 */

// ---- suggestion vocabulary ----

/** The label key for a suggestion's kind. */
export function suggestionKindKey(kind: QualitySuggestionKind): string {
  switch (kind) {
    case QualitySuggestionKind.RULE_DRAFT:
      return "dataQuality.suggestionKind.ruleDraft";
    case QualitySuggestionKind.LABEL:
      return "dataQuality.suggestionKind.label";
    default:
      return "dataQuality.suggestionKind.unspecified";
  }
}

/** The label key for where a suggestion came from. */
export function suggestionSourceKey(source: QualitySuggestionSource): string {
  switch (source) {
    case QualitySuggestionSource.NATURAL_LANGUAGE:
      return "dataQuality.suggestionSource.naturalLanguage";
    case QualitySuggestionSource.PROFILE:
      return "dataQuality.suggestionSource.profile";
    default:
      return "dataQuality.suggestionSource.unspecified";
  }
}

/** The label key for a suggestion's decision state. */
export function suggestionStatusKey(status: QualitySuggestionStatus): string {
  switch (status) {
    case QualitySuggestionStatus.PENDING:
      return "dataQuality.suggestionStatus.pending";
    case QualitySuggestionStatus.ACCEPTED:
      return "dataQuality.suggestionStatus.accepted";
    case QualitySuggestionStatus.REJECTED:
      return "dataQuality.suggestionStatus.rejected";
    default:
      return "dataQuality.suggestionStatus.unspecified";
  }
}

/** The badge variant for a suggestion's decision state. */
export function suggestionStatusVariant(
  status: QualitySuggestionStatus
): "success" | "warning" | "neutral" | "secondary" {
  switch (status) {
    case QualitySuggestionStatus.PENDING:
      return "warning";
    case QualitySuggestionStatus.ACCEPTED:
      return "success";
    case QualitySuggestionStatus.REJECTED:
      return "neutral";
    default:
      return "secondary";
  }
}

/** The label key for where a column's accepted label came from. */
export function labelSourceKey(source: QualityLabelSource): string {
  switch (source) {
    case QualityLabelSource.MANUAL:
      return "dataQuality.labelSource.manual";
    case QualityLabelSource.LLM:
      return "dataQuality.labelSource.llm";
    default:
      return "dataQuality.labelSource.unspecified";
  }
}

/** The label key for a label's review state. */
export function labelStatusKey(status: QualityLabelStatus): string {
  switch (status) {
    case QualityLabelStatus.SUGGESTED:
      return "dataQuality.labelStatus.suggested";
    case QualityLabelStatus.ACCEPTED:
      return "dataQuality.labelStatus.accepted";
    case QualityLabelStatus.REJECTED:
      return "dataQuality.labelStatus.rejected";
    default:
      return "dataQuality.labelStatus.unspecified";
  }
}

/** The label key for a semantic type. */
export function semanticTypeKey(label: QualitySemanticType): string {
  switch (label) {
    case QualitySemanticType.EMAIL:
      return "dataQuality.semanticType.email";
    case QualitySemanticType.PHONE:
      return "dataQuality.semanticType.phone";
    case QualitySemanticType.URL:
      return "dataQuality.semanticType.url";
    case QualitySemanticType.UUID:
      return "dataQuality.semanticType.uuid";
    case QualitySemanticType.IP:
      return "dataQuality.semanticType.ip";
    case QualitySemanticType.COUNTRY_CODE:
      return "dataQuality.semanticType.countryCode";
    case QualitySemanticType.CURRENCY:
      return "dataQuality.semanticType.currency";
    case QualitySemanticType.AMOUNT:
      return "dataQuality.semanticType.amount";
    case QualitySemanticType.QUANTITY:
      return "dataQuality.semanticType.quantity";
    case QualitySemanticType.PERCENTAGE:
      return "dataQuality.semanticType.percentage";
    case QualitySemanticType.ID:
      return "dataQuality.semanticType.id";
    case QualitySemanticType.BUSINESS_KEY:
      return "dataQuality.semanticType.businessKey";
    case QualitySemanticType.TIMESTAMP:
      return "dataQuality.semanticType.timestamp";
    case QualitySemanticType.DATE:
      return "dataQuality.semanticType.date";
    case QualitySemanticType.ENUM:
      return "dataQuality.semanticType.enum";
    case QualitySemanticType.FREE_TEXT:
      return "dataQuality.semanticType.freeText";
    case QualitySemanticType.UNKNOWN:
      return "dataQuality.semanticType.unknown";
    default:
      return "dataQuality.semanticType.unspecified";
  }
}

/** The rule-draft payload of a suggestion, when it is one. */
export function ruleDraftOf(
  suggestion: QualitySuggestion
): QualityRuleDraftConfig | undefined {
  const payload = suggestion.payload?.payload;
  return payload?.case === "rule" ? payload.value : undefined;
}

/** The label payload of a suggestion, when it is one. */
export function labelSuggestionOf(
  suggestion: QualitySuggestion
): SemanticLabelSuggestion | undefined {
  const payload = suggestion.payload?.payload;
  return payload?.case === "label" ? payload.value : undefined;
}

/** Confidence as a percentage; 0/absent reads "-" rather than a fake 0%. */
export function formatConfidence(confidence: number): string {
  return confidence > 0 ? formatPercent(confidence) : "-";
}

/** The evidence lines a label suggestion cites, as plain display strings. */
export function labelEvidenceLines(
  evidence: SemanticLabelEvidence | undefined
): string[] {
  if (!evidence) {
    return [];
  }
  return [
    ...evidence.metrics,
    ...(evidence.columns.length > 0 ? [evidence.columns.join(", ")] : []),
    ...evidence.lineage,
    ...(evidence.rationale ? [evidence.rationale] : []),
  ];
}

// ---- suggestion → editor ----

export interface SuggestionPrefill {
  form: RuleFormState;
  target: TargetSelection | undefined;
  ruleType: QualityRuleType;
}

/**
 * A suggested rule draft as the editor's form. Returns undefined when the
 * suggestion is not a rule draft or carries a config this build does not know:
 * the queue then offers the raw parameters instead of opening a half-filled form.
 */
export function suggestionToEditorPrefill(
  suggestion: QualitySuggestion
): SuggestionPrefill | undefined {
  const draft = ruleDraftOf(suggestion);
  if (!draft?.config?.config.case) {
    return undefined;
  }
  const ruleType = ruleTypeForConfigCase(draft.config.config.case);
  if (ruleType === undefined) {
    return undefined;
  }
  const rule = create(QualityRuleSchema, {
    ruleType,
    config: draft.config,
    severity: draft.severity,
    title: draft.title,
    target: suggestion.target,
  });
  const form = ruleToForm(rule);
  // A draft names the check and its config, not the whole rule: fill the fields
  // the API defaults so the form opens as a valid new rule, not with blank
  // selects the user has to notice.
  if (form.dimension === QualityDimension.UNSPECIFIED) {
    form.dimension = suggestedDimension(ruleType);
  }
  if (form.state === QualityRuleState.UNSPECIFIED) {
    form.state = QualityRuleState.ENABLED;
  }
  if (form.severity === QualitySeverity.UNSPECIFIED) {
    form.severity = QualitySeverity.WARNING;
  }
  const schedule = emptyRuleForm().schedule;
  if (form.schedule.value === "") {
    form.schedule = { ...schedule };
  }
  if (draft.title) {
    form.title = draft.title;
  }
  if (draft.rationale) {
    form.description = draft.rationale;
  }
  return {
    form,
    target: targetSelectionFromRule(rule),
    ruleType,
  };
}

// ---- draft parameters ----

export interface ConfigEntry {
  /** The config field name; `dataQuality.editor.<field>` names it. */
  field: string;
  value: string;
}

/**
 * The non-default config fields of a draft, in a stable order, for the
 * suggestion drawer's parameter list. This is a technical read-back of the
 * draft, not the editor: the editor is where values are reviewed properly.
 */
export function ruleConfigEntries(config: RuleConfigForm): ConfigEntry[] {
  const defaults = emptyConfig();
  const entries: ConfigEntry[] = [];
  for (const [field, value] of Object.entries(config)) {
    const text = typeof value === "string" ? value.trim() : "";
    if (typeof value === "string" && text !== "") {
      // A field still at its blank-form default is not a draft parameter.
      if (text !== String(defaults[field as keyof RuleConfigForm] ?? "")) {
        entries.push({ field, value: text });
      }
    } else if (typeof value === "boolean" && value) {
      // A default-on toggle (`check_types` defaults to true) is not a draft
      // parameter until it is switched off.
      if (defaults[field as keyof RuleConfigForm] !== true) {
        entries.push({ field, value: "✓" });
      }
    } else if (Array.isArray(value) && value.length > 0) {
      entries.push({ field, value: String(value.length) });
    }
  }
  return entries;
}

/** The editor labels for a draft's entries; the component translates them. */
export function configEntryLabelKey(field: string): string {
  return `dataQuality.editor.${field}`;
}

// ---- column profiles and labels ----

export interface ProfileHighlight {
  key: string;
  params: Record<string, string | number>;
}

/**
 * The readable facts of a column profile: null rate, distinct count, extremes,
 * lengths and how the values were sampled. Empty metrics are omitted rather
 * than rendered as zeros.
 */
export function columnProfileHighlights(
  profile: ColumnProfile | undefined
): ProfileHighlight[] {
  const stats = profile?.profile;
  if (!stats) {
    return [];
  }
  return profileStatsHighlights(stats, profile);
}

function profileStatsHighlights(
  stats: ColumnProfileStats,
  profile: ColumnProfile
): ProfileHighlight[] {
  const highlights: ProfileHighlight[] = [];
  if (stats.totalRows > 0n) {
    highlights.push({
      key: "dataQuality.profile.nullRate",
      params: {
        percent: formatPercent(
          Number(stats.nullCount) / Number(stats.totalRows)
        ),
      },
    });
    highlights.push({
      key: "dataQuality.profile.distinctValues",
      params: { count: formatRowCount(stats.distinctCount) },
    });
  }
  if (stats.minValue) {
    highlights.push({
      key: "dataQuality.profile.minValue",
      params: { value: stats.minValue },
    });
  }
  if (stats.maxValue) {
    highlights.push({
      key: "dataQuality.profile.maxValue",
      params: { value: stats.maxValue },
    });
  }
  if (stats.minLength !== undefined || stats.maxLength !== undefined) {
    highlights.push({
      key: "dataQuality.profile.length",
      params: {
        min: stats.minLength !== undefined ? String(stats.minLength) : "-",
        max: stats.maxLength !== undefined ? String(stats.maxLength) : "-",
      },
    });
  }
  if (stats.topValues.length > 0) {
    highlights.push({
      key: "dataQuality.profile.topValues",
      params: {
        values: stats.topValues
          .slice(0, 3)
          .map((item) => item.value)
          .join(", "),
      },
    });
  }
  if (profile.truncated) {
    highlights.push({ key: "dataQuality.profile.truncated", params: {} });
  }
  highlights.push({
    key: "dataQuality.profile.sampled",
    params: {
      method: profile.sampleMethod || "-",
      rows: formatRowCount(profile.sampleRows),
    },
  });
  return highlights;
}

/** A stored label's one-line summary for the column rows. */
export function labelSummary(label: ColumnSemanticLabel): string[] {
  const parts = [];
  if (label.confidence > 0) {
    parts.push(formatConfidence(label.confidence));
  }
  for (const metric of label.evidence?.metrics ?? []) {
    parts.push(metric);
  }
  return parts;
}
