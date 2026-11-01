import {
  QualityComparison,
  type QualityRuleTrendPoint,
  QualityRuleType,
  QualityThresholdSuggestionMethod,
  type QualityThresholds,
  QualityTrendNotice,
  QualityTrendUnit,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatRowCount, formatSeconds } from "./dataQuality";
import { formatDate } from "./datetime";

/**
 * Geometry and vocabulary for a single check's observed-value trend.
 *
 * The app draws no charting library, so the line, its axis and the threshold
 * band are computed here as plain numbers (and unit-tested), while the
 * component only maps them into SVG. Two things make this chart different from
 * the workspace overview's stacked bars:
 *
 *  - The x axis is the execution's position in the window, not time: the
 *    service returns judged executions oldest first and a gap between two runs
 *    is not more interesting than the runs themselves.
 *  - The y domain must hold the threshold band even when every measured value
 *    sits far below it. A band drawn outside the plot would silently hide the
 *    fact that the check is judging against a bound the data never approaches.
 */

const DEFAULT_LOCALE = "en-US";

// ---- value formatting ----

/**
 * One observed value in the trend's own unit. A PERCENT value is already a
 * 0-100 number (the service computes failed/total × 100), so it is never
 * multiplied again; a COUNT is a row count and may need grouping.
 */
export function formatTrendValue(
  value: number,
  unit: QualityTrendUnit,
  locale = DEFAULT_LOCALE
): string {
  if (!Number.isFinite(value)) {
    return "-";
  }
  switch (unit) {
    case QualityTrendUnit.COUNT:
      return formatRowCount(Math.round(value), locale);
    case QualityTrendUnit.PERCENT:
      return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(value)}%`;
    case QualityTrendUnit.SECONDS:
      return formatSeconds(value);
    case QualityTrendUnit.NUMBER:
    default:
      return new Intl.NumberFormat(locale, { maximumFractionDigits: 2 }).format(
        value
      );
  }
}

/** An axis tick in the same unit, compact enough for the 52px gutter. */
export function formatTrendAxisValue(
  value: number,
  unit: QualityTrendUnit,
  locale = DEFAULT_LOCALE
): string {
  if (!Number.isFinite(value)) {
    return "-";
  }
  if (unit === QualityTrendUnit.PERCENT) {
    return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(value)}%`;
  }
  if (unit === QualityTrendUnit.SECONDS) {
    return formatSeconds(value);
  }
  return new Intl.NumberFormat(locale, {
    notation: "compact",
    maximumFractionDigits: 1,
  }).format(value);
}

/** The point's calendar day, for the x axis; the time travels in the tooltip. */
export function formatTrendPointDate(
  point: Pick<QualityRuleTrendPoint, "startTime">,
  locale = DEFAULT_LOCALE
): string {
  return formatDate(point.startTime, locale);
}

/**
 * The point indices the x axis labels, evenly spread and always including the
 * first and the last. Thirty executions get a handful of dated ticks rather
 * than thirty overlapping ones.
 */
export function trendAxisIndices(count: number, maxLabels = 4): number[] {
  if (count <= 0) {
    return [];
  }
  const wanted = Math.max(2, Math.min(Math.round(maxLabels), count));
  if (count <= wanted) {
    return Array.from({ length: count }, (_, index) => index);
  }
  const indices: number[] = [];
  let previous = -1;
  for (let slot = 0; slot < wanted; slot += 1) {
    const index = Math.round((slot * (count - 1)) / (wanted - 1));
    if (index !== previous) {
      indices.push(index);
      previous = index;
    }
  }
  return indices;
}

// ---- threshold band ----

/** Which side of the value a threshold bounds. */
export type TrendBoundSide = "upper" | "lower";

/** One threshold bound of the band, in the trend's unit. */
export interface TrendThresholdBound {
  key: "fail" | "warn";
  bound: TrendBoundSide;
  value: number;
}

function pushBound(
  out: TrendThresholdBound[],
  key: TrendThresholdBound["key"],
  bound: TrendBoundSide,
  value: number | undefined
) {
  if (value !== undefined && Number.isFinite(value)) {
    out.push({ key, bound, value });
  }
}

/**
 * The bounds the execution's threshold snapshot implies, ready to draw. A
 * single-upper-bound comparison contributes one line per level; a range
 * comparison contributes a lower and an upper line per level. The comparison
 * decides which side a bound belongs to, because that is what makes the shaded
 * side the failing one.
 */
