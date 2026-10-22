import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import {
  type BatchCreateQualityRuleResult,
  BatchCreateQualityRulesRequestSchema,
  type BatchUpdateQualitySuggestionResult,
  BatchUpdateQualitySuggestionsRequestSchema,
  CancelQualitySuggestionJobRequestSchema,
  ColumnLabelAction,
  type ColumnProfile,
  type ColumnSemanticLabel,
  CreateQualityRuleRequestSchema,
  CreateQualitySuggestionJobRequestSchema,
  DeleteQualityRuleRequestSchema,
  ExplainQualityExecutionRequestSchema,
  type ExplainQualityExecutionResponse,
  GetQualityExecutionRequestSchema,
  GetQualityIssueRequestSchema,
  GetQualityOverviewRequestSchema,
  GetQualityRuleRequestSchema,
  GetQualitySuggestionJobRequestSchema,
  GetQualitySuggestionJobTargetDebugRequestSchema,
  type GetQualitySuggestionJobTargetDebugResponse,
  GetQualitySuggestionRequestSchema,
  ListColumnLabelsRequestSchema,
  ListColumnProfilesRequestSchema,
  ListQualityExecutionsRequestSchema,
  ListQualityIssuesRequestSchema,
  ListQualityRulesRequestSchema,
  ListQualitySuggestionJobsRequestSchema,
  ListQualitySuggestionsRequestSchema,
  PreviewQualityRuleRequestSchema,
  ProfileColumnsRequestSchema,
  QualityDimension,
  type QualityExecution,
  QualityExecutionState,
  QualityExecutionTrigger,
  type QualityIssue,
  QualityIssueSchema,
  QualityIssueStatus,
  type QualityOverview,
  QualityResolutionReason,
  type QualityRule,
  QualityRuleState,
  QualityRuleType,
  type QualitySemanticType,
  QualitySeverity,
  QualityStatus,
  type QualitySuggestion,
  type QualitySuggestionJob,
  QualitySuggestionJobStatus,
  QualitySuggestionKind,
  QualitySuggestionSource,
  QualitySuggestionStatus,
  type QualityTarget,
  RunQualityRulesRequestSchema,
  SuggestColumnLabelsRequestSchema,
  SuggestQualityRuleRequestSchema,
  UpdateColumnLabelRequestSchema,
  UpdateQualityIssueRequestSchema,
  UpdateQualityRuleRequestSchema,
  UpdateQualitySuggestionRequestSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { dataQualityClient } from "./client";
import { type ListPage, listAll } from "./list";

/**
 * The workspace the quality surfaces belong to. The service scopes every query
 * to the authenticated caller's workspace, so `-` only names the resource.
 */
const WORKSPACE_PARENT = "workspaces/-";

/** The resource-name segment a quality rule's id occupies. */
const RULE_SEGMENT = "qualityRules";

/** Format: workspaces/{workspace}/qualityRules/{quality_rule}. */
export function qualityRuleName(id: string): string {
  return `${WORKSPACE_PARENT}/${RULE_SEGMENT}/${id}`;
}

/** The id part of a quality rule's resource name; the name itself if it has none. */
export function qualityRuleID(name: string): string {
  return name.slice(name.lastIndexOf("/") + 1);
}

/** The resource-name segment a quality execution's id occupies. */
const EXECUTION_SEGMENT = "qualityExecutions";

/** Format: workspaces/{workspace}/qualityExecutions/{quality_execution}. */
export function qualityExecutionName(id: string): string {
  return `${WORKSPACE_PARENT}/${EXECUTION_SEGMENT}/${id}`;
}

/** The id part of a quality execution's resource name; the name itself if it has none. */
export function qualityExecutionID(name: string): string {
  return name.slice(name.lastIndexOf("/") + 1);
}

/** The resource-name segment a quality issue's id occupies. */
const ISSUE_SEGMENT = "qualityIssues";

/** Format: workspaces/{workspace}/qualityIssues/{quality_issue}. */
export function qualityIssueName(id: string): string {
  return `${WORKSPACE_PARENT}/${ISSUE_SEGMENT}/${id}`;
}

/** The id part of a quality issue's resource name; the name itself if it has none. */
export function qualityIssueID(name: string): string {
  return name.slice(name.lastIndexOf("/") + 1);
}

/** One page of rules, newest first. */
export async function listQualityRules(options: {
  pageSize?: number;
  pageToken?: string;
  /** A GUID path prefix, not a name match. */
  instance?: string;
  database?: string;
  schema?: string;
  table?: string;
  targetGuid?: string;
  dimension?: QualityDimension;
  ruleType?: QualityRuleType;
  severity?: QualitySeverity;
  state?: QualityRuleState;
  owner?: string;
  /** When true, only rules with no owner; mutually exclusive with a non-empty owner. */
  ownerUnassigned?: boolean;
  /** A substring match over the title and description. */
  search?: string;
  signal?: AbortSignal;
}): Promise<ListPage<QualityRule>> {
  const request = create(ListQualityRulesRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    instance: options.instance ?? "",
    database: options.database ?? "",
    schema: options.schema ?? "",
    table: options.table ?? "",
    targetGuid: options.targetGuid ?? "",
    dimension: options.dimension ?? QualityDimension.UNSPECIFIED,
    ruleType: options.ruleType ?? QualityRuleType.UNSPECIFIED,
    severity: options.severity ?? QualitySeverity.UNSPECIFIED,
    state: options.state ?? QualityRuleState.UNSPECIFIED,
    owner: options.owner ?? "",
    ownerUnassigned: options.ownerUnassigned ?? false,
    search: options.search ?? "",
  });
  const response = await dataQualityClient.listQualityRules(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityRules,
    nextPageToken: response.nextPageToken,
  };
}

