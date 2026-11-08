import {
  type QualityAgentDatabaseEnrollmentSummary,
  type QualityAgentEnrollment,
  QualityAgentEnrollmentState,
  type QualityAgentEnrollmentSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatTargetPath, type QualityBadgeVariant } from "./dataQuality";

/**
 * The enrollment queue's vocabulary, as i18n keys and display strings.
 *
 * An enrollment row is the desired state of one target in the runner's database
 * scope; a session is created lazily when the row is admitted. These helpers keep
 * the queue page and the settings confirmation rendering the same state, priority
 * reason and estimate the same way, and keep the proto enum off the screen.
 *
 * The keys the switch helpers return are declared in
 * `scripts/check-vue-i18n.mjs`'s DYNAMIC_PREFIXES: the checker cannot follow a key
 * a helper returns.
 */

/** The order the summary shows the queue's states in: the workflow's order. */
export const ENROLLMENT_STATE_ORDER: readonly QualityAgentEnrollmentState[] = [
  QualityAgentEnrollmentState.PENDING,
  QualityAgentEnrollmentState.ACTIVE,
  QualityAgentEnrollmentState.DORMANT,
  QualityAgentEnrollmentState.EXCLUDED,
  QualityAgentEnrollmentState.OUT_OF_SCOPE,
  QualityAgentEnrollmentState.GONE,
];

/** The label key for an enrollment's queue state. */
export function enrollmentStateKey(state: QualityAgentEnrollmentState): string {
  switch (state) {
    case QualityAgentEnrollmentState.PENDING:
      return "dataQuality.agent.enrollment.state.pending";
    case QualityAgentEnrollmentState.ACTIVE:
      return "dataQuality.agent.enrollment.state.active";
    case QualityAgentEnrollmentState.DORMANT:
      return "dataQuality.agent.enrollment.state.dormant";
    case QualityAgentEnrollmentState.EXCLUDED:
      return "dataQuality.agent.enrollment.state.excluded";
    case QualityAgentEnrollmentState.OUT_OF_SCOPE:
      return "dataQuality.agent.enrollment.state.outOfScope";
    case QualityAgentEnrollmentState.GONE:
      return "dataQuality.agent.enrollment.state.gone";
    default:
      return "dataQuality.agent.enrollment.state.unspecified";
  }
}

/** The badge variant for an enrollment's state. Exclusion and a lost object warn. */
export function enrollmentStateVariant(
  state: QualityAgentEnrollmentState
): QualityBadgeVariant {
  switch (state) {
    case QualityAgentEnrollmentState.PENDING:
      return "info";
    case QualityAgentEnrollmentState.ACTIVE:
      return "success";
    case QualityAgentEnrollmentState.DORMANT:
      return "secondary";
    case QualityAgentEnrollmentState.EXCLUDED:
      return "warning";
    case QualityAgentEnrollmentState.OUT_OF_SCOPE:
      return "neutral";
    case QualityAgentEnrollmentState.GONE:
      return "outline";
    default:
      return "outline";
  }
}

/** The order a state ranks in, or -1 for one the order does not carry. */
export function enrollmentStateRank(
  state: QualityAgentEnrollmentState
): number {
  return ENROLLMENT_STATE_ORDER.indexOf(state);
}

/** A reason rendered as either an i18n key or the raw text the server stored. */
export interface EnrollmentReasonLabel {
  /** The label key; absent means render `text` as written. */
  key?: string;
  params?: Record<string, string | number>;
  text?: string;
}

const PRIORITY_SIGNAL_KEYS: Record<string, string> = {
  rules: "dataQuality.agent.enrollment.prioritySignal.rules",
  failing: "dataQuality.agent.enrollment.prioritySignal.failing",
  critical: "dataQuality.agent.enrollment.prioritySignal.critical",
  important: "dataQuality.agent.enrollment.prioritySignal.important",
  "schema-changed": "dataQuality.agent.enrollment.prioritySignal.schemaChanged",
  new: "dataQuality.agent.enrollment.prioritySignal.new",
  unmonitored: "dataQuality.agent.enrollment.prioritySignal.unmonitored",
};

const CONSUMER_PREFIX = "consumers:";

/**
 * A priority reason as the signals behind it. The runner stores a comma-separated
 * list (`rules,failing,consumers:12,critical`); each known token becomes a label,
 * and an unknown one is shown as written rather than dropped.
 */
export function enrollmentPriorityReasonLabels(
  reason: string
): EnrollmentReasonLabel[] {
  const labels: EnrollmentReasonLabel[] = [];
  for (const raw of reason.split(",")) {
    const token = raw.trim();
    if (!token) {
      continue;
    }
    if (token.startsWith(CONSUMER_PREFIX)) {
      const count = Number(token.slice(CONSUMER_PREFIX.length));
      if (Number.isInteger(count) && count > 0) {
        labels.push({
          key: "dataQuality.agent.enrollment.prioritySignal.consumers",
          params: { count },
        });
        continue;
      }
    }
    const key = PRIORITY_SIGNAL_KEYS[token];
    labels.push(key ? { key } : { text: token });
  }
  return labels;
}