export function trendThresholdBounds(
  thresholds: QualityThresholds | undefined
): TrendThresholdBound[] {
  if (!thresholds) {
    return [];
  }
  const out: TrendThresholdBound[] = [];
  switch (thresholds.comparison) {
    case QualityComparison.GREATER_THAN:
      pushBound(out, "warn", "upper", thresholds.warnValue);
      pushBound(out, "fail", "upper", thresholds.failValue);
      break;
    case QualityComparison.LESS_THAN:
      pushBound(out, "warn", "lower", thresholds.warnValue);
      pushBound(out, "fail", "lower", thresholds.failValue);
      break;
    case QualityComparison.INSIDE_RANGE:
    case QualityComparison.OUTSIDE_RANGE:
      pushBound(out, "fail", "lower", thresholds.failValue);
      pushBound(out, "fail", "upper", thresholds.failValueUpper);
      pushBound(out, "warn", "lower", thresholds.warnValue);
      pushBound(out, "warn", "upper", thresholds.warnValueUpper);
      break;
    default:
      // A snapshot recorded before the comparison existed cannot be read with
      // certainty. A lone value is the upper bound a count rule writes; a pair is
      // the range a row-count or aggregate rule writes (those snapshots always
      // carry a comparison, so this is a legacy fallback, not a live path).
      if (
        thresholds.failValueUpper !== undefined ||
        thresholds.warnValueUpper !== undefined
      ) {
        pushBound(out, "fail", "lower", thresholds.failValue);
        pushBound(out, "fail", "upper", thresholds.failValueUpper);
        pushBound(out, "warn", "lower", thresholds.warnValue);
        pushBound(out, "warn", "upper", thresholds.warnValueUpper);
      } else {
        pushBound(out, "fail", "upper", thresholds.failValue);
        pushBound(out, "warn", "upper", thresholds.warnValue);
      }
      break;
  }
  return out;
}

// ---- notices and methods ----

const BAND_NOTICES: Array<{
  notice: QualityTrendNotice;
  labelKey: string;
}> = [
  {
    notice: QualityTrendNotice.EDITED_SINCE_LAST_RUN,
    labelKey: "dataQuality.trend.band.edited",
  },
  {
    notice: QualityTrendNotice.NO_JUDGED_EXECUTION,
    labelKey: "dataQuality.trend.band.noExecution",
  },
  {
    notice: QualityTrendNotice.UNIT_NOT_COMPARABLE,
    labelKey: "dataQuality.trend.band.notComparable",
  },
  {
    notice: QualityTrendNotice.UNSUPPORTED_RULE_TYPE,
    labelKey: "dataQuality.trend.band.unsupported",
  },
];

/**
 * Why the trend has no threshold band, as an i18n key; undefined means the
 * notice explains nothing the chart needs to say.
 */
export function trendBandNoticeKey(
  notice: QualityTrendNotice
): string | undefined {
  return BAND_NOTICES.find((entry) => entry.notice === notice)?.labelKey;
}

const SUGGESTION_NOTICES: Array<{
  notice: QualityTrendNotice;
  labelKey: string;
}> = [
  {
    // "No judged execution" is a stricter version of "not enough history": there
    // is no suggestion either way, and the copy that mentions the five-run
    // minimum is the useful next step.
    notice: QualityTrendNotice.NO_JUDGED_EXECUTION,
    labelKey: "dataQuality.trend.suggestion.insufficient",
  },
  {
    notice: QualityTrendNotice.INSUFFICIENT_HISTORY,
    labelKey: "dataQuality.trend.suggestion.insufficient",
  },
  {
    notice: QualityTrendNotice.UNSUPPORTED_RULE_TYPE,
    labelKey: "dataQuality.trend.suggestion.unsupportedType",
  },
  {
    notice: QualityTrendNotice.UNSUPPORTED_CONFIG,
    labelKey: "dataQuality.trend.suggestion.unsupportedConfig",
  },
  {
    notice: QualityTrendNotice.UNIT_NOT_COMPARABLE,
    labelKey: "dataQuality.trend.suggestion.unitNotComparable",
  },
];

/**
 * Why a suggestion could not be derived, as an i18n key. EDITED_SINCE_LAST_RUN
 * returns undefined: an edit after the last run does not invalidate a
 * suggestion drawn from earlier observations, so the panel shows the suggestion
 * rather than a warning.
 */
export function trendSuggestionNoticeKey(
  notice: QualityTrendNotice
): string | undefined {
  return SUGGESTION_NOTICES.find((entry) => entry.notice === notice)?.labelKey;
}

const SUGGESTION_METHODS: Array<{
  method: QualityThresholdSuggestionMethod;
  labelKey: string;
}> = [
  {
    method: QualityThresholdSuggestionMethod.P95_P99,
    labelKey: "dataQuality.trend.suggestion.method.p95P99",
  },
  {
    method: QualityThresholdSuggestionMethod.OBSERVED_RANGE,
    labelKey: "dataQuality.trend.suggestion.method.observedRange",
  },
];

/** How a suggested threshold was derived, as an i18n key. */
export function trendSuggestionMethodKey(
  method: QualityThresholdSuggestionMethod
): string | undefined {
  return SUGGESTION_METHODS.find((entry) => entry.method === method)?.labelKey;
}