/** Every rule the caller can see, walking the cursor pages. */
export async function listAllQualityRules(
  options: { pageSize?: number; state?: QualityRuleState } = {}
): Promise<QualityRule[]> {
  return await listAll((pageToken) =>
    listQualityRules({
      pageSize: options.pageSize ?? 1000,
      pageToken,
      state: options.state,
    })
  );
}

/** Read one rule, including its latest result summary. */
export async function getQualityRule(name: string): Promise<QualityRule> {
  const request = create(GetQualityRuleRequestSchema, { name });
  return await dataQualityClient.getQualityRule(request);
}

/**
 * Create a rule. The server assigns its name and first revision.
 *
 * When `suggestionName` is set the rule is created from that suggestion in one
 * request: the server takes the target from the suggestion, compiles the rule,
 * saves it and marks the suggestion accepted. The decision is a side effect —
 * a failed decision does not roll the rule back.
 */
export async function createQualityRule(
  rule: QualityRule,
  suggestionName = ""
): Promise<QualityRule> {
  const request = create(CreateQualityRuleRequestSchema, {
    parent: WORKSPACE_PARENT,
    rule,
    suggestion: suggestionName,
  });
  return await dataQualityClient.createQualityRule(request);
}

/**
 * Update exactly the fields named by `updateMask`. A semantic edit appends a
 * revision, so the mask should stay as narrow as the change.
 */
export async function updateQualityRule(
  rule: QualityRule,
  updateMask: string[]
): Promise<QualityRule> {
  const request = create(UpdateQualityRuleRequestSchema, {
    rule,
    updateMask: create(FieldMaskSchema, { paths: updateMask }),
  });
  return await dataQualityClient.updateQualityRule(request);
}

/** Archive a rule. Its revisions and executions stay readable. */
export async function deleteQualityRule(name: string): Promise<void> {
  const request = create(DeleteQualityRuleRequestSchema, { name });
  await dataQualityClient.deleteQualityRule(request);
}

/** Compile and probe a rule without saving it or creating an execution. */
export async function previewQualityRule(rule: QualityRule) {
  const request = create(PreviewQualityRuleRequestSchema, {
    parent: WORKSPACE_PARENT,
    rule,
  });
  return await dataQualityClient.previewQualityRule(request);
}

/**
 * Run rules now. At least one of `rules`, `instance` or `targetGuid` selects the
 * rules; the server plans one execution per rule and returns them PENDING.
 */
