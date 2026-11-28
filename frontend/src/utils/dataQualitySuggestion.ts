import { create } from "@bufbuild/protobuf";
import {
  type ColumnProfile,
  type ColumnProfileStats,
  type ColumnSemanticLabel,
  QualityDimension,
  QualityLabelSource,
  QualityLabelStatus,
  type QualityRule,
  type QualityRuleDraftConfig,
  QualityRuleSchema,
  QualityRuleState,
  type QualityRuleType,
  QualitySemanticType,
  QualitySeverity,
  type QualitySuggestion,
  QualitySuggestionKind,
  type QualitySuggestionQueueSummary,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  type SemanticLabelEvidence,
  type SemanticLabelSuggestion,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  formatRowCount,
  type QualityBadgeVariant,
  qualityRuleTypeKey,
} from "@/utils/dataQuality";
import {
  emptyConfig,
  formToRule,
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
 * conversion of a suggested rule draft into an editor form, the durable drafting
 * job's labels and progress, and the display of column profiles and semantic
 * labels in the asset page's quality section.
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
    case QualitySuggestionSource.RECOMMENDATION:
      return "dataQuality.suggestionSource.recommendation";
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
    case QualitySuggestionStatus.SUPERSEDED:
      return "dataQuality.suggestionStatus.superseded";
    case QualitySuggestionStatus.WITHDRAWN:
      return "dataQuality.suggestionStatus.withdrawn";
    default:
      return "dataQuality.suggestionStatus.unspecified";
  }
}

/**
 * The badge variant for a suggestion's decision state. Superseded and withdrawn are
 * closings nobody decided, so they stay distinct from rejected: the server closed one,
 * the proposing agent retracted the other.
 */
export function suggestionStatusVariant(
  status: QualitySuggestionStatus
): QualityBadgeVariant {
  switch (status) {
    case QualitySuggestionStatus.PENDING:
      return "warning";
    case QualitySuggestionStatus.ACCEPTED:
      return "success";
    case QualitySuggestionStatus.REJECTED:
      return "neutral";
    case QualitySuggestionStatus.SUPERSEDED:
    case QualitySuggestionStatus.WITHDRAWN:
      return "outline";
    default:
      return "secondary";
  }
}

// ---- generation summary ----

/** One non-zero count a generation pass reported, with its catalog key. */
export interface SuggestionSummaryCount {
  key: string;
  count: number;
}

/**
 * One generation pass's effect on the queue, as the numbers its toast shows.
 * Generation reconciles proposals with facts and the live queue instead of
 * appending rows, so a pass that queues nothing is normal and has to be
 * explained rather than shown as "0 suggestions".
 */
export interface SuggestionSummaryView {
  /** False when the response carried no summary (an older server). */
  available: boolean;
  created: number;
  refreshed: number;
  superseded: number;
  /** Every skip reason added up: existing fact, pending proposal, past decision. */
  skipped: number;
  /** The non-zero counts, in display order. */
  counts: SuggestionSummaryCount[];
  /** This pass created or refreshed at least one proposal. */
  queued: boolean;
}

/** One generation pass's queue summary in the shape its toast needs. */
export function suggestionSummaryView(
  summary: QualitySuggestionQueueSummary | undefined
): SuggestionSummaryView {
  const created = summary?.created ?? 0;
  const refreshed = summary?.refreshed ?? 0;
  const superseded = summary?.superseded ?? 0;
  const skipped =
    (summary?.skippedExistingFact ?? 0) +
    (summary?.skippedPending ?? 0) +
    (summary?.skippedDecided ?? 0);
  return {
    available: summary !== undefined,
    created,
    refreshed,
    superseded,
    skipped,
    counts: [
      { key: "dataQuality.suggestionSummary.created", count: created },
      { key: "dataQuality.suggestionSummary.refreshed", count: refreshed },
      { key: "dataQuality.suggestionSummary.superseded", count: superseded },
      { key: "dataQuality.suggestionSummary.skipped", count: skipped },
    ].filter((item) => item.count > 0),
    queued: created + refreshed > 0,
  };
}

/** How much of one refusal's reason a toast shows before eliding it. */
const REJECTION_REASON_CHARS = 160;

/** One refused draft, as a queue summary's toast names it. */
export interface SuggestionRejectionNotice {
  /** The refused draft's title, when the model wrote one. */
  title: string;
  /** The server's reason, elided to the toast's width. */
  reason: string;
  /** How many drafts the pass refused. */
  count: number;
}

/**
 * The first draft a generation pass refused, when it refused any. A model answer
 * can name a value the target cannot hold (an accepted value outside an enum
 * column's labels, for example); the toast explains the smaller queue with that
 * reason instead of leaving it unexplained.
 */
