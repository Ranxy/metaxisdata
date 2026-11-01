import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  QualityComparison,
  QualityRuleTrendPointSchema,
  QualityRuleType,
  QualityThresholdSuggestionMethod,
  QualityThresholdsSchema,
  QualityTrendNotice,
  QualityTrendUnit,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  buildObservedTrendGeometry,
  emptyTrendKey,
  formatTrendAxisValue,
  formatTrendPointDate,
  formatTrendValue,
  niceTrendScale,
  trendAxisIndices,
  trendBandNoticeKey,
  trendSuggestionMethodKey,
  trendSuggestionNoticeKey,
  trendThresholdBounds,
} from "./dataQualityTrend";

function point(value: number, overrides: Record<string, unknown> = {}) {
  return create(QualityRuleTrendPointSchema, {
    execution: `workspaces/ws/qualityExecutions/${value}`,
    value,
    startTime: timestampFromDate(new Date("2026-03-10T08:00:00Z")),
    ...overrides,
  });
}

const NOW = new Date("2026-03-10T08:00:00Z");

describe("trend value formatting", () => {
  it("formats each unit in its own terms", () => {
    // A PERCENT value is already 0-100: multiplying by 100 again would print 450%.
    expect(formatTrendValue(4.5, QualityTrendUnit.PERCENT)).toBe("4.5%");
    expect(formatTrendValue(1250, QualityTrendUnit.COUNT)).toBe("1,250");
    // A count is a whole number of rows even when the double is fractional.
    expect(formatTrendValue(1250.4, QualityTrendUnit.COUNT)).toBe("1,250");
    expect(formatTrendValue(125, QualityTrendUnit.SECONDS)).toBe("2m 5s");
    expect(formatTrendValue(1.235, QualityTrendUnit.NUMBER)).toBe("1.24");
    expect(formatTrendValue(Number.NaN, QualityTrendUnit.COUNT)).toBe("-");
  });

  it("formats axis ticks compactly per unit", () => {
    expect(formatTrendAxisValue(12_000, QualityTrendUnit.COUNT)).toBe("12K");
    expect(formatTrendAxisValue(12.25, QualityTrendUnit.PERCENT)).toBe("12.3%");
    expect(formatTrendAxisValue(3600, QualityTrendUnit.SECONDS)).toBe("1h");
    expect(formatTrendAxisValue(Number.NaN, QualityTrendUnit.NUMBER)).toBe("-");
  });

  it("dates a point by its start day", () => {
    expect(
      formatTrendPointDate({ startTime: timestampFromDate(NOW) }, "en-US")
    ).toBe("03/10/2026");
    expect(formatTrendPointDate({}, "en-US")).toBe("-");
  });
});

describe("trend axis labels", () => {
  it("always labels the first and last point", () => {
    expect(trendAxisIndices(0)).toEqual([]);
    expect(trendAxisIndices(1)).toEqual([0]);
    expect(trendAxisIndices(3)).toEqual([0, 1, 2]);
    const indices = trendAxisIndices(30, 4);
    expect(indices[0]).toBe(0);
    expect(indices[indices.length - 1]).toBe(29);
    expect(indices).toHaveLength(4);
    // The labels never repeat, which would print one day twice.
    expect(new Set(indices).size).toBe(indices.length);
  });
});

