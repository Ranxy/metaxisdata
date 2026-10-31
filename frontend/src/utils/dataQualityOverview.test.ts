import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  QualityCoverageBreakdownSchema,
  QualityDimension,
  QualityDimensionScoreSchema,
  QualityOwnerScoreSchema,
  QualityStatusCountsSchema,
  QualityTrendPointSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  buildTrendGeometry,
  clampTrendDays,
  coveragePercent,
  DEFAULT_TREND_WINDOW,
  failurePercent,
  formatAxisCount,
  formatPassRate,
  formatPercent,
  formatPercentValue,
  formatQualityScore,
  formatTrendAxisDate,
  hasTrendData,
  issueSeverityParts,
  MAX_TREND_DAYS,
  niceScale,
  normalizeCoverageBreakdown,
  normalizeDimensionScores,
  normalizeOwnerScores,
  normalizeTrend,
  openIssueCount,
  TREND_WINDOWS,
  type TrendDay,
  trendDateLabels,
  trendFailurePercent,
  utcDateString,
} from "./dataQualityOverview";

const NOW = new Date("2026-03-10T12:00:00Z");

function day(overrides: Partial<TrendDay> = {}): TrendDay {
  return {
    date: "2026-03-10",
    pass: 0,
    warn: 0,
    fail: 0,
    error: 0,
    skipped: 0,
    notEvaluated: 0,
    total: 0,
    ...overrides,
  };
}

describe("trend window", () => {
  it("clamps the window to the service's 1-30 day range", () => {
    expect(DEFAULT_TREND_WINDOW).toBe(7);
    expect(TREND_WINDOWS).toEqual([7, 30]);
    expect(clampTrendDays(7)).toBe(7);
    expect(clampTrendDays(30)).toBe(30);
    expect(clampTrendDays(45)).toBe(MAX_TREND_DAYS);
    expect(clampTrendDays(0)).toBe(DEFAULT_TREND_WINDOW);
    expect(clampTrendDays(-3)).toBe(DEFAULT_TREND_WINDOW);
    expect(clampTrendDays(Number.NaN)).toBe(DEFAULT_TREND_WINDOW);
    expect(clampTrendDays(7.4)).toBe(7);
  });

  it("formats a UTC date", () => {
    expect(utcDateString(new Date("2026-03-10T23:59:59Z"))).toBe("2026-03-10");
  });

  it("fills the window with zero days and keeps the server's own points", () => {
    const days = normalizeTrend(
      [
        create(QualityTrendPointSchema, {
          date: "2026-03-08",
          pass: 3,
          fail: 1,
          total: 4,
        }),
        // A point whose total the server did not fill: the statuses add up.
        create(QualityTrendPointSchema, {
          date: "2026-03-10",
          pass: 1,
          warn: 1,
          fail: 2,
          error: 1,
          skipped: 1,
          notEvaluated: 1,
        }),
      ],
      5,
      NOW
    );
    expect(days.map((item) => item.date)).toEqual([
      "2026-03-06",
      "2026-03-07",
      "2026-03-08",
      "2026-03-09",
      "2026-03-10",
    ]);
    expect(days[2]).toMatchObject({ pass: 3, fail: 1, total: 4 });
    expect(days[4]).toMatchObject({
      pass: 1,
      warn: 1,
      fail: 2,
      error: 1,
      skipped: 1,
      notEvaluated: 1,
      total: 7,
    });
    expect(days[0]).toMatchObject({ total: 0, pass: 0 });
    expect(days).toHaveLength(5);
  });

  it("reports whether any day holds a result", () => {
    expect(hasTrendData([])).toBe(false);
    expect(hasTrendData([day(), day({ total: 1, pass: 1 })])).toBe(true);
  });
});

describe("axis scale", () => {
  it("rounds up to a readable step", () => {
    expect(niceScale(0)).toEqual({ max: 1, step: 1, ticks: [0, 1] });
    expect(niceScale(-4)).toEqual({ max: 1, step: 1, ticks: [0, 1] });
    expect(niceScale(8)).toEqual({
      max: 8,
      step: 2,
      ticks: [0, 2, 4, 6, 8],
    });
    expect(niceScale(37)).toEqual({
      max: 40,
      step: 10,
      ticks: [0, 10, 20, 30, 40],
    });
    expect(niceScale(1).ticks[0]).toBe(0);
  });
});

