import { create } from "@bufbuild/protobuf";
import type { Duration, Timestamp } from "@bufbuild/protobuf/wkt";
import { FieldMaskSchema } from "@bufbuild/protobuf/wkt";
import {
  AcknowledgeQualityTargetSchemaRequestSchema,
  ArchiveQualityAgentSessionRequestSchema,
  type BatchCreateQualityRuleResult,
  BatchCreateQualityRulesRequestSchema,
  type BatchUpdateQualityRuleResult,
  BatchUpdateQualityRulesRequestSchema,
  type BatchUpdateQualitySuggestionResult,
  BatchUpdateQualitySuggestionsRequestSchema,
  CancelQualitySuggestionJobRequestSchema,
  ColumnLabelAction,
  type ColumnProfile,
  type ColumnSemanticLabel,
  type ConfirmQualityMemoryRequest_Decision,
  ConfirmQualityMemoryRequestSchema,
  CreateQualityAgentRequestSchema,
  type CreateQualityAgentResponse,
  CreateQualityAgentSessionRequestSchema,
  CreateQualityCodeTableRequestSchema,
  CreateQualityMemoryRequestSchema,
  CreateQualityRuleRequestSchema,
  CreateQualityRuleTemplateRequestSchema,
  CreateQualitySuggestionJobRequestSchema,
  DecideQualityChangeRequestRequest_Decision,
  DecideQualityChangeRequestRequestSchema,
  DeleteQualityCodeTableRequestSchema,
  DeleteQualityRuleRequestSchema,
  DeleteQualityRuleTemplateRequestSchema,
  DisableQualityAgentRequestSchema,
  ExplainQualityExecutionRequestSchema,
  type ExplainQualityExecutionResponse,
  ExportQualityExecutionsRequestSchema,
  ExportQualityIssuesRequestSchema,
  ExportQualityReportRequestSchema,
  type ExportQualityReportResponse,
  ExportQualityTargetsRequestSchema,
  GetQualityAgentEnrollmentSummaryRequestSchema,
  GetQualityAgentMetricsRequestSchema,
  GetQualityAgentRunnerStatusRequestSchema,
  GetQualityAgentSessionRequestSchema,
  GetQualityCodeTableRequestSchema,
  GetQualityConsumptionCoverageRequestSchema,
  type GetQualityConsumptionCoverageResponse,
  GetQualityContractRequestSchema,
  GetQualityContractRevisionRequestSchema,
  type GetQualityContractRevisionResponse,
  GetQualityExecutionRequestSchema,
  GetQualityIssueRequestSchema,
  GetQualityLLMUsageRequestSchema,
  GetQualityOverviewRequestSchema,
  GetQualityRuleRequestSchema,
  GetQualityRuleTemplateRequestSchema,
  GetQualityRuleTrendRequestSchema,
  GetQualityRunRequestSchema,
  GetQualitySuggestionJobRequestSchema,
  GetQualitySuggestionJobTargetDebugRequestSchema,
  type GetQualitySuggestionJobTargetDebugResponse,
  GetQualitySuggestionRequestSchema,
  GetQualityTargetImpactRequestSchema,
  type GetQualityTargetImpactResponse,
  GetQualityTargetRequestSchema,
  type GetQualityTargetResponse,
  GetQualityTargetScheduleRequestSchema,
  ListColumnLabelsRequestSchema,
  ListColumnProfilesRequestSchema,
  ListQualityAgentEnrollmentsRequestSchema,
  ListQualityAgentSessionMessagesRequestSchema,
  ListQualityAgentSessionsRequestSchema,
  ListQualityAgentsRequestSchema,
  ListQualityChangeRequestsRequestSchema,
  ListQualityCodeTablesRequestSchema,
  ListQualityContractRevisionsRequestSchema,
  ListQualityExecutionsRequestSchema,
  ListQualityInvestigationsRequestSchema,
  ListQualityIssuesRequestSchema,
  ListQualityMemoriesRequestSchema,
  ListQualityRulesRequestSchema,
  ListQualityRuleTemplatesRequestSchema,
  ListQualityRunsRequestSchema,
  ListQualitySuggestionJobsRequestSchema,
  ListQualitySuggestionsRequestSchema,
  ListQualityTargetsRequestSchema,
  PauseQualityAgentSessionRequestSchema,
  PreviewQualityRuleRequestSchema,
  ProfileColumnsRequestSchema,
  PublishQualityContractRequestSchema,
  type PublishQualityContractResponse,
  type QualityAgent,
  type QualityAgentEnrollment,
  QualityAgentEnrollmentState,
  type QualityAgentEnrollmentSummary,
  type QualityAgentMetrics,
  type QualityAgentRunnerStatus,
  type QualityAgentSession,
  type QualityAgentSessionMessage,
  QualityAgentSessionMode,
  QualityAgentSessionSource,
  QualityAgentSessionState,
  QualityChangeRejectionReason,
  type QualityChangeRequest,
  QualityChangeRequestState,
  type QualityCodeTable,
  type QualityContract,
  type QualityContractRevision,
  type QualityContractSettings,
  QualityDimension,
  type QualityExecution,
  QualityExecutionState,
  QualityExecutionTrigger,
  type QualityInvestigation,
  type QualityIssue,
  QualityIssueSchema,
  QualityIssueStatus,
  type QualityLLMUsage,
  type QualityMemory,
  type QualityMemoryKind,
  type QualityOverview,
  type QualityPlan,
  QualityResolutionReason,
  type QualityRule,
  QualityRuleState,
  type QualityRuleTemplate,
  type QualityRuleTrend,
  QualityRuleType,
  type QualityRun,
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
  QualityTargetKeySchema,
  QualityTargetOrder,
  type QualityTargetSchedule,
  type QualityTargetSummary,
  RecommendQualityChecksRequestSchema,
  type RecommendQualityChecksResponse,
  ResumeQualityAgentSessionRequestSchema,
  RetryQualityAgentEnrollmentRequestSchema,
  RotateQualityAgentTokenRequestSchema,
  type RotateQualityAgentTokenResponse,
  RunQualityRulesRequestSchema,
  SuggestColumnLabelsRequestSchema,
  type SuggestColumnLabelsResponse,
  SuggestQualityRuleRequestSchema,
  type SuggestQualityRuleResponse,
  UpdateColumnLabelRequestSchema,
  UpdateQualityAgentEnrollmentRequestSchema,
  UpdateQualityCodeTableRequestSchema,
  UpdateQualityContractRequestSchema,
  UpdateQualityIssueRequestSchema,
  type UpdateQualityRuleRequest,
  UpdateQualityRuleRequestSchema,
  UpdateQualityRuleTemplateRequestSchema,
  UpdateQualitySuggestionRequestSchema,
  UpdateQualityTargetScheduleRequestSchema,
  WakeQualityAgentSessionRequestSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { type CsvDownload, collectCsvStream } from "@/utils/csv";
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

/** The resource-name segment a quality run's id occupies. */
const RUN_SEGMENT = "qualityRuns";

/** Format: workspaces/{workspace}/qualityRuns/{quality_run}. */
export function qualityRunName(id: string): string {
  return `${WORKSPACE_PARENT}/${RUN_SEGMENT}/${id}`;
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

// ---- targets ----

/** One page of the target directory: every target with rules, in the requested order. */
export async function listQualityTargets(options: {
  pageSize?: number;
  pageToken?: string;
  /** A GUID path prefix, not a name match. */
  instance?: string;
  database?: string;
  schema?: string;
  table?: string;
  /** A case-insensitive substring over the target's path and GUID. */
  search?: string;
  /**
   * The exact targets to answer, in request order. A selection is not a page:
   * the server ignores `pageSize`, `pageToken` and `orderBy`, caps one request
   * at 200 keys, and omits a key with no live rule — that target is simply not
   * monitored.
   */
  targets?: ReadonlyArray<{ targetGuid: string; metaType: MetaType }>;
  /** Only targets whose worst latest result is this status. */
  worstStatus?: QualityStatus;
  hasOpenIssue?: boolean;
  hasPendingDraft?: boolean;
  orderBy?: QualityTargetOrder;
  signal?: AbortSignal;
}): Promise<ListPage<QualityTargetSummary>> {
  const request = create(ListQualityTargetsRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    instance: options.instance ?? "",
    database: options.database ?? "",
    schema: options.schema ?? "",
    table: options.table ?? "",
    search: options.search ?? "",
    targets: (options.targets ?? []).map((target) =>
      create(QualityTargetKeySchema, {
        targetGuid: target.targetGuid,
        metaType: target.metaType,
      })
    ),
    worstStatus: options.worstStatus ?? QualityStatus.UNSPECIFIED,
    hasOpenIssue: options.hasOpenIssue ?? false,
    hasPendingDraft: options.hasPendingDraft ?? false,
    orderBy: options.orderBy ?? QualityTargetOrder.UNSPECIFIED,
  });
  const response = await dataQualityClient.listQualityTargets(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityTargets,
    nextPageToken: response.nextPageToken,
  };
}

/**
 * One target's workspace: its health summary, every live rule and its unresolved
 * issues. AI drafts are absent because reading them needs the suggest
 * permission this RPC does not require; the caller reads that queue separately.
 * A target with no rules still resolves when its metadata type is known.
 */
export async function getQualityTarget(options: {
  targetGuid: string;
  /** UNSPECIFIED lets the server derive it from the target's rules. */
  metaType?: MetaType;
  signal?: AbortSignal;
}): Promise<GetQualityTargetResponse> {
  const request = create(GetQualityTargetRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
  });
  return await dataQualityClient.getQualityTarget(request, {
    signal: options.signal,
  });
}