const EMPTY_TREND_KEYS = {
  empty: { labelKey: "dataQuality.trend.empty" },
  noValue: { labelKey: "dataQuality.trend.noValue" },
} as const;

/**
 * The empty-state sentence for a trend with no points. Schema drift reads
 * metadata rather than rows, so its history has no numeric value to plot at
 * all; every other type simply has not run yet.
 */
export function emptyTrendKey(
  ruleType: QualityRuleType,
  unit: QualityTrendUnit
): string {
  return ruleType === QualityRuleType.SCHEMA_DRIFT ||
    unit === QualityTrendUnit.UNSPECIFIED
    ? EMPTY_TREND_KEYS.noValue.labelKey
    : EMPTY_TREND_KEYS.empty.labelKey;
}

// ---- geometry ----

export interface TrendChartTick {
  value: number;
  y: number;
}

export interface TrendPointGeometry {
  index: number;
  point: QualityRuleTrendPoint;
  /** Center of the point's slot: the x position of its marker. */
  x: number;
  y: number;
  /** The clickable/hoverable column the point owns. */
  slotX: number;
  slotWidth: number;
}

export interface TrendThresholdLine extends TrendThresholdBound {
  y: number;
}

/** A shaded rectangle: the region where that level of severity fails. */
export interface TrendThresholdRegion {
  key: TrendThresholdBound["key"];
  y: number;
  height: number;
}

export interface ObservedTrendGeometry {
  width: number;
  height: number;
  plotWidth: number;
  plotHeight: number;
  paddingTop: number;
  paddingLeft: number;
  paddingRight: number;
  paddingBottom: number;
  /** The bottom of the plot area, i.e. where minValue is drawn. */
  baseline: number;
  minValue: number;
  maxValue: number;
  ticks: TrendChartTick[];
  points: TrendPointGeometry[];
  lines: TrendThresholdLine[];
  regions: TrendThresholdRegion[];
  /** The band in value units, including bounds drawn outside the data's range. */
  bounds: TrendThresholdBound[];
  /** The x axis labels (point indices), evenly spread over the window. */
  axisIndices: number[];
}

export interface TrendChartOptions {
  width?: number;
  height?: number;
  paddingTop?: number;
  paddingRight?: number;
  paddingBottom?: number;
  paddingLeft?: number;
  /** Grid rows to aim for; the axis still rounds to a readable step. */
  tickCount?: number;
  /** Anchoring the domain at zero reads naturally for counts, rates and lags. */
  includeZero?: boolean;
  /** x labels to show, in addition to always showing the first and last. */
  maxAxisLabels?: number;
}

const DEFAULT_CHART: Required<TrendChartOptions> = {
  width: 640,
  height: 224,
  paddingTop: 16,
  paddingRight: 16,
  paddingBottom: 30,
  paddingLeft: 52,
  tickCount: 4,
  includeZero: true,
  maxAxisLabels: 4,
};

function roundValue(value: number): number {
  const rounded = Math.round(value * 1e6) / 1e6;
  return Object.is(rounded, -0) ? 0 : rounded;
}

/**
 * A round domain around `[min, max]`: evenly spaced ticks that include every
 * input value. Unlike the overview's count axis this one may start below zero,
 * because an aggregate or a table-to-table difference can be negative.
 */
export function niceTrendScale(
  min: number,
  max: number,
  tickCount = 4
): { min: number; max: number; step: number; ticks: number[] } {
  if (!Number.isFinite(min) || !Number.isFinite(max)) {
    return { min: 0, max: 1, step: 1, ticks: [0, 1] };
  }
  let low = Math.min(min, max);
  let high = Math.max(min, max);
  if (low === high) {
    // A flat line still needs a band to sit in; a single value gets a small
    // symmetric margin rather than a zero-height plot.
    const pad = Math.abs(low) * 0.1 || 1;
    low -= pad;
    high += pad;
  }
  const rough = (high - low) / Math.max(1, tickCount);
  const magnitude = 10 ** Math.floor(Math.log10(rough));
  const candidates = [1, 2, 5, 10].map((factor) => factor * magnitude);
  const step =
    candidates.find((candidate) => candidate >= rough) ??
    candidates[candidates.length - 1] ??
    1;
  const niceMin = roundValue(Math.floor(low / step) * step);
  const niceMax = roundValue(Math.ceil(high / step) * step);
  const ticks: number[] = [];
  for (let tick = niceMin; tick <= niceMax + step / 2; tick += step) {
    ticks.push(roundValue(tick));
  }
  return { min: niceMin, max: niceMax, step, ticks };
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(Math.max(value, low), high);
}

