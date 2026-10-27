import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityComparison,
  type QualityExecution,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityObservedKind,
  QualityRuleSchema,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityThresholdsSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityExecutionDetailPage from "./DataQualityExecutionDetailPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityExecution: vi.fn(),
  getQualityRule: vi.fn(),
  getWorkspaceProfileSetting: vi.fn(),
  listAllUsers: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityExecution: mocks.getQualityExecution,
  getQualityRule: mocks.getQualityRule,
}));

vi.mock("@/api/setting", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/setting")>()),
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
}));

vi.mock("@/api/user", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/user")>()),
  listAllUsers: mocks.listAllUsers,
}));

const RULE = "workspaces/ws/qualityRules/rule-1";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const START = new Date("2026-10-07T09:30:00Z");
const Dummy = { template: "<div />" };

const PERMISSIONS = [
  "metaxisdata.dataQuality.executions.get",
  "metaxisdata.dataQuality.executions.list",
  "metaxisdata.dataQuality.rules.get",
  "metaxisdata.dataQuality.explain",
  "metaxisdata.users.list",
];

function executionFixture(
  overrides: Record<string, unknown> = {}
): QualityExecution {
  return create(QualityExecutionSchema, {
    name: EXECUTION,
    rule: RULE,
    ruleRevision: 3,
    ruleEngineVersion: "2026.10",
    state: QualityExecutionState.COMPLETED,
    outcome: QualityStatus.FAIL,
    trigger: QualityExecutionTrigger.MANUAL,
    initiator: "users/7",
    instance: "instances/inst1",
    target: create(QualityTargetSchema, {
      metaGuid: "inst1;shop;public;orders",
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object: "orders",
        column: "id",
      }),
    }),
    observedValue: 0.04,
    observedKind: QualityObservedKind.RATIO,
    thresholds: create(QualityThresholdsSchema, {
      comparison: QualityComparison.GREATER_THAN,
      failValue: 0,
    }),
    failedRows: 40n,
    totalRows: 1000n,
    samples: [
      {
        columns: [
          { column: "id", value: "1" },
          { column: "email", value: "a@example.com" },
          { column: "note", value: "", isNull: true },
          { column: "title", value: "", isNull: false },
          { column: "legacy", value: "" },
        ],
      },
      {
        columns: [
          { column: "id", value: "2" },
          { column: "email", value: "b@example.com" },
        ],
      },
    ],
    executedSql: "SELECT COUNT(*) FROM public.orders WHERE id IS NULL",
    startTime: timestampFromDate(START),
    finishTime: timestampFromDate(new Date(START.getTime() + 2_000)),
    ...overrides,
  });
}

async function mountPage(permissions = PERMISSIONS) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/targets",
        name: "DataQualityTargets",
        component: Dummy,
      },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
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
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleRedirect",
        component: Dummy,
      },
    ],
  });
  await router.push(`/data-quality/executions/${EXECUTION}`);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualityExecutionDetailPage, {
    global: {
      plugins: [router, i18n, pinia],
      stubs: { QualityExplainDrawer: true },
    },
  });
  await flushPromises();
  return { wrapper, router };
}

describe("DataQualityExecutionDetailPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getQualityExecution.mockResolvedValue(executionFixture());
    mocks.getQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: RULE,
        title: "Null rate on orders.id",
      })
    );
    mocks.getWorkspaceProfileSetting.mockResolvedValue({
      dataQuality: { enableLlmAnalysis: true },
    });
    mocks.listAllUsers.mockResolvedValue([
      create(UserSchema, { name: "users/7", email: "owner@example.com" }),
    ]);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders the verdict and its evidence", async () => {
    const { wrapper } = await mountPage();
    const text = wrapper.text();

    expect(text).toContain("Null rate on orders.id");
    expect(text).toContain("prod · shop · public.orders.id");
    expect(text).toContain(i18n.global.t("dataQuality.status.fail"));
    expect(text).toContain("4%");
    expect(text).toContain("> 0%");
    expect(text).toContain("40 / 1,000");
    expect(text).toContain("2 s");
    expect(text).toContain(
      i18n.global.t("dataQuality.executionTrigger.manual")
    );
    expect(text).toContain("inst1");
    // The initiator is a principal resource name; the page resolves it through
    // the workspace directory rather than printing the numeric id.
    expect(text).toContain("owner@example.com");
    expect(wrapper.text()).toContain("a@example.com");
    expect(wrapper.text()).toContain("b@example.com");
    // Every sampled row is numbered, so two failures on the same column do not
    // read as one row repeated.
    expect(text).toContain(
      i18n.global.t("dataQuality.sampleNumber", { index: 1 })
    );
    expect(text).toContain(
      i18n.global.t("dataQuality.sampleNumber", { index: 2 })
    );
    // A NULL and an empty string are both empty on the wire; the page says which
    // one it was instead of printing a blank cell.
    expect(text).toContain(i18n.global.t("dataQuality.sampleNull"));
    expect(text).toContain(i18n.global.t("dataQuality.sampleEmpty"));
    // A sample recorded before the marker existed cannot be either: it says so,
    // with the explanation on the cell.
    expect(text).toContain(i18n.global.t("dataQuality.sampleAmbiguous"));
    expect(wrapper.html()).toContain(
      i18n.global.t("dataQuality.sampleLegacyHint")
    );
    expect(wrapper.text()).toContain("SELECT COUNT(*) FROM public.orders");
    // A finished execution does not poll.
    expect(text).not.toContain(i18n.global.t("dataQuality.polling"));
  });

  it("polls a running execution until it reaches a verdict", async () => {
    vi.useFakeTimers();
    mocks.getQualityExecution.mockResolvedValue(
      executionFixture({
        state: QualityExecutionState.RUNNING,
        outcome: QualityStatus.UNSPECIFIED,
        finishTime: undefined,
      })
    );
    const { wrapper } = await mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.executionState.running")
    );
    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.polling"));

    mocks.getQualityExecution.mockResolvedValue(executionFixture());
    await vi.advanceTimersByTimeAsync(3_000);
    await flushPromises();

    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.status.fail"));
    expect(wrapper.text()).not.toContain(i18n.global.t("dataQuality.polling"));
    wrapper.unmount();
  });

  it("offers the model explanation only for a failed verdict", async () => {
    const { wrapper } = await mountPage();

    const explain = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("dataQuality.llm.explain.action"))
      );
    expect(explain).toBeTruthy();

    mocks.getWorkspaceProfileSetting.mockResolvedValue({
      dataQuality: { enableLlmAnalysis: false },
    });
    const disabled = await mountPage();
    expect(
      disabled.wrapper
        .findAll("button")
        .some((button) =>
          button
            .text()
            .includes(i18n.global.t("dataQuality.llm.explain.action"))
        )
    ).toBe(false);
    disabled.wrapper.unmount();
    wrapper.unmount();
  });

  it("surfaces a load failure instead of an empty page", async () => {
    mocks.getQualityExecution.mockRejectedValue(new Error("not found"));

    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.fetchExecutionError")
    );
  });
});