/**
 * One target's recorded lineage impact: the assets its data reaches downstream
 * and the upstream targets whose checks failed or whose structure changed in
 * the window before `asOf`. Both lists are bounded walks over the recorded
 * lineage, so an empty list means no recorded edge, not "no consumers".
 */
export async function getQualityTargetImpact(options: {
  targetGuid: string;
  metaType: MetaType;
  /** How many levels downstream to walk, 1-3; zero means two. */
  depth?: number;
  /** The instant the root-cause window ends; unset means now. */
  asOf?: Timestamp;
  /** How far back root-cause evidence is read, in hours 1-168; zero means 24. */
  windowHours?: number;
  signal?: AbortSignal;
}): Promise<GetQualityTargetImpactResponse> {
  const request = create(GetQualityTargetImpactRequestSchema, {
    parent: WORKSPACE_PARENT,
    target: create(QualityTargetKeySchema, {
      targetGuid: options.targetGuid,
      metaType: options.metaType,
    }),
    depth: options.depth ?? 2,
    asOf: options.asOf,
    windowHours: options.windowHours ?? 24,
  });
  return await dataQualityClient.getQualityTargetImpact(request, {
    signal: options.signal,
  });
}

/**
 * Re-baseline a target's checks to its current structure, clearing the
 * schema-changed signal after a review. The checks keep their definitions; only
 * their saved structure snapshot moves, so no rule revision is written. Returns
 * how many checks moved (zero when they were already current).
 */
export async function acknowledgeQualityTargetSchema(options: {
  targetGuid: string;
  /** UNSPECIFIED lets the server derive it from the target's rules. */
  metaType?: MetaType;
}): Promise<number> {
  const request = create(AcknowledgeQualityTargetSchemaRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
  });
  const response =
    await dataQualityClient.acknowledgeQualityTargetSchema(request);
  return response.baselinedRuleCount;
}

/**
 * Set a target's one schedule. Every enabled check on the target runs at this
 * cadence; scheduling is rescheduled from now and does not run the checks. The
 * two forms are exclusive: a fixed interval, or a five-field cron expression in
 * an IANA time zone.
 */
export async function updateQualityTargetSchedule(
  options: {
    targetGuid: string;
    /** UNSPECIFIED lets the server derive it from the target's rules. */
    metaType?: MetaType;
  } & ({ scheduleInterval: Duration } | { cron: string; timezone: string })
): Promise<QualityTargetSchedule> {
  const request = create(UpdateQualityTargetScheduleRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
    ...("scheduleInterval" in options
      ? { scheduleInterval: options.scheduleInterval }
      : { cron: options.cron, timezone: options.timezone }),
  });
  return await dataQualityClient.updateQualityTargetSchedule(request);
}

/**
 * Read one target's schedule without its rules, executions and issues. A rule
 * lifecycle change can move the target's next run, so the workspace refreshes
 * this instead of re-reading the whole target after every check edit.
 */
