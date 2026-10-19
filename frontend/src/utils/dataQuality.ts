import type { Duration } from "@bufbuild/protobuf/wkt";
import {
  QualityObservedKind as ObservedKind,
  QualityComparison,
  QualityDimension,
  type QualityObservedKind,
  QualityRuleState,
  QualityRuleType,
  type QualityRun,
  QualityRunStatus,
  QualityRunTrigger,
  QualitySeverity,
  QualityStatus,
  type QualityTargetPath,
  type QualityThresholds,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime } from "./datetime";

/**
 * The data-quality vocabulary, as i18n keys and display strings.
 *
 * The helpers here are pure so the two lists and the run detail cannot render
 * the same state differently, and so the enum never reaches the screen: a
 * zh-CN page must print 通过, not `QUALITY_STATUS_PASS`. They return a key (or a
 * locale-formatted string) rather than text, because `t()` belongs in views.
 *
 * The keys the switch helpers return are declared in
 * `scripts/check-vue-i18n.mjs`'s DYNAMIC_PREFIXES: the checker cannot follow a
 * key a helper returns, and listing each family keeps a typo from hiding behind
 * a broad exemption.
 */

/** A badge variant, spelled out so `utils/` does not import from `components/`. */
export type QualityBadgeVariant =
  | "success"
  | "warning"
  | "destructive"
  | "danger"
  | "info"
  | "secondary"
  | "outline"
  | "neutral";

/** A threshold rendered as a catalog key plus its formatted bounds. */
export interface QualityThresholdText {
  key: string;
  params: Record<string, string>;
}

const DEFAULT_LOCALE = "en-US";

// ---- status ----

/** The label key for a rule result's outcome. */
export function qualityStatusKey(status: QualityStatus): string {
  switch (status) {
    case QualityStatus.PASS:
      return "dataQuality.status.pass";
    case QualityStatus.WARN:
      return "dataQuality.status.warn";
    case QualityStatus.FAIL:
      return "dataQuality.status.fail";
    case QualityStatus.ERROR:
      return "dataQuality.status.error";
    case QualityStatus.SKIPPED:
      return "dataQuality.status.skipped";
    case QualityStatus.NOT_EVALUATED:
      return "dataQuality.status.notEvaluated";
    default:
      return "dataQuality.status.unspecified";
  }
}

/** The badge variant for a rule result's outcome. FAIL and ERROR stay distinct. */
export function qualityStatusVariant(
  status: QualityStatus
): QualityBadgeVariant {
  switch (status) {
    case QualityStatus.PASS:
      return "success";
    case QualityStatus.WARN:
      return "warning";
    case QualityStatus.FAIL:
      return "destructive";
    case QualityStatus.ERROR:
      return "danger";
    case QualityStatus.SKIPPED:
      return "neutral";
    case QualityStatus.NOT_EVALUATED:
      return "outline";
    default:
      return "secondary";
  }
}

// ---- severity ----

/** The label key for a rule's severity. */
export function qualitySeverityKey(severity: QualitySeverity): string {
  switch (severity) {
    case QualitySeverity.WARNING:
      return "dataQuality.severity.warning";
    case QualitySeverity.ERROR:
      return "dataQuality.severity.error";
    case QualitySeverity.CRITICAL:
      return "dataQuality.severity.critical";
    default:
      return "dataQuality.severity.unspecified";
  }
}

/** The badge variant for a rule's severity. */
export function qualitySeverityVariant(
  severity: QualitySeverity
): QualityBadgeVariant {
  switch (severity) {
    case QualitySeverity.WARNING:
      return "warning";
    case QualitySeverity.ERROR:
      return "destructive";
    case QualitySeverity.CRITICAL:
      return "danger";
    default:
      return "secondary";
  }
}

// ---- dimension ----

/** The label key for a rule's quality dimension. */
export function qualityDimensionKey(dimension: QualityDimension): string {
  switch (dimension) {
    case QualityDimension.COMPLETENESS:
      return "dataQuality.dimension.completeness";
    case QualityDimension.VALIDITY:
      return "dataQuality.dimension.validity";
    case QualityDimension.UNIQUENESS:
      return "dataQuality.dimension.uniqueness";
    case QualityDimension.CONSISTENCY:
      return "dataQuality.dimension.consistency";
    case QualityDimension.TIMELINESS:
      return "dataQuality.dimension.timeliness";
    case QualityDimension.ACCURACY:
      return "dataQuality.dimension.accuracy";
    default:
      return "dataQuality.dimension.unspecified";
  }
}

