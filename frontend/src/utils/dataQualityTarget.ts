// The target workspace's pure projections.
//
// The server hands the directory one `QualityTargetSummary` per target, and the
// workspace reads a target's rules back as one list. These helpers keep the
// pages about layout: a summary is named, searched and sorted from its own
// fields, and rules become dataset-level, cross-column or per-column sections.
// Nothing here fetches or renders.

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  type QualityIssue,
  QualityIssueStatus,
  type QualityRule,
  type QualityRuleConfig,
  QualityRuleState,
  QualityStatus,
  type QualitySuggestion,
  QualitySuggestionKind,
  QualitySuggestionStatus,
  QualityTargetOrder,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  formatTargetPath,
  type QualityTargetScope,
  targetScopeLabel,
} from "@/utils/dataQuality";
import {
  ruleDraftOf,
  suggestionTargetPath,
} from "@/utils/dataQualitySuggestion";
import { type Query, queryString } from "@/utils/queryFilters";

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

/**
 * The single column a typed config observes, or "" for table-level and
 * multi-column checks.
 *
 * The config is authoritative (the target path's column is only a display
 * snapshot and may be empty). Filter columns and schema-drift snapshots are
 * deliberately excluded: they are read, not checked.
 */
function configColumn(config: QualityRuleConfig | undefined): string {
  const arm = config?.config;
  if (!arm) {
    return "";
  }
  const value = arm.value as Record<string, unknown>;
  switch (arm.case) {
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

/** The column a rule is filed under; empty for dataset-level rules. */
export function ruleColumn(rule: QualityRule): string {
  return configColumn(rule.config) || (rule.target?.path?.column?.trim() ?? "");
}

/**
 * Whether a rule belongs in the cross-column section. It is decided by the
 * config arm only: `target.columns` also lists filter columns and the
 * schema-drift snapshot, so it must not be read as the checked column set.
 */
export function isCrossColumnRule(rule: QualityRule): boolean {
  if (ruleColumn(rule)) {
    return false;
  }
  return CROSS_COLUMN_CASES.has(rule.config?.config.case ?? "");
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
  return configColumn(ruleDraftOf(suggestion)?.config);
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

function isUnresolved(issue: QualityIssue): boolean {
  return issue.status !== RESOLVED_ISSUE;
}

/** How bad a status is, for picking the worst latest result. */
function statusRank(status: QualityStatus): number {
  switch (status) {
    case QualityStatus.ERROR:
      return 6;
    case QualityStatus.FAIL:
      return 5;
    case QualityStatus.WARN:
      return 4;
    case QualityStatus.PASS:
      return 3;
    // NOT_EVALUATED means the check ran but had nothing to judge; SKIPPED means
    // it was deliberately not run. They are different outcomes, so they rank
    // separately here exactly as the server's aggregate does.
    case QualityStatus.NOT_EVALUATED:
      return 2;
    case QualityStatus.SKIPPED:
      return 1;
    default:
      return 0;
  }
}

/** Rules of a target, excluding the archived ones a directory would hide. */
function liveRules(rules: readonly QualityRule[]): QualityRule[] {
  return rules.filter((rule) => rule.state !== QualityRuleState.ARCHIVED);
}

/** The target's GUID, empty when the summary carries no target. */
function summaryGuid(summary: QualityTargetSummary): string {
  return summary.target?.metaGuid?.trim() ?? "";
}

/** The object name the summary's own path snapshot carries, when it has one. */
function summaryObject(summary: QualityTargetSummary): string {
  return summary.target?.path?.object?.trim() ?? "";
}

/** The target's name for a heading: the object name, else the GUID's own. */
export function targetDisplayName(summary: QualityTargetSummary): string {
  const guid = summaryGuid(summary);
  return summaryObject(summary) || suggestionTargetPath(guid).object || guid;
}

/** The target's coarse location for a subtitle: `instance · database`. */
export function targetScope(summary: QualityTargetSummary): string {
  const guid = summaryGuid(summary);
  const fromPath = targetScopeLabel(summary.target?.path);
  if (fromPath !== "-") {
    return fromPath;
  }
  const fromGuid = targetScopeLabel(suggestionTargetPath(guid));
  return fromGuid === "-" ? guid || "-" : fromGuid;
}

/** The target's full display path, e.g. `schema.orders` or `db.schema.orders`. */
export function targetFullPath(summary: QualityTargetSummary): string {
  const guid = summaryGuid(summary);
  const fromPath = formatTargetPath(summary.target?.path);
  return fromPath === "-" && guid
    ? formatTargetPath(suggestionTargetPath(guid))
    : fromPath;
}

/** Whether a target matches a free-text search over its path and GUID. */
export function targetMatchesSearch(
  summary: QualityTargetSummary,
  query: string
): boolean {
  const term = query.trim().toLowerCase();
  if (!term) {
    return true;
  }
  const guid = summaryGuid(summary);
  const path = summary.target?.path;
  const fallback = suggestionTargetPath(guid);
  const haystack = [
    path?.instance || fallback.instance,
    path?.database || fallback.database,
    path?.schema || fallback.schema,
    path?.object || fallback.object,
    guid,
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return haystack.includes(term);
}

function compareText(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

/** How a run time orders against another; never-run sorts after every run. */
function compareRunTime(a: Timestamp | undefined, b: Timestamp | undefined) {
  if (!a && !b) {
    return 0;
  }
  if (!a) {
    return 1;
  }
  if (!b) {
    return -1;
  }
  const aSeconds = a.seconds ?? 0n;
  const bSeconds = b.seconds ?? 0n;
  if (aSeconds !== bSeconds) {
    return aSeconds > bSeconds ? -1 : 1;
  }
  return (b.nanos ?? 0) - (a.nanos ?? 0);
}

/**
 * Sort summaries for the directory. Attention ranks the worst latest result
 * first (ERROR outranks FAIL: a check that could not run needs attention more
 * than one whose data failed), then open issues and pending drafts. Every order
 * falls back to the name and then the GUID, so ties stay stable across loads.
 */
export function sortQualityTargets(
  summaries: readonly QualityTargetSummary[],
  order: QualityTargetOrder
): QualityTargetSummary[] {
  const sorted = [...summaries];
  sorted.sort((a, b) => {
    switch (order) {
      case QualityTargetOrder.NAME:
        return (
          compareText(targetDisplayName(a), targetDisplayName(b)) ||
          compareText(summaryGuid(a), summaryGuid(b))
        );
      case QualityTargetOrder.RULE_COUNT:
        return (
          b.ruleCount - a.ruleCount ||
          compareText(targetDisplayName(a), targetDisplayName(b)) ||
          compareText(summaryGuid(a), summaryGuid(b))
        );
      case QualityTargetOrder.LAST_RUN:
        return (
          compareRunTime(a.lastRunTime, b.lastRunTime) ||
          compareText(targetDisplayName(a), targetDisplayName(b)) ||
          compareText(summaryGuid(a), summaryGuid(b))
        );
      case QualityTargetOrder.ATTENTION:
      default:
        return (
          statusRank(b.worstStatus) - statusRank(a.worstStatus) ||
          b.openIssueCount - a.openIssueCount ||
          b.pendingDraftCount - a.pendingDraftCount ||
          compareText(targetDisplayName(a), targetDisplayName(b)) ||
          compareText(summaryGuid(a), summaryGuid(b))
        );
    }
  });
  return sorted;
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

/** The target-scope filter from a URL; no instance means no scope. */
export function scopeFromQuery(query: Query): QualityTargetScope | undefined {
  const instance = queryString(query.instance).trim();
  if (!instance) {
    return undefined;
  }
  const database = queryString(query.database).trim();
  const schema = queryString(query.schema).trim();
  const table = queryString(query.table).trim();
  return {
    instance,
    database,
    schema,
    table,
    // The picker's label is "instance title > path"; a URL carries ids, so the
    // id stands in until the reader picks the scope again.
    label: [instance, database, schema, table].filter(Boolean).join(" > "),
  };
}

/** Add a scope's ids to a query; the picker's label is not a URL value. */
export function writeScopeToQuery(
  query: Record<string, string>,
  scope: QualityTargetScope | undefined
): void {
  if (!scope?.instance) {
    return;
  }
  query.instance = scope.instance;
  if (scope.database) {
    query.database = scope.database;
  }
  if (scope.schema) {
    query.schema = scope.schema;
  }
  if (scope.table) {
    query.table = scope.table;
  }
}
