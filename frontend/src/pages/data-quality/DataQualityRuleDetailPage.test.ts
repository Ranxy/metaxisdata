import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityDimension,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityRuleDetailPage from "./DataQualityRuleDetailPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityRule: vi.fn(),
  listQualityExecutions: vi.fn(),
  runQualityRules: vi.fn(),
  listAllUsers: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityRule: mocks.getQualityRule,
  listQualityExecutions: mocks.listQualityExecutions,
  runQualityRules: mocks.runQualityRules,
}));

vi.mock("@/api/user", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/user")>()),
  listAllUsers: mocks.listAllUsers,
}));

const RULE = "workspaces/ws/qualityRules/rule-1";
const START = new Date("2026-10-07T09:30:00Z");
const Dummy = { template: "<div />" };

const PERMISSIONS = ["metaxisdata.dataQuality.rules.get"];

const USERS = [
  create(UserSchema, { name: "users/101", email: "alice@example.com" }),
];

function ruleFixture(overrides = {}) {
  return create(QualityRuleSchema, {
    name: RULE,
    title: "Orders should have rows",
    description: "The fact table must never be empty.",
    dimension: QualityDimension.COMPLETENESS,
    ruleType: QualityRuleType.ROW_COUNT,
    target: create(QualityTargetSchema, {
      metaGuid: "instance_1;shop;public;orders",
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object: "orders",
      }),
      columns: ["id"],
    }),
    severity: QualitySeverity.ERROR,
    state: QualityRuleState.ENABLED,
    revision: 3,
    scheduleInterval: { seconds: 3_600n },
    createTime: timestampFromDate(new Date(START.getTime() - 86_400_000)),
    updateTime: timestampFromDate(START),
    tags: ["fact"],
    ...overrides,
  });
}

/** Executions arrive newest-first, exactly as the API returns them. */
function executionFixture(index: number, outcome: QualityStatus) {
  const at = timestampFromDate(new Date(START.getTime() - index * 60_000));
  return create(QualityExecutionSchema, {
    name: `workspaces/ws/qualityExecutions/${index + 1}`,
    rule: RULE,
    state: QualityExecutionState.COMPLETED,
    outcome,
    observedValue: index,
    startTime: at,
    finishTime: at,
  });
}

function executionsFixture() {
  return [
    executionFixture(0, QualityStatus.FAIL),
    executionFixture(1, QualityStatus.PASS),
    executionFixture(2, QualityStatus.PASS),
  ];
}

async function mountPage(permissions: string[] = PERMISSIONS) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+/edit",
        name: "DataQualityRuleEdit",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: Dummy,
      },
    ],
  });
  await router.push(`/data-quality/rules/${RULE}`);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualityRuleDetailPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

describe("DataQualityRuleDetailPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getQualityRule.mockResolvedValue(ruleFixture());
    mocks.listQualityExecutions.mockResolvedValue({
      items: executionsFixture(),
      nextPageToken: "",
    });
    mocks.listAllUsers.mockResolvedValue(USERS);
  });

  it("answers 'how is this rule doing?' at a glance", async () => {
    const { wrapper } = await mountPage([
      ...PERMISSIONS,
      "metaxisdata.dataQuality.rules.run",
      "metaxisdata.dataQuality.rules.update",
    ]);

    // The identity and definition of the rule.
    expect(wrapper.text()).toContain("Orders should have rows");
    expect(wrapper.text()).toContain("prod · shop · public.orders");
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.ruleType.rowCount")
    );

    // The latest result and its share of the recent window.
    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.status.fail"));
    expect(wrapper.text()).toContain("67%");

    // One square per recent result, plus one row in the history table.
    const strip = wrapper.find('[role="list"]');
    expect(strip.exists()).toBe(true);
    expect(strip.findAll("button")).toHaveLength(3);
    expect(wrapper.findAll("tbody tr")).toHaveLength(3);
  });

  it("gates running and editing on their permissions", async () => {
    const { wrapper } = await mountPage(PERMISSIONS);
    const runNow = i18n.global.t("dataQuality.runNow");
    const editRule = i18n.global.t("dataQuality.editRule");

    const buttonTexts = wrapper
      .findAll("button")
      .map((button) => button.text());
    expect(buttonTexts).not.toContain(runNow);
    expect(buttonTexts).not.toContain(editRule);
  });

  it("offers the actions to a caller who may use them", async () => {
    const { wrapper } = await mountPage([
      ...PERMISSIONS,
      "metaxisdata.dataQuality.rules.run",
      "metaxisdata.dataQuality.rules.update",
    ]);

    const buttonTexts = wrapper
      .findAll("button")
      .map((button) => button.text());
    expect(buttonTexts).toContain(i18n.global.t("dataQuality.runNow"));
    expect(buttonTexts).toContain(i18n.global.t("dataQuality.editRule"));
  });

  it("shows the empty state when the rule has never executed", async () => {
    mocks.listQualityExecutions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });

    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.noResults"));
    expect(wrapper.find('[role="list"]').exists()).toBe(false);
  });

  it("opens the execution behind a history row", async () => {
    const { wrapper, router } = await mountPage();

    await wrapper.findAll("tbody tr")[0].trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.name).toBe("DataQualityExecutionDetail");
    expect(router.currentRoute.value.params.executionId).toEqual(
      executionsFixture()[0]?.name.split("/")
    );
  });

  it("names the owner from the workspace directory", async () => {
    mocks.getQualityRule.mockResolvedValue(ruleFixture({ owner: "users/101" }));

    const { wrapper } = await mountPage([
      ...PERMISSIONS,
      "metaxisdata.users.list",
    ]);

    expect(wrapper.text()).toContain("alice@example.com");
  });

  it("falls back to the owner's resource name when the directory fails", async () => {
    mocks.listAllUsers.mockRejectedValue(new Error("directory unavailable"));
    mocks.getQualityRule.mockResolvedValue(ruleFixture({ owner: "users/101" }));

    const { wrapper } = await mountPage([
      ...PERMISSIONS,
      "metaxisdata.users.list",
    ]);

    expect(wrapper.text()).toContain("users/101");
  });

  it("says the owner is unassigned when the rule has none", async () => {
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.unassignedOwner")
    );
  });
});