// ---- rule type ----

/** The label key for the observation a rule makes. */
export function qualityRuleTypeKey(ruleType: QualityRuleType): string {
  switch (ruleType) {
    case QualityRuleType.ROW_COUNT:
      return "dataQuality.ruleType.rowCount";
    case QualityRuleType.ROW_COUNT_CHANGE:
      return "dataQuality.ruleType.rowCountChange";
    case QualityRuleType.NULL_PERCENT:
      return "dataQuality.ruleType.nullPercent";
    case QualityRuleType.UNIQUE:
      return "dataQuality.ruleType.unique";
    case QualityRuleType.UNIQUE_COMBINATION:
      return "dataQuality.ruleType.uniqueCombination";
    case QualityRuleType.ACCEPTED_VALUES:
      return "dataQuality.ruleType.acceptedValues";
    case QualityRuleType.RANGE:
      return "dataQuality.ruleType.range";
    case QualityRuleType.REGEX:
      return "dataQuality.ruleType.regex";
    case QualityRuleType.STRING_LENGTH:
      return "dataQuality.ruleType.stringLength";
    case QualityRuleType.REFERENTIAL_INTEGRITY:
      return "dataQuality.ruleType.referentialIntegrity";
    case QualityRuleType.CROSS_COLUMN:
      return "dataQuality.ruleType.crossColumn";
    case QualityRuleType.FRESHNESS:
      return "dataQuality.ruleType.freshness";
    case QualityRuleType.CUSTOM_SQL:
      return "dataQuality.ruleType.customSql";
    case QualityRuleType.CEL_EXPRESSION:
      return "dataQuality.ruleType.celExpression";
    default:
      return "dataQuality.ruleType.unspecified";
  }
}

// ---- rule state ----

/** The label key for whether a rule is scheduled. */
export function qualityRuleStateKey(state: QualityRuleState): string {
  switch (state) {
    case QualityRuleState.ENABLED:
      return "dataQuality.ruleState.enabled";
    case QualityRuleState.PAUSED:
      return "dataQuality.ruleState.paused";
    case QualityRuleState.ARCHIVED:
      return "dataQuality.ruleState.archived";
    default:
      return "dataQuality.ruleState.unspecified";
  }
}

/** The badge variant for whether a rule is scheduled. */
export function qualityRuleStateVariant(
  state: QualityRuleState
): QualityBadgeVariant {
  switch (state) {
    case QualityRuleState.ENABLED:
      return "success";
    case QualityRuleState.PAUSED:
      return "warning";
    case QualityRuleState.ARCHIVED:
      return "neutral";
    default:
      return "secondary";
  }
}

// ---- run ----

/** The label key for a run's lifecycle state. */
export function qualityRunStatusKey(status: QualityRunStatus): string {
  switch (status) {
    case QualityRunStatus.PENDING:
      return "dataQuality.runStatus.pending";
    case QualityRunStatus.RUNNING:
      return "dataQuality.runStatus.running";
    case QualityRunStatus.COMPLETED:
      return "dataQuality.runStatus.completed";
    case QualityRunStatus.ABANDONED:
      return "dataQuality.runStatus.abandoned";
    default:
      return "dataQuality.runStatus.unspecified";
  }
}

/** The badge variant for a run's lifecycle state. */
export function qualityRunStatusVariant(
  status: QualityRunStatus
): QualityBadgeVariant {
  switch (status) {
    case QualityRunStatus.PENDING:
      return "info";
    case QualityRunStatus.RUNNING:
      return "info";
    case QualityRunStatus.COMPLETED:
      return "success";
    case QualityRunStatus.ABANDONED:
      return "warning";
    default:
      return "secondary";
  }
}