export async function runQualityRules(options: {
  rules?: string[];
  /** Format: instances/{instance}. */
  instance?: string;
  targetGuid?: string;
}): Promise<QualityExecution[]> {
  const request = create(RunQualityRulesRequestSchema, {
    parent: WORKSPACE_PARENT,
    rules: options.rules ?? [],
    instance: options.instance ?? "",
    targetGuid: options.targetGuid ?? "",
  });
  const response = await dataQualityClient.runQualityRules(request);
  return response.executions;
}

/** One page of executions, newest first. */
export async function listQualityExecutions(options: {
  pageSize?: number;
  pageToken?: string;
  /** The lifecycle to filter by, e.g. only PENDING executions. */
  state?: QualityExecutionState;
  /** The verdict to filter by; completed executions only. */
  outcome?: QualityStatus;
  trigger?: QualityExecutionTrigger;
  /** Format: workspaces/{workspace}/qualityRules/{quality_rule}. */
  rule?: string;
  /** The exact target GUID. */
  targetGuid?: string;
  /** Format: instances/{instance}. */
  instance?: string;
  /** Format: users/{user}. */
  initiator?: string;
  startTime?: Timestamp;
  endTime?: Timestamp;
  signal?: AbortSignal;
}): Promise<ListPage<QualityExecution>> {
  const request = create(ListQualityExecutionsRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    state: options.state ?? QualityExecutionState.UNSPECIFIED,
    outcome: options.outcome ?? QualityStatus.UNSPECIFIED,
    trigger: options.trigger ?? QualityExecutionTrigger.UNSPECIFIED,
    rule: options.rule ?? "",
    targetGuid: options.targetGuid ?? "",
    instance: options.instance ?? "",
    initiator: options.initiator ?? "",
    ...(options.startTime ? { startTime: options.startTime } : {}),
    ...(options.endTime ? { endTime: options.endTime } : {}),
  });
  const response = await dataQualityClient.listQualityExecutions(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityExecutions,
    nextPageToken: response.nextPageToken,
  };
}

/** Read one execution with its state, verdict and evidence. */
export async function getQualityExecution(
  name: string
): Promise<QualityExecution> {
  const request = create(GetQualityExecutionRequestSchema, { name });
  return await dataQualityClient.getQualityExecution(request);
}

/** One page of issues, most recently seen first. */
export async function listQualityIssues(options: {
  pageSize?: number;
  pageToken?: string;
  status?: QualityIssueStatus;
  severity?: QualitySeverity;
  /** Format: workspaces/{workspace}/qualityRules/{quality_rule}. */
  rule?: string;
  targetGuid?: string;
  /** Format: users/{user}. */
  owner?: string;
  startTime?: Timestamp;
  endTime?: Timestamp;
  signal?: AbortSignal;
}): Promise<ListPage<QualityIssue>> {
  const request = create(ListQualityIssuesRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    status: options.status ?? QualityIssueStatus.UNSPECIFIED,
    severity: options.severity ?? QualitySeverity.UNSPECIFIED,
    rule: options.rule ?? "",
    targetGuid: options.targetGuid ?? "",
    owner: options.owner ?? "",
    ...(options.startTime ? { startTime: options.startTime } : {}),
    ...(options.endTime ? { endTime: options.endTime } : {}),
  });
  const response = await dataQualityClient.listQualityIssues(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityIssues,
    nextPageToken: response.nextPageToken,
  };
}

/** Every issue the caller can see, walking the cursor pages. */
export async function listAllQualityIssues(
  options: { pageSize?: number } = {}
): Promise<QualityIssue[]> {
  return await listAll((pageToken) =>
    listQualityIssues({ pageSize: options.pageSize ?? 1000, pageToken })
  );
}

/** Read one issue by its full resource name. */
export async function getQualityIssue(name: string): Promise<QualityIssue> {
  const request = create(GetQualityIssueRequestSchema, { name });
  return await dataQualityClient.getQualityIssue(request);
}

/**
 * Acknowledge an open issue. Acknowledgement is an action rather than a stored
 * field, so the update mask names the input-only path `acknowledged`.
 */
export async function acknowledgeQualityIssue(
  issue: QualityIssue
): Promise<QualityIssue> {
  const request = create(UpdateQualityIssueRequestSchema, {
    issue,
    updateMask: create(FieldMaskSchema, { paths: ["acknowledged"] }),
    acknowledged: true,
  });
  return await dataQualityClient.updateQualityIssue(request);
}