describe("threshold bounds", () => {
  it("returns nothing without a snapshot", () => {
    expect(trendThresholdBounds(undefined)).toEqual([]);
  });

  it("reads a one-sided comparison as upper bounds", () => {
    const thresholds = create(QualityThresholdsSchema, {
      comparison: QualityComparison.GREATER_THAN,
      failValue: 5,
      warnValue: 3,
    });
    expect(trendThresholdBounds(thresholds)).toEqual([
      { key: "warn", bound: "upper", value: 3 },
      { key: "fail", bound: "upper", value: 5 },
    ]);
  });

  it("reads a less-than comparison as lower bounds", () => {
    const thresholds = create(QualityThresholdsSchema, {
      comparison: QualityComparison.LESS_THAN,
      failValue: 10,
    });
    expect(trendThresholdBounds(thresholds)).toEqual([
      { key: "fail", bound: "lower", value: 10 },
    ]);
  });

  it("keeps both sides of a range comparison", () => {
    const thresholds = create(QualityThresholdsSchema, {
      comparison: QualityComparison.OUTSIDE_RANGE,
      failValue: 10,
      failValueUpper: 20,
      warnValue: 12,
      warnValueUpper: 18,
    });
    expect(trendThresholdBounds(thresholds)).toEqual([
      { key: "fail", bound: "lower", value: 10 },
      { key: "fail", bound: "upper", value: 20 },
      { key: "warn", bound: "lower", value: 12 },
      { key: "warn", bound: "upper", value: 18 },
    ]);
  });

  it("falls back to the values a legacy snapshot carries", () => {
    const twoSided = create(QualityThresholdsSchema, {
      failValue: 10,
      failValueUpper: 20,
    });
    // The upper pair means a range even without a comparison; the warn pair is
    // simply absent, because the legacy snapshot recorded none.
    expect(trendThresholdBounds(twoSided)).toEqual([
      { key: "fail", bound: "lower", value: 10 },
      { key: "fail", bound: "upper", value: 20 },
    ]);
    const oneSided = create(QualityThresholdsSchema, { failValue: 5 });
    expect(trendThresholdBounds(oneSided)).toEqual([
      { key: "fail", bound: "lower", value: 5 },
    ]);
    // A non-finite value cannot be drawn and is dropped rather than crashing.
    expect(
      trendThresholdBounds(
        create(QualityThresholdsSchema, {
          comparison: QualityComparison.GREATER_THAN,
          failValue: Number.POSITIVE_INFINITY,
        })
      )
    ).toEqual([]);
  });
});

describe("notice vocabulary", () => {
  it("maps a band notice to its sentence", () => {
    expect(trendBandNoticeKey(QualityTrendNotice.EDITED_SINCE_LAST_RUN)).toBe(
      "dataQuality.trend.band.edited"
    );
    expect(trendBandNoticeKey(QualityTrendNotice.NO_JUDGED_EXECUTION)).toBe(
      "dataQuality.trend.band.noExecution"
    );
    expect(trendBandNoticeKey(QualityTrendNotice.UNIT_NOT_COMPARABLE)).toBe(
      "dataQuality.trend.band.notComparable"
    );
    expect(trendBandNoticeKey(QualityTrendNotice.UNSUPPORTED_RULE_TYPE)).toBe(
      "dataQuality.trend.band.unsupported"
    );
    // Unspecified means "the band explains itself", not a missing translation.
    expect(trendBandNoticeKey(QualityTrendNotice.UNSPECIFIED)).toBeUndefined();
  });

  it("maps a suggestion notice to its sentence", () => {
    expect(
      trendSuggestionNoticeKey(QualityTrendNotice.NO_JUDGED_EXECUTION)
    ).toBe("dataQuality.trend.suggestion.insufficient");
    expect(
      trendSuggestionNoticeKey(QualityTrendNotice.INSUFFICIENT_HISTORY)
    ).toBe("dataQuality.trend.suggestion.insufficient");
    expect(
      trendSuggestionNoticeKey(QualityTrendNotice.UNSUPPORTED_RULE_TYPE)
    ).toBe("dataQuality.trend.suggestion.unsupportedType");
    expect(
      trendSuggestionNoticeKey(QualityTrendNotice.UNSUPPORTED_CONFIG)
    ).toBe("dataQuality.trend.suggestion.unsupportedConfig");
    expect(
      trendSuggestionNoticeKey(QualityTrendNotice.UNIT_NOT_COMPARABLE)
    ).toBe("dataQuality.trend.suggestion.unitNotComparable");
    // An edit after the last run does not invalidate a suggestion.
    expect(
      trendSuggestionNoticeKey(QualityTrendNotice.EDITED_SINCE_LAST_RUN)
    ).toBeUndefined();
    expect(
      trendSuggestionNoticeKey(QualityTrendNotice.UNSPECIFIED)
    ).toBeUndefined();
  });

  it("maps a suggestion method to how it was derived", () => {
    expect(
      trendSuggestionMethodKey(QualityThresholdSuggestionMethod.P95_P99)
    ).toBe("dataQuality.trend.suggestion.method.p95P99");
    expect(
      trendSuggestionMethodKey(QualityThresholdSuggestionMethod.OBSERVED_RANGE)
    ).toBe("dataQuality.trend.suggestion.method.observedRange");
    expect(
      trendSuggestionMethodKey(QualityThresholdSuggestionMethod.UNSPECIFIED)
    ).toBeUndefined();
  });

  it("says why an empty trend has nothing to plot", () => {
    expect(
      emptyTrendKey(QualityRuleType.SCHEMA_DRIFT, QualityTrendUnit.UNSPECIFIED)
    ).toBe("dataQuality.trend.noValue");
    expect(
      emptyTrendKey(QualityRuleType.ROW_COUNT, QualityTrendUnit.UNSPECIFIED)
    ).toBe("dataQuality.trend.noValue");
    expect(
      emptyTrendKey(QualityRuleType.ROW_COUNT, QualityTrendUnit.COUNT)
    ).toBe("dataQuality.trend.empty");
  });
});