/** The label key for what started a run. */
export function qualityRunTriggerKey(trigger: QualityRunTrigger): string {
  switch (trigger) {
    case QualityRunTrigger.MANUAL:
      return "dataQuality.runTrigger.manual";
    case QualityRunTrigger.SCHEDULED:
      return "dataQuality.runTrigger.scheduled";
    default:
      return "dataQuality.runTrigger.unspecified";
  }
}

/**
 * True while a run can still change, i.e. while its detail page should poll.
 * COMPLETED and ABANDONED are final: nothing will update them.
 */
export function isRunInProgress(status: QualityRunStatus): boolean {
  return (
    status === QualityRunStatus.PENDING || status === QualityRunStatus.RUNNING
  );
}

/** The seconds between a run's start and finish, or undefined while it runs. */
export function runDurationSeconds(
  run: Pick<QualityRun, "startTime" | "finishTime">
): number | undefined {
  const start = timestampSeconds(run.startTime);
  const finish = timestampSeconds(run.finishTime);
  if (start === undefined || finish === undefined || finish < start) {
    return undefined;
  }
  return finish - start;
}

// ---- thresholds ----

/**
 * The fail threshold as a catalog key plus already-formatted bounds, so the
 * caller renders it with the locale's own operator wording. Returns undefined
 * when the snapshot carries no fail bound (an empty-behaviour-only result).
 */
export function describeThreshold(
  thresholds: QualityThresholds | undefined,
  kind?: QualityObservedKind
): QualityThresholdText | undefined {
  if (!thresholds) {
    return undefined;
  }
  const fail = thresholds.failValue;
  const upper = thresholds.failValueUpper;
  switch (thresholds.comparison) {
    case QualityComparison.GREATER_THAN:
      return fail === undefined
        ? undefined
        : {
            key: "dataQuality.thresholdGreaterThan",
            params: { value: formatObservedValue(fail, kind) },
          };
    case QualityComparison.LESS_THAN:
      return fail === undefined
        ? undefined
        : {
            key: "dataQuality.thresholdLessThan",
            params: { value: formatObservedValue(fail, kind) },
          };
    case QualityComparison.OUTSIDE_RANGE:
    case QualityComparison.INSIDE_RANGE: {
      if (fail === undefined || upper === undefined) {
        return undefined;
      }
      return {
        key:
          thresholds.comparison === QualityComparison.OUTSIDE_RANGE
            ? "dataQuality.thresholdOutside"
            : "dataQuality.thresholdInside",
        params: {
          lower: formatObservedValue(fail, kind),
          upper: formatObservedValue(upper, kind),
        },
      };
    }
    default:
      return fail === undefined
        ? undefined
        : {
            key: "dataQuality.thresholdValue",
            params: { value: formatObservedValue(fail, kind) },
          };
  }
}

/** The warn bound of a snapshot, formatted for a tooltip. */
export function formatWarnThreshold(
  thresholds: QualityThresholds | undefined,
  kind?: QualityObservedKind
): string | undefined {
  if (!thresholds) {
    return undefined;
  }
  if (thresholds.warnValue !== undefined) {
    return formatObservedValue(thresholds.warnValue, kind);
  }
  if (thresholds.warnValueUpper !== undefined) {
    return formatObservedValue(thresholds.warnValueUpper, kind);
  }
  return undefined;
}

/** The label key for a result's machine-readable failure class, or "" if unknown. */
export function qualityErrorCodeKey(code: string): string {
  switch (code.trim().toUpperCase()) {
    case "CONNECTION":
      return "dataQuality.errorCode.connection";
    case "AUTH":
      return "dataQuality.errorCode.auth";
    case "TIMEOUT":
      return "dataQuality.errorCode.timeout";
    case "PERMISSION":
      return "dataQuality.errorCode.permission";
    case "UNSUPPORTED":
      return "dataQuality.errorCode.unsupported";
    case "SQL_SYNTAX":
      return "dataQuality.errorCode.sqlSyntax";
    case "BAD_DATA":
      return "dataQuality.errorCode.badData";
    case "LIMIT_EXCEEDED":
      return "dataQuality.errorCode.limitExceeded";
    case "ORPHANED":
      return "dataQuality.errorCode.orphaned";
    default:
      return "";
  }
}

// ---- target ----

/**
 * Where a rule's target lived, as one display name: `schema.object.column`,
 * e.g. `public.orders.id`. Returns "-" when the snapshot is absent.
 */
