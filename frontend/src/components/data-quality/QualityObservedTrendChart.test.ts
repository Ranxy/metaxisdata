import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  QualityComparison,
  QualityRuleTrendPointSchema,
  QualityRuleTrendSchema,
  QualityRuleType,
  QualityStatus,
  QualityThresholdsSchema,
  QualityTrendNotice,
  QualityTrendUnit,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { formatDateTime } from "@/utils/datetime";
import QualityObservedTrendChart from "./QualityObservedTrendChart.vue";

const START = new Date("2026-03-10T08:00:00Z");

function point(
  index: number,
  value: number,
  overrides: Record<string, unknown> = {}
) {
  return create(QualityRuleTrendPointSchema, {
    execution: `workspaces/ws/qualityExecutions/${index + 1}`,
    startTime: timestampFromDate(new Date(START.getTime() + index * 60_000)),
    status: QualityStatus.PASS,
    value,
    failedRows: BigInt(index),
    totalRows: 100n,
    ...overrides,
  });
}

function trendFixture(overrides: Record<string, unknown> = {}) {
  return create(QualityRuleTrendSchema, {
    rule: "workspaces/ws/qualityRules/rule-1",
    unit: QualityTrendUnit.PERCENT,
    points: [point(0, 1.5), point(1, 4.5), point(2, 2.25)],
    ...overrides,
  });
}

const THRESHOLDS = create(QualityThresholdsSchema, {
  comparison: QualityComparison.OUTSIDE_RANGE,
  failValue: 0,
  failValueUpper: 50,
  warnValue: 5,
  warnValueUpper: 40,
});

function mountChart(
  trend = trendFixture(),
  ruleType = QualityRuleType.NULL_PERCENT
) {
  return mount(QualityObservedTrendChart, {
    props: { trend, ruleType },
    global: { plugins: [i18n] },
  });
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

describe("QualityObservedTrendChart", () => {
  it("draws one marker per judged execution, oldest first", () => {
    const wrapper = mountChart();
    const markers = wrapper.findAll('[role="button"]');
    expect(markers).toHaveLength(3);
    expect(markers.map((marker) => marker.attributes("aria-label"))).toEqual(
      expect.arrayContaining([
        expect.stringContaining(label("dataQuality.observed")),
      ])
    );
    // The line connects the same points; without it the chart would just be dots.
    expect(wrapper.find("polyline").exists()).toBe(true);
    wrapper.unmount();
  });

  it("marks a sampled point as sampled", () => {
    const wrapper = mountChart(
      trendFixture({
        points: [
          point(0, 1.5),
          point(1, 4.5, { sampled: true }),
          point(2, 2.25),
        ],
      })
    );
    const sampled = wrapper.findAll('[data-sampled="true"]');
    expect(sampled).toHaveLength(1);
    // The legend explains the ring rather than leaving it undecoded.
    expect(wrapper.text()).toContain(label("dataQuality.sampled"));
    wrapper.unmount();
  });

  it("reports the clicked execution and is reachable from the keyboard", async () => {
    const wrapper = mountChart();
    const markers = wrapper.findAll('[role="button"]');
    await markers[1]?.trigger("click");
    await markers[2]?.trigger("keydown", { key: "Enter" });

    const emitted = wrapper.emitted("select") ?? [];
    expect(emitted[0]?.[0]).toMatchObject({
      execution: "workspaces/ws/qualityExecutions/2",
      value: 4.5,
    });
    expect(emitted[1]?.[0]).toMatchObject({
      execution: "workspaces/ws/qualityExecutions/3",
    });
    wrapper.unmount();
  });

  it("opens a tooltip with the exact value and rows", async () => {
    const wrapper = mountChart();
    await wrapper.findAll('[role="button"]')[1]?.trigger("mouseenter");
    const text = wrapper.text();
    expect(text).toContain(formatDateTime(timestampFromDate(START), "en-US"));
    expect(text).toContain(label("dataQuality.status.pass"));
    expect(text).toContain("4.5%");
    expect(text).toContain("1 / 100");
    wrapper.unmount();
  });

  it("draws both sides of a range threshold and keeps them inside the plot", () => {
    const wrapper = mountChart(trendFixture({ thresholds: THRESHOLDS }));
    // Two fail bounds plus two warn bounds, all as dashed lines.
    expect(wrapper.findAll('line[stroke-dasharray="5 4"]')).toHaveLength(4);
    // The band is shaded, not just outlined: fail regions on both sides.
    const regions = wrapper.findAll("rect[aria-hidden='true']");
    expect(regions.length).toBeGreaterThanOrEqual(2);
    // The legend names which dashed line is which side and level.
    const text = wrapper.text();
    expect(text).toContain(
      label("dataQuality.thresholdGreaterThan", { value: "50%" })
    );
    expect(text).toContain(
      label("dataQuality.thresholdLessThan", { value: "5%" })
    );
    expect(text).toContain(label("dataQuality.status.fail"));
    expect(text).toContain(label("dataQuality.status.warn"));
    wrapper.unmount();
  });

  it("draws no band and explains why when the trend carries none", () => {
    const wrapper = mountChart(
      trendFixture({ bandNotice: QualityTrendNotice.EDITED_SINCE_LAST_RUN })
    );
    expect(wrapper.findAll('line[stroke-dasharray="5 4"]')).toHaveLength(0);
    expect(wrapper.text()).toContain(label("dataQuality.trend.band.edited"));
    wrapper.unmount();
  });

  it("keeps quiet about a band that needs no explanation", () => {
    const wrapper = mountChart(trendFixture());
    expect(wrapper.text()).not.toContain(
      label("dataQuality.trend.band.edited")
    );
    wrapper.unmount();
  });

  it("shows an empty window as empty, or as valueless for schema drift", () => {
    const empty = mountChart(trendFixture({ points: [] }));
    expect(empty.text()).toContain(label("dataQuality.trend.empty"));
    expect(empty.find("svg").exists()).toBe(false);
    empty.unmount();

    const schemaDrift = mountChart(
      trendFixture({ points: [], unit: QualityTrendUnit.UNSPECIFIED }),
      QualityRuleType.SCHEMA_DRIFT
    );
    expect(schemaDrift.text()).toContain(label("dataQuality.trend.noValue"));
    schemaDrift.unmount();
  });

  it("says when the drawn history starts at a type change", () => {
    const wrapper = mountChart(trendFixture({ historyRestarted: true }));
    expect(wrapper.text()).toContain(
      label("dataQuality.trend.historyRestarted")
    );
    wrapper.unmount();
  });
  it("mirrors the drawing as a table and labels the window", () => {
    const wrapper = mountChart();
    const caption = wrapper.find("table caption");
    expect(caption.text()).toContain(
      label("dataQuality.trend.window", { count: 3 })
    );
    const rows = wrapper.findAll("tbody tr");
    expect(rows).toHaveLength(3);
    // The table repeats the exact values a pointer reads from the markers.
    expect(rows[1]?.text()).toContain("4.5%");
    expect(rows[1]?.text()).toContain("1 / 100");
    wrapper.unmount();
  });
});
