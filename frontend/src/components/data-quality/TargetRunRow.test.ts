import { create } from "@bufbuild/protobuf";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  QualityExecutionState,
  QualityRunSchema,
  QualityStatus,
  QualityStatusCountsSchema,
  QualityTargetPathSchema,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import TargetRunRow from "./TargetRunRow.vue";

function summary(
  fields: { runCount?: number; outcome?: QualityStatus } = {}
): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object: "orders",
      }),
    }),
    latestRun: create(QualityRunSchema, {
      state: QualityExecutionState.COMPLETED,
      outcome: fields.outcome ?? QualityStatus.PASS,
      counts: create(QualityStatusCountsSchema, {
        pass: 3,
        fail: 1,
        total: 4,
      }),
    }),
    runCount: fields.runCount ?? 2,
    enabledRuleCount: 4,
  });
}

function mountRow(props: {
  summary: QualityTargetSummary;
  expanded?: boolean;
}) {
  return mount(TargetRunRow, {
    props: {
      ruleTitles: {},
      expandRun: "",
      expanded: false,
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs: {
        RouterLink: { template: "<a><slot /></a>" },
        TargetScanHistory: {
          name: "TargetScanHistory",
          template: '<div class="history-stub" />',
        },
      },
    },
  });
}

describe("TargetRunRow", () => {
  it("reads the target and its latest batch", () => {
    const wrapper = mountRow({ summary: summary() });

    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("prod · shop · public.orders");
    expect(wrapper.text()).toContain("3 Pass · 1 Fail");
    expect(wrapper.text()).toContain("2 runs");
  });

  it("asks the page to expand the row", async () => {
    const wrapper = mountRow({ summary: summary() });

    await wrapper.find("button").trigger("click");

    expect(wrapper.emitted("toggle")).toHaveLength(1);
    expect(wrapper.find(".history-stub").exists()).toBe(false);
  });

  it("shows the scan history only when expanded", async () => {
    const wrapper = mountRow({ summary: summary(), expanded: true });

    expect(wrapper.find(".history-stub").exists()).toBe(true);

    // A run chip click is the history's event, forwarded to the page.
    const history = wrapper.findComponent({ name: "TargetScanHistory" });
    history.vm.$emit("update:expand-run", "runs/r1");
    await wrapper.vm.$nextTick();
    expect(wrapper.emitted("update:expand-run")?.[0]).toEqual(["runs/r1"]);
  });
});