export async function getQualityTargetSchedule(options: {
  targetGuid: string;
  /** UNSPECIFIED lets the server derive it from the target's rules. */
  metaType?: MetaType;
  signal?: AbortSignal;
}): Promise<QualityTargetSchedule> {
  const request = create(GetQualityTargetScheduleRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
  });
  return await dataQualityClient.getQualityTargetSchedule(request, {
    signal: options.signal,
  });
}

// ---- contract ----

/**
 * Read one target's contract: its target-level defaults, the newest published
 * version and whether the live rules differ from that snapshot. The target is
 * named by GUID, like GetQualityTarget; a contract is a projection of the rules,
 * so it needs no resource name of its own.
 */
export async function getQualityContract(options: {
  targetGuid: string;
  /** UNSPECIFIED lets the server derive it from the target's rules. */
  metaType?: MetaType;
  signal?: AbortSignal;
}): Promise<QualityContract> {
  const request = create(GetQualityContractRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
  });
  return await dataQualityClient.getQualityContract(request, {
    signal: options.signal,
  });
}

/**
 * Replace the target-level defaults. The settings message is complete, so a field
 * left unset is cleared rather than kept; the server records the change as an
 * unpublished difference from the published version.
 */
export async function updateQualityContract(options: {
  targetGuid: string;
  metaType?: MetaType;
  settings: QualityContractSettings;
  /** The complete plan declaration; absent leaves the current one untouched. */
  draft?: QualityPlan;
  /** The published version the declaration is aligned to; required with draft. */
  stagedBaseVersion?: number;
}): Promise<QualityContract> {
  const request = create(UpdateQualityContractRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
    settings: options.settings,
    draft: options.draft,
    stagedBaseVersion: options.stagedBaseVersion ?? 0,
  });
  return await dataQualityClient.updateQualityContract(request);
}

/**
 * Record the live rules and settings as the target's next version. Republishing an
 * unchanged contract is a no-op: the response says so rather than creating a
 * version that promises nothing new.
 */
export async function publishQualityContract(options: {
  targetGuid: string;
  metaType?: MetaType;
  /** Why this version exists; shown in the history. */
  note?: string;
}): Promise<PublishQualityContractResponse> {
  const request = create(PublishQualityContractRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
    note: options.note ?? "",
  });
  return await dataQualityClient.publishQualityContract(request);
}

/** One page of published versions, newest first. */
export async function listQualityContractRevisions(options: {
  targetGuid: string;
  metaType?: MetaType;
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
}): Promise<ListPage<QualityContractRevision>> {
  const request = create(ListQualityContractRevisionsRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
    pageSize: options.pageSize ?? 20,
    pageToken: options.pageToken ?? "",
  });
  const response = await dataQualityClient.listQualityContractRevisions(
    request,
    { signal: options.signal }
  );
  return {
    items: response.revisions,
    nextPageToken: response.nextPageToken,
  };
}

/** One published version with its checks, so a client can diff it locally. */
export async function getQualityContractRevision(options: {
  targetGuid: string;
  metaType?: MetaType;
  version: number;
}): Promise<GetQualityContractRevisionResponse> {
  const request = create(GetQualityContractRevisionRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
    version: options.version,
  });
  return await dataQualityClient.getQualityContractRevision(request);
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
const listAllPageSize = 1000;

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
 * One check's measured history, oldest point first, plus the threshold band its
 * newest point was judged against and a threshold suggested from the same
 * history. `limit` counts judged executions, not executions: skipped or
 * never-judged runs are not points.
 */
export async function getQualityRuleTrend(
  name: string,
  options: { limit?: number; signal?: AbortSignal } = {}
): Promise<QualityRuleTrend> {
  const request = create(GetQualityRuleTrendRequestSchema, {
    name,
    limit: options.limit ?? 0,
  });
  return await dataQualityClient.getQualityRuleTrend(request, {
    signal: options.signal,
  });
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

/**
 * Update up to 200 rules in one call. Every item carries its own mask, so one
 * batch can edit different fields, and its rule's revision, so a stale item is
 * rejected instead of overwriting a newer definition. Results are aligned with
 * `requests`; a failed item carries its error and does not stop the rest.
 */
export async function batchUpdateQualityRules(
  requests: UpdateQualityRuleRequest[]
): Promise<BatchUpdateQualityRuleResult[]> {
  const request = create(BatchUpdateQualityRulesRequestSchema, {
    parent: WORKSPACE_PARENT,
    requests,
  });
  const response = await dataQualityClient.batchUpdateQualityRules(request);
  return response.results;
}

/** Archive a rule. Its revisions and executions stay readable. */
export async function deleteQualityRule(name: string): Promise<void> {
  const request = create(DeleteQualityRuleRequestSchema, { name });
  await dataQualityClient.deleteQualityRule(request);
}

// ---- rule templates and code tables ----

/** One page of the workspace's custom rule templates, newest first. */
export async function listQualityRuleTemplates(
  options: { pageSize?: number; pageToken?: string; signal?: AbortSignal } = {}
): Promise<ListPage<QualityRuleTemplate>> {
  const request = create(ListQualityRuleTemplatesRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
  });
  const response = await dataQualityClient.listQualityRuleTemplates(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityRuleTemplates,
    nextPageToken: response.nextPageToken,
  };
}

/** Every custom rule template the caller can see, walking the cursor pages. */
export async function listAllQualityRuleTemplates(
  options: { pageSize?: number } = {}
): Promise<QualityRuleTemplate[]> {
  return await listAll((pageToken) =>
    listQualityRuleTemplates({ pageSize: options.pageSize ?? 1000, pageToken })
  );
}

/** Read one custom rule template by resource name. */
export async function getQualityRuleTemplate(
  name: string,
  signal?: AbortSignal
): Promise<QualityRuleTemplate> {
  const request = create(GetQualityRuleTemplateRequestSchema, { name });
  return await dataQualityClient.getQualityRuleTemplate(request, { signal });
}

/**
 * Create a custom rule template. The server assigns its name and clears the
 * target-bound fields (columns, filter, schema-drift snapshot) from the config.
 */
export async function createQualityRuleTemplate(
  template: QualityRuleTemplate
): Promise<QualityRuleTemplate> {
  const request = create(CreateQualityRuleTemplateRequestSchema, {
    parent: WORKSPACE_PARENT,
    qualityRuleTemplate: template,
  });
  return await dataQualityClient.createQualityRuleTemplate(request);
}