describe("nice trend scale", () => {
  it("rounds the domain outwards and includes the data", () => {
    const scale = niceTrendScale(0, 9, 4);
    expect(scale.min).toBe(0);
    expect(scale.max).toBeGreaterThanOrEqual(9);
    expect(scale.ticks[0]).toBe(0);
    expect(scale.ticks[scale.ticks.length - 1]).toBe(scale.max);
    expect(scale.ticks.every((tick) => Number.isFinite(tick))).toBe(true);
  });

  it("expands a flat line into a band", () => {
    const scale = niceTrendScale(5, 5, 4);
    expect(scale.min).toBeLessThan(5);
    expect(scale.max).toBeGreaterThan(5);
  });

  it("keeps a negative domain negative", () => {
    const scale = niceTrendScale(-8, -2, 4);
    expect(scale.min).toBeLessThanOrEqual(-8);
    expect(scale.max).toBeGreaterThanOrEqual(-2);
  });

  it("falls back to 0..1 for non-finite input", () => {
    expect(niceTrendScale(Number.NaN, 2)).toEqual({
      min: 0,
      max: 1,
      step: 1,
      ticks: [0, 1],
    });
  });
});

describe("trend geometry", () => {
  it("places points on equal slots, oldest first", () => {
    const geometry = buildObservedTrendGeometry(
      [point(1), point(2), point(3)],
      undefined,
      { width: 640 }
    );
    expect(geometry.points.map((entry) => entry.index)).toEqual([0, 1, 2]);
    const [first, second, third] = geometry.points;
    if (!first || !second || !third) {
      throw new Error("missing points");
    }
    const firstGap = second.x - first.x;
    const secondGap = third.x - second.x;
    expect(firstGap).toBeCloseTo(secondGap, 5);
    expect(first.slotWidth).toBeCloseTo(second.slotWidth, 5);
    // The window spans the plot, so the first slot starts at the gutter edge.
    expect(first.slotX).toBe(geometry.paddingLeft);
    expect(third.slotX + third.slotWidth).toBeCloseTo(
      geometry.width - geometry.paddingRight,
      5
    );
  });

  it("centers a single point in the plot", () => {
    const geometry = buildObservedTrendGeometry([point(7)], undefined, {
      width: 640,
    });
    const [only] = geometry.points;
    expect(only?.x).toBeCloseTo(
      geometry.paddingLeft + geometry.plotWidth / 2,
      5
    );
  });

  it("stretches the y domain to hold thresholds the data never reaches", () => {
    const thresholds = create(QualityThresholdsSchema, {
      comparison: QualityComparison.GREATER_THAN,
      failValue: 90,
      warnValue: 70,
    });
    const geometry = buildObservedTrendGeometry(
      [point(1), point(2), point(3)],
      thresholds
    );
    expect(geometry.maxValue).toBeGreaterThanOrEqual(90);
    const failLine = geometry.lines.find((line) => line.key === "fail");
    expect(failLine).toBeDefined();
    expect(failLine?.y).toBeGreaterThanOrEqual(geometry.paddingTop);
    expect(failLine?.y).toBeLessThanOrEqual(geometry.baseline);
    // Both bound lines sit above every data point, because the data is far
    // below the warn threshold.
    for (const entry of geometry.points) {
      expect(entry.y).toBeGreaterThan(failLine?.y ?? 0);
    }
  });

  it("anchors counts at zero but lets a plain number float", () => {
    const count = buildObservedTrendGeometry(
      [point(100), point(120)],
      undefined,
      { includeZero: true }
    );
    expect(count.minValue).toBe(0);
    const number = buildObservedTrendGeometry(
      [point(100), point(120)],
      undefined,
      { includeZero: false }
    );
    expect(number.minValue).toBeGreaterThan(0);
    expect(number.minValue).toBeLessThanOrEqual(100);
  });

  it("keeps an all-zero window drawable", () => {
    const geometry = buildObservedTrendGeometry([point(0)], undefined, {
      width: 640,
    });
    expect(geometry.maxValue).toBeGreaterThan(geometry.minValue);
    expect(geometry.points[0]?.y).toBeLessThanOrEqual(geometry.baseline);
    expect(geometry.points[0]?.y).toBeGreaterThanOrEqual(geometry.paddingTop);
  });

  it("draws a warn tint between the warn and fail lines", () => {
    const thresholds = create(QualityThresholdsSchema, {
      comparison: QualityComparison.GREATER_THAN,
      failValue: 5,
      warnValue: 3,
    });
    const geometry = buildObservedTrendGeometry(
      [point(1), point(2)],
      thresholds
    );
    expect(
      geometry.regions
        .map((region) => region.key)
        .sort((a, b) => a.localeCompare(b))
    ).toEqual(["fail", "warn"]);
    const warn = geometry.regions.find((region) => region.key === "warn");
    const fail = geometry.regions.find((region) => region.key === "fail");
    // The fail tint runs from the top of the plot down to the fail line; the warn
    // tint fills the band between the two lines below it.
    expect(fail?.y).toBe(geometry.paddingTop);
    expect(warn?.y).toBeGreaterThan(fail?.y ?? 0);
  });

  it("draws both sides of a range threshold", () => {
    const thresholds = create(QualityThresholdsSchema, {
      comparison: QualityComparison.OUTSIDE_RANGE,
      failValue: 10,
      failValueUpper: 20,
      warnValue: 12,
      warnValueUpper: 18,
    });
    const geometry = buildObservedTrendGeometry(
      [point(5), point(15), point(25)],
      thresholds
    );
    expect(geometry.lines).toHaveLength(4);
    expect(geometry.regions).toHaveLength(4);
    expect(geometry.minValue).toBeLessThanOrEqual(10);
    expect(geometry.maxValue).toBeGreaterThanOrEqual(20);
  });

  it("shades the inside of an inside-range comparison", () => {
    const thresholds = create(QualityThresholdsSchema, {
      comparison: QualityComparison.INSIDE_RANGE,
      failValue: 10,
      failValueUpper: 20,
      warnValue: 12,
      warnValueUpper: 18,
    });
    const geometry = buildObservedTrendGeometry([point(15)], thresholds);
    expect(
      geometry.regions
        .map((region) => region.key)
        .sort((a, b) => a.localeCompare(b))
    ).toEqual(["fail", "warn"]);
    const [fail, warn] = geometry.regions;
    expect(fail?.y).toBeGreaterThan(geometry.paddingTop - 1);
    expect(fail?.height).toBeGreaterThan(1);
    // The warn tint sits inside the failing interval, where the check warns.
    expect(warn?.height).toBeGreaterThan(0);
    expect(warn?.y).toBeGreaterThanOrEqual(fail?.y ?? 0);
  });

  it("draws a less-than comparison below its bounds", () => {
    const thresholds = create(QualityThresholdsSchema, {
      comparison: QualityComparison.LESS_THAN,
      failValue: 10,
      warnValue: 15,
    });
    const geometry = buildObservedTrendGeometry(
      [point(20), point(30)],
      thresholds
    );
    // Fail below 10, warn between 10 and 15: both regions sit under the data.
    expect(
      geometry.regions
        .map((region) => region.key)
        .sort((a, b) => a.localeCompare(b))
    ).toEqual(["fail", "warn"]);
    for (const region of geometry.regions) {
      expect(region.y).toBeGreaterThan(geometry.paddingTop);
    }
  });

  it("drops a region too thin to see", () => {
    const thresholds = create(QualityThresholdsSchema, {
      comparison: QualityComparison.GREATER_THAN,
      failValue: 5,
      // A hundredth of a unit between the warn and fail lines is under a pixel of
      // plot: a tint that thin would be a smudge, so it is not drawn.
      warnValue: 4.99,
    });
    const geometry = buildObservedTrendGeometry([point(1)], thresholds, {
      height: 224,
    });
    expect(geometry.regions.map((region) => region.key)).toEqual(["fail"]);
  });

  it("draws no band without a snapshot", () => {
    const geometry = buildObservedTrendGeometry([point(4)], undefined);
    expect(geometry.lines).toEqual([]);
    expect(geometry.regions).toEqual([]);
    expect(geometry.ticks.length).toBeGreaterThan(1);
    expect(geometry.axisIndices).toEqual([0]);
  });
});
