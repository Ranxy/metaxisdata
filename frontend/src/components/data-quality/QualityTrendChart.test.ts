import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import type { TrendDay } from "@/utils/dataQualityOverview";
import { formatDate } from "@/utils/datetime";
import QualityTrendChart from "./QualityTrendChart.vue";

/** One day of the weekly window; `total` follows the drawn statuses. */
function day(date: string, overrides: Partial<TrendDay> = {}): TrendDay {
  const merged: TrendDay = {
    date,
    pass: 0,
    warn: 0,
    fail: 0,
    error: 0,
    skipped: 0,
    notEvaluated: 0,
    total: 0,
    ...overrides,
  };
  merged.total ||= merged.pass + merged.warn + merged.fail + merged.error;
  return merged;
}

const WEEK: TrendDay[] = [
  day("2026-10-19"),
  day("2026-10-20", { warn: 3, fail: 4 }),
  day("2026-10-21", { pass: 10, warn: 11, error: 1 }),
  day("2026-10-22", { pass: 5, warn: 5 }),
  day("2026-10-23"),
  day("2026-10-24", { pass: 4, warn: 4 }),
  day("2026-10-25", { pass: 1, warn: 1 }),
];

function mountChart(days: TrendDay[] = WEEK) {
  return mount(QualityTrendChart, {
    props: { days },
    global: { plugins: [i18n] },
  });
}

function tooltip(wrapper: ReturnType<typeof mountChart>) {
  return wrapper
    .findAll("div")
    .find((element) => element.classes().includes("absolute"));
}

describe("QualityTrendChart", () => {
  it("gives every day a hover surface, including the days with no executions", () => {
    const wrapper = mountChart();
    expect(wrapper.findAll('rect[pointer-events="all"]')).toHaveLength(7);
    // The two zero days still mark their slot at the baseline.
    const stubs = wrapper
      .findAll("rect")
      .filter((rect) => (rect.attributes("style") ?? "").includes("0.3"));
    expect(stubs).toHaveLength(2);
  });

  it("sums each status over the window in the legend", () => {
    const legend = mountChart().find("div.flex-wrap");
    const text = legend.text().replace(/\s+/g, " ");
    expect(text).toContain("Pass 20");
    expect(text).toContain("Warn 24");
    expect(text).toContain("Fail 4");
    expect(text).toContain("Error 1");
  });

  it("opens the day's exact counts on hover", async () => {
    const wrapper = mountChart();
    await wrapper
      .findAll('rect[pointer-events="all"]')[2]
      .trigger("mouseenter");
    const tip = tooltip(wrapper);
    expect(tip).toBeDefined();
    expect(tip?.text()).toContain(formatDate("2026-10-21", "en-US"));
    expect(tip?.text()).toContain("22 executions");
    expect(tip?.text()).toContain("Pass");
    expect(tip?.text()).toContain("11");
    expect(tip?.text()).toContain("Error");
    // A status the day did not see has no row.
    expect(tip?.text()).not.toContain("Fail");
  });

  it("says a day with no executions has none, instead of drawing an empty tooltip", async () => {
    const wrapper = mountChart();
    await wrapper
      .findAll('rect[pointer-events="all"]')[0]
      .trigger("mouseenter");
    expect(tooltip(wrapper)?.text()).toContain("no executions");
  });

  it("labels the axis with short dates and marks the last day as today", () => {
    const texts = mountChart()
      .findAll("svg text")
      .map((text) => text.text());
    expect(texts).toContain("10/19");
    expect(texts).toContain("10/24");
    expect(texts).toContain("Today");
  });

  it("mirrors the drawing as a table for a screen reader", () => {
    const rows = mountChart().findAll("table tbody tr");
    expect(rows).toHaveLength(7);
    expect(rows[2].text()).toContain(formatDate("2026-10-21", "en-US"));
    expect(rows[2].text()).toContain("22");
  });
});
