import { create } from "@bufbuild/protobuf";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import {
  ColumnLabelAction,
  type ColumnProfile,
  type ColumnSemanticLabel,
  CreateQualityRuleRequestSchema,
  DeleteQualityRuleRequestSchema,
  GetQualityIssueRequestSchema,
  GetQualityOverviewRequestSchema,
  GetQualityRuleRequestSchema,
  GetQualityRunRequestSchema,
  ListColumnLabelsRequestSchema,
  ListColumnProfilesRequestSchema,
  ListQualityIssuesRequestSchema,
  ListQualityRuleResultsRequestSchema,
  ListQualityRulesRequestSchema,
  ListQualityRunsRequestSchema,
  ListQualitySuggestionsRequestSchema,
  PreviewQualityRuleRequestSchema,
  ProfileColumnsRequestSchema,
  QualityDimension,
  type QualityIssue,
  QualityIssueSchema,
  QualityIssueStatus,
  type QualityOverview,
  QualityResolutionReason,
  type QualityRule,
  type QualityRuleResult,
  QualityRuleState,
  QualityRuleType,
  type QualityRun,
  QualityRunStatus,
  QualityRunTrigger,
  type QualitySemanticType,
  QualitySeverity,
  QualityStatus,
  type QualitySuggestion,
  QualitySuggestionKind,
  QualitySuggestionSource,
  QualitySuggestionStatus,
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

/** The id part of a quality run's resource name; the name itself if it has none. */
export function qualityRunID(name: string): string {
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
  options: { pageSize?: number } = {}
): Promise<QualityRule[]> {
  return await listAll((pageToken) =>
    listQualityRules({ pageSize: options.pageSize ?? 1000, pageToken })
  );
}

/** Read one rule, including its latest result summary. */
export async function getQualityRule(name: string): Promise<QualityRule> {
  const request = create(GetQualityRuleRequestSchema, { name });
  return await dataQualityClient.getQualityRule(request);
}

/** Create a rule. The server assigns its name and first revision. */
export async function createQualityRule(
  rule: QualityRule
): Promise<QualityRule> {
  const request = create(CreateQualityRuleRequestSchema, {
    parent: WORKSPACE_PARENT,
    rule,
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

/** Archive a rule. Its revisions and results stay readable. */
export async function deleteQualityRule(name: string): Promise<void> {
  const request = create(DeleteQualityRuleRequestSchema, { name });
  await dataQualityClient.deleteQualityRule(request);
}

/** Compile and probe a rule without saving it or creating a run. */
export async function previewQualityRule(rule: QualityRule) {
  const request = create(PreviewQualityRuleRequestSchema, {
    parent: WORKSPACE_PARENT,
    rule,
  });
  return await dataQualityClient.previewQualityRule(request);
}

/**
 * Run rules now. At least one of `rules`, `instance` or `targetGuid` selects
 * the rules; the server groups them by instance and returns the queued runs.
 */
export async function runQualityRules(options: {
  rules?: string[];
  /** Format: instances/{instance}. */
  instance?: string;
  targetGuid?: string;
}): Promise<QualityRun[]> {
  const request = create(RunQualityRulesRequestSchema, {
    parent: WORKSPACE_PARENT,
    rules: options.rules ?? [],
    instance: options.instance ?? "",
    targetGuid: options.targetGuid ?? "",
  });
  const response = await dataQualityClient.runQualityRules(request);
  return response.runs;
}

/** One page of runs, newest first. */
export async function listQualityRuns(options: {
  pageSize?: number;
  pageToken?: string;
  status?: QualityRunStatus;
  trigger?: QualityRunTrigger;
  /** Format: instances/{instance}. */
  instance?: string;
  startTime?: Timestamp;
  endTime?: Timestamp;
  /** Format: users/{user}. */
  initiator?: string;
  outcome?: QualityStatus;
  signal?: AbortSignal;
}): Promise<ListPage<QualityRun>> {
  const request = create(ListQualityRunsRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    status: options.status ?? QualityRunStatus.UNSPECIFIED,
    trigger: options.trigger ?? QualityRunTrigger.UNSPECIFIED,
    instance: options.instance ?? "",
    ...(options.startTime ? { startTime: options.startTime } : {}),
    ...(options.endTime ? { endTime: options.endTime } : {}),
    initiator: options.initiator ?? "",
    outcome: options.outcome ?? QualityStatus.UNSPECIFIED,
  });
  const response = await dataQualityClient.listQualityRuns(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityRuns,
    nextPageToken: response.nextPageToken,
  };
}

/** Read one run with its lifecycle state and six-state tallies. */
export async function getQualityRun(name: string): Promise<QualityRun> {
  const request = create(GetQualityRunRequestSchema, { name });
  return await dataQualityClient.getQualityRun(request);
}

/** One page of per-rule results, typically scoped to one run. */
export async function listQualityRuleResults(options: {
  pageSize?: number;
  pageToken?: string;
  /** Format: workspaces/{workspace}/qualityRuns/{quality_run}. */
  run?: string;
  /** Format: workspaces/{workspace}/qualityRules/{quality_rule}. */
  rule?: string;
  status?: QualityStatus;
  targetGuid?: string;
  startTime?: Timestamp;
  endTime?: Timestamp;
  signal?: AbortSignal;
}): Promise<ListPage<QualityRuleResult>> {
  const request = create(ListQualityRuleResultsRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 100,
    pageToken: options.pageToken ?? "",
    run: options.run ?? "",
    rule: options.rule ?? "",
    status: options.status ?? QualityStatus.UNSPECIFIED,
    targetGuid: options.targetGuid ?? "",
    ...(options.startTime ? { startTime: options.startTime } : {}),
    ...(options.endTime ? { endTime: options.endTime } : {}),
  });
  const response = await dataQualityClient.listQualityRuleResults(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityRuleResults,
    nextPageToken: response.nextPageToken,
  };
}

/** Every result of one run, walking the cursor pages. */
export async function listAllQualityRuleResults(
  run: string
): Promise<QualityRuleResult[]> {
  return await listAll((pageToken) =>
    listQualityRuleResults({ run, pageSize: 1000, pageToken })
  );
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
 * results keep being recorded; only the notifications stop.
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
  });
  const response = await dataQualityClient.listQualitySuggestions(request, {
    signal: options.signal,
  });
  return {
    items: response.qualitySuggestions,
    nextPageToken: response.nextPageToken,
  };
}

/** Every suggestion the caller can see, walking the cursor pages. */
export async function listAllQualitySuggestions(
  options: { pageSize?: number } = {}
): Promise<QualitySuggestion[]> {
  return await listAll((pageToken) =>
    listQualitySuggestions({ pageSize: options.pageSize ?? 1000, pageToken })
  );
}

/**
 * One suggestion by resource name. The service has no Get method, so the editor's
 * `?suggestion=` prefill walks the queue; a queue is bounded in practice and this
 * keeps the API shape honest rather than adding a client-side cache.
 */
export async function findQualitySuggestion(
  name: string
): Promise<QualitySuggestion | undefined> {
  const suggestions = await listAllQualitySuggestions();
  return suggestions.find((suggestion) => suggestion.name === name);
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
