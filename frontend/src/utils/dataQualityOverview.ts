import type {
  QualityCoverageBreakdown,
  QualityDimension,
  QualityDimensionScore,
  QualityOwnerScore,
  QualityStatusCounts,
  QualityTrendPoint,
} from "@/types/proto-es/v1/data_quality_service_pb";

/**
 * The overview page's pure layer: the trend window, the stacked-bar geometry
 * for the inline SVG, and the small percentages the summary shows.
 *
 * There is no charting library in this app, so the chart is a handful of
 * rectangles computed here and rendered by `QualityTrendChart.vue`. Keeping the
 * geometry pure is what lets it be tested without mounting an SVG.
 */

/** The windows the page offers, in days. */
export const TREND_WINDOWS = [7, 30] as const;
export type TrendWindow = (typeof TREND_WINDOWS)[number];

export const DEFAULT_TREND_WINDOW: TrendWindow = 7;
export const MAX_TREND_DAYS = 30;

/** One day of the trend, with every status the chart can stack. */
export interface TrendDay {
  /** YYYY-MM-DD in UTC, the server's own bucket. */
  date: string;
  pass: number;
  warn: number;
  fail: number;
  error: number;
  skipped: number;
  notEvaluated: number;
  total: number;
}

/** The statuses drawn as stacked bars. skipped/not-evaluated are not drawn. */
export type TrendStackKey = "pass" | "warn" | "fail" | "error";

export interface ChartSegment {
  key: TrendStackKey;
  /** Top edge in SVG units, y growing downwards. */
  y: number;
  height: number;
}

export interface ChartBar {
  /** The whole day's slot: the bar plus the gap on either side of it. */
  slotX: number;
  slotWidth: number;
  x: number;
  width: number;
  /** Top edge of the stack, i.e. the highest segment's y (the baseline when empty). */
  top: number;
  total: number;
  segments: ChartSegment[];
  date: string;
}

export interface ChartTick {
  value: number;
  y: number;
}

export interface TrendChartGeometry {
  width: number;
  height: number;
  /** The drawing area, excluding the axis gutters. */
  plotWidth: number;
  plotHeight: number;
  /** y of the baseline (the bottom of the plot area). */
  baseline: number;
  paddingTop: number;
  paddingLeft: number;
  paddingRight: number;
  maxValue: number;
  bars: ChartBar[];
  ticks: ChartTick[];
  dates: string[];
}

export interface TrendChartOptions {
  width?: number;
  height?: number;
  paddingTop?: number;
  paddingRight?: number;
  paddingBottom?: number;
  paddingLeft?: number;
  /** Bars stop widening past this: seven days must not become seven slabs. */
  maxBarWidth?: number;
  /** Grid rows to aim for; the axis still rounds to a readable step. */
  tickCount?: number;
}

const DEFAULT_CHART: Required<TrendChartOptions> = {
  width: 720,
  height: 232,
  paddingTop: 16,
  paddingRight: 12,
  paddingBottom: 30,
  paddingLeft: 44,
  maxBarWidth: 72,
  tickCount: 5,
};

/** Clamp a caller-chosen window to what the service accepts. */
export function clampTrendDays(days: number): number {
  if (!Number.isFinite(days) || days <= 0) {
    return DEFAULT_TREND_WINDOW;
  }
  return Math.min(MAX_TREND_DAYS, Math.round(days));
}

/** YYYY-MM-DD of a Date, in UTC. */
export function utcDateString(date: Date): string {
  return date.toISOString().slice(0, 10);
}

/**
 * The server's per-day points as exactly `days` consecutive UTC days, oldest
 * first. A day the server did not report is a real zero (a workspace with no
 * executions that day), so it is filled rather than skipped: a gap in the line would
 * otherwise read as missing data instead of as "nothing executed".
 */
export function normalizeTrend(
  points: readonly QualityTrendPoint[],
  days: number,
  now: Date = new Date()
): TrendDay[] {
  const wanted = clampTrendDays(days);
  const byDate = new Map(points.map((point) => [point.date, point]));
  const end = Date.UTC(
    now.getUTCFullYear(),
    now.getUTCMonth(),
    now.getUTCDate()
  );
  const out: TrendDay[] = [];
  for (let offset = wanted - 1; offset >= 0; offset -= 1) {
    const date = utcDateString(new Date(end - offset * 86_400_000));
    const point = byDate.get(date);
    out.push(trendDay(date, point));
  }
  return out;
}

