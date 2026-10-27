// The target workspace's pure projections.
//
// A quality rule names one target; the workspace reads a target's rules back as
// one list. These helpers do that grouping so the pages stay about layout: rules
// become dataset-level, cross-column or per-column sections, and a target's
// rule/issue/draft counts and worst latest result become one health summary.
// Nothing here fetches or renders.

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  type QualityExecutionSummary,
  type QualityIssue,
  QualityIssueStatus,
  type QualityRule,
  QualityRuleState,
  QualityStatus,
  type QualitySuggestion,
  QualitySuggestionKind,
  QualitySuggestionStatus,
  type QualityTargetPath,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { formatTargetPath, targetScopeLabel } from "@/utils/dataQuality";
import {
  ruleDraftOf,
  suggestionTargetPath,
} from "@/utils/dataQualitySuggestion";

/** The target identity a rule, execution or issue carries. */
export interface QualityTargetIdentity {
  guid: string;
  metaType: MetaType;
}

/** One target's rules with the counts its list row and header show. */
export interface QualityTargetSummary extends QualityTargetIdentity {
  path: QualityTargetPath | undefined;
  rules: QualityRule[];
  ruleCount: number;
  enabledCount: number;
  pausedCount: number;
  /** Distinct columns the target's rules read, in section order. */
  columns: string[];
  openIssueCount: number;
  pendingDraftCount: number;
  pendingLabelCount: number;
  /** The worst latest result across the target's rules; unset when none ran. */
  worstStatus: QualityStatus | undefined;
  lastRunTime: Timestamp | undefined;
  /** True when at least one rule on the target can no longer resolve it. */
  orphaned: boolean;
  /** True when at least one rule's saved schema hash no longer matches. */
  schemaChanged: boolean;
}

/** The three sections a target's checks are read in. */
export type TargetCheckSectionKind = "dataset" | "multi" | "column";

/** One section of the workspace: dataset rules, cross-column rules, or a column. */
export interface TargetCheckSection {
  /** Stable key: `dataset`, `multi`, or `column:<name>`. */
  key: string;
  kind: TargetCheckSectionKind;
  /** The column name; empty for the dataset and cross-column sections. */
  column: string;
  rules: QualityRule[];
  /** Pending rule drafts that belong to this section. */
  drafts: QualitySuggestion[];
}

/** Rule types whose config relates several columns and names no single one. */
const CROSS_COLUMN_CASES = new Set([
  "uniqueCombination",
  "crossColumn",
  "celExpression",
  "groupCheck",
]);

const RESOLVED_ISSUE = QualityIssueStatus.RESOLVED;

/** The column a rule is filed under; empty for dataset-level rules. */
export function ruleColumn(rule: QualityRule): string {
  return rule.target?.path?.column?.trim() ?? "";
}

/** Whether a rule belongs in the cross-column section. */
export function isCrossColumnRule(rule: QualityRule): boolean {
  if (ruleColumn(rule)) {
    return false;
  }
  const configCase = rule.config?.config.case ?? "";
  if (CROSS_COLUMN_CASES.has(configCase)) {
    return true;
  }
  return (rule.target?.columns.length ?? 0) > 1;
}

/** The section a rule is filed under. */
export function ruleSectionKey(rule: QualityRule): string {
  const column = ruleColumn(rule);
  if (column) {
    return `column:${column}`;
  }
  return isCrossColumnRule(rule) ? "multi" : "dataset";
}

/**
 * The column a rule draft names, when it names exactly one.
 *
 * The draft payload holds the typed config, not a target path, so the column is
 * read from the config arm. A draft for several columns (or for none) has no
 * single column and is filed with the cross-column or dataset section.
 */
export function draftColumn(suggestion: QualitySuggestion): string {
  const config = ruleDraftOf(suggestion)?.config?.config;
  if (!config) {
    return "";
  }
  const value = config.value as Record<string, unknown>;
  switch (config.case) {
    case "nullPercent":
    case "unique":
    case "acceptedValues":
    case "range":
    case "regex":
    case "stringLength":
    case "aggregate":
      return String(value.column ?? "").trim();
    case "freshness":
      return String(value.timestampColumn ?? "").trim();
    case "referentialIntegrity":
      return String(value.childColumn ?? "").trim();
    default:
      return "";
  }
}

/** The section a pending draft is filed under. */
export function draftSectionKey(suggestion: QualitySuggestion): string {
  const column = draftColumn(suggestion);
  if (column) {
    return `column:${column}`;
  }
  const configCase = ruleDraftOf(suggestion)?.config?.config.case ?? "";
  return CROSS_COLUMN_CASES.has(configCase) ? "multi" : "dataset";
}

function isPendingRuleDraft(suggestion: QualitySuggestion): boolean {
  return (
    suggestion.status === QualitySuggestionStatus.PENDING &&
    suggestion.kind === QualitySuggestionKind.RULE_DRAFT
  );
}

function isPendingLabel(suggestion: QualitySuggestion): boolean {
  return (
    suggestion.status === QualitySuggestionStatus.PENDING &&
    suggestion.kind === QualitySuggestionKind.LABEL
  );
}

