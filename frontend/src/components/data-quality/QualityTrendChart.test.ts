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
  merged.total ||=
    merged.pass +
    merged.warn +
    merged.fail +
    merged.error +
    merged.skipped +
    merged.notEvaluated;
  return merged;
}

/** The UTC bucket `daysBack` days before today, as the service reports it. */
function utcDaysBack(daysBack: number): string {
  const now = new Date();
  const end = Date.UTC(
    now.getUTCFullYear(),
    now.getUTCMonth(),
    now.getUTCDate()
  );
  return new Date(end - daysBack * 86_400_000).toISOString().slice(0, 10);
}

/** The reader's local day, which is what makes the last bucket "today". */
function localToday(): string {
  const now = new Date();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const dayOfMonth = String(now.getDate()).padStart(2, "0");
  return `${now.getFullYear()}-${month}-${dayOfMonth}`;
}

const WEEK: TrendDay[] = [
  day(utcDaysBack(6)),
  day(utcDaysBack(5), { warn: 3, fail: 4 }),
  day(utcDaysBack(4), { pass: 10, warn: 11, error: 1 }),
  day(utcDaysBack(3), { pass: 5, warn: 5 }),
  day(utcDaysBack(2)),
  day(utcDaysBack(1), { pass: 4, warn: 4 }),
  day(utcDaysBack(0), { pass: 1, warn: 1 }),
];

function mountChart(days: TrendDay[] = WEEK) {
  return mount(QualityTrendChart, {
    props: { days },
    global: { plugins: [i18n] },
  });
}

type Chart = ReturnType<typeof mountChart>;

/** The per-day hover surfaces, in day order. */
function hitSurfaces(wrapper: Chart) {
  return wrapper
    .findAll("svg g")
    .filter((group) => group.find('rect[pointer-events="all"]').exists())
    .map((group) => group.find('rect[pointer-events="all"]'));
}

function tooltip(wrapper: Chart) {
  return wrapper
    .findAll("div")
    .find((element) => element.classes().includes("absolute"));
}

function styleValue(wrapper: Chart, property: string): number {
  const style = (tooltip(wrapper)?.element as HTMLElement | undefined)?.style;
  const value = style?.getPropertyValue(property) ?? "0";
  return Number.parseFloat(value);
}