function trendDay(
  date: string,
  point: QualityTrendPoint | undefined
): TrendDay {
  const pass = point?.pass ?? 0;
  const warn = point?.warn ?? 0;
  const fail = point?.fail ?? 0;
  const error = point?.error ?? 0;
  const skipped = point?.skipped ?? 0;
  const notEvaluated = point?.notEvaluated ?? 0;
  const total =
    point?.total || pass + warn + fail + error + skipped + notEvaluated;
  return { date, pass, warn, fail, error, skipped, notEvaluated, total };
}

/** True when at least one day reported an execution. */
export function hasTrendData(days: readonly TrendDay[]): boolean {
  return days.some((day) => day.total > 0);
}

/**
 * A "nice" axis: a round maximum at or above `value` and evenly spaced ticks
 * including zero. An all-zero window still gets a 0..1 axis so the baseline is
 * drawn instead of the chart collapsing.
 */
export function niceScale(
  value: number,
  tickCount = 4
): { max: number; step: number; ticks: number[] } {
  if (!Number.isFinite(value) || value <= 0) {
    return { max: 1, step: 1, ticks: [0, 1] };
  }
  const rough = value / Math.max(1, tickCount);
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  const candidates = [1, 2, 5, 10].map((factor) => factor * magnitude);
  const step =
    candidates.find((candidate) => candidate >= rough) ??
    candidates[candidates.length - 1] ??
    1;
  const max = Math.ceil(value / step) * step;
  const ticks: number[] = [];
  for (let tick = 0; tick <= max + step / 2; tick += step) {
    ticks.push(Math.round(tick * 1000) / 1000);
  }
  return { max, step, ticks };
}

/**
 * The stacked-bar geometry for one window. Bars stack pass → warn → fail →
 * error from the baseline; skipped and not-evaluated are counted in `total` but
 * not drawn, because they are the absence of a verdict rather than a severity.
 *
 * Width is the caller's measured pixel width, so the SVG can be drawn 1:1 and
 * nothing in it is stretched: a scaled viewBox distorts the glyphs in the axis
 * labels as much as the bars.
 */
export function buildTrendGeometry(
  days: readonly TrendDay[],
  options: TrendChartOptions = {}
): TrendChartGeometry {
  const chart = { ...DEFAULT_CHART, ...options };
  const plotWidth = Math.max(
    0,
    chart.width - chart.paddingLeft - chart.paddingRight
  );
  const plotHeight = Math.max(
    0,
    chart.height - chart.paddingTop - chart.paddingBottom
  );
  const baseline = chart.paddingTop + plotHeight;
  const highest = days.reduce(
    (max, day) => Math.max(max, day.pass + day.warn + day.fail + day.error),
    0
  );
  const scale = niceScale(highest, chart.tickCount);
  const slot = days.length > 0 ? plotWidth / days.length : 0;
  const barWidth = Math.min(slot * 0.62, chart.maxBarWidth);
  const bars: ChartBar[] = days.map((day, index) => {
    const values: Array<[TrendStackKey, number]> = [
      ["pass", day.pass],
      ["warn", day.warn],
      ["fail", day.fail],
      ["error", day.error],
    ];
    let stacked = 0;
    const segments: ChartSegment[] = [];
    for (const [key, value] of values) {
      if (value <= 0) {
        continue;
      }
      const height = (value / scale.max) * plotHeight;
      stacked += height;
      segments.push({ key, y: baseline - stacked, height });
    }
    const slotX = chart.paddingLeft + index * slot;
    return {
      slotX,
      slotWidth: slot,
      x: slotX + (slot - barWidth) / 2,
      width: barWidth,
      top: segments.length > 0 ? segments[segments.length - 1].y : baseline,
      total: day.total,
      segments,
      date: day.date,
    };
  });
  const ticks: ChartTick[] = scale.ticks.map((value) => ({
    value,
    y: baseline - (value / scale.max) * plotHeight,
  }));
  return {
    width: chart.width,
    height: chart.height,
    plotWidth,
    plotHeight,
    baseline,
    paddingTop: chart.paddingTop,
    paddingLeft: chart.paddingLeft,
    paddingRight: chart.paddingRight,
    maxValue: scale.max,
    bars,
    ticks,
    dates: days.map((day) => day.date),
  };
}