/** Close an issue with a reason and an optional note. */
export async function resolveQualityIssue(
  issue: QualityIssue,
  reason: QualityResolutionReason,
  note: string
): Promise<QualityIssue> {
  const resolved = create(QualityIssueSchema, issue);
  resolved.status = QualityIssueStatus.RESOLVED;
  resolved.resolutionReason = reason;
  resolved.resolutionNote = note.trim();
  const request = create(UpdateQualityIssueRequestSchema, {
    issue: resolved,
    updateMask: create(FieldMaskSchema, {
      paths: ["status", "resolution_reason", "resolution_note"],
    }),
  });
  return await dataQualityClient.updateQualityIssue(request);
}

/**
 * Mute an issue's notifications until `mutedUntil`. The rule keeps running and
 * executions keep being recorded; only the notifications stop.
 */
export async function muteQualityIssue(
  issue: QualityIssue,
  mutedUntil: Timestamp
): Promise<QualityIssue> {
  const muted = create(QualityIssueSchema, issue);
  muted.mutedUntil = mutedUntil;
  const request = create(UpdateQualityIssueRequestSchema, {
    issue: muted,
    updateMask: create(FieldMaskSchema, { paths: ["muted_until"] }),
  });
  return await dataQualityClient.updateQualityIssue(request);
}

/** The workspace-level quality summary the overview page renders. */
export async function getQualityOverview(
  options: { trendDays?: number; signal?: AbortSignal } = {}
): Promise<QualityOverview> {
  const request = create(GetQualityOverviewRequestSchema, {
    parent: WORKSPACE_PARENT,
    trendDays: options.trendDays ?? 0,
  });
  return await dataQualityClient.getQualityOverview(request, {
    signal: options.signal,
  });
}

// ---- suggestions ----

/** One page of the suggestion queue, newest first. */
export async function listQualitySuggestions(options: {
  pageSize?: number;
  pageToken?: string;
  status?: QualitySuggestionStatus;
  kind?: QualitySuggestionKind;
  source?: QualitySuggestionSource;
  targetGuid?: string;
  /** GUID path prefixes, like the rule list: the deepest one narrows the list. */
  instance?: string;
  database?: string;
  schema?: string;
  table?: string;
  signal?: AbortSignal;
}): Promise<ListPage<QualitySuggestion>> {
  const request = create(ListQualitySuggestionsRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    status: options.status ?? QualitySuggestionStatus.UNSPECIFIED,
    kind: options.kind ?? QualitySuggestionKind.UNSPECIFIED,
    source: options.source ?? QualitySuggestionSource.UNSPECIFIED,
    targetGuid: options.targetGuid ?? "",
    instance: options.instance ?? "",
    database: options.database ?? "",
    schema: options.schema ?? "",
    table: options.table ?? "",
  });
  const response = await dataQualityClient.listQualitySuggestions(request, {
    signal: options.signal,
  });
  return {
    items: response.qualitySuggestions,
    nextPageToken: response.nextPageToken,
  };
}

/** Read one suggestion by resource name; a missing one is `NotFound`. */
export async function getQualitySuggestion(
  name: string,
  signal?: AbortSignal
): Promise<QualitySuggestion> {
  const request = create(GetQualitySuggestionRequestSchema, { name });
  return await dataQualityClient.getQualitySuggestion(request, { signal });
}

/** Accept or reject a suggestion, optionally recording why. */
export async function updateQualitySuggestion(options: {
  name: string;
  decision: QualitySuggestionStatus;
  note?: string;
  signal?: AbortSignal;
}): Promise<QualitySuggestion> {
  const request = create(UpdateQualitySuggestionRequestSchema, {
    name: options.name,
    decision: options.decision,
    decisionNote: options.note ?? "",
  });
  return await dataQualityClient.updateQualitySuggestion(request, {
    signal: options.signal,
  });
}

/**
 * Decide up to 200 suggestions in one call. The results are aligned with
 * `items`; a decided suggestion has its message set, a failed one has `error`
 * set and does not stop the rest.
 */