/**
 * The line-chart geometry for one trend. Every point owns an equal-width slot
 * along the x axis, so execution 1 and execution 30 are the same distance apart
 * regardless of how much time passed between them.
 *
 * The y domain is computed from the points *and* the band, so a fail bound the
 * data never reaches is still drawn inside the plot; the band is the reference
 * the reader needs in order to read the line at all.
 */
export function buildObservedTrendGeometry(
  points: readonly QualityRuleTrendPoint[],
  thresholds: QualityThresholds | undefined,
  options: TrendChartOptions = {}
): ObservedTrendGeometry {
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
  const bounds = trendThresholdBounds(thresholds);

  const values = [
    ...points.map((point) => point.value),
    ...bounds.map((bound) => bound.value),
  ].filter((value) => Number.isFinite(value));
  let low = values.length > 0 ? Math.min(...values) : 0;
  let high = values.length > 0 ? Math.max(...values) : 1;
  if (chart.includeZero) {
    low = Math.min(low, 0);
    high = Math.max(high, 0);
  }
  if (low === high && low === 0) {
    // An all-zero window: a 0..1 axis keeps the baseline visible instead of
    // collapsing the drawing to a line on its own edge.
    high = 1;
  }
  const scale = niceTrendScale(low, high, chart.tickCount);
  const span = scale.max - scale.min || 1;
  const yOf = (value: number) =>
    roundValue(baseline - ((value - scale.min) / span) * plotHeight);

  const slot = points.length > 0 ? plotWidth / points.length : 0;
  const pointGeometry: TrendPointGeometry[] = points.map((point, index) => {
    const slotX = roundValue(chart.paddingLeft + index * slot);
    return {
      index,
      point,
      x: roundValue(slotX + slot / 2),
      y: yOf(point.value),
      slotX,
      slotWidth: roundValue(slot),
    };
  });

  const lines: TrendThresholdLine[] = bounds.map((bound) => ({
    ...bound,
    y: yOf(bound.value),
  }));
  const regions = buildThresholdRegions(
    lines,
    thresholds?.comparison ?? QualityComparison.UNSPECIFIED,
    chart.paddingTop,
    baseline
  );

  const ticks: TrendChartTick[] = scale.ticks.map((value) => ({
    value,
    y: yOf(value),
  }));

  return {
    width: chart.width,
    height: chart.height,
    plotWidth,
    plotHeight,
    paddingTop: chart.paddingTop,
    paddingLeft: chart.paddingLeft,
    paddingRight: chart.paddingRight,
    paddingBottom: chart.paddingBottom,
    baseline,
    minValue: scale.min,
    maxValue: scale.max,
    ticks,
    points: pointGeometry,
    lines,
    regions,
    bounds,
    axisIndices: trendAxisIndices(points.length, chart.maxAxisLabels),
  };
}

/**
 * The failing regions of the band, as rectangles between a threshold line and
 * the level beyond it. The fail tint always sits outside the warn tint, so a
 * value between two lines reads as the level it actually crossed.
 */
function buildThresholdRegions(
  lines: readonly TrendThresholdLine[],
  comparison: QualityComparison,
  top: number,
  bottom: number
): TrendThresholdRegion[] {
  const regions: TrendThresholdRegion[] = [];
  const add = (key: TrendThresholdBound["key"], start: number, end: number) => {
    const upper = clamp(Math.min(start, end), top, bottom);
    const lower = clamp(Math.max(start, end), top, bottom);
    const height = lower - upper;
    if (height > 0.5) {
      regions.push({ key, y: roundValue(upper), height: roundValue(height) });
    }
  };
  const at = (key: TrendThresholdBound["key"], bound: TrendBoundSide) =>
    lines.find((line) => line.key === key && line.bound === bound)?.y;

  const failUpper = at("fail", "upper");
  const failLower = at("fail", "lower");
  const warnUpper = at("warn", "upper");
  const warnLower = at("warn", "lower");

  if (comparison === QualityComparison.INSIDE_RANGE) {
    // The accepted band is the interval itself: values inside it fail, so the
    // failing tint is the space between the two fail bounds.
    if (failLower !== undefined && failUpper !== undefined) {
      add("fail", failLower, failUpper);
    }
    if (warnLower !== undefined && warnUpper !== undefined) {
      add("warn", warnLower, warnUpper);
    }
    return regions;
  }

  // Everything above an upper fail bound (or below a lower one) fails.
  if (failUpper !== undefined) {
    add("fail", top, failUpper);
  }
  if (failLower !== undefined) {
    add("fail", failLower, bottom);
  }
  // Warn fills the space between its own line and the fail line beyond it, so
  // the two tints never cover each other.
  if (warnUpper !== undefined) {
    add("warn", failUpper ?? top, warnUpper);
  }
  if (warnLower !== undefined) {
    add("warn", warnLower, failLower ?? bottom);
  }
  return regions;
}