describe("QualityTrendChart", () => {
  it("gives every day a hover surface above the bars", async () => {
    const wrapper = mountChart();
    const groups = wrapper
      .findAll("svg g")
      .filter((group) => group.find('rect[pointer-events="all"]').exists());
    expect(groups).toHaveLength(7);
    // The surface must be the last thing in the day's group, or a painted bar
    // segment swallows the pointer and nothing is ever hovered.
    for (const group of groups) {
      const rects = group.findAll("rect");
      expect(rects[rects.length - 1].attributes("pointer-events")).toBe("all");
    }
    await hitSurfaces(wrapper)[2].trigger("mouseenter");
    expect(tooltip(wrapper)).toBeDefined();
  });

  it("marks the slots of the days it could not draw", () => {
    const stubs = mountChart()
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
    await hitSurfaces(wrapper)[2].trigger("mouseenter");
    const tip = tooltip(wrapper);
    expect(tip?.text()).toContain(formatDate(WEEK[2].date, "en-US"));
    expect(tip?.text()).toContain("22 executions");
    expect(tip?.text()).toContain("Pass");
    expect(tip?.text()).toContain("11");
    // A status the day did not see has no row.
    expect(tip?.text()).not.toContain("Fail");
  });

  it("says a day with no executions has none", async () => {
    const wrapper = mountChart();
    await hitSurfaces(wrapper)[0].trigger("mouseenter");
    expect(tooltip(wrapper)?.text()).toContain("no executions");
  });

  it("counts verdict-less executions instead of calling the day empty", async () => {
    const days = [day(utcDaysBack(1), { skipped: 2, notEvaluated: 1 })];
    const wrapper = mountChart(days);
    await hitSurfaces(wrapper)[0].trigger("mouseenter");
    const tip = tooltip(wrapper)?.text() ?? "";
    expect(tip).toContain("3 executions");
    expect(tip).toContain("Skipped");
    expect(tip).toContain("Not evaluated");
    expect(tip).not.toContain("no executions");
    // The legend's hidden statuses appear only when the window has one, and
    // then its sum equals the window total.
    const legend = wrapper.find("div.flex-wrap").text().replace(/\s+/g, " ");
    expect(legend).toContain("Skipped 2");
    expect(legend).toContain("Not evaluated 1");
  });

  it("keeps the tooltip beside a bar it cannot sit above", async () => {
    const wrapper = mountChart();
    await hitSurfaces(wrapper)[2].trigger("mouseenter");
    const left = styleValue(wrapper, "left");
    const top = styleValue(wrapper, "top");
    expect(left).toBeGreaterThan(0);
    expect(left + 176).toBeLessThanOrEqual(720 + 1);
    expect(top).toBeGreaterThanOrEqual(0);
    // The tall bar has no room above it, so the panel is not flipped up.
    expect(styleValue(wrapper, "transform") || 0).toBe(0);
  });

  it("clamps the tooltip inside the drawing at both ends", async () => {
    const wrapper = mountChart();
    const surface = hitSurfaces(wrapper);
    await surface[0].trigger("mouseenter");
    expect(styleValue(wrapper, "left")).toBeGreaterThanOrEqual(96);
    await surface[6].trigger("mouseenter");
    expect(styleValue(wrapper, "left")).toBeLessThanOrEqual(720 - 96 + 1);
  });

  it("labels the axis with short dates and marks the last bucket as today only on the local day", () => {
    const texts = mountChart()
      .findAll("svg text")
      .map((text) => text.text());
    expect(texts).toContain(shortUtcDate(WEEK[0].date));
    expect(texts.includes("Today")).toBe(WEEK[6].date === localToday());
  });

  it("clears the hovered day when the window changes", async () => {
    const wrapper = mountChart();
    await hitSurfaces(wrapper)[2].trigger("mouseenter");
    expect(tooltip(wrapper)).toBeDefined();
    await wrapper.setProps({ days: WEEK.slice(3) });
    expect(tooltip(wrapper)).toBeUndefined();
  });

  it("keeps clip paths unique when two charts share a document", () => {
    const first = mountChart();
    const second = mountChart();
    const ids = [
      ...first.findAll("clipPath"),
      ...second.findAll("clipPath"),
    ].map((clip) => clip.attributes("id"));
    expect(ids).toHaveLength(14);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("mirrors the drawing as a table that adds up", () => {
    const today = utcDaysBack(0);
    const wrapper = mountChart([day(today, { skipped: 2, pass: 1 })]);
    const rows = wrapper.findAll("table tbody tr");
    expect(rows).toHaveLength(1);
    expect(rows[0].text()).toContain(formatDate(today, "en-US"));
    // date + six statuses + total: the columns the legend sums.
    const cells = rows[0].findAll("th, td");
    expect(cells).toHaveLength(8);
    const numbers = cells.slice(1).map((cell) => Number(cell.text()));
    const total = numbers[numbers.length - 1];
    expect(numbers.slice(0, -1).reduce((sum, value) => sum + value, 0)).toBe(
      total
    );
    expect(total).toBe(3);
  });
});

/** The axis label for a UTC bucket, independent of the test's local zone. */
function shortUtcDate(date: string): string {
  return new Intl.DateTimeFormat("en-US", {
    month: "numeric",
    day: "numeric",
    timeZone: "UTC",
  }).format(
    Date.UTC(
      Number(date.slice(0, 4)),
      Number(date.slice(5, 7)) - 1,
      Number(date.slice(8, 10))
    )
  );
}
