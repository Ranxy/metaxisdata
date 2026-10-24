import { create } from "@bufbuild/protobuf";
import { DurationSchema } from "@bufbuild/protobuf/wkt";
import {
  type ColumnProfile,
  type ColumnProfileStats,
  type ColumnSemanticLabel,
  QualityDimension,
  QualityLabelSource,
  QualityLabelStatus,
  type QualityLLMDiagnostic,
  type QualityRule,
  type QualityRuleDraftConfig,
  QualityRuleSchema,
  QualityRuleState,
  type QualityRuleType,
  QualitySemanticType,
  QualitySeverity,
  type QualitySuggestion,
  type QualitySuggestionJob,
  QualitySuggestionJobStatus,
  type QualitySuggestionJobTarget,
  QualitySuggestionJobTargetState,
  QualitySuggestionKind,
  type QualitySuggestionQueueSummary,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  type SemanticLabelEvidence,
  type SemanticLabelSuggestion,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatRowCount, type QualityBadgeVariant } from "@/utils/dataQuality";
import {
  emptyConfig,
  emptyRuleForm,
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
    default:
      return "dataQuality.suggestionStatus.unspecified";
  }
}

/**
 * The badge variant for a suggestion's decision state. Superseded is the server's
 * own closing, never a person's decision, so it stays distinct from rejected.
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
  /**
   * Unset leaves the schedule to the server's workspace default; otherwise the
   * interval in seconds (15 minutes to 7 days).
   */
  scheduleIntervalSeconds?: number;
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
 * when the suggestion is named), the state is always enabled, and an unset
 * interval is left empty so the server applies the workspace default. Returns
 * undefined when the suggestion is not a draft this build can turn into a rule.
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
  rule.scheduleInterval = undefined;
  if (
    overrides?.scheduleIntervalSeconds !== undefined &&
    overrides.scheduleIntervalSeconds > 0
  ) {
    rule.scheduleInterval = create(DurationSchema, {
      seconds: BigInt(overrides.scheduleIntervalSeconds),
      nanos: 0,
    });
  }
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

// ---- drafting jobs ----

/** The maximum number of targets one drafting job accepts. */
export const MAX_SUGGESTION_JOB_TARGETS = 20;

/** The label key for a drafting job's lifecycle status. */
export function suggestionJobStatusKey(
  status: QualitySuggestionJobStatus
): string {
  switch (status) {
    case QualitySuggestionJobStatus.PENDING:
      return "dataQuality.suggestionJobStatus.pending";
    case QualitySuggestionJobStatus.RUNNING:
      return "dataQuality.suggestionJobStatus.running";
    case QualitySuggestionJobStatus.COMPLETED:
      return "dataQuality.suggestionJobStatus.completed";
    case QualitySuggestionJobStatus.FAILED:
      return "dataQuality.suggestionJobStatus.failed";
    case QualitySuggestionJobStatus.CANCELED:
      return "dataQuality.suggestionJobStatus.canceled";
    default:
      return "dataQuality.suggestionJobStatus.unspecified";
  }
}

/** The badge variant for a drafting job's lifecycle status. */
export function suggestionJobStatusVariant(
  status: QualitySuggestionJobStatus
): QualityBadgeVariant {
  switch (status) {
    case QualitySuggestionJobStatus.PENDING:
      return "warning";
    case QualitySuggestionJobStatus.RUNNING:
      return "info";
    case QualitySuggestionJobStatus.COMPLETED:
      return "success";
    case QualitySuggestionJobStatus.FAILED:
      return "danger";
    case QualitySuggestionJobStatus.CANCELED:
      return "neutral";
    default:
      return "secondary";
  }
}

/** The label key for one job target's state. */
export function suggestionJobTargetStateKey(
  state: QualitySuggestionJobTargetState
): string {
  switch (state) {
    case QualitySuggestionJobTargetState.PENDING:
      return "dataQuality.suggestionJobTargetState.pending";
    case QualitySuggestionJobTargetState.RUNNING:
      return "dataQuality.suggestionJobTargetState.running";
    case QualitySuggestionJobTargetState.DONE:
      return "dataQuality.suggestionJobTargetState.done";
    case QualitySuggestionJobTargetState.FAILED:
      return "dataQuality.suggestionJobTargetState.failed";
    case QualitySuggestionJobTargetState.SKIPPED:
      return "dataQuality.suggestionJobTargetState.skipped";
    default:
      return "dataQuality.suggestionJobTargetState.unspecified";
  }
}

/** The badge variant for one job target's state. */
export function suggestionJobTargetStateVariant(
  state: QualitySuggestionJobTargetState
): QualityBadgeVariant {
  switch (state) {
    case QualitySuggestionJobTargetState.PENDING:
      return "secondary";
    case QualitySuggestionJobTargetState.RUNNING:
      return "info";
    case QualitySuggestionJobTargetState.DONE:
      return "success";
    case QualitySuggestionJobTargetState.FAILED:
      return "danger";
    case QualitySuggestionJobTargetState.SKIPPED:
      return "neutral";
    default:
      return "secondary";
  }
}

/** A job the runner can still make progress on; polling only matters then. */
export function isActiveSuggestionJob(job: QualitySuggestionJob): boolean {
  return (
    job.status === QualitySuggestionJobStatus.PENDING ||
    job.status === QualitySuggestionJobStatus.RUNNING
  );
}