/** Update exactly the fields named by `updateMask`. */
export async function updateQualityRuleTemplate(
  template: QualityRuleTemplate,
  updateMask: string[]
): Promise<QualityRuleTemplate> {
  const request = create(UpdateQualityRuleTemplateRequestSchema, {
    qualityRuleTemplate: template,
    updateMask: create(FieldMaskSchema, { paths: updateMask }),
  });
  return await dataQualityClient.updateQualityRuleTemplate(request);
}

/** Delete a custom template. Checks already created from it are unaffected. */
export async function deleteQualityRuleTemplate(name: string): Promise<void> {
  const request = create(DeleteQualityRuleTemplateRequestSchema, { name });
  await dataQualityClient.deleteQualityRuleTemplate(request);
}

/**
 * One page of the workspace's code tables, newest first. A summary carries each
 * table's value count but not its values; read one with GetQualityCodeTable.
 */
export async function listQualityCodeTables(
  options: { pageSize?: number; pageToken?: string; signal?: AbortSignal } = {}
): Promise<ListPage<QualityCodeTable>> {
  const request = create(ListQualityCodeTablesRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
  });
  const response = await dataQualityClient.listQualityCodeTables(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityCodeTables,
    nextPageToken: response.nextPageToken,
  };
}

/** Every code table summary the caller can see, walking the cursor pages. */
export async function listAllQualityCodeTables(
  options: { pageSize?: number } = {}
): Promise<QualityCodeTable[]> {
  return await listAll((pageToken) =>
    listQualityCodeTables({ pageSize: options.pageSize ?? 1000, pageToken })
  );
}

/** Read one code table including its values. */
export async function getQualityCodeTable(
  name: string,
  signal?: AbortSignal
): Promise<QualityCodeTable> {
  const request = create(GetQualityCodeTableRequestSchema, { name });
  return await dataQualityClient.getQualityCodeTable(request, { signal });
}

/** Create a code table. The server sorts, de-duplicates and caps the values. */
export async function createQualityCodeTable(
  table: QualityCodeTable
): Promise<QualityCodeTable> {
  const request = create(CreateQualityCodeTableRequestSchema, {
    parent: WORKSPACE_PARENT,
    qualityCodeTable: table,
  });
  return await dataQualityClient.createQualityCodeTable(request);
}

/** Update exactly the fields named by `updateMask`; replacing values is the import path. */
export async function updateQualityCodeTable(
  table: QualityCodeTable,
  updateMask: string[]
): Promise<QualityCodeTable> {
  const request = create(UpdateQualityCodeTableRequestSchema, {
    qualityCodeTable: table,
    updateMask: create(FieldMaskSchema, { paths: updateMask }),
  });
  return await dataQualityClient.updateQualityCodeTable(request);
}

/**
 * Delete a code table. A table still referenced by a rule or a template is
 * refused with FailedPrecondition rather than silently breaking those checks.
 */
export async function deleteQualityCodeTable(name: string): Promise<void> {
  const request = create(DeleteQualityCodeTableRequestSchema, { name });
  await dataQualityClient.deleteQualityCodeTable(request);
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
  /** Only the executions planned by this run. */
  run?: string;
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
    run: options.run ?? "",
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
  name: string,
  options: { signal?: AbortSignal } = {}
): Promise<QualityExecution> {
  const request = create(GetQualityExecutionRequestSchema, { name });
  return await dataQualityClient.getQualityExecution(request, {
    signal: options.signal,
  });
}

/**
 * One page of runs, newest first. A run is one dispatch batch: the executions a
 * scheduler tick or a manual request planned for exactly one target, so the run's
 * target, counts and outcome answer "what did this batch do to this table?"
 * without reading every execution.
 */
export async function listQualityRuns(options: {
  pageSize?: number;
  pageToken?: string;
  /** Only runs that planned at least one execution for this target GUID. */
  targetGuid?: string;
  /** The target's object type; pairs with targetGuid to filter by identity. */
  metaType?: MetaType;
  /** Format: instances/{instance}. */
  instance?: string;
  trigger?: QualityExecutionTrigger;
  state?: QualityExecutionState;
  outcome?: QualityStatus;
  startTime?: Timestamp;
  endTime?: Timestamp;
  signal?: AbortSignal;
}): Promise<ListPage<QualityRun>> {
  const request = create(ListQualityRunsRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    targetGuid: options.targetGuid ?? "",
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
    instance: options.instance ?? "",
    trigger: options.trigger ?? QualityExecutionTrigger.UNSPECIFIED,
    state: options.state ?? QualityExecutionState.UNSPECIFIED,
    outcome: options.outcome ?? QualityStatus.UNSPECIFIED,
    ...(options.startTime ? { startTime: options.startTime } : {}),
    ...(options.endTime ? { endTime: options.endTime } : {}),
  });
  const response = await dataQualityClient.listQualityRuns(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityRuns,
    nextPageToken: response.nextPageToken,
  };
}