export async function batchUpdateQualitySuggestions(
  items: Array<{
    name: string;
    decision: QualitySuggestionStatus;
    note?: string;
  }>
): Promise<BatchUpdateQualitySuggestionResult[]> {
  const request = create(BatchUpdateQualitySuggestionsRequestSchema, {
    parent: WORKSPACE_PARENT,
    requests: items.map((item) =>
      create(UpdateQualitySuggestionRequestSchema, {
        name: item.name,
        decision: item.decision,
        decisionNote: item.note ?? "",
      })
    ),
  });
  const response =
    await dataQualityClient.batchUpdateQualitySuggestions(request);
  return response.results;
}

/**
 * Create up to 200 rules in one call. Each item may name the suggestion it is
 * created from, which accepts that suggestion server-side. The results are
 * aligned with `items`; a failed item does not stop the rest.
 */
export async function batchCreateQualityRules(
  items: Array<{ rule: QualityRule; suggestion?: string }>
): Promise<BatchCreateQualityRuleResult[]> {
  const request = create(BatchCreateQualityRulesRequestSchema, {
    parent: WORKSPACE_PARENT,
    requests: items.map((item) =>
      create(CreateQualityRuleRequestSchema, {
        parent: WORKSPACE_PARENT,
        rule: item.rule,
        suggestion: item.suggestion ?? "",
      })
    ),
  });
  const response = await dataQualityClient.batchCreateQualityRules(request);
  return response.results;
}

// ---- drafting jobs ----

/**
 * Submit one durable AI drafting job for several targets. The server runs the
 * analyses in the background; the returned job is safe to poll and outlives the
 * browser tab.
 */
export async function createQualitySuggestionJob(options: {
  intent: string;
  language?: string;
  targets: QualityTarget[];
}): Promise<QualitySuggestionJob> {
  const request = create(CreateQualitySuggestionJobRequestSchema, {
    parent: WORKSPACE_PARENT,
    intent: options.intent,
    language: options.language ?? "",
    targets: options.targets,
  });
  return await dataQualityClient.createQualitySuggestionJob(request);
}

/** Read one drafting job, including its per-target progress. */
export async function getQualitySuggestionJob(
  name: string,
  signal?: AbortSignal
): Promise<QualitySuggestionJob> {
  const request = create(GetQualitySuggestionJobRequestSchema, { name });
  return await dataQualityClient.getQualitySuggestionJob(request, { signal });
}

/**
 * The raw request/response bodies of one failed drafting target. Only available
 * when runtime LLM debug was on at the time and the 7-day debug log has not been
 * pruned; `available` says which it is.
 */
export async function getQualitySuggestionJobTargetDebug(
  name: string,
  targetIndex: number
): Promise<GetQualitySuggestionJobTargetDebugResponse> {
  const request = create(GetQualitySuggestionJobTargetDebugRequestSchema, {
    name,
    targetIndex,
  });
  return await dataQualityClient.getQualitySuggestionJobTargetDebug(request);
}

/** One page of drafting jobs, newest first. */
export async function listQualitySuggestionJobs(
  options: {
    pageSize?: number;
    pageToken?: string;
    status?: QualitySuggestionJobStatus;
    signal?: AbortSignal;
  } = {}
): Promise<ListPage<QualitySuggestionJob>> {
  const request = create(ListQualitySuggestionJobsRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 20,
    pageToken: options.pageToken ?? "",
    status: options.status ?? QualitySuggestionJobStatus.UNSPECIFIED,
  });
  const response = await dataQualityClient.listQualitySuggestionJobs(request, {
    signal: options.signal,
  });
  return {
    items: response.qualitySuggestionJobs,
    nextPageToken: response.nextPageToken,
  };
}

/**
 * Ask a job to stop. Canceling between targets is cooperative: a PENDING job
 * becomes CANCELED immediately, a RUNNING one stops after the current target,
 * and canceling a terminal job returns it unchanged.
 */
export async function cancelQualitySuggestionJob(
  name: string
): Promise<QualitySuggestionJob> {
  const request = create(CancelQualitySuggestionJobRequestSchema, { name });
  return await dataQualityClient.cancelQualitySuggestionJob(request);
}

// ---- column profiles and labels ----