describe("stacked-bar geometry", () => {
  const options = {
    width: 100,
    height: 100,
    paddingTop: 10,
    paddingRight: 10,
    paddingBottom: 10,
    paddingLeft: 10,
  };

  it("places bars in the plot area and stacks the statuses from the baseline", () => {
    const geometry = buildTrendGeometry(
      [
        day({ date: "2026-03-09", pass: 2, warn: 1, fail: 1, total: 4 }),
        day({ date: "2026-03-10", error: 2, total: 2 }),
      ],
      options
    );
    expect(geometry.plotWidth).toBe(80);
    expect(geometry.plotHeight).toBe(80);
    expect(geometry.baseline).toBe(90);
    expect(geometry.maxValue).toBeGreaterThanOrEqual(4);
    expect(geometry.bars).toHaveLength(2);
    const first = geometry.bars[0];
    expect(first?.date).toBe("2026-03-09");
    expect(first?.segments.map((segment) => segment.key)).toEqual([
      "pass",
      "warn",
      "fail",
    ]);
    // pass is drawn at the bottom (larger y), fail on top of the stack.
    expect(first?.segments[0]?.y).toBeGreaterThan(first?.segments[2]?.y ?? 0);
    expect(first?.segments[2]?.y).toBeLessThan(geometry.baseline);
    expect(first?.width).toBeGreaterThan(0);
    expect(geometry.bars[1]?.segments.map((segment) => segment.key)).toEqual([
      "error",
    ]);
  });

  it("keeps a flat axis for an empty window and draws no segments", () => {
    const geometry = buildTrendGeometry([day(), day()], options);
    expect(geometry.maxValue).toBe(1);
    expect(geometry.bars.every((bar) => bar.segments.length === 0)).toBe(true);
    expect(geometry.ticks).toEqual([
      { value: 0, y: 90 },
      { value: 1, y: 10 },
    ]);
  });

  it("caps a bar's width on a wide chart and centres it in its slot", () => {
    const days = Array.from({ length: 7 }, (_, index) =>
      day({ date: `2026-03-0${index + 1}`, pass: 1, total: 1 })
    );
    const geometry = buildTrendGeometry(days, { width: 1000 });
    const bar = geometry.bars[0];
    expect(bar?.slotX).toBe(44);
    expect(bar?.slotWidth).toBeCloseTo(134.857, 2);
    expect(bar?.width).toBe(72);
    expect(bar?.x).toBeCloseTo(44 + (134.857 - 72) / 2, 2);
  });

  it("marks the top of each stack, and the baseline for an empty day", () => {
    const geometry = buildTrendGeometry(
      [
        day({ date: "2026-03-09", pass: 2, warn: 1, total: 3 }),
        day({ date: "2026-03-10" }),
      ],
      options
    );
    const filled = geometry.bars[0];
    const topSegment = filled?.segments[filled.segments.length - 1];
    expect(filled?.top).toBe(topSegment?.y);
    expect(filled?.top).toBeLessThan(geometry.baseline);
    expect(geometry.bars[1]?.top).toBe(geometry.baseline);
  });

  it("aims the axis at the requested tick count", () => {
    const geometry = buildTrendGeometry(
      [day({ date: "2026-03-09", pass: 22, total: 22 })],
      { ...options, tickCount: 5 }
    );
    expect(geometry.maxValue).toBe(25);
    expect(geometry.ticks.map((tick) => tick.value)).toEqual([
      0, 5, 10, 15, 20, 25,
    ]);
  });

  it("names the first, middle and last day", () => {
    expect(trendDateLabels([])).toEqual([]);
    expect(trendDateLabels(["2026-03-09"])).toEqual(["2026-03-09"]);
    expect(trendDateLabels(["2026-03-09", "2026-03-10"])).toEqual([
      "2026-03-09",
      "2026-03-10",
    ]);
    expect(trendDateLabels(["a", "b", "c", "d", "e", "f", "g"])).toEqual([
      "a",
      "d",
      "g",
    ]);
  });

  it("shows more labels when the axis asks for room", () => {
    const week = ["a", "b", "c", "d", "e", "f", "g"];
    expect(trendDateLabels(week, 7)).toEqual(week);
    const month = Array.from({ length: 30 }, (_, index) => `d${index}`);
    const labels = trendDateLabels(month, 7);
    expect(labels).toHaveLength(7);
    expect(labels[0]).toBe("d0");
    expect(labels[6]).toBe("d29");
    expect(new Set(labels).size).toBe(labels.length);
    // Asking for fewer slots than two still keeps both ends.
    expect(trendDateLabels(week, 1)).toEqual(["a", "g"]);
  });

  it("formats an axis bucket without a timezone shift", () => {
    expect(formatTrendAxisDate("2026-03-10", "en-US")).toBe("3/10");
    expect(formatTrendAxisDate("2026-03-10", "zh-CN")).toBe("3/10");
    expect(formatTrendAxisDate("not-a-date", "en-US")).toBe("not-a-date");
  });

  it("compacts large axis counts", () => {
    expect(formatAxisCount(12, "en-US")).toBe("12");
    expect(formatAxisCount(1200, "en-US")).toBe("1.2K");
    expect(formatAxisCount(0, "en-US")).toBe("0");
  });
});

