import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import FormField from "@/components/ui/form-field/FormField.vue";
import { i18n } from "@/locales";
import {
  QualityAggregateFunction,
  QualityRuleType,
  QualityThresholdMetric,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  emptyConfig,
  type RuleConfigForm as RuleConfigState,
  THRESHOLD_METRIC_RULE_TYPES,
} from "@/utils/dataQualityForm";
import RuleConfigForm from "./RuleConfigForm.vue";

function label(key: string): string {
  return i18n.global.t(key);
}

function mountForm(
  ruleType: QualityRuleType,
  config: Partial<RuleConfigState> = {}
) {
  return mount(RuleConfigForm, {
    props: {
      ruleType,
      modelValue: { ...emptyConfig(), ...config },
      thresholdMetric: QualityThresholdMetric.COUNT,
    },
    global: { plugins: [i18n] },
  });
}

const THRESHOLD_LABEL = () => label("dataQuality.editor.thresholdMetric");

const NON_METRIC_TYPES = [
  QualityRuleType.ROW_COUNT,
  QualityRuleType.ROW_COUNT_CHANGE,
  QualityRuleType.NULL_PERCENT,
  QualityRuleType.FRESHNESS,
  QualityRuleType.SCHEMA_DRIFT,
  QualityRuleType.AGGREGATE,
  QualityRuleType.CROSS_TABLE,
  QualityRuleType.CUSTOM_SQL,
];

describe("RuleConfigForm", () => {
  for (const ruleType of THRESHOLD_METRIC_RULE_TYPES) {
    it(`shows the threshold unit select for ${QualityRuleType[ruleType]}`, () => {
      const wrapper = mountForm(ruleType);
      expect(wrapper.text()).toContain(THRESHOLD_LABEL());
      wrapper.unmount();
    });
  }

  // The unit is only meaningful where the rule has a total-rows denominator;
  // the server rejects PERCENT on every other type.
  for (const ruleType of NON_METRIC_TYPES) {
    it(`hides the threshold unit select for ${QualityRuleType[ruleType]}`, () => {
      const wrapper = mountForm(ruleType);
      expect(wrapper.text()).not.toContain(THRESHOLD_LABEL());
      wrapper.unmount();
    });
  }

  it("keeps custom SQL thresholds but hides the unit that has no denominator", () => {
    const wrapper = mountForm(QualityRuleType.CUSTOM_SQL);
    expect(wrapper.find("#dq-config-sql").exists()).toBe(true);
    expect(wrapper.text()).not.toContain(THRESHOLD_LABEL());
    const fields = wrapper
      .findAllComponents(FormField)
      .map((field) => field.props("label"));
    expect(fields).toContain(label("dataQuality.editor.failThreshold"));
    expect(fields).toContain(label("dataQuality.editor.warnThreshold"));
    wrapper.unmount();
  });

  it("renders the cross-table failure and warning thresholds", () => {
    const wrapper = mountForm(QualityRuleType.CROSS_TABLE, {
      rightTable: "public.orders_ref",
      leftAggFunction: QualityAggregateFunction.SUM,
      leftAggColumn: "amount",
    });
    const fields = wrapper
      .findAllComponents(FormField)
      .map((field) => field.props("label"));
    expect(fields).toContain(label("dataQuality.editor.failThreshold"));
    expect(fields).toContain(label("dataQuality.editor.warnThreshold"));
    expect(fields).toContain(label("dataQuality.editor.tolerance"));
    expect(wrapper.find("#dq-field-failThreshold").exists()).toBe(true);
    expect(wrapper.find("#dq-field-warnThreshold").exists()).toBe(true);
    expect(wrapper.text()).not.toContain(THRESHOLD_LABEL());
    wrapper.unmount();
  });
});