/** Stored profiles of one table's columns. */
export async function listColumnProfiles(options: {
  tableGuid: string;
  metaType: MetaType;
  columns?: string[];
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
}): Promise<ColumnProfile[]> {
  const request = create(ListColumnProfilesRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 200,
    pageToken: options.pageToken ?? "",
    tableGuid: options.tableGuid,
    metaType: options.metaType,
    columns: options.columns ?? [],
  });
  const response = await dataQualityClient.listColumnProfiles(request, {
    signal: options.signal,
  });
  return response.columnProfiles;
}

/** Stored semantic labels of one table's columns. */
export async function listColumnLabels(options: {
  tableGuid: string;
  metaType: MetaType;
  columns?: string[];
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
}): Promise<ColumnSemanticLabel[]> {
  const request = create(ListColumnLabelsRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 200,
    pageToken: options.pageToken ?? "",
    tableGuid: options.tableGuid,
    metaType: options.metaType,
    columns: options.columns ?? [],
  });
  const response = await dataQualityClient.listColumnLabels(request, {
    signal: options.signal,
  });
  return response.columnLabels;
}

/** Accept a label, or clear the column's label. */
export async function updateColumnLabel(options: {
  tableGuid: string;
  metaType: MetaType;
  column: string;
  action: ColumnLabelAction;
  label?: QualitySemanticType;
  signal?: AbortSignal;
}): Promise<ColumnSemanticLabel | undefined> {
  const request = create(UpdateColumnLabelRequestSchema, {
    parent: WORKSPACE_PARENT,
    tableGuid: options.tableGuid,
    metaType: options.metaType,
    column: options.column,
    action: options.action,
    label: options.label ?? 0,
  });
  const response = await dataQualityClient.updateColumnLabel(request, {
    signal: options.signal,
  });
  return response.columnLabel;
}

/** Measure the columns now (the deterministic profile, no model). */
export async function profileColumns(options: {
  tableGuid: string;
  metaType: MetaType;
  columns: string[];
  signal?: AbortSignal;
}): Promise<ColumnProfile[]> {
  const request = create(ProfileColumnsRequestSchema, {
    parent: WORKSPACE_PARENT,
    tableGuid: options.tableGuid,
    metaType: options.metaType,
    columns: options.columns,
  });
  const response = await dataQualityClient.profileColumns(request, {
    signal: options.signal,
  });
  return response.columnProfiles;
}

/** Ask the model for rule drafts and queue them as suggestions. */
export async function suggestQualityRule(options: {
  tableGuid: string;
  metaType: MetaType;
  columns?: string[];
  intent?: string;
  language?: string;
  signal?: AbortSignal;
}): Promise<QualitySuggestion[]> {
  const request = create(SuggestQualityRuleRequestSchema, {
    parent: WORKSPACE_PARENT,
    tableGuid: options.tableGuid,
    metaType: options.metaType,
    columns: options.columns ?? [],
    intent: options.intent ?? "",
    language: options.language ?? "",
  });
  const response = await dataQualityClient.suggestQualityRule(request, {
    signal: options.signal,
  });
  return response.qualitySuggestions;
}

/** Ask the model for semantic column labels and queue them as suggestions. */
export async function suggestColumnLabels(options: {
  tableGuid: string;
  metaType: MetaType;
  columns?: string[];
  language?: string;
  signal?: AbortSignal;
}): Promise<QualitySuggestion[]> {
  const request = create(SuggestColumnLabelsRequestSchema, {
    parent: WORKSPACE_PARENT,
    tableGuid: options.tableGuid,
    metaType: options.metaType,
    columns: options.columns ?? [],
    language: options.language ?? "",
  });
  const response = await dataQualityClient.suggestColumnLabels(request, {
    signal: options.signal,
  });
  return response.qualitySuggestions;
}

/**
 * Stream an AI explanation of one failed execution. The server sends progress
 * steps, markdown content fragments and a final metadata frame;
 * `forceRegenerate` bypasses a cached explanation.
 */
export function explainQualityExecution(options: {
  name: string;
  language?: string;
  forceRegenerate?: boolean;
}): AsyncIterable<ExplainQualityExecutionResponse> {
  const request = create(ExplainQualityExecutionRequestSchema, {
    name: options.name,
    language: options.language ?? "",
    forceRegenerate: options.forceRegenerate ?? false,
  });
  return dataQualityClient.explainQualityExecution(request);
}
