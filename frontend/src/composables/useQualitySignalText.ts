import { useI18n } from "vue-i18n";
import {
  QualityStatus,
  type QualityTargetSummary,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { qualityStatusKey } from "@/utils/dataQuality";
import {
  formatPassRate,
  formatQualityScore,
} from "@/utils/dataQualityOverview";
import {
  type QualitySignalTone,
  qualitySignalAllPaused,
  qualitySignalCoverage,
  qualitySignalTone,
} from "@/utils/dataQualitySignal";
import { formatDateTime, formatRelativeTime } from "@/utils/datetime";

/** One labeled fact of the tab's summary strip. */
export interface QualitySignalFact {
  key: "score" | "passRate" | "coverage" | "lastRun";
  label: string;
  value: string;
  /** The absolute instant, for the last-run fact's tooltip. */
  title?: string;
}

/**
 * The words one trust signal renders, shared by the list cell, the asset-header
 * badge, the search result dot and the tab's summary strip.
 *
 * It is a composable rather than a `utils/` helper because the wording goes
 * through vue-i18n — and because "not monitored", "never run" and "-" are three
 * different facts that must not collapse into one another: no signal means no
 * checks watch the object, an unjudged target means its checks never produced a
 * verdict, and "-" is a missing number.
 */
export function useQualitySignalText() {
  const { t, locale } = useI18n();

  /**
   * Line one: the worst latest verdict, or one of the two neutral states that
   * are not verdicts at all — no check at all, and every check paused.
   */
  function statusText(signal: QualityTargetSummary | undefined): string {
    if (!signal) {
      return t("dataQuality.signal.notMonitored");
    }
    if (qualitySignalAllPaused(signal)) {
      return t("dataQuality.signal.allPaused");
    }
    if (signal.worstStatus === QualityStatus.UNSPECIFIED) {
      return t("dataQuality.targets.neverRun");
    }
    return t(qualityStatusKey(signal.worstStatus));
  }

  /** `4/9 columns`; "" when the target has no current column list. */
  function coverageText(signal: QualityTargetSummary | undefined): string {
    const coverage = qualitySignalCoverage(signal);
    if (coverage === null) {
      return "";
    }
    return t("dataQuality.signal.coverage", {
      covered: coverage.covered,
      total: coverage.total,
    });
  }

  /** How long ago the checks last produced a verdict; "Never run" otherwise. */
  function lastRunText(signal: QualityTargetSummary | undefined): string {
    if (!signal?.lastRunTime) {
      return t("dataQuality.targets.neverRun");
    }
    return formatRelativeTime(signal.lastRunTime, locale.value);
  }

  /**
   * Line two: column coverage and the last run, joined the way the rest of the
   * detail views join a muted fact line. Empty for an unmonitored or paused
   * target: neither is being verified, so neither has a coverage verdict.
   */
  function detailText(signal: QualityTargetSummary | undefined): string {
    if (!signal || qualitySignalAllPaused(signal)) {
      return "";
    }
    return [coverageText(signal), lastRunText(signal)]
      .filter(Boolean)
      .join(" · ");
  }

  /** Everything the cell or badge cannot show, for its tooltip. */
  function tooltip(signal: QualityTargetSummary | undefined): string {
    if (!signal) {
      return t("dataQuality.signal.notMonitoredHint");
    }
    if (qualitySignalAllPaused(signal)) {
      return t("dataQuality.signal.allPausedHint");
    }
    const parts = [statusText(signal)];
    if (signal.qualityScore !== undefined) {
      parts.push(
        t("dataQuality.signal.score", {
          score: formatQualityScore(signal.qualityScore),
        })
      );
    }
    if (signal.recentPassRate !== undefined) {
      parts.push(
        t("dataQuality.signal.passRate", {
          rate: formatPassRate(signal.recentPassRate),
        })
      );
    }
    if (signal.openIssueCount > 0) {
      parts.push(
        t("dataQuality.signal.issues", { count: signal.openIssueCount })
      );
    }
    const coverage = coverageText(signal);
    if (coverage) {
      parts.push(coverage);
    }
    // The status word already says "Never run" when nothing has run; repeating it
    // as "Last run Never run" reads like a bug.
    if (signal.lastRunTime) {
      parts.push(
        t("dataQuality.signal.lastRun", { time: lastRunText(signal) })
      );
    }
    return parts.join(" · ");
  }

  /**
   * The judged facts behind the status, as labeled values for the tab's strip.
   * Empty for an unmonitored or paused target: its only fact is the status word
   * (and, for paused, the explanation the strip renders next to it).
   */
  function facts(
    signal: QualityTargetSummary | undefined
  ): QualitySignalFact[] {
    if (!signal || qualitySignalAllPaused(signal)) {
      return [];
    }
    const rows: QualitySignalFact[] = [];
    if (signal.qualityScore !== undefined) {
      rows.push({
        key: "score",
        label: t("dataQuality.signal.scoreLabel"),
        value: formatQualityScore(signal.qualityScore),
      });
    }
    if (signal.recentPassRate !== undefined) {
      rows.push({
        key: "passRate",
        label: t("dataQuality.signal.passRateLabel"),
        value: formatPassRate(signal.recentPassRate),
      });
    }
    const coverage = coverageText(signal);
    if (coverage) {
      rows.push({
        key: "coverage",
        label: t("dataQuality.signal.coverageLabel"),
        value: coverage,
      });
    }
    rows.push({
      key: "lastRun",
      label: t("dataQuality.signal.lastRunLabel"),
      value: lastRunText(signal),
      title: signal.lastRunTime
        ? formatDateTime(signal.lastRunTime, locale.value)
        : undefined,
    });
    return rows;
  }

  return {
    statusText,
    detailText,
    tooltip,
    facts,
    allPaused: qualitySignalAllPaused,
    tone: (signal: QualityTargetSummary | undefined): QualitySignalTone =>
      qualitySignalTone(signal),
  };
}
