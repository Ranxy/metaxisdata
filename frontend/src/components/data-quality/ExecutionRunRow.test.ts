import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";
import { i18n } from "@/locales";
import {
  type QualityExecution,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityObservedKind,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import ExecutionRunRow from "./ExecutionRunRow.vue";

const START = new Date("2026-10-07T09:30:00Z");

function execution(
  overrides: Partial<QualityExecution> = {}
): QualityExecution {
  return create(QualityExecutionSchema, {
    name: "workspaces/ws/qualityExecutions/exec-1",
    rule: "workspaces/ws/qualityRules/rule-1",
    state: QualityExecutionState.COMPLETED,
    outcome: overrides.outcome ?? QualityStatus.FAIL,
    trigger: QualityExecutionTrigger.SCHEDULED,
    startTime: timestampFromDate(START),
    finishTime: timestampFromDate(new Date(START.getTime() + 2_000)),
    observedValue: 0.04,
    observedKind: QualityObservedKind.RATIO,
    failedRows: 2n,
    totalRows: 10n,
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
  });
}

function mountRow(props: Record<string, unknown> = {}) {
  return mount(ExecutionRunRow, {
    props: {
      execution: execution(),
      ruleTitles: {},
      canViewRule: true,
      ...props,
    },
    global: {
      plugins: [i18n],
      stubs: {
        RouterLink: {
          name: "RouterLink",
          props: ["to"],
          template: '<a class="route-link"><slot /></a>',
        },
      },
    },
  });
}

describe("ExecutionRunRow", () => {
  it("prints the execution's state, rule, target and measured value", () => {
    const wrapper = mountRow();

    expect(wrapper.text()).toContain("rule-1");
    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("2 / 10");
    expect(wrapper.text()).toContain("4%");
    expect(wrapper.text()).toContain("2 s");
  });

  it("uses the rule title when the page has loaded one", () => {
    const wrapper = mountRow({
      ruleTitles: { "workspaces/ws/qualityRules/rule-1": "Orders have rows" },
    });

    expect(wrapper.text()).toContain("Orders have rows");
  });

  it("links the rule and target only when the caller may view rules", () => {
    expect(mountRow().findAllComponents({ name: "RouterLink" })).toHaveLength(
      4
    );
    expect(
      mountRow({ canViewRule: false }).findAllComponents({ name: "RouterLink" })
    ).toHaveLength(2);
  });

  it("asks the page to open the execution", async () => {
    const wrapper = mountRow();

    await wrapper.trigger("click");
    expect(wrapper.emitted("open")).toHaveLength(1);

    const eye = wrapper
      .findAll("button")
      .find((button) => button.text() === "");
    await eye?.trigger("click");
    expect(wrapper.emitted("open")).toHaveLength(2);
  });

  it("falls back to a dash before a verdict exists", () => {
    const wrapper = mountRow({
      execution: execution({ outcome: QualityStatus.UNSPECIFIED }),
    });

    expect(wrapper.text()).not.toContain("4%");
  });
});
