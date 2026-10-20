import type {
  QualityRunCounts,
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
  x: number;
  width: number;
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
}

const DEFAULT_CHART: Required<TrendChartOptions> = {
  width: 720,
  height: 220,
  paddingTop: 12,
  paddingRight: 12,
  paddingBottom: 28,
  paddingLeft: 44,
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
 * runs that day), so it is filled rather than skipped: a gap in the line would
 * otherwise read as missing data instead of as "nothing ran".
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

/** True when at least one day reported a result. */
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
  const scale = niceScale(highest);
  const slot = days.length > 0 ? plotWidth / days.length : 0;
  const barWidth = slot * 0.62;
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
    return {
      x: chart.paddingLeft + index * slot + (slot - barWidth) / 2,
      width: barWidth,
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
    paddingLeft: chart.paddingLeft,
    paddingRight: chart.paddingRight,
    maxValue: scale.max,
    bars,
    ticks,
    dates: days.map((day) => day.date),
  };
}

/** The date labels the axis shows: first, middle and last day. */
export function trendDateLabels(dates: readonly string[]): string[] {
  if (dates.length === 0) {
    return [];
  }
  const first = dates[0];
  const last = dates[dates.length - 1];
  if (dates.length <= 2) {
    return [first, last].filter(
      (value, index, all) => all.indexOf(value) === index
    );
  }
  return [first, dates[Math.floor((dates.length - 1) / 2)], last];
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
 * "nothing ran" and "nothing failed" are different facts.
 */
export function formatPercent(value: number, fractionDigits = 1): string {
  if (!Number.isFinite(value)) {
    return "-";
  }
  const percent = value * 100;
  return `${percent.toFixed(fractionDigits)}%`;
}

/** Failed and errored results as a share of every result in the tally. */
export function failurePercent(counts: QualityRunCounts | undefined): string {
  const total = counts?.total ?? 0;
  if (total <= 0) {
    return "-";
  }
  return formatPercent(((counts?.fail ?? 0) + (counts?.error ?? 0)) / total);
}

/** Failed and errored results as a share of the whole window. */
export function trendFailurePercent(days: readonly TrendDay[]): string {
  let total = 0;
  let bad = 0;
  for (const day of days) {
    total += day.total;
    bad += day.fail + day.error;
  }
  return total <= 0 ? "-" : formatPercent(bad / total);
}