function isUnresolved(issue: QualityIssue): boolean {
  return issue.status !== RESOLVED_ISSUE;
}

/** How bad a status is, for picking the worst latest result. */
function statusRank(status: QualityStatus): number {
  switch (status) {
    case QualityStatus.ERROR:
      return 5;
    case QualityStatus.FAIL:
      return 4;
    case QualityStatus.WARN:
      return 3;
    case QualityStatus.PASS:
      return 2;
    case QualityStatus.NOT_EVALUATED:
      return 1;
    case QualityStatus.SKIPPED:
      return 1;
    default:
      return 0;
  }
}

/**
 * The worst status of a set of latest results, ignoring rules that never ran.
 * ERROR outranks FAIL: a rule that could not be evaluated needs attention more
 * than one whose data failed.
 */
export function worstQualityStatus(
  statuses: Iterable<QualityStatus | undefined>
): QualityStatus | undefined {
  let worst: QualityStatus | undefined;
  for (const status of statuses) {
    if (status === undefined || status === QualityStatus.UNSPECIFIED) {
      continue;
    }
    if (worst === undefined || statusRank(status) > statusRank(worst)) {
      worst = status;
    }
  }
  return worst;
}

function latestTime(
  a: Timestamp | undefined,
  b: Timestamp | undefined
): Timestamp | undefined {
  if (!a) {
    return b;
  }
  if (!b) {
    return a;
  }
  const aSeconds = a.seconds ?? 0n;
  const bSeconds = b.seconds ?? 0n;
  if (aSeconds !== bSeconds) {
    return aSeconds > bSeconds ? a : b;
  }
  return (a.nanos ?? 0) >= (b.nanos ?? 0) ? a : b;
}

function finishTime(
  summary: QualityExecutionSummary | undefined
): Timestamp | undefined {
  return summary?.finishTime;
}

/** Rules of a target, excluding the archived ones a directory would hide. */
function liveRules(rules: readonly QualityRule[]): QualityRule[] {
  return rules.filter((rule) => rule.state !== QualityRuleState.ARCHIVED);
}

/** The distinct columns a target's rules read, sorted for stable section order. */
export function targetColumns(rules: readonly QualityRule[]): string[] {
  const columns = new Set<string>();
  for (const rule of rules) {
    const column = ruleColumn(rule);
    if (column) {
      columns.add(column);
    }
  }
  return [...columns].sort((a, b) => (a < b ? -1 : a > b ? 1 : 0));
}

/**
 * Group rules, issues and pending suggestions into one summary per target.
 *
 * The list page reads every rule once and hands the rows here; the target key is
 * the rule's own target GUID, so a target with no rules never appears. Issues and
 * suggestions are counted by target GUID as well; they do not create a target.
 */
export function summarizeQualityTargets(
  rules: readonly QualityRule[],
  issues: readonly QualityIssue[] = [],
  suggestions: readonly QualitySuggestion[] = []
): QualityTargetSummary[] {
  const summaries = new Map<string, QualityTargetSummary>();
  for (const rule of liveRules(rules)) {
    const guid = rule.target?.metaGuid?.trim() ?? "";
    if (!guid) {
      continue;
    }
    let summary = summaries.get(guid);
    if (!summary) {
      summary = {
        guid,
        metaType: rule.target?.metaType ?? MetaType.UNSPECIFIED,
        path: rule.target?.path,
        rules: [],
        ruleCount: 0,
        enabledCount: 0,
        pausedCount: 0,
        columns: [],
        openIssueCount: 0,
        pendingDraftCount: 0,
        pendingLabelCount: 0,
        worstStatus: undefined,
        lastRunTime: undefined,
        orphaned: false,
        schemaChanged: false,
      };
      summaries.set(guid, summary);
    }
    summary.rules.push(rule);
    summary.ruleCount += 1;
    if (rule.state === QualityRuleState.PAUSED) {
      summary.pausedCount += 1;
    } else if (rule.state === QualityRuleState.ENABLED) {
      summary.enabledCount += 1;
    }
    if (rule.orphanedReason) {
      summary.orphaned = true;
    }
    if (rule.targetSchemaChanged) {
      summary.schemaChanged = true;
    }
    summary.worstStatus = worstQualityStatus([
      summary.worstStatus,
      rule.lastResult?.status,
    ]);
    summary.lastRunTime = latestTime(
      summary.lastRunTime,
      finishTime(rule.lastResult)
    );
    summary.columns = targetColumns(summary.rules);
  }
  for (const issue of issues) {
    if (!isUnresolved(issue)) {
      continue;
    }
    const guid = issue.target?.metaGuid?.trim() ?? "";
    const summary = summaries.get(guid);
    if (summary) {
      summary.openIssueCount += 1;
    }
  }
  for (const suggestion of suggestions) {
    const guid = suggestion.target?.metaGuid?.trim() ?? "";
    const summary = summaries.get(guid);
    if (!summary) {
      continue;
    }
    if (isPendingRuleDraft(suggestion)) {
      summary.pendingDraftCount += 1;
    } else if (isPendingLabel(suggestion)) {
      summary.pendingLabelCount += 1;
    }
  }
  return [...summaries.values()];
}

