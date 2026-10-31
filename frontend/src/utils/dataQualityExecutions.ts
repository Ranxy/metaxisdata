/**
 * The target-row readings of the executions page: what a target's latest batch
 * is, how its verdicts split, and how the row prints them.
 *
 * These used to be private functions in a 1,600-line view, so none of the
 * verdict arithmetic (unfinished checks, failed checks, the summary sentence)
 * could be tested. The translate-dependent readings take the caller's `t`, which
 * keeps the module pure.
 */

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import {
  QualityExecutionState as ExecutionState,
  type QualityExecutionState,
  type QualityStatus,
  type QualityStatusCounts,
  type QualityTargetSummary,
  QualityStatus as Status,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  executionDurationSeconds,
  formatSeconds,
  formatTargetPath,
  qualityStatusKey,
  targetScopeLabel,
} from "@/utils/dataQuality";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";
import type { Translate } from "@/utils/i18n";

type QualityLatestRun = NonNullable<QualityTargetSummary["latestRun"]>;

/** The verdicts a batch's counts can report, in the order a summary reads them. */
export const TARGET_VERDICT_ORDER: QualityStatus[] = [
  Status.PASS,
  Status.WARN,
  Status.FAIL,
  Status.ERROR,
  Status.SKIPPED,
  Status.NOT_EVALUATED,
];

/** The target's GUID, or the empty string for a target without one. */
export function targetGuid(summary: QualityTargetSummary): string {
  return summary.target?.metaGuid ?? "";
}

/** The target's object name, or its GUID when the path is empty. */
export function targetName(summary: QualityTargetSummary): string {
  return summary.target?.path?.object || summary.target?.metaGuid || "-";
}

/** The target's coarse scope and path, e.g. `prod · public.orders`. */
export function targetPathLine(summary: QualityTargetSummary): string {
  const path = summary.target?.path;
  const scope = targetScopeLabel(path);
  const name = formatTargetPath(path);
  return scope === "-" ? name : `${scope} · ${name}`;
}

/** The target's latest batch, if it ever ran. */
export function latestRun(
  summary: QualityTargetSummary
): QualityLatestRun | undefined {
  return summary.latestRun;
}

/** The latest batch's per-verdict counts. */
export function runCounts(
  summary: QualityTargetSummary
): QualityStatusCounts | undefined {
  return summary.latestRun?.counts;
}

/** The latest batch's lifecycle state. */
export function runState(summary: QualityTargetSummary): QualityExecutionState {
  return summary.latestRun?.state ?? ExecutionState.UNSPECIFIED;
}

/** The latest batch's verdict. */
export function runOutcome(summary: QualityTargetSummary): QualityStatus {
  return summary.latestRun?.outcome ?? Status.UNSPECIFIED;
}

/** How many checks the counts report for one verdict. */
export function verdictCount(
  counts: QualityStatusCounts | undefined,
  status: QualityStatus
): number {
  if (!counts) {
    return 0;
  }
  switch (status) {
    case Status.PASS:
      return counts.pass;
    case Status.WARN:
      return counts.warn;
    case Status.FAIL:
      return counts.fail;
    case Status.ERROR:
      return counts.error;
    case Status.SKIPPED:
      return counts.skipped;
    case Status.NOT_EVALUATED:
      return counts.notEvaluated;
    default:
      return 0;
  }
}

/** The verdicts the latest batch recorded, e.g. `3 Pass · 1 Fail`. */
export function countsSummary(
  summary: QualityTargetSummary,
  t: Translate
): string {
  const counts = runCounts(summary);
  if (!counts) {
    return "";
  }
  return TARGET_VERDICT_ORDER.map((status) => ({
    status,
    count: verdictCount(counts, status),
  }))
    .filter((verdict) => verdict.count > 0)
    .map((verdict) => `${verdict.count} ${t(qualityStatusKey(verdict.status))}`)
    .join(" · ");
}

/** Checks that failed or errored in the latest batch. */
export function failedChecks(summary: QualityTargetSummary): number {
  const counts = runCounts(summary);
  if (!counts) {
    return 0;
  }
  return counts.fail + counts.error;
}

/** Planned checks that never reached a verdict in the latest batch. */
export function unfinishedChecks(summary: QualityTargetSummary): number {
  const counts = runCounts(summary);
  if (!counts) {
    return 0;
  }
  const finished = TARGET_VERDICT_ORDER.reduce(
    (sum, status) => sum + verdictCount(counts, status),
    0
  );
  return Math.max(0, counts.total - finished);
}

/** When the latest batch ran: its finish time, or its start while running. */
export function scanTime(summary: QualityTargetSummary): Timestamp | undefined {
  const run = latestRun(summary);
  return run?.finishTime ?? run?.startTime;
}

/** The latest batch's relative time. */
export function scanTimeLabel(
  summary: QualityTargetSummary,
  locale: string
): string {
  return formatRelativeTime(scanTime(summary), locale);
}

/** The latest batch's absolute time, for the row's tooltip. */
export function scanTimeTitle(
  summary: QualityTargetSummary,
  locale: string
): string {
  return formatDateTime(scanTime(summary), locale);
}

/** How long the latest batch took, or `-` when it is unknown. */
export function runDurationLabel(summary: QualityTargetSummary): string {
  const run = latestRun(summary);
  if (!run) {
    return "-";
  }
  const seconds = executionDurationSeconds({
    startTime: run.startTime,
    finishTime: run.finishTime,
  });
  return seconds === undefined ? "-" : formatSeconds(seconds);
}

/** A batch can be queued, running or abandoned; only the last two need words. */
export function runStateHint(
  summary: QualityTargetSummary,
  t: Translate
): string {
  switch (runState(summary)) {
    case ExecutionState.PENDING:
    case ExecutionState.RUNNING:
      return t("dataQuality.executions.runRunningHint");
    case ExecutionState.ABANDONED:
      return t("dataQuality.executions.runAbandonedHint");
    default:
      return "";
  }
}
