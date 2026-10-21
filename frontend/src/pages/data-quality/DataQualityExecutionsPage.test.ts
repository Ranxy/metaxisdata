import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityExecution,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityObservedKind,
  QualityRuleSchema,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityExecutionsPage from "./DataQualityExecutionsPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityExecutions: vi.fn(),
  listAllQualityRules: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityExecutions: mocks.listQualityExecutions,
  listAllQualityRules: mocks.listAllQualityRules,
}));

const RULE = "workspaces/ws/qualityRules/rule-1";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const START = new Date("2026-10-07T09:30:00Z");
const Dummy = { template: "<div />" };

const PERMISSIONS = [
  "metaxisdata.dataQuality.executions.list",
  "metaxisdata.dataQuality.executions.get",
  "metaxisdata.dataQuality.rules.get",
];

function executionFixture(
  overrides: Record<string, unknown> = {}
): QualityExecution {
  return create(QualityExecutionSchema, {
    name: EXECUTION,
    rule: RULE,
    state: QualityExecutionState.COMPLETED,
    outcome: QualityStatus.FAIL,
    trigger: QualityExecutionTrigger.MANUAL,
    target: create(QualityTargetSchema, {
      metaGuid: "inst1;shop;public;orders",
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object: "orders",
      }),
    }),
    observedValue: 0.04,
    observedKind: QualityObservedKind.RATIO,
    failedRows: 40n,
    totalRows: 1000n,
    startTime: timestampFromDate(START),
    finishTime: timestampFromDate(new Date(START.getTime() + 2_000)),
    ...overrides,
  });
}

function page(items: QualityExecution[]) {
  return { items, nextPageToken: "" };
}

async function mountPage(path = "/data-quality/executions") {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/executions",
        name: "DataQualityExecutions",
        component: Dummy,
      },
      {
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleDetail",
        component: Dummy,
      },
    ],
  });
  await router.push(path);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const wrapper = mount(DataQualityExecutionsPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

describe("DataQualityExecutionsPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.listQualityExecutions.mockResolvedValue(page([executionFixture()]));
    mocks.listAllQualityRules.mockResolvedValue([
      create(QualityRuleSchema, {
        name: RULE,
        title: "Null rate on orders.id",
      }),
    ]);
  });

  it("renders one row per execution with its rule, target and verdict", async () => {
    const { wrapper } = await mountPage();

    const text = wrapper.text();
    expect(text).toContain("Null rate on orders.id");
    expect(text).toContain("public.orders");
    expect(text).toContain(
      i18n.global.t("dataQuality.executionState.completed")
    );
    expect(text).toContain(i18n.global.t("dataQuality.status.fail"));
    expect(text).toContain("4%");
    expect(text).toContain(
      i18n.global.t("dataQuality.executionTrigger.manual")
    );
    expect(wrapper.findAll("tbody tr")).toHaveLength(1);
  });

  it("shows a pending execution honestly, without inventing a verdict", async () => {
    mocks.listQualityExecutions.mockResolvedValue(
      page([
        executionFixture({
          state: QualityExecutionState.PENDING,
          outcome: QualityStatus.UNSPECIFIED,
          finishTime: undefined,
        }),
      ])
    );
    const { wrapper } = await mountPage();

    const row = wrapper.findAll("tbody tr")[0];
    expect(row?.text()).toContain(
      i18n.global.t("dataQuality.executionState.pending")
    );
    expect(row?.text()).not.toContain(i18n.global.t("dataQuality.status.fail"));
    wrapper.unmount();
  });

  it("opens the execution detail from a row", async () => {
    const { wrapper, router } = await mountPage();

    await wrapper.findAll("tbody tr")[0].trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.name).toBe("DataQualityExecutionDetail");
    expect(router.currentRoute.value.params.executionId).toEqual(
      EXECUTION.split("/")
    );
  });

  it("carries a rule filter from the URL", async () => {
    const { wrapper } = await mountPage(
      `/data-quality/executions?rule=${encodeURIComponent(RULE)}`
    );

    expect(mocks.listQualityExecutions).toHaveBeenCalledWith(
      expect.objectContaining({ rule: RULE })
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.filteredByRule", {
        rule: "Null rate on orders.id",
      })
    );
  });

  it("clears the rule filter and reloads the unfiltered list", async () => {
    const { wrapper } = await mountPage(
      `/data-quality/executions?rule=${encodeURIComponent(RULE)}`
    );

    const clear = wrapper.find(
      `[aria-label="${i18n.global.t("dataQuality.clearRuleFilter")}"]`
    );
    await clear.trigger("click");
    await flushPromises();

    expect(mocks.listQualityExecutions).toHaveBeenLastCalledWith(
      expect.objectContaining({ rule: "" })
    );
  });

  it("offers the rules page when there is nothing to show", async () => {
    mocks.listQualityExecutions.mockResolvedValue(page([]));
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.noExecutions"));
    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.viewRules"));
  });
});