/** Read one run with every target slice it touched. */
export async function getQualityRun(name: string): Promise<QualityRun> {
  const request = create(GetQualityRunRequestSchema, { name });
  return await dataQualityClient.getQualityRun(request);
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

/**
 * The share of recorded consumer assets — assets another object's lineage reads
 * — that have an enabled check, plus the unmonitored ones. It is a separate read
 * because answering it costs one grouped scan of the lineage table, which the
 * overview must not pay on every load.
 */
export async function getQualityConsumptionCoverage(
  options: {
    /** How many gaps to return, 1-100; zero means 20. */
    pageSize?: number;
    signal?: AbortSignal;
  } = {}
): Promise<GetQualityConsumptionCoverageResponse> {
  const request = create(GetQualityConsumptionCoverageRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 0,
  });
  return await dataQualityClient.getQualityConsumptionCoverage(request, {
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

// ---- LLM usage ----

/**
 * The process-wide data-quality LLM usage: today's L1/L2 and column-analysis
 * counters, the concurrency slot usage, and when the daily counters reset.
 * Read-only; requires the data-quality suggest permission.
 */
export async function getQualityLLMUsage(
  options: { parent?: string; signal?: AbortSignal } = {}
): Promise<QualityLLMUsage> {
  const request = create(GetQualityLLMUsageRequestSchema, {
    parent: options.parent ?? WORKSPACE_PARENT,
  });
  return await dataQualityClient.getQualityLLMUsage(request, {
    signal: options.signal,
  });
}

// ---- drafting jobs ----

/**
 * Submit one durable AI drafting job for several targets. The server runs the
 * analyses in the background; the returned job is safe to poll and outlives the
 * browser tab.
 *
 * An identical request while its job is still PENDING/RUNNING returns that job
 * with `deduplicated` set instead of starting a second analysis.
 */
export async function createQualitySuggestionJob(options: {
  intent: string;
  language?: string;
  targets: QualityTarget[];
  /** Re-propose drafts the queue already decided; pending ones are refreshed. */
  forceRegenerate?: boolean;
}): Promise<QualitySuggestionJob> {
  const request = create(CreateQualitySuggestionJobRequestSchema, {
    parent: WORKSPACE_PARENT,
    intent: options.intent,
    language: options.language ?? "",
    targets: options.targets,
    forceRegenerate: options.forceRegenerate ?? false,
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

/**
 * Ask the model for rule drafts and queue them as suggestions. The response
 * carries the queue summary, which explains why a pass may queue nothing.
 */
export async function suggestQualityRule(options: {
  tableGuid: string;
  metaType: MetaType;
  columns?: string[];
  intent?: string;
  language?: string;
  /** Re-propose drafts the queue already decided; pending ones are refreshed. */
  forceRegenerate?: boolean;
  signal?: AbortSignal;
}): Promise<SuggestQualityRuleResponse> {
  const request = create(SuggestQualityRuleRequestSchema, {
    parent: WORKSPACE_PARENT,
    tableGuid: options.tableGuid,
    metaType: options.metaType,
    columns: options.columns ?? [],
    intent: options.intent ?? "",
    language: options.language ?? "",
    forceRegenerate: options.forceRegenerate ?? false,
  });
  return await dataQualityClient.suggestQualityRule(request, {
    signal: options.signal,
  });
}

/**
 * Ask the model for semantic column labels and queue them as suggestions. The
 * response carries the queue summary, including the columns the pass left out.
 */
export async function suggestColumnLabels(options: {
  tableGuid: string;
  metaType: MetaType;
  columns?: string[];
  language?: string;
  /** Re-analyze labeled columns and decided proposals; pending ones are refreshed. */
  forceRegenerate?: boolean;
  signal?: AbortSignal;
}): Promise<SuggestColumnLabelsResponse> {
  const request = create(SuggestColumnLabelsRequestSchema, {
    parent: WORKSPACE_PARENT,
    tableGuid: options.tableGuid,
    metaType: options.metaType,
    columns: options.columns ?? [],
    language: options.language ?? "",
    forceRegenerate: options.forceRegenerate ?? false,
  });
  return await dataQualityClient.suggestColumnLabels(request, {
    signal: options.signal,
  });
}

/**
 * Derive checks and starting thresholds from the stored column profiles. The
 * deterministic counterpart to the label pass: no model is involved, but every
 * requested column must carry a fresh profile. The response carries the queue
 * summary, including the columns no check was derived for.
 */
export async function recommendQualityChecks(options: {
  tableGuid: string;
  metaType: MetaType;
  columns?: string[];
  language?: string;
  /** Re-propose checks already decided; pending ones are refreshed in place. */
  forceRegenerate?: boolean;
  signal?: AbortSignal;
}): Promise<RecommendQualityChecksResponse> {
  const request = create(RecommendQualityChecksRequestSchema, {
    parent: WORKSPACE_PARENT,
    tableGuid: options.tableGuid,
    metaType: options.metaType,
    columns: options.columns ?? [],
    language: options.language ?? "",
    forceRegenerate: options.forceRegenerate ?? false,
  });
  return await dataQualityClient.recommendQualityChecks(request, {
    signal: options.signal,
  });
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
  signal?: AbortSignal;
}): AsyncIterable<ExplainQualityExecutionResponse> {
  const request = create(ExplainQualityExecutionRequestSchema, {
    name: options.name,
    language: options.language ?? "",
    forceRegenerate: options.forceRegenerate ?? false,
  });
  return dataQualityClient.explainQualityExecution(request, {
    signal: options.signal,
  });
}

// ---- exports ----
//
// The CSV exports are server-streaming and select every row the directory's
// current filters match, not one page of it. The server writes the CSV itself
// (the first slice carries the UTF-8 BOM and the header row) and the client
// only concatenates the slices into one file. The report export is one unary
// call that returns a finished, self-contained HTML document.

/** The filters a CSV execution export accepts: the list's, minus the paging. */
export interface QualityExecutionExportOptions {
  state?: QualityExecutionState;
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
  /** Format: workspaces/{workspace}/qualityRuns/{quality_run}. */
  run?: string;
  /** The server's row ceiling; zero means its default (100,000). */
  maxRows?: number;
  signal?: AbortSignal;
}

/** Every execution the filters select, as one CSV file. */
export async function exportQualityExecutions(
  options: QualityExecutionExportOptions = {}
): Promise<CsvDownload> {
  const request = create(ExportQualityExecutionsRequestSchema, {
    parent: WORKSPACE_PARENT,
    state: options.state ?? QualityExecutionState.UNSPECIFIED,
    outcome: options.outcome ?? QualityStatus.UNSPECIFIED,
    trigger: options.trigger ?? QualityExecutionTrigger.UNSPECIFIED,
    rule: options.rule ?? "",
    targetGuid: options.targetGuid ?? "",
    instance: options.instance ?? "",
    initiator: options.initiator ?? "",
    run: options.run ?? "",
    maxRows: options.maxRows ?? 0,
    ...(options.startTime ? { startTime: options.startTime } : {}),
    ...(options.endTime ? { endTime: options.endTime } : {}),
  });
  return await collectCsvStream(
    dataQualityClient.exportQualityExecutions(request, {
      signal: options.signal,
    })
  );
}

/** The filters a CSV issue export accepts: the list's, minus the paging. */
export interface QualityIssueExportOptions {
  status?: QualityIssueStatus;
  severity?: QualitySeverity;
  /** Format: workspaces/{workspace}/qualityRules/{quality_rule}. */
  rule?: string;
  /** The exact target GUID. */
  targetGuid?: string;
  /** Format: users/{user}. */
  owner?: string;
  startTime?: Timestamp;
  endTime?: Timestamp;
  /** The server's row ceiling; zero means its default (100,000). */
  maxRows?: number;
  signal?: AbortSignal;
}

/** Every issue the filters select, as one CSV file. */
export async function exportQualityIssues(
  options: QualityIssueExportOptions = {}
): Promise<CsvDownload> {
  const request = create(ExportQualityIssuesRequestSchema, {
    parent: WORKSPACE_PARENT,
    status: options.status ?? QualityIssueStatus.UNSPECIFIED,
    severity: options.severity ?? QualitySeverity.UNSPECIFIED,
    rule: options.rule ?? "",
    targetGuid: options.targetGuid ?? "",
    owner: options.owner ?? "",
    maxRows: options.maxRows ?? 0,
    ...(options.startTime ? { startTime: options.startTime } : {}),
    ...(options.endTime ? { endTime: options.endTime } : {}),
  });
  return await collectCsvStream(
    dataQualityClient.exportQualityIssues(request, { signal: options.signal })
  );
}

/** The filters a CSV target export accepts: the list's, minus the paging. */
export interface QualityTargetExportOptions {
  /** A GUID path prefix, not a name match. */
  instance?: string;
  database?: string;
  schema?: string;
  table?: string;
  /** A case-insensitive substring over the target's path and GUID. */
  search?: string;
  /** Only targets whose worst latest result is this status. */
  worstStatus?: QualityStatus;
  /** Only failing targets (FAIL or ERROR); never combine with `worstStatus`. */
  failingOnly?: boolean;
  hasOpenIssue?: boolean;
  hasPendingDraft?: boolean;
  orderBy?: QualityTargetOrder;
  /** The server's row ceiling; zero means its default (100,000). */
  maxRows?: number;
  signal?: AbortSignal;
}

/** Every target the filters select, as one CSV file. */
export async function exportQualityTargets(
  options: QualityTargetExportOptions = {}
): Promise<CsvDownload> {
  const request = create(ExportQualityTargetsRequestSchema, {
    parent: WORKSPACE_PARENT,
    instance: options.instance ?? "",
    database: options.database ?? "",
    schema: options.schema ?? "",
    table: options.table ?? "",
    search: options.search ?? "",
    worstStatus: options.worstStatus ?? QualityStatus.UNSPECIFIED,
    failingOnly: options.failingOnly ?? false,
    hasOpenIssue: options.hasOpenIssue ?? false,
    hasPendingDraft: options.hasPendingDraft ?? false,
    orderBy: options.orderBy ?? QualityTargetOrder.UNSPECIFIED,
    maxRows: options.maxRows ?? 0,
  });
  return await collectCsvStream(
    dataQualityClient.exportQualityTargets(request, { signal: options.signal })
  );
}

/**
 * One target's report as a self-contained HTML document. `language` decides
 * the language its text is written in; empty means "en-US".
 */
export async function exportQualityReport(options: {
  targetGuid: string;
  /** UNSPECIFIED lets the server derive it from the target's rules. */
  metaType?: MetaType;
  /** Days of execution history; zero means 30 (capped at the 90-day retention). */
  trendDays?: number;
  language?: string;
  signal?: AbortSignal;
}): Promise<ExportQualityReportResponse> {
  const request = create(ExportQualityReportRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
    trendDays: options.trendDays ?? 0,
    language: options.language ?? "",
  });
  return await dataQualityClient.exportQualityReport(request, {
    signal: options.signal,
  });
}

// ---- change requests ----

/** One page of change requests, newest first. */
export async function listQualityChangeRequests(options: {
  targetGuid?: string;
  state?: QualityChangeRequestState;
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
}): Promise<ListPage<QualityChangeRequest>> {
  const request = create(ListQualityChangeRequestsRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid ?? "",
    state: options.state ?? QualityChangeRequestState.UNSPECIFIED,
    pageSize: options.pageSize ?? 20,
    pageToken: options.pageToken ?? "",
  });
  const response = await dataQualityClient.listQualityChangeRequests(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityChangeRequests,
    nextPageToken: response.nextPageToken,
  };
}

/** Approve (applying each item) or reject one change request. */
export async function decideQualityChangeRequest(options: {
  name: string;
  decision: DecideQualityChangeRequestRequest_Decision;
  rejectionReasons?: QualityChangeRejectionReason[];
  decisionNote?: string;
  signal?: AbortSignal;
}): Promise<QualityChangeRequest> {
  const request = create(DecideQualityChangeRequestRequestSchema, {
    name: options.name,
    decision: options.decision,
    rejectionReasons: options.rejectionReasons ?? [],
    decisionNote: options.decisionNote ?? "",
  });
  return await dataQualityClient.decideQualityChangeRequest(request, {
    signal: options.signal,
  });
}

// ---- quality agents ----

/** Create an agent principal; the returned token is shown exactly once. */
export async function createQualityAgent(options: {
  displayName: string;
  description?: string;
  signal?: AbortSignal;
}): Promise<CreateQualityAgentResponse> {
  const request = create(CreateQualityAgentRequestSchema, {
    parent: WORKSPACE_PARENT,
    displayName: options.displayName,
    description: options.description ?? "",
  });
  return await dataQualityClient.createQualityAgent(request, {
    signal: options.signal,
  });
}

/** The workspace's agent principals; tokens are never listed. */
export async function listQualityAgents(
  signal?: AbortSignal
): Promise<QualityAgent[]> {
  const request = create(ListQualityAgentsRequestSchema, {
    parent: WORKSPACE_PARENT,
  });
  const response = await dataQualityClient.listQualityAgents(request, {
    signal,
  });
  return response.qualityAgents;
}

/** Mint a new token for an agent and invalidate the previous ones. */
export async function rotateQualityAgentToken(
  name: string,
  signal?: AbortSignal
): Promise<RotateQualityAgentTokenResponse> {
  const request = create(RotateQualityAgentTokenRequestSchema, { name });
  return await dataQualityClient.rotateQualityAgentToken(request, { signal });
}

/** Disable an agent: every token it holds stops authenticating. */
export async function disableQualityAgent(
  name: string,
  signal?: AbortSignal
): Promise<QualityAgent> {
  const request = create(DisableQualityAgentRequestSchema, { name });
  return await dataQualityClient.disableQualityAgent(request, { signal });
}

// ---- quality agent sessions ----

/** The filters both session reads share; a page and a full walk accept the same. */
export interface QualityAgentSessionListOptions {
  state?: QualityAgentSessionState;
  database?: string;
  targetGuid?: string;
  /** UNSPECIFIED keeps both manual and automatic sessions. */
  source?: QualityAgentSessionSource;
  /** UNSPECIFIED keeps both monitor and triage sessions. */
  mode?: QualityAgentSessionMode;
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
}

/** One page of agent sessions, newest activity first. */
export async function listQualityAgentSessionsPage(
  options: QualityAgentSessionListOptions = {}
): Promise<ListPage<QualityAgentSession>> {
  const request = create(ListQualityAgentSessionsRequestSchema, {
    parent: WORKSPACE_PARENT,
    state: options.state ?? QualityAgentSessionState.UNSPECIFIED,
    database: options.database ?? "",
    targetGuid: options.targetGuid ?? "",
    source: options.source ?? QualityAgentSessionSource.UNSPECIFIED,
    mode: options.mode ?? QualityAgentSessionMode.UNSPECIFIED,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
  });
  const response = await dataQualityClient.listQualityAgentSessions(request, {
    signal: options.signal,
  });
  return {
    items: response.qualityAgentSessions,
    nextPageToken: response.nextPageToken,
  };
}

/**
 * The workspace's agent sessions, one live session per target, newest activity
 * first. `state` defaults to every non-archived session; archived sessions are a
 * tombstone and must be asked for explicitly.
 */
export async function listQualityAgentSessions(
  options: QualityAgentSessionListOptions = {}
): Promise<QualityAgentSession[]> {
  return await listAll(async (pageToken) => {
    const request = create(ListQualityAgentSessionsRequestSchema, {
      parent: WORKSPACE_PARENT,
      state: options.state ?? QualityAgentSessionState.UNSPECIFIED,
      database: options.database ?? "",
      targetGuid: options.targetGuid ?? "",
      source: options.source ?? QualityAgentSessionSource.UNSPECIFIED,
      mode: options.mode ?? QualityAgentSessionMode.UNSPECIFIED,
      pageSize: listAllPageSize,
      pageToken,
    });
    const response = await dataQualityClient.listQualityAgentSessions(request, {
      signal: options.signal,
    });
    return {
      items: response.qualityAgentSessions,
      nextPageToken: response.nextPageToken,
    };
  });
}

/** One target's session; the name is a real workspace name, not the `-` shorthand. */
export async function getQualityAgentSession(
  name: string,
  signal?: AbortSignal
): Promise<QualityAgentSession> {
  const request = create(GetQualityAgentSessionRequestSchema, { name });
  return await dataQualityClient.getQualityAgentSession(request, { signal });
}

/**
 * Start the one long-lived session of a target. `metaType` may be omitted when
 * the target already has rules; the server resolves it from them.
 */
export async function createQualityAgentSession(options: {
  targetGuid: string;
  metaType?: MetaType;
  objective?: string;
  agent?: string;
  signal?: AbortSignal;
}): Promise<QualityAgentSession> {
  const request = create(CreateQualityAgentSessionRequestSchema, {
    parent: WORKSPACE_PARENT,
    targetGuid: options.targetGuid,
    metaType: options.metaType ?? MetaType.UNSPECIFIED,
    objective: options.objective ?? "",
    agent: options.agent ?? "",
  });
  return await dataQualityClient.createQualityAgentSession(request, {
    signal: options.signal,
  });
}

/** Pause a session; `expectedRevision` is the revision the caller saw. */
export async function pauseQualityAgentSession(
  name: string,
  expectedRevision: number,
  signal?: AbortSignal
): Promise<QualityAgentSession> {
  const request = create(PauseQualityAgentSessionRequestSchema, {
    name,
    expectedRevision,
  });
  return await dataQualityClient.pauseQualityAgentSession(request, { signal });
}

/** Resume a paused or blocked session and make it due now. */
export async function resumeQualityAgentSession(
  name: string,
  expectedRevision: number,
  signal?: AbortSignal
): Promise<QualityAgentSession> {
  const request = create(ResumeQualityAgentSessionRequestSchema, {
    name,
    expectedRevision,
  });
  return await dataQualityClient.resumeQualityAgentSession(request, { signal });
}

/** Archive a session: it stops waking and its conversation stays readable. */
export async function archiveQualityAgentSession(
  name: string,
  expectedRevision: number,
  signal?: AbortSignal
): Promise<void> {
  const request = create(ArchiveQualityAgentSessionRequestSchema, {
    name,
    expectedRevision,
  });
  await dataQualityClient.archiveQualityAgentSession(request, { signal });
}

/**
 * Append a person's question to the conversation and wake the session.
 *
 * `requestId` is a client-generated idempotency id: repeating a wake with the
 * same id returns the session without appending a second message, so a retried
 * or double-clicked send cannot post the question twice. An empty id lets the
 * server generate a random one, which makes a repeat a new message.
 */
export async function wakeQualityAgentSession(
  name: string,
  content: string,
  options: { requestId?: string; signal?: AbortSignal } = {}
): Promise<QualityAgentSession> {
  const request = create(WakeQualityAgentSessionRequestSchema, {
    name,
    content,
    requestId: options.requestId ?? "",
  });
  return await dataQualityClient.wakeQualityAgentSession(request, {
    signal: options.signal,
  });
}

/**
 * One page of a session's transcript. With no `afterSeq` the NEWEST page is
 * returned in chronological order and `nextPageToken` walks towards older
 * messages; with `afterSeq` only newer messages are returned, ascending, and no
 * token is issued. The two modes are mutually exclusive.
 */
export async function listQualityAgentSessionMessages(
  sessionName: string,
  options: {
    afterSeq?: bigint;
    pageSize?: number;
    pageToken?: string;
    signal?: AbortSignal;
  } = {}
): Promise<{ messages: QualityAgentSessionMessage[]; nextPageToken: string }> {
  const request = create(ListQualityAgentSessionMessagesRequestSchema, {
    parent: sessionName,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    afterSeq: options.afterSeq ?? 0n,
  });
  const response = await dataQualityClient.listQualityAgentSessionMessages(
    request,
    { signal: options.signal }
  );
  return {
    messages: response.qualityAgentSessionMessages,
    nextPageToken: response.nextPageToken,
  };
}

/** The runner's live state: budgets, session counts and a process-local tick. */
export async function getQualityAgentRunnerStatus(
  signal?: AbortSignal
): Promise<QualityAgentRunnerStatus> {
  const request = create(GetQualityAgentRunnerStatusRequestSchema, {
    parent: WORKSPACE_PARENT,
  });
  return await dataQualityClient.getQualityAgentRunnerStatus(request, {
    signal,
  });
}

/** The runner's evaluation: session counts, spend and proposal outcomes. */
export async function getQualityAgentMetrics(
  signal?: AbortSignal
): Promise<QualityAgentMetrics> {
  const request = create(GetQualityAgentMetricsRequestSchema, {
    parent: WORKSPACE_PARENT,
  });
  return await dataQualityClient.getQualityAgentMetrics(request, { signal });
}

// ---- quality agent enrollments ----

/** The resource-name segment an enrollment's id occupies. */
const ENROLLMENT_SEGMENT = "qualityAgentEnrollments";

/** Format: workspaces/{workspace}/qualityAgentEnrollments/{id}. */
export function qualityAgentEnrollmentName(id: string): string {
  return `${WORKSPACE_PARENT}/${ENROLLMENT_SEGMENT}/${id}`;
}

/** The id part of an enrollment's resource name; the name itself if it has none. */
export function qualityAgentEnrollmentID(name: string): string {
  return name.slice(name.lastIndexOf("/") + 1);
}

/** The filters and paging of the queue. */
export interface QualityAgentEnrollmentListOptions {
  /** An exact database resource name; empty keeps every database. */
  database?: string;
  /** UNSPECIFIED keeps every state. */
  state?: QualityAgentEnrollmentState;
  /** UNSPECIFIED keeps both modes. */
  mode?: QualityAgentSessionMode;
  /** A case-insensitive substring over the target path and GUID. */
  search?: string;
  pageSize?: number;
  pageToken?: string;
  signal?: AbortSignal;
}

/** One page of the runner's enrollment queue. */
export async function listQualityAgentEnrollments(
  options: QualityAgentEnrollmentListOptions = {}
): Promise<ListPage<QualityAgentEnrollment>> {
  const request = create(ListQualityAgentEnrollmentsRequestSchema, {
    parent: WORKSPACE_PARENT,
    pageSize: options.pageSize ?? 50,
    pageToken: options.pageToken ?? "",
    database: options.database ?? "",
    state: options.state ?? QualityAgentEnrollmentState.UNSPECIFIED,
    mode: options.mode ?? QualityAgentSessionMode.UNSPECIFIED,
    search: options.search ?? "",
  });
  const response = await dataQualityClient.listQualityAgentEnrollments(
    request,
    {
      signal: options.signal,
    }
  );
  return {
    items: response.qualityAgentEnrollments,
    nextPageToken: response.nextPageToken,
  };
}

/**
 * The queue at a glance: its state counts, a per-database breakdown, and when
 * discovery last scanned a database (process-local, so a restart clears it).
 */
export async function getQualityAgentEnrollmentSummary(
  signal?: AbortSignal
): Promise<QualityAgentEnrollmentSummary> {
  const request = create(GetQualityAgentEnrollmentSummaryRequestSchema, {
    parent: WORKSPACE_PARENT,
  });
  return await dataQualityClient.getQualityAgentEnrollmentSummary(request, {
    signal,
  });
}

/**
 * Apply a person's queue change with a revision compare-and-set. Only `state`
 * (PENDING to re-include, EXCLUDED to opt out) and `mode` may be masked.
 */
export async function updateQualityAgentEnrollment(options: {
  enrollment: QualityAgentEnrollment;
  updateMask: string[];
  expectedRevision: number;
}): Promise<QualityAgentEnrollment> {
  const request = create(UpdateQualityAgentEnrollmentRequestSchema, {
    enrollment: options.enrollment,
    updateMask: create(FieldMaskSchema, { paths: options.updateMask }),
    expectedRevision: options.expectedRevision,
  });
  return await dataQualityClient.updateQualityAgentEnrollment(request);
}

/** Requeue one enrollment now with a priority bump. */
export async function retryQualityAgentEnrollment(
  name: string,
  expectedRevision: number,
  signal?: AbortSignal
): Promise<QualityAgentEnrollment> {
  const request = create(RetryQualityAgentEnrollmentRequestSchema, {
    name,
    expectedRevision,
  });
  return await dataQualityClient.retryQualityAgentEnrollment(request, {
    signal,
  });
}

// ---- investigations and memories ----

/** The target's investigations, newest first. */
export async function listQualityInvestigations(
  targetGuid: string,
  signal?: AbortSignal
): Promise<QualityInvestigation[]> {
  return await listAll(async (pageToken) => {
    const request = create(ListQualityInvestigationsRequestSchema, {
      parent: WORKSPACE_PARENT,
      targetGuid,
      pageSize: listAllPageSize,
      pageToken,
    });
    const response = await dataQualityClient.listQualityInvestigations(
      request,
      {
        signal,
      }
    );
    return {
      items: response.qualityInvestigations,
      nextPageToken: response.nextPageToken,
    };
  });
}

/** The memories recorded about one subject (a target GUID here), newest first. */
export async function listQualityMemories(
  subject: string,
  signal?: AbortSignal
): Promise<QualityMemory[]> {
  return await listAll(async (pageToken) => {
    const request = create(ListQualityMemoriesRequestSchema, {
      parent: WORKSPACE_PARENT,
      subject,
      pageSize: listAllPageSize,
      pageToken,
    });
    const response = await dataQualityClient.listQualityMemories(request, {
      signal,
    });
    return {
      items: response.qualityMemories,
      nextPageToken: response.nextPageToken,
    };
  });
}

/** Confirm or reject a proposed memory: a person is what turns it into knowledge. */
export async function confirmQualityMemory(
  name: string,
  decision: ConfirmQualityMemoryRequest_Decision,
  signal?: AbortSignal
): Promise<QualityMemory> {
  const request = create(ConfirmQualityMemoryRequestSchema, { name, decision });
  return await dataQualityClient.confirmQualityMemory(request, { signal });
}

/** Record a memory: an asset fact, a decision with its reason, or an event. */
export async function createQualityMemory(options: {
  kind: QualityMemoryKind;
  subject: string;
  statement: string;
  evidence: Array<{ kind: string; reference: string; note?: string }>;
  signal?: AbortSignal;
}): Promise<QualityMemory> {
  const request = create(CreateQualityMemoryRequestSchema, {
    parent: WORKSPACE_PARENT,
    kind: options.kind,
    subject: options.subject,
    statement: options.statement,
    evidence: options.evidence.map((reference) => ({
      kind: reference.kind,
      reference: reference.reference,
      note: reference.note ?? "",
    })),
  });
  return await dataQualityClient.createQualityMemory(request, {
    signal: options.signal,
  });
}