/** How the target list is ordered. */
export type QualityTargetOrder = "attention" | "name" | "rules" | "lastRun";

function attentionRank(summary: QualityTargetSummary): number {
  return statusRank(summary.worstStatus ?? QualityStatus.UNSPECIFIED);
}

function summaryName(summary: QualityTargetSummary): string {
  return (
    summary.path?.object ||
    suggestionTargetPath(summary.guid).object ||
    summary.guid
  );
}

function compareText(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

/** Sort summaries for the list page; ties fall back to the target name. */
export function sortQualityTargets(
  summaries: readonly QualityTargetSummary[],
  order: QualityTargetOrder
): QualityTargetSummary[] {
  const sorted = [...summaries];
  sorted.sort((a, b) => {
    switch (order) {
      case "name":
        return compareText(summaryName(a), summaryName(b));
      case "rules":
        return (
          b.ruleCount - a.ruleCount ||
          compareText(summaryName(a), summaryName(b))
        );
      case "lastRun": {
        const diff = toSeconds(b.lastRunTime) - toSeconds(a.lastRunTime);
        return diff !== 0 ? diff : compareText(summaryName(a), summaryName(b));
      }
      case "attention":
      default:
        return (
          attentionRank(b) - attentionRank(a) ||
          b.openIssueCount - a.openIssueCount ||
          b.pendingDraftCount - a.pendingDraftCount ||
          compareText(summaryName(a), summaryName(b))
        );
    }
  });
  return sorted;
}

function toSeconds(time: Timestamp | undefined): number {
  return time?.seconds !== undefined ? Number(time.seconds) : 0;
}

/** Whether a target matches a free-text search over its path and instance. */
export function targetMatchesSearch(
  summary: QualityTargetSummary,
  query: string
): boolean {
  const term = query.trim().toLowerCase();
  if (!term) {
    return true;
  }
  const path = summary.path;
  const fallback = suggestionTargetPath(summary.guid);
  const haystack = [
    path?.instance || fallback.instance,
    path?.database || fallback.database,
    path?.schema || fallback.schema,
    path?.object || fallback.object,
    summary.guid,
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return haystack.includes(term);
}

/** A target's name for a heading: the object name, or the formatted path. */
export function targetDisplayName(summary: QualityTargetSummary): string {
  return summary.path?.object?.trim() || summaryName(summary);
}

/** The target's coarse location for a subtitle: `instance · database`. */
export function targetScope(summary: QualityTargetSummary): string {
  const path = summary.path ?? suggestionTargetPath(summary.guid);
  return targetScopeLabel(path) || summary.guid;
}

/** The target's full display path, e.g. `schema.orders` or `db.schema.orders`. */
export function targetFullPath(summary: QualityTargetSummary): string {
  return formatTargetPath(summary.path ?? suggestionTargetPath(summary.guid));
}

/**
 * One target's rules and pending drafts as ordered sections: dataset-level first,
 * then cross-column, then each covered column. Issues are grouped by rule for the
 * row-level badge and do not form a section of their own.
 */
export function groupTargetChecks(
  rules: readonly QualityRule[],
  drafts: readonly QualitySuggestion[] = []
): TargetCheckSection[] {
  const sections = new Map<string, TargetCheckSection>();
  const section = (
    key: string,
    kind: TargetCheckSectionKind,
    column: string
  ) => {
    let found = sections.get(key);
    if (!found) {
      found = { key, kind, column, rules: [], drafts: [] };
      sections.set(key, found);
    }
    return found;
  };
  for (const rule of liveRules(rules)) {
    const key = ruleSectionKey(rule);
    const kind: TargetCheckSectionKind = key.startsWith("column:")
      ? "column"
      : key === "multi"
        ? "multi"
        : "dataset";
    section(key, kind, kind === "column" ? ruleColumn(rule) : "").rules.push(
      rule
    );
  }
  for (const draft of drafts) {
    if (!isPendingRuleDraft(draft)) {
      continue;
    }
    const key = draftSectionKey(draft);
    const kind: TargetCheckSectionKind = key.startsWith("column:")
      ? "column"
      : key === "multi"
        ? "multi"
        : "dataset";
    section(key, kind, kind === "column" ? draftColumn(draft) : "").drafts.push(
      draft
    );
  }
  const rank = (kind: TargetCheckSectionKind) =>
    kind === "dataset" ? 0 : kind === "multi" ? 1 : 2;
  return [...sections.values()].sort(
    (a, b) => rank(a.kind) - rank(b.kind) || compareText(a.column, b.column)
  );
}

/** The unresolved issues of one rule, newest first. */
export function unresolvedIssuesByRule(
  issues: readonly QualityIssue[]
): Map<string, QualityIssue[]> {
  const byRule = new Map<string, QualityIssue[]>();
  for (const issue of issues) {
    if (!isUnresolved(issue)) {
      continue;
    }
    const rule = issue.rule;
    const list = byRule.get(rule) ?? [];
    list.push(issue);
    byRule.set(rule, list);
  }
  return byRule;
}