const BLOCKED_REASON_KEYS: Record<string, string> = {
  "runner-off": "dataQuality.agent.enrollment.blocked.runnerOff",
  "autonomy-off": "dataQuality.agent.enrollment.blocked.autonomyOff",
  "no-agent": "dataQuality.agent.enrollment.blocked.noAgent",
  "no-model": "dataQuality.agent.enrollment.blocked.noModel",
  "admission-limit": "dataQuality.agent.enrollment.blocked.admissionLimit",
  "active-limit": "dataQuality.agent.enrollment.blocked.activeLimit",
  "paused-by-human": "dataQuality.agent.enrollment.blocked.pausedByHuman",
  "session-blocked": "dataQuality.agent.enrollment.blocked.sessionBlocked",
  "review-due": "dataQuality.agent.enrollment.blocked.reviewDue",
  "coverage-appeared": "dataQuality.agent.enrollment.blocked.coverageAppeared",
};

/**
 * Why an enrollment is not progressing, as a label. The server stores either a
 * stable code or, for one legacy row, an English sentence; a code is translated
 * and anything else is shown as written.
 */
export function enrollmentBlockedReasonLabel(
  reason: string
): EnrollmentReasonLabel | undefined {
  const token = reason.trim();
  if (!token) {
    return undefined;
  }
  const key = BLOCKED_REASON_KEYS[token];
  return key ? { key } : { text: token };
}

/** `instances/{instance}/databases/{database}` as `instance · database`. */
export function databaseRefLabel(databaseRef: string): string {
  const segments = databaseRef.split("/");
  if (
    segments.length === 4 &&
    segments[0] === "instances" &&
    segments[2] === "databases" &&
    segments[1] &&
    segments[3]
  ) {
    return `${segments[1]} · ${segments[3]}`;
  }
  return databaseRef || "-";
}

/** An enrollment's target name: the object's own name, or its GUID as a fallback. */
export function enrollmentTargetName(
  enrollment: QualityAgentEnrollment
): string {
  return enrollment.target?.path?.object || enrollment.target?.metaGuid || "-";
}

/** An enrollment's target path for a subtitle: `schema.orders`. */
export function enrollmentTargetPath(
  enrollment: QualityAgentEnrollment
): string {
  return formatTargetPath(enrollment.target?.path);
}

/** Whether an enrollment matches a free-text search over its target and database. */
export function enrollmentMatchesSearch(
  enrollment: QualityAgentEnrollment,
  query: string
): boolean {
  const term = query.trim().toLowerCase();
  if (!term) {
    return true;
  }
  const path = enrollment.target?.path;
  const haystack = [
    path?.instance,
    path?.database,
    path?.schema,
    path?.object,
    enrollment.target?.metaGuid,
    enrollment.database,
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return haystack.includes(term);
}

/** The queue's counts keyed by state, from the summary RPC. */
export function enrollmentStateCounts(
  summary: QualityAgentEnrollmentSummary | undefined
): Map<QualityAgentEnrollmentState, number> {
  const counts = new Map<QualityAgentEnrollmentState, number>();
  for (const count of summary?.states ?? []) {
    counts.set(count.state, count.count);
  }
  return counts;
}

/** The count of one state in the summary; zero when the row carries none. */
export function enrollmentStateCount(
  counts: Map<QualityAgentEnrollmentState, number>,
  state: QualityAgentEnrollmentState
): number {
  return counts.get(state) ?? 0;
}

/**
 * The targets the runner is actively working through: pending, active and dormant
 * rows. Excluded, out-of-scope and gone rows are history, not workload.
 */
export function enrollmentWorkloadCount(
  counts: Map<QualityAgentEnrollmentState, number>
): number {
  return (
    enrollmentStateCount(counts, QualityAgentEnrollmentState.PENDING) +
    enrollmentStateCount(counts, QualityAgentEnrollmentState.ACTIVE) +
    enrollmentStateCount(counts, QualityAgentEnrollmentState.DORMANT)
  );
}

/** One database's counts in the order the queue shows them. */
export function databaseStateCounts(
  database: QualityAgentDatabaseEnrollmentSummary
): Array<{ state: QualityAgentEnrollmentState; count: number }> {
  const byState: Record<number, number> = {
    [QualityAgentEnrollmentState.PENDING]: database.pending,
    [QualityAgentEnrollmentState.ACTIVE]: database.active,
    [QualityAgentEnrollmentState.DORMANT]: database.dormant,
    [QualityAgentEnrollmentState.EXCLUDED]: database.excluded,
    [QualityAgentEnrollmentState.OUT_OF_SCOPE]: database.outOfScope,
    [QualityAgentEnrollmentState.GONE]: database.gone,
  };
  return ENROLLMENT_STATE_ORDER.map((state) => ({
    state,
    count: byState[state] ?? 0,
  }));
}

/** `part` as a whole percentage of `total`; zero when there is nothing to divide. */
export function enrollmentPercent(part: number, total: number): number {
  if (!Number.isFinite(part) || !Number.isFinite(total) || total <= 0) {
    return 0;
  }
  return Math.round((Math.max(0, part) / total) * 100);
}

/**
 * A rough "wakes per day" for the settings confirmation: every target is reviewed
 * once per dormancy window, plus the active set keeps its own cadence. It ignores
 * events and manual wakes on purpose — the dialog calls it an estimate.
 */
export function enrollmentWorkloadEstimate(options: {
  /** Targets discovered so far; zero before the first scan. */
  tables: number;
  /** The configured dormancy window in days; zero means the default (30). */
  reviewDays: number;
  /** Enrollments currently active. */
  activeSessions: number;
}): number {
  const tables = nonNegative(options.tables);
  const active = nonNegative(options.activeSessions);
  const reviewDays = options.reviewDays > 0 ? options.reviewDays : 30;
  return Math.ceil(tables / reviewDays) + active;
}

function nonNegative(value: number): number {
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
}
