import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  QualityStatusCountsSchema,
  QualityTrendPointSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  buildTrendGeometry,
  clampTrendDays,
  DEFAULT_TREND_WINDOW,
  failurePercent,
  formatPercent,
  hasTrendData,
  issueSeverityParts,
  MAX_TREND_DAYS,
  niceScale,
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