export function formatTargetPath(path: QualityTargetPath | undefined): string {
  if (!path) {
    return "-";
  }
  const name = path.object || "";
  const qualified = path.schema ? [path.schema, name].filter(Boolean) : [name];
  if (path.column) {
    qualified.push(path.column);
  }
  return qualified.filter(Boolean).join(".") || "-";
}

/** The coarser half of the target: `instance · database`. */
export function targetScopeLabel(path: QualityTargetPath | undefined): string {
  return [path?.instance, path?.database].filter(Boolean).join(" · ") || "-";
}

// ---- formatting ----

/** A whole number in the reader's locale, e.g. 1,204. */
export function formatRowCount(
  value: bigint | number,
  locale = DEFAULT_LOCALE
): string {
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(
    value
  );
}

/**
 * The observed value in the unit the result recorded: a count, a ratio as a
 * percentage, a duration, an instant, or a plain number.
 */
export function formatObservedValue(
  value: number,
  kind?: QualityObservedKind,
  locale = DEFAULT_LOCALE
): string {
  if (!Number.isFinite(value)) {
    return "-";
  }
  switch (kind) {
    case ObservedKind.COUNT:
      return formatRowCount(Math.round(value), locale);
    case ObservedKind.RATIO:
      return `${new Intl.NumberFormat(locale, {
        maximumFractionDigits: 2,
      }).format(value * 100)}%`;
    case ObservedKind.SECONDS:
      return formatSeconds(value);
    case ObservedKind.TIMESTAMP:
      return formatDateTime(new Date(value * 1000), locale, { seconds: true });
    case ObservedKind.NUMBER:
    default:
      return new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(
        value
      );
  }
}

/**
 * A duration in seconds, compactly: `850 ms`, `1.25 s`, `2m 5s`, `3h 4m`.
 * Technical notation rather than prose: it sits in a tabular cell in both
 * languages, and the column header carries the word.
 */
export function formatSeconds(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return "-";
  }
  if (seconds < 1) {
    return `${Math.round(seconds * 1000)} ms`;
  }
  if (seconds < 60) {
    return `${Number(seconds.toFixed(2))} s`;
  }
  const whole = Math.floor(seconds);
  if (whole < 3600) {
    const minutes = Math.floor(whole / 60);
    const rest = whole % 60;
    return rest === 0 ? `${minutes}m` : `${minutes}m ${rest}s`;
  }
  const hours = Math.floor(whole / 3600);
  const minutes = Math.floor((whole % 3600) / 60);
  return minutes === 0 ? `${hours}h` : `${hours}h ${minutes}m`;
}

/** A protobuf Duration as a compact duration, or "-" when unset. */
export function formatDuration(duration: Duration | undefined): string {
  const seconds = secondsOf(duration);
  return seconds === undefined ? "-" : formatSeconds(seconds);
}

/**
 * A schedule interval as compact clock notation: `15m`, `1h`, `7d`. Rules are
 * scheduled in whole minutes, so the remainder fallback is defensive.
 */
export function formatScheduleInterval(duration: Duration | undefined): string {
  const seconds = secondsOf(duration);
  if (seconds === undefined || seconds <= 0) {
    return "-";
  }
  if (seconds % 86_400 === 0) {
    return `${seconds / 86_400}d`;
  }
  if (seconds % 3_600 === 0) {
    return `${seconds / 3_600}h`;
  }
  if (seconds % 60 === 0) {
    return `${seconds / 60}m`;
  }
  return `${seconds}s`;
}

// ---- internals ----

/** A Duration's length in fractional seconds, or undefined when unset/zero. */
function secondsOf(duration: Duration | undefined): number | undefined {
  if (!duration) {
    return undefined;
  }
  const seconds = Number(duration.seconds) + duration.nanos / 1_000_000_000;
  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined;
}

/** A Timestamp as epoch seconds, or undefined when unset (zero is unset). */
function timestampSeconds(
  timestamp: { seconds?: bigint | number | string | null } | undefined
): number | undefined {
  if (!timestamp) {
    return undefined;
  }
  const seconds = Number(timestamp.seconds ?? 0);
  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined;
}
