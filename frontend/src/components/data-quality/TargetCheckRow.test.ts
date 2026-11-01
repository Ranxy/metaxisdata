import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  QualityFrequencyTier,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import TargetCheckRow from "./TargetCheckRow.vue";

function ruleFixture(frequencyTier: QualityFrequencyTier) {
  return create(QualityRuleSchema, {
    name: "workspaces/ws/qualityRules/rule-1",
    title: "Orders have rows",
    ruleType: QualityRuleType.ROW_COUNT,
    state: QualityRuleState.ENABLED,
    severity: QualitySeverity.ERROR,
    frequencyTier,
    target: create(QualityTargetSchema, {
      metaGuid: "inst1;db1;public;orders",
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "inst1",
        database: "db1",
        schema: "public",
        object: "orders",
      }),
    }),
  });
}

function mountRow(frequencyTier: QualityFrequencyTier) {
  return mount(TargetCheckRow, {
    props: {
      rule: ruleFixture(frequencyTier),
      issueCount: 0,
      canRun: false,
      canUpdate: false,
    },
    global: { plugins: [i18n] },
  });
}

function label(key: string): string {
  return i18n.global.t(key);
}

describe("TargetCheckRow", () => {
  beforeEach(() => {
    i18n.global.locale.value = "en-US";
  });

  it("leaves a default row without a cadence badge", () => {
    const wrapper = mountRow(QualityFrequencyTier.UNSPECIFIED);

    expect(wrapper.text()).not.toContain(
      label("dataQuality.frequencyTier.hourly")
    );
    expect(wrapper.text()).not.toContain(
      label("dataQuality.frequencyTierLabel")
    );
    wrapper.unmount();
  });

  it("badges a check that runs less often than its target", () => {
    const wrapper = mountRow(QualityFrequencyTier.DAILY);

    expect(wrapper.text()).toContain(label("dataQuality.frequencyTier.daily"));
    // The badge explains that the target's tick stays the floor.
    expect(
      wrapper
        .find(`[title="${label("dataQuality.frequencyTierHint")}"]`)
        .exists()
    ).toBe(true);
    wrapper.unmount();
  });
});
