import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import ColumnSelect from "@/components/common/ColumnSelect.vue";
import FormField from "@/components/ui/form-field/FormField.vue";
import { i18n } from "@/locales";
import { QualityRuleType } from "@/types/proto-es/v1/data_quality_service_pb";
import { emptyConfig, type RuleConfigForm } from "@/utils/dataQualityForm";
import SegmentCheckConfig from "./SegmentCheckConfig.vue";

const COLUMNS = [
  { name: "region", type: "text", nullable: false },
  { name: "channel", type: "text", nullable: true },
];

function mountConfig(config: Partial<RuleConfigForm> = {}) {
  const model: RuleConfigForm = {
    ...emptyConfig(),
    segmentColumns: "region, channel",
    ...config,
  };
  const wrapper = mount(SegmentCheckConfig, {
    props: {
      ruleType: QualityRuleType.SEGMENT_CHECK,
      modelValue: model,
      columnDetails: COLUMNS,
      fieldErrors: {},
    },
    global: { plugins: [i18n] },
  });
  return { wrapper, model };
}

describe("SegmentCheckConfig", () => {
  it("renders the grouping columns and the two baseline numbers", () => {
    const { wrapper, model } = mountConfig();
    expect(model.segmentBaselineRuns).toBe("3");
    expect(model.segmentMinOccurrences).toBe("2");

    const picker = wrapper.findComponent(ColumnSelect);
    expect(picker.props("multiple")).toBe(true);
    expect(picker.props("modelValue")).toEqual(["region", "channel"]);
    expect(wrapper.find("#dq-field-segmentColumns").exists()).toBe(true);
    expect(wrapper.find("#dq-field-segmentBaselineRuns").exists()).toBe(true);
    expect(wrapper.find("#dq-field-segmentMinOccurrences").exists()).toBe(true);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.segmentBaselineRunsHint")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.editor.segmentMinOccurrencesHint")
    );
    expect(
      wrapper.findAllComponents(FormField).map((field) => field.props("label"))
    ).toEqual([
      i18n.global.t("dataQuality.editor.segmentBaselineRuns"),
      i18n.global.t("dataQuality.editor.segmentMinOccurrences"),
    ]);
    wrapper.unmount();
  });

  it("writes the picked grouping columns back as a CSV field", async () => {
    const { wrapper, model } = mountConfig({ segmentColumns: "" });
    wrapper
      .findComponent(ColumnSelect)
      .vm.$emit("update:modelValue", ["region", "channel"]);
    await wrapper.vm.$nextTick();
    expect(model.segmentColumns).toBe("region, channel");
    wrapper.unmount();
  });

  it("shows the inline error for a rejected baseline window", async () => {
    const { wrapper } = mountConfig();
    await wrapper.setProps({
      fieldErrors: {
        segmentBaselineRuns:
          "Baseline runs must be a whole number between 1 and 10; blank uses 3.",
      },
    });
    expect(wrapper.text()).toContain(
      "Baseline runs must be a whole number between 1 and 10; blank uses 3."
    );
    wrapper.unmount();
  });
});