export interface SuggestionJobProgress {
  /** Targets the job will visit. */
  total: number;
  /** Targets already attempted (done, failed or skipped). */
  completed: number;
  /** Completed share, 0-100, for a progress bar or compact label. */
  percent: number;
  /** Suggestions the job has queued so far. */
  suggestions: number;
}

/** The counts a job row shows: target progress plus queued suggestions. */
export function suggestionJobProgress(
  job: QualitySuggestionJob
): SuggestionJobProgress {
  const total = Math.max(0, job.targetCount || job.targets.length);
  const completed = Math.min(total, Math.max(0, job.completedTargetCount));
  return {
    total,
    completed,
    percent: total > 0 ? Math.round((completed / total) * 100) : 0,
    suggestions: Math.max(0, job.suggestionCount),
  };
}

/** Every suggestion resource name the job has queued so far. */
export function jobSuggestionNames(job: QualitySuggestionJob): string[] {
  return job.targets.flatMap((target) => [...target.suggestions]);
}

/**
 * A job target as one display name. The job snapshots the path when it is
 * submitted, so progress stays readable after the registry changes; the caller's
 * instance title replaces the raw instance id when it is known.
 */
export function jobTargetLabel(
  target: QualitySuggestionJobTarget,
  instanceTitle = ""
): string {
  const path = target.target?.path;
  if (path && (path.instance || path.database || path.schema || path.object)) {
    const parts = [
      instanceTitle || path.instance,
      path.database,
      path.schema,
      path.object,
    ].filter(Boolean);
    if (parts.length > 0) {
      return parts.join(" · ");
    }
  }
  const guid = target.target?.metaGuid ?? "";
  return guid ? suggestionTargetLabel(guid, instanceTitle) : "-";
}

/** The intent on one line, shortened so a row cannot grow without bound. */
export function jobIntentSnippet(
  job: QualitySuggestionJob,
  maxLength = 80
): string {
  const intent = job.intent.trim().replace(/\s+/g, " ");
  if (intent.length <= maxLength) {
    return intent;
  }
  return `${intent.slice(0, Math.max(0, maxLength - 1))}…`;
}

// ---- LLM failure diagnostics ----

/** The classifier suffixes the server may store on a failed model call. */
function diagnosticSuffix(code: string): string {
  switch (code.trim().toUpperCase()) {
    case "TRUNCATED":
      return "truncated";
    case "STREAM_INCOMPLETE":
      return "streamIncomplete";
    case "PROVIDER_STATUS":
      return "providerStatus";
    case "PROVIDER_REQUEST":
      return "providerRequest";
    case "RESPONSE_TOO_LARGE":
      return "responseTooLarge";
    case "UNEXPECTED_FINISH_REASON":
      return "unexpectedFinishReason";
    case "TIMEOUT":
      return "timeout";
    case "MALFORMED_RESPONSE":
      return "malformedResponse";
    case "PARSE":
      return "parse";
    default:
      return "unknown";
  }
}

/** The short label key for a failure classifier code. */
export function diagnosticCodeKey(code: string): string {
  return `dataQuality.suggestions.diagnostic.code.${diagnosticSuffix(code)}`;
}

/** The actionable hint key for a failure classifier code. */
export function diagnosticHintKey(code: string): string {
  return `dataQuality.suggestions.diagnostic.hint.${diagnosticSuffix(code)}`;
}

/**
 * A token/character count from a diagnostic. Zero means the provider did not
 * report it, which reads as "-" rather than a fake zero.
 */
export function formatDiagnosticCount(value: bigint | number): string {
  const reported = typeof value === "bigint" ? value > 0n : value > 0;
  return reported ? formatRowCount(value) : "-";
}

export interface DiagnosticSummary {
  /** `provider · model`, or whichever half the provider reported. */
  providerModel: string;
  promptVersion: string;
  finishReason: string;
  maxOutputTokens: string;
  promptTokens: string;
  completionTokens: string;
  totalTokens: string;
  promptChars: string;
  providerStatus: string;
  correlationId: string;
}

/** The at-a-glance facts of a failed model call, already formatted for display. */
export function diagnosticSummary(
  diagnostic: QualityLLMDiagnostic
): DiagnosticSummary {
  return {
    providerModel:
      [diagnostic.provider, diagnostic.model].filter(Boolean).join(" · ") ||
      "-",
    promptVersion: diagnostic.promptVersion || "-",
    finishReason: diagnostic.finishReason || "-",
    maxOutputTokens: formatDiagnosticCount(diagnostic.maxOutputTokens),
    promptTokens: formatDiagnosticCount(diagnostic.promptTokens),
    completionTokens: formatDiagnosticCount(diagnostic.completionTokens),
    totalTokens: formatDiagnosticCount(diagnostic.totalTokens),
    promptChars: formatDiagnosticCount(diagnostic.promptChars),
    providerStatus:
      diagnostic.providerStatus > 0 ? String(diagnostic.providerStatus) : "-",
    correlationId: diagnostic.correlationId,
  };
}

/** The copy that explains an output-token truncation on the synchronous path. */
const TRUNCATED_ERROR_PATTERN =
  /finish_reason=length|truncated by the provider|output[- ]token limit/i;

/**
 * The localized hint for a synchronous drafting error caused by the model
 * stopping at its output-token limit; the server text stays visible beside it.
 */
export function syncDraftErrorHintKey(message: string): string | undefined {
  return TRUNCATED_ERROR_PATTERN.test(message)
    ? "dataQuality.suggestions.diagnostic.syncTruncatedHint"
    : undefined;
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