describe("summary numbers", () => {
  it("sums the three open severities and lists the non-zero ones worst first", () => {
    expect(
      openIssueCount({
        openWarningCount: 2,
        openErrorCount: 1,
        openCriticalCount: 3,
      })
    ).toBe(6);
    expect(
      issueSeverityParts({
        openWarningCount: 0,
        openErrorCount: 1,
        openCriticalCount: 3,
      })
    ).toEqual([
      { severity: "critical", count: 3 },
      { severity: "error", count: 1 },
    ]);
    expect(
      issueSeverityParts({
        openWarningCount: 0,
        openErrorCount: 0,
        openCriticalCount: 0,
      })
    ).toEqual([]);
  });

  it("formats a ratio without pretending missing data is zero", () => {
    expect(formatPercent(0.042)).toBe("4.2%");
    expect(formatPercent(0.042, 0)).toBe("4%");
    expect(formatPercent(Number.NaN)).toBe("-");
    expect(failurePercent(undefined)).toBe("-");
    expect(
      failurePercent(create(QualityStatusCountsSchema, { total: 0, fail: 1 }))
    ).toBe("-");
    expect(
      failurePercent(
        create(QualityStatusCountsSchema, { total: 10, fail: 1, error: 1 })
      )
    ).toBe("20.0%");
  });

  it("computes the window's failure share", () => {
    expect(trendFailurePercent([day(), day({ total: 0 })])).toBe("-");
    expect(
      trendFailurePercent([
        day({ total: 8, pass: 6, fail: 2 }),
        day({ total: 2, error: 1, pass: 1 }),
      ])
    ).toBe("30.0%");
  });
});

describe("coverage and score", () => {
  it("turns a covered share into a percentage, and keeps an empty denominator null", () => {
    expect(coveragePercent(4, 8)).toBe(50);
    expect(coveragePercent(1, 3)).toBe(33.3);
    expect(coveragePercent(0, 5)).toBe(0);
    expect(coveragePercent(2, 0)).toBeNull();
    expect(coveragePercent(2, -1)).toBeNull();
    expect(coveragePercent(Number.NaN, 3)).toBeNull();
    expect(coveragePercent(1, Number.POSITIVE_INFINITY)).toBeNull();
    expect(formatPercentValue(coveragePercent(2, 3))).toBe("66.7%");
    expect(formatPercentValue(null)).toBe("-");
  });

  it("formats a quality score and a pass rate, and says '-' while nothing was judged", () => {
    expect(formatQualityScore(82.54)).toBe("82.5");
    expect(formatQualityScore(100)).toBe("100.0");
    expect(formatQualityScore(undefined)).toBe("-");
    expect(formatQualityScore(Number.NaN)).toBe("-");
    expect(formatPassRate(0.875)).toBe("87.5%");
    expect(formatPassRate(0)).toBe("0.0%");
    expect(formatPassRate(undefined)).toBe("-");
    expect(formatPassRate(Number.NaN)).toBe("-");
  });
});