/**
 * The date labels the axis shows: at most `maxLabels`, evenly spaced and
 * always including the first and last day. Seven bars get seven labels; thirty
 * get enough to stay readable without colliding.
 */
export function trendDateLabels(
  dates: readonly string[],
  maxLabels = 3
): string[] {
  if (dates.length === 0) {
    return [];
  }
  const wanted = Math.max(2, Math.min(Math.round(maxLabels), dates.length));
  if (dates.length <= wanted) {
    return [...dates];
  }
  const labels: string[] = [];
  let previous = -1;
  for (let slot = 0; slot < wanted; slot += 1) {
    const index = Math.round((slot * (dates.length - 1)) / (wanted - 1));
    if (index !== previous) {
      const date = dates[index];
      if (date !== undefined) {
        labels.push(date);
      }
      previous = index;
    }
  }
  return labels;
}

/**
 * A date bucket on the x axis: `3/10` in both locales. The bucket is a UTC
 * date, so it is formatted in UTC — a local timezone must not shift it to the
 * day before.
 */
export function formatTrendAxisDate(date: string, locale: string): string {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!match) {
    return date;
  }
  const [, year, month, day] = match;
  return new Intl.DateTimeFormat(locale, {
    month: "numeric",
    day: "numeric",
    timeZone: "UTC",
  }).format(Date.UTC(Number(year), Number(month) - 1, Number(day)));
}