export function suggestionRejectionNotice(
  summary: QualitySuggestionQueueSummary | undefined
): SuggestionRejectionNotice | undefined {
  const rejected = summary?.rejectedDrafts ?? [];
  if (rejected.length === 0) {
    return undefined;
  }
  const first = rejected[0];
  const reason = first?.reason || first?.title || "";
  const characters = Array.from(reason);
  return {
    title: first?.title ?? "",
    reason:
      characters.length > REJECTION_REASON_CHARS
        ? `${characters.slice(0, REJECTION_REASON_CHARS).join("")}…`
        : reason,
    count: rejected.length,
  };
}

/** Which suggestion kind a pass handled; it changes how a skip is explained. */
export type SuggestionSummarySubject = "label" | "rule";

export interface SuggestionSummaryReason {
  key: string;
  count: number;
}

/**
 * Why a pass that queued nothing did nothing, strongest explanation first: the
 * fact already exists, an equivalent proposal is already pending, or the
 * proposal had already been decided.
 */
export function suggestionSummaryReason(
  summary: QualitySuggestionQueueSummary | undefined,
  subject: SuggestionSummarySubject
): SuggestionSummaryReason {
  const existingFact = summary?.skippedExistingFact ?? 0;
  if (existingFact > 0) {
    return {
      key:
        subject === "label"
          ? "dataQuality.suggestionSummary.emptyLabelExistingFact"
          : "dataQuality.suggestionSummary.emptyRuleExistingFact",
      count: existingFact,
    };
  }
  const pending = summary?.skippedPending ?? 0;
  if (pending > 0) {
    return {
      key: "dataQuality.suggestionSummary.emptyPending",
      count: pending,
    };
  }
  const decided = summary?.skippedDecided ?? 0;
  if (decided > 0) {
    return {
      key: "dataQuality.suggestionSummary.emptyDecided",
      count: decided,
    };
  }
  const superseded = summary?.superseded ?? 0;
  if (superseded > 0) {
    return {
      key: "dataQuality.suggestionSummary.emptySuperseded",
      count: superseded,
    };
  }
  return { key: "dataQuality.suggestionSummary.emptyGeneric", count: 0 };
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
    case QualitySemanticType.UNIFIED_SOCIAL_CREDIT_CODE:
      return "dataQuality.semanticType.unifiedSocialCreditCode";
    case QualitySemanticType.ORGANIZATION_CODE:
      return "dataQuality.semanticType.organizationCode";
    case QualitySemanticType.RESIDENT_ID_CARD:
      return "dataQuality.semanticType.residentIdCard";
    case QualitySemanticType.ADMINISTRATIVE_DIVISION:
      return "dataQuality.semanticType.administrativeDivision";
    case QualitySemanticType.INDUSTRY_CLASSIFICATION:
      return "dataQuality.semanticType.industryClassification";
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

/** Whether a suggestion is a semantic label proposal, whatever its status. */
export function isLabelSuggestion(suggestion: QualitySuggestion): boolean {
  return labelSuggestionOf(suggestion) !== undefined;
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

/**
 * The model's one-line justification for a proposal, whitespace collapsed for a
 * single-line display: a draft's own rationale, or the rationale in a label's
 * evidence.
 */
export function suggestionRationale(suggestion: QualitySuggestion): string {
  const rationale =
    ruleDraftOf(suggestion)?.rationale ||
    labelSuggestionOf(suggestion)?.evidence?.rationale ||
    "";
  return rationale.replace(/\s+/g, " ").trim();
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

// ---- target grouping and labels ----

/** The four levels of a metadata GUID. */
export interface SuggestionTargetPath {
  instance: string;
  database: string;
  schema: string;
  object: string;
}

/**
 * A suggestion's target GUID split into its path levels. A GUID is the
 * `;`-joined resource path, and a MySQL-family target keeps a deliberately empty
 * schema segment; a shorter GUID (a suggestion queued before a target level
 * existed, or a hand-written fixture) is read from the right.
 */
export function suggestionTargetPath(guid: string): SuggestionTargetPath {
  // `split` always returns at least one segment, so length 0 cannot occur.
  const segments = guid.split(";");
  switch (segments.length) {
    case 1:
      return {
        instance: segments[0],
        database: "",
        schema: "",
        object: "",
      };
    case 2:
      return {
        instance: segments[0],
        database: segments[1],
        schema: "",
        object: "",
      };
    case 3:
      // MySQL-family GUIDs carry no schema segment.
      return {
        instance: segments[0],
        database: segments[1],
        schema: "",
        object: segments[2],
      };
    default:
      return {
        instance: segments[0],
        database: segments[1],
        schema: segments[2],
        object: segments[3],
      };
  }
}

/**
 * A target GUID as one display name. The instance title replaces the raw id when
 * the caller knows it; the remaining levels stay as the GUID carries them.
 */
export function suggestionTargetLabel(
  guid: string,
  instanceTitle = ""
): string {
  const path = suggestionTargetPath(guid);
  const parts = [
    instanceTitle || path.instance,
    path.database,
    path.schema,
    path.object,
  ].filter(Boolean);
  return parts.length > 0 ? parts.join(" · ") : "-";
}

export interface SuggestionTargetGroup {
  targetGuid: string;
  targetLabel: string;
  suggestions: QualitySuggestion[];
}

/**
 * The queue grouped by target table, in first-seen order. `instanceTitles` maps a
 * bare instance id to its display title; an unknown id falls back to the GUID's
 * own instance segment.
 */
export function groupSuggestionsByTarget(
  suggestions: readonly QualitySuggestion[],
  instanceTitles: Record<string, string> = {}
): SuggestionTargetGroup[] {
  const groups = new Map<string, SuggestionTargetGroup>();
  for (const suggestion of suggestions) {
    const targetGuid = suggestion.target?.metaGuid ?? "";
    let group = groups.get(targetGuid);
    if (!group) {
      const instance = suggestionTargetPath(targetGuid).instance;
      group = {
        targetGuid,
        targetLabel: suggestionTargetLabel(
          targetGuid,
          instanceTitles[instance] ?? ""
        ),
        suggestions: [],
      };
      groups.set(targetGuid, group);
    }
    group.suggestions.push(suggestion);
  }
  return [...groups.values()];
}

// ---- selection ----

/** A suggestion still waiting for a person's decision. */
export function isPendingSuggestion(suggestion: QualitySuggestion): boolean {
  return suggestion.status === QualitySuggestionStatus.PENDING;
}

/** A pending rule draft: the only suggestion kind a rule can be created from. */
export function isPendingDraft(suggestion: QualitySuggestion): boolean {
  return (
    isPendingSuggestion(suggestion) &&
    suggestion.kind === QualitySuggestionKind.RULE_DRAFT
  );
}

/** A pending semantic label: the only suggestion kind accepted without a rule. */
export function isPendingLabel(suggestion: QualitySuggestion): boolean {
  return (
    isPendingSuggestion(suggestion) &&
    suggestion.kind === QualitySuggestionKind.LABEL
  );
}

export interface SuggestionSelectionCounts {
  total: number;
  drafts: number;
  labels: number;
}

/** How many selected suggestions are drafts and how many are labels. */
export function selectionCountsByKind(
  selected: readonly QualitySuggestion[]
): SuggestionSelectionCounts {
  let drafts = 0;
  let labels = 0;
  for (const suggestion of selected) {
    if (suggestion.kind === QualitySuggestionKind.RULE_DRAFT) {
      drafts += 1;
    } else if (suggestion.kind === QualitySuggestionKind.LABEL) {
      labels += 1;
    }
  }
  return { total: selected.length, drafts, labels };
}

// ---- suggestion → batch rule ----

/** The row's own text: a draft's title, a label's column, or the resource id. */
export function suggestionRowText(suggestion: QualitySuggestion): string {
  const draft = ruleDraftOf(suggestion);
  if (draft?.title) {
    return draft.title;
  }
  const label = labelSuggestionOf(suggestion);
  if (label) {
    // The kind and the semantic type already have their own badges; the row's
    // own text is the column the label belongs to.
    return (
      label.column || (suggestion.name.split("/").pop() ?? suggestion.name)
    );
  }
  return suggestion.name.split("/").pop() ?? suggestion.name;
}

/** The semantic type a label suggestion proposes, as its badge key. */
export function suggestionLabelTypeKey(
  suggestion: QualitySuggestion
): string | undefined {
  const label = labelSuggestionOf(suggestion);
  return label && label.label !== QualitySemanticType.UNSPECIFIED
    ? semanticTypeKey(label.label)
    : undefined;
}

/** The rule-type label of a draft this build can prefill, if it is one. */
export function suggestionRuleDraftTypeKey(
  suggestion: QualitySuggestion
): string | undefined {
  const prefill = suggestionToEditorPrefill(suggestion);
  return prefill ? qualityRuleTypeKey(prefill.ruleType) : undefined;
}

/** A draft's declared severity, if it has one. */
export function suggestionSeverity(
  suggestion: QualitySuggestion
): QualitySeverity | undefined {
  const draft = ruleDraftOf(suggestion);
  return draft?.severity ? draft.severity : undefined;
}

/** Both kinds carry their own confidence, 0-1; 0 reads as unset. */
export function suggestionConfidence(suggestion: QualitySuggestion): number {
  const draft = ruleDraftOf(suggestion);
  if (draft) {
    return draft.confidence;
  }
  return labelSuggestionOf(suggestion)?.confidence ?? 0;
}

/** The same confidence as a 0-100 meter width. */
export function suggestionConfidencePercent(
  suggestion: QualitySuggestion
): number {
  return Math.round(
    Math.min(1, Math.max(0, suggestionConfidence(suggestion))) * 100
  );
}

/** The metadata type a target group's rows share. */
export function suggestionGroupMetaType(
  group: SuggestionTargetGroup
): MetaType {
  return group.suggestions[0]?.target?.metaType ?? MetaType.TABLE;
}

/** The rows in a group a batch decision may touch: every still-pending one. */
export function selectableSuggestions(
  group: SuggestionTargetGroup
): QualitySuggestion[] {
  return group.suggestions.filter(isPendingSuggestion);
}

/**
 * The tri-state checkbox of one target group: none, all or some of its
 * selectable rows are in the current selection.
 */
export function groupSelectionState(
  group: SuggestionTargetGroup,
  selectedNames: readonly string[]
): boolean | "indeterminate" {
  const selectable = selectableSuggestions(group);
  if (selectable.length === 0) {
    return false;
  }
  const selected = selectable.filter((suggestion) =>
    selectedNames.includes(suggestion.name)
  ).length;
  if (selected === 0) {
    return false;
  }
  return selected === selectable.length ? true : "indeterminate";
}

/** The server's batch limit for rules and suggestions in one call. */
export const MAX_SUGGESTION_BATCH = 200;

/** The common settings a batch review applies to every selected draft. */
export interface SuggestionBatchOverrides {
  /** Unset keeps the draft's own severity. */
  severity?: QualitySeverity;
  /** Format: users/{user}; empty means unassigned. */
  owner?: string;
  /** Unset keeps the draft's own dimension. */
  dimension?: QualityDimension;
}

export interface BuildRuleFromSuggestionOptions {
  suggestion: QualitySuggestion;
  overrides?: SuggestionBatchOverrides;
  /** An inline-edited form for this row; defaults to the draft's prefill. */
  form?: RuleFormState;
}

/**
 * A `CreateQualityRule` payload for one suggestion, ready for the batch call.
 *
 * The target comes from the suggestion (the server ignores the request's target
 * when the suggestion is named) and the state is always enabled. Scheduling is
 * the target's, so a new check inherits it server-side. Returns undefined when
 * the suggestion is not a draft this build can turn into a rule.
 */
export function buildRuleFromSuggestion({
  suggestion,
  overrides,
  form: editedForm,
}: BuildRuleFromSuggestionOptions): QualityRule | undefined {
  const prefill = suggestionToEditorPrefill(suggestion);
  if (!prefill?.target) {
    return undefined;
  }
  const rule = formToRule(editedForm ?? prefill.form, {
    target: prefill.target,
  });
  rule.state = QualityRuleState.ENABLED;
  if (overrides?.severity !== undefined) {
    rule.severity = overrides.severity;
  }
  if (overrides?.owner !== undefined) {
    rule.owner = overrides.owner;
  }
  if (overrides?.dimension !== undefined) {
    rule.dimension = overrides.dimension;
  }
  return rule;
}

// ---- batch results ----

export interface BatchResultFailure {
  /** The request's index, so the dialog can point at the offending row. */
  index: number;
  message: string;
}

export interface BatchResultSummary {
  succeeded: number;
  failed: BatchResultFailure[];
}

/**
 * The per-item outcome of a batch call. A result with an error failed; anything
 * else succeeded. The server aligns results with the request order and keeps
 * going after a failure, so the summary is all the UI needs.
 */
export function summarizeBatchResults(
  results: ReadonlyArray<{ error: string }>
): BatchResultSummary {
  const summary: BatchResultSummary = { succeeded: 0, failed: [] };
  results.forEach((result, index) => {
    if (result.error) {
      summary.failed.push({ index, message: result.error });
    } else {
      summary.succeeded += 1;
    }
  });
  return summary;
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
    const distinct = formatRowCount(stats.distinctCount);
    highlights.push({
      key: "dataQuality.profile.distinctValues",
      // A type without SQL equality has no exact count; the sample's figure is
      // marked so the reader does not take a lower bound for the truth.
      params: {
        count: stats.distinctEstimated ? `≈${distinct}` : distinct,
      },
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