describe("breakdown normalization", () => {
  it("orders coverage rows by title, falls back to the key and keeps an empty denominator null", () => {
    const rows = normalizeCoverageBreakdown([
      create(QualityCoverageBreakdownSchema, {
        key: "inst-2",
        title: "Beta",
        totalTableCount: 4,
        monitoredTableCount: 1,
        totalColumnCount: 8,
        coveredColumnCount: 2,
      }),
      create(QualityCoverageBreakdownSchema, {
        key: "inst-1",
        title: "Alpha",
        totalTableCount: 0,
        monitoredTableCount: 0,
        totalColumnCount: 0,
        coveredColumnCount: 0,
      }),
      create(QualityCoverageBreakdownSchema, {
        key: "inst-3",
        title: "",
        totalTableCount: 1,
        monitoredTableCount: 1,
        totalColumnCount: 3,
        coveredColumnCount: 0,
      }),
      // Two rows with the same title compare as equal and keep their order.
      create(QualityCoverageBreakdownSchema, {
        key: "inst-4",
        title: "Beta",
        totalTableCount: 2,
        monitoredTableCount: 0,
        totalColumnCount: 4,
        coveredColumnCount: 0,
      }),
    ]);

    expect(rows.map((row) => row.key)).toEqual([
      "inst-1",
      "inst-2",
      "inst-4",
      "inst-3",
    ]);
    expect(rows[0]?.tableCoveragePercent).toBeNull();
    expect(rows[0]?.columnCoveragePercent).toBeNull();
    expect(rows[1]?.tableCoveragePercent).toBe(25);
    expect(rows[1]?.columnCoveragePercent).toBe(25);
    expect(rows[2]?.title).toBe("Beta");
    expect(rows[2]?.tableCoveragePercent).toBe(0);
    expect(rows[3]?.title).toBe("");
    expect(rows[3]?.tableCoveragePercent).toBe(100);
    expect(rows[3]?.columnCoveragePercent).toBe(0);
  });

  it("orders dimensions by their enum value and keeps an unset score unset", () => {
    const rows = normalizeDimensionScores([
      create(QualityDimensionScoreSchema, {
        dimension: QualityDimension.ACCURACY,
        ruleCount: 3,
        scoredRuleCount: 2,
        failingRuleCount: 1,
        qualityScore: 70,
      }),
      create(QualityDimensionScoreSchema, {
        dimension: QualityDimension.COMPLETENESS,
        ruleCount: 1,
      }),
      create(QualityDimensionScoreSchema, {
        dimension: QualityDimension.UNSPECIFIED,
        ruleCount: 5,
      }),
    ]);

    expect(rows.map((row) => row.dimension)).toEqual([
      QualityDimension.UNSPECIFIED,
      QualityDimension.COMPLETENESS,
      QualityDimension.ACCURACY,
    ]);
    expect(rows[0]?.key).toBe(String(QualityDimension.UNSPECIFIED));
    expect(rows[1]?.qualityScore).toBeUndefined();
    expect(rows[2]?.qualityScore).toBe(70);
  });

  it("orders owners alphabetically with the unassigned bucket last", () => {
    const rows = normalizeOwnerScores([
      create(QualityOwnerScoreSchema, {
        owner: "",
        ruleCount: 2,
        scoredRuleCount: 1,
      }),
      create(QualityOwnerScoreSchema, {
        owner: "users/zoe",
        ruleCount: 1,
        scoredRuleCount: 1,
        qualityScore: 90,
      }),
      create(QualityOwnerScoreSchema, {
        owner: "users/alice",
        ruleCount: 1,
        scoredRuleCount: 0,
      }),
      create(QualityOwnerScoreSchema, {
        owner: "users/alice",
        ruleCount: 1,
        scoredRuleCount: 1,
        qualityScore: 80,
      }),
    ]);

    expect(rows.map((row) => row.key)).toEqual([
      "users/alice",
      "users/alice",
      "users/zoe",
      "",
    ]);
    expect(rows[0]?.qualityScore).toBeUndefined();
    expect(rows[3]?.qualityScore).toBeUndefined();
    expect(rows[3]?.failingRuleCount).toBe(0);
  });
});
