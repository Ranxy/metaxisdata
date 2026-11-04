import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  QualityAssetCriticality,
  QualityContractSchema,
  QualityCoverageProperty,
  QualityCoverageRequirementSchema,
  QualityPlanCoverageSchema,
  QualityPlanSchema,
  QualityPlanState,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import TargetPlanCard from "./TargetPlanCard.vue";

function contractFixture(state: QualityPlanState, gaps: number) {
  return create(QualityContractSchema, {
    target: create(QualityTargetSchema, {
      metaGuid: "inst;shop;public;orders",
      metaType: MetaType.TABLE,
    }),
    plan: create(QualityPlanSchema, {
      state,
      criticality: QualityAssetCriticality.IMPORTANT,
      checkCount: 3,
      gapCount: gaps,
      requirements: [
        create(QualityCoverageRequirementSchema, {
          property: QualityCoverageProperty.VOLUME,
          satisfied: true,
        }),
        create(QualityCoverageRequirementSchema, {
          property: QualityCoverageProperty.SCHEMA_STABILITY,
          satisfied: gaps === 0,
        }),
      ],
    }),
  });
}

function mountCard(contract: ReturnType<typeof contractFixture>) {
  return mount(TargetPlanCard, {
    props: { contract },
    global: { plugins: [i18n] },
  });
}

describe("TargetPlanCard", () => {
  it("renders the plan state, requirements and gaps", () => {
    const wrapper = mountCard(contractFixture(QualityPlanState.PLANNED, 1));
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planStatePlanned")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planPropertyVolume")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planSatisfied")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planGap")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planChecks", {
        checks: 3,
        gaps: 1,
      })
    );
  });

  it("explains an unplanned target instead of inventing coverage", () => {
    const wrapper = mountCard(contractFixture(QualityPlanState.UNPLANNED, 0));
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planUnplannedHint")
    );
  });
  it("labels a declared plan and offers the editor to a writer", () => {
    const contract = create(QualityContractSchema, {
      target: create(QualityTargetSchema, {
        metaGuid: "inst;shop;public;orders",
        metaType: MetaType.TABLE,
      }),
      plan: create(QualityPlanSchema, {
        state: QualityPlanState.PLANNED,
        criticality: QualityAssetCriticality.IMPORTANT,
        requirements: [],
      }),
      stagedPlan: create(QualityPlanSchema, {
        coverage: [
          create(QualityPlanCoverageSchema, {
            property: QualityCoverageProperty.NULL_RATE,
            column: "email",
            minChecks: 1,
          }),
        ],
      }),
      stagedBaseVersion: 2,
    });
    const wrapper = mount(TargetPlanCard, {
      props: { contract, canUpdate: true },
      global: { plugins: [i18n] },
    });
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planDeclared")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planPropertyNullRate")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planEdit")
    );
    wrapper.unmount();
  });

  it("labels a derived plan and hides the editor without write access", () => {
    const wrapper = mount(TargetPlanCard, {
      props: { contract: contractFixture(QualityPlanState.PLANNED, 1) },
      global: { plugins: [i18n] },
    });
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.workspace.planDerived")
    );
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.workspace.planEdit")
    );
    wrapper.unmount();
  });
});
