import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import {
  QualityComparison,
  QualityRuleSchema,
  QualityRuleTrendSchema,
  QualityRuleType,
  QualityThresholdMetric,
  QualityThresholdSuggestionMethod,
  QualityThresholdSuggestionSchema,
  QualityThresholdsSchema,
  QualityTrendNotice,
  QualityTrendUnit,
  RowCountConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityThresholdSuggestion from "./QualityThresholdSuggestion.vue";

const mocks = vi.hoisted(() => ({
  getQualityRuleTrend: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityRuleTrend: mocks.getQualityRuleTrend,
}));

const RULE = "workspaces/ws/qualityRules/rule-1";

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function ruleFixture(overrides: Record<string, unknown> = {}) {
  return create(QualityRuleSchema, {
    name: RULE,
    title: "Orders have rows",
    ruleType: QualityRuleType.ROW_COUNT,
    config: {
      thresholdMetric: QualityThresholdMetric.COUNT,
      config: {
        case: "rowCount",
        value: create(RowCountConfigSchema, { minRows: 1n }),
      },
    },
    ...overrides,
  });
}

function trendFixture(overrides: Record<string, unknown> = {}) {
  return create(QualityRuleTrendSchema, {
    rule: RULE,
    unit: QualityTrendUnit.COUNT,
    points: [],
    suggestion: create(QualityThresholdSuggestionSchema, {
      sampleSize: 12,
      method: QualityThresholdSuggestionMethod.OBSERVED_RANGE,
      thresholds: create(QualityThresholdsSchema, {
        comparison: QualityComparison.OUTSIDE_RANGE,
        failValue: 10,
        failValueUpper: 100,
        warnValue: 20,
        warnValueUpper: 80,
      }),
    }),
    ...overrides,
  });
}

function hintTrend(notice: QualityTrendNotice) {
  return create(QualityRuleTrendSchema, {
    rule: RULE,
    unit: QualityTrendUnit.COUNT,
    suggestion: create(QualityThresholdSuggestionSchema, { notice }),
  });
}

async function mountPanel(
  options: {
    rule?: ReturnType<typeof ruleFixture> | undefined;
    ruleType?: QualityRuleType;
    thresholdMetric?: QualityThresholdMetric;
    nullMetric?: "count" | "percent";
  } = {}
) {
  const wrapper = mount(QualityThresholdSuggestion, {
    props: {
      rule: "rule" in options ? options.rule : ruleFixture(),
      ruleType: options.ruleType ?? QualityRuleType.ROW_COUNT,
      thresholdMetric: options.thresholdMetric ?? QualityThresholdMetric.COUNT,
      nullMetric: options.nullMetric,
    },
    global: { plugins: [i18n] },
  });
  await flushPromises();
  return wrapper;
}

function applyButton(wrapper: Awaited<ReturnType<typeof mountPanel>>) {
  return wrapper
    .findAll("button")
    .find(
      (button) =>
        button.text().trim() === label("dataQuality.trend.suggestion.apply")
    );
}

describe("QualityThresholdSuggestion", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    i18n.global.locale.value = "en-US";
    mocks.getQualityRuleTrend.mockResolvedValue(trendFixture());
  });

  it("derives the suggestion from the check's own trend", async () => {
    const wrapper = await mountPanel();

    expect(mocks.getQualityRuleTrend).toHaveBeenCalledWith(RULE, {
      limit: 30,
      signal: expect.any(AbortSignal),
    });
    const text = wrapper.text();
    expect(text).toContain(label("dataQuality.trend.suggestion.title"));
    expect(text).toContain(label("dataQuality.trend.suggestion.description"));
    // The panel says how many runs it looked at and how the number was reached.
    expect(text).toContain(
      label("dataQuality.trend.suggestion.sample", { count: 12 })
    );
    expect(text).toContain(
      label("dataQuality.trend.suggestion.method.observedRange")
    );
    // Both sides of the row-count range are offered.
    expect(text).toContain(
      `${label("dataQuality.status.fail")} ${label("dataQuality.thresholdLessThan", { value: "10" })}`
    );
    expect(text).toContain(
      `${label("dataQuality.status.fail")} ${label("dataQuality.thresholdGreaterThan", { value: "100" })}`
    );
    wrapper.unmount();
  });

  it("fills the form on request without saving anything", async () => {
    const wrapper = await mountPanel();
    const button = applyButton(wrapper);
    expect(button).toBeDefined();

    await button?.trigger("click");

    const emitted = wrapper.emitted("apply") ?? [];
    expect(emitted).toHaveLength(1);
    expect(
      (emitted[0]?.[0] as { thresholds?: { failValue?: number } }).thresholds
        ?.failValue
    ).toBe(10);
    wrapper.unmount();
  });

  it("says why a suggestion cannot be derived", async () => {
    mocks.getQualityRuleTrend.mockResolvedValue(
      hintTrend(QualityTrendNotice.INSUFFICIENT_HISTORY)
    );
    const insufficient = await mountPanel();
    expect(insufficient.text()).toContain(
      label("dataQuality.trend.suggestion.insufficient")
    );
    expect(applyButton(insufficient)).toBeUndefined();
    insufficient.unmount();

    mocks.getQualityRuleTrend.mockResolvedValue(
      hintTrend(QualityTrendNotice.UNSUPPORTED_RULE_TYPE)
    );
    const unsupportedType = await mountPanel();
    expect(unsupportedType.text()).toContain(
      label("dataQuality.trend.suggestion.unsupportedType")
    );
    unsupportedType.unmount();

    mocks.getQualityRuleTrend.mockResolvedValue(
      hintTrend(QualityTrendNotice.UNSUPPORTED_CONFIG)
    );
    const unsupportedConfig = await mountPanel();
    expect(unsupportedConfig.text()).toContain(
      label("dataQuality.trend.suggestion.unsupportedConfig")
    );
    unsupportedConfig.unmount();

    mocks.getQualityRuleTrend.mockResolvedValue(
      hintTrend(QualityTrendNotice.UNIT_NOT_COMPARABLE)
    );
    const notComparable = await mountPanel();
    expect(notComparable.text()).toContain(
      label("dataQuality.trend.suggestion.unitNotComparable")
    );
    notComparable.unmount();
  });

  it("disables the suggestion once the form's unit no longer matches the check", async () => {
    const typeChanged = await mountPanel({ ruleType: QualityRuleType.UNIQUE });
    expect(typeChanged.text()).toContain(
      label("dataQuality.trend.suggestion.unitChanged")
    );
    expect(
      (applyButton(typeChanged)?.element as HTMLButtonElement | undefined)
        ?.disabled
    ).toBe(true);
    typeChanged.unmount();

    mocks.getQualityRuleTrend.mockResolvedValue(
      trendFixture({ unit: QualityTrendUnit.PERCENT })
    );
    const metricChanged = await mountPanel({
      thresholdMetric: QualityThresholdMetric.PERCENT,
    });
    expect(metricChanged.text()).toContain(
      label("dataQuality.trend.suggestion.unitChanged")
    );
    expect(
      (applyButton(metricChanged)?.element as HTMLButtonElement | undefined)
        ?.disabled
    ).toBe(true);
    metricChanged.unmount();
  });

  it("disables the suggestion when null_percent's own unit changed", async () => {
    const rule = ruleFixture({
      ruleType: QualityRuleType.NULL_PERCENT,
    });
    const wrapper = await mountPanel({
      rule,
      ruleType: QualityRuleType.NULL_PERCENT,
      nullMetric: "count",
    });
    expect(wrapper.text()).toContain(
      label("dataQuality.trend.suggestion.unitChanged")
    );
    expect(
      (applyButton(wrapper)?.element as HTMLButtonElement | undefined)?.disabled
    ).toBe(true);
    wrapper.unmount();
  });

  it("shows nothing while creating a check", async () => {
    const wrapper = await mountPanel({ rule: undefined });
    expect(mocks.getQualityRuleTrend).not.toHaveBeenCalled();
    expect(wrapper.find(".rounded-md.border").exists()).toBe(false);
    wrapper.unmount();
  });

  it("stays out of the way when the trend cannot be read", async () => {
    mocks.getQualityRuleTrend.mockRejectedValue(new Error("trend down"));
    const wrapper = await mountPanel();
    expect(wrapper.find(".rounded-md.border").exists()).toBe(false);
    wrapper.unmount();
  });
});