/** A count-axis tick: exact while small, compact once it would crowd the gutter. */
export function formatAxisCount(value: number, locale: string): string {
  return new Intl.NumberFormat(locale, {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(value);
}

// ---- summary numbers ----

export interface IssueSeverityCounts {
  openWarningCount: number;
  openErrorCount: number;
  openCriticalCount: number;
}

/** Issues that are open or acknowledged, summed over the three severities. */
export function openIssueCount(counts: IssueSeverityCounts): number {
  return (
    counts.openWarningCount + counts.openErrorCount + counts.openCriticalCount
  );
}

/** The non-zero severities, worst first, for a hint line. */
export function issueSeverityParts(
  counts: IssueSeverityCounts
): Array<{ severity: "warning" | "error" | "critical"; count: number }> {
  return (
    [
      { severity: "critical" as const, count: counts.openCriticalCount },
      { severity: "error" as const, count: counts.openErrorCount },
      { severity: "warning" as const, count: counts.openWarningCount },
    ] satisfies Array<{
      severity: "warning" | "error" | "critical";
      count: number;
    }>
  ).filter((part) => part.count > 0);
}

/**
 * A ratio as a percentage: `4.2%`. A missing denominator is "-", not "0%":
 * "nothing executed" and "nothing failed" are different facts.
 */
export function formatPercent(value: number, fractionDigits = 1): string {
  if (!Number.isFinite(value)) {
    return "-";
  }
  const percent = value * 100;
  return `${percent.toFixed(fractionDigits)}%`;
}

/** Failed and errored executions as a share of every execution in the tally. */
export function failurePercent(
  counts: QualityStatusCounts | undefined
): string {
  const total = counts?.total ?? 0;
  if (total <= 0) {
    return "-";
  }
  return formatPercent(((counts?.fail ?? 0) + (counts?.error ?? 0)) / total);
}

/** Failed and errored executions as a share of the whole window. */
export function trendFailurePercent(days: readonly TrendDay[]): string {
  let total = 0;
  let bad = 0;
  for (const day of days) {
    total += day.total;
    bad += day.fail + day.error;
  }
  return total <= 0 ? "-" : formatPercent(bad / total);
}

// ---- coverage, score and pass rate ----

/**
 * Covered / total as a 0-100 percentage with one decimal. `null` when the
 * denominator is empty: "no column to watch" and "none of the columns are
 * watched" are different facts, and the card must not print 0% for the first.
 */
export function coveragePercent(covered: number, total: number): number | null {
  if (!Number.isFinite(covered) || !Number.isFinite(total) || total <= 0) {
    return null;
  }
  return Math.round((covered / total) * 1000) / 10;
}

/**
 * A 0-100 percentage for display: `66.7%`. Unlike `formatPercent` the value is
 * already a percentage, not a fraction; `null` stays "-" for the same reason.
 */
export function formatPercentValue(percent: number | null): string {
  return percent === null ? "-" : `${percent.toFixed(1)}%`;
}

/** A 0-100 quality score with one decimal; "-" while nothing was judged. */
export function formatQualityScore(score: number | undefined): string {
  if (score === undefined || !Number.isFinite(score)) {
    return "-";
  }
  return score.toFixed(1);
}

/** A 0-1 pass rate as a percentage; "-" when no execution was judged. */
export function formatPassRate(rate: number | undefined): string {
  if (rate === undefined || !Number.isFinite(rate)) {
    return "-";
  }
  return formatPercent(rate);
}

/** One coverage lens row: an instance or an environment. */
export interface CoverageBreakdownRow {
  key: string;
  /** The server's title, or the key when it resolved none; empty if unassigned. */
  title: string;
  totalTableCount: number;
  monitoredTableCount: number;
  totalColumnCount: number;
  coveredColumnCount: number;
  /** 0-100, or null when the row has no table in the denominator. */
  tableCoveragePercent: number | null;
  /** 0-100, or null when the row has no column in the denominator. */
  columnCoveragePercent: number | null;
}

/**
 * The instance/environment lenses as table rows, alphabetical by title (the key
 * is the fallback for a row whose title did not resolve). The server's order is
 * not meaningful; a reader compares the rows with each other.
 */
export function normalizeCoverageBreakdown(
  rows: readonly QualityCoverageBreakdown[]
): CoverageBreakdownRow[] {
  return rows
    .map((row) => ({
      key: row.key,
      title: row.title,
      totalTableCount: row.totalTableCount,
      monitoredTableCount: row.monitoredTableCount,
      totalColumnCount: row.totalColumnCount,
      coveredColumnCount: row.coveredColumnCount,
      tableCoveragePercent: coveragePercent(
        row.monitoredTableCount,
        row.totalTableCount
      ),
      columnCoveragePercent: coveragePercent(
        row.coveredColumnCount,
        row.totalColumnCount
      ),
    }))
    .sort((left, right) => {
      const a = left.title || left.key;
      const b = right.title || right.key;
      return a < b ? -1 : a > b ? 1 : 0;
    });
}

/** One score lens row: a dimension or an owner. */
export interface ScoreBreakdownRow {
  /** The dimension as a string, or the owner resource name ("" if unassigned). */
  key: string;
  /** Set on the dimension lens; undefined on the owner lens. */
  dimension?: QualityDimension;
  ruleCount: number;
  scoredRuleCount: number;
  failingRuleCount: number;
  qualityScore?: number;
}

/**
 * The dimension lens in enum order: the dimensions are a fixed catalog, so a
 * stable order is what lets a reader compare the same rows between visits.
 */
export function normalizeDimensionScores(
  rows: readonly QualityDimensionScore[]
): ScoreBreakdownRow[] {
  return rows
    .map((row) => ({
      key: String(row.dimension),
      dimension: row.dimension,
      ruleCount: row.ruleCount,
      scoredRuleCount: row.scoredRuleCount,
      failingRuleCount: row.failingRuleCount,
      qualityScore: row.qualityScore,
    }))
    .sort((left, right) => (left.dimension ?? 0) - (right.dimension ?? 0));
}

/**
 * The owner lens alphabetically by resource name, with the unassigned bucket
 * last: a missing owner is not a member's name, so it does not belong in the
 * middle of the alphabet.
 */
export function normalizeOwnerScores(
  rows: readonly QualityOwnerScore[]
): ScoreBreakdownRow[] {
  return rows
    .map((row) => ({
      key: row.owner,
      ruleCount: row.ruleCount,
      scoredRuleCount: row.scoredRuleCount,
      failingRuleCount: row.failingRuleCount,
      qualityScore: row.qualityScore,
    }))
    .sort((left, right) => {
      if (left.key === right.key) {
        return 0;
      }
      if (left.key === "") {
        return 1;
      }
      if (right.key === "") {
        return -1;
      }
      return left.key < right.key ? -1 : 1;
    });
}
