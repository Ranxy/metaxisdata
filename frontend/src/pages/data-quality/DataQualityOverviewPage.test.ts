import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityExecutionSchema,
  QualityExecutionState,
  QualityIssueSchema,
  QualityRuleSchema,
  QualityStatus,
  QualityStatusCountsSchema,
  QualityTrendPointSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityOverviewPage from "./DataQualityOverviewPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityOverview: vi.fn(),
  listAllQualityRules: vi.fn(),
  listQualityIssues: vi.fn(),
  listQualityExecutions: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityOverview: mocks.getQualityOverview,
  listAllQualityRules: mocks.listAllQualityRules,
  listQualityIssues: mocks.listQualityIssues,
  listQualityExecutions: mocks.listQualityExecutions,
}));

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.issues.list",
  "metaxisdata.dataQuality.executions.list",
  "metaxisdata.dataQuality.executions.get",
];

const Dummy = { template: "<div />" };

const RULE = "workspaces/ws/qualityRules/rule-1";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const ISSUE = "workspaces/ws/qualityIssues/issue-1";

function today(offsetDays = 0): string {
  return new Date(Date.now() - offsetDays * 86_400_000)
    .toISOString()
    .slice(0, 10);
}

function overviewFixture(overrides = {}) {
  return {
    ruleCount: 5,
    enabledRuleCount: 3,
    pausedRuleCount: 1,
    monitoredTableCount: 4,
    openWarningCount: 1,
    openErrorCount: 2,
    openCriticalCount: 1,
    failingRuleCount: 2,
    executionCount: 10,
    lastDayCounts: create(QualityStatusCountsSchema, {
      total: 10,
      pass: 7,
      fail: 2,
      error: 1,
    }),
    trend: [
      create(QualityTrendPointSchema, {
        date: today(1),
        pass: 3,
        fail: 1,
        total: 4,
      }),
      create(QualityTrendPointSchema, {
        date: today(0),
        pass: 4,
        fail: 2,
        total: 6,
      }),
    ],
    trendDays: 7,
    ...overrides,
  };
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality",
        name: "DataQualityOverview",
        component: Dummy,
      },
      {
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/new",
        name: "DataQualityRuleNew",
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
        path: "/data-quality/issues",
        name: "DataQualityIssues",
        component: Dummy,
      },
      {
        path: "/data-quality/issues/:issueId+",
        name: "DataQualityIssueDetail",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality");
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const wrapper = mount(DataQualityOverviewPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("DataQualityOverviewPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getQualityOverview.mockResolvedValue(overviewFixture());
    mocks.listAllQualityRules.mockResolvedValue([
      create(QualityRuleSchema, {
        name: RULE,
        title: "Null rate on orders.id",
      }),
    ]);
    mocks.listQualityExecutions.mockResolvedValue({
      items: [
        create(QualityExecutionSchema, {
          name: EXECUTION,
          rule: RULE,
          state: QualityExecutionState.COMPLETED,
          outcome: QualityStatus.FAIL,
          failedRows: 40n,
          totalRows: 1000n,
        }),
      ],
      nextPageToken: "",
    });
    mocks.listQualityIssues.mockResolvedValue({
      items: [
        create(QualityIssueSchema, {
          name: ISSUE,
          rule: RULE,
          status: 1,
          severity: 2,
          occurrenceCount: 3,
        }),
      ],
      nextPageToken: "",
    });
  });

  it("renders the counts, the trend and the attention lists", async () => {
    const wrapper = await mountPage();
    const text = wrapper.text();

    expect(text).toContain(
      i18n.global.t("dataQuality.overview.statEnabledRules")
    );
    expect(text).toContain(
      i18n.global.t("dataQuality.overview.statOpenIssues")
    );
    expect(text).toContain("3");
    expect(text).toContain("10");
    // The failing rule and the open issue are both named by the rule's title.
    expect(text).toContain("Null rate on orders.id");
    // The chart is a labelled inline SVG, not an <img>.
    const svg = wrapper.find("svg[role='img']");
    expect(svg.exists()).toBe(true);
    expect(svg.attributes("aria-label")).toContain("7");
    expect(wrapper.findAll("rect").length).toBeGreaterThan(0);
  });

  it("asks for the other window when the 30-day toggle is used", async () => {
    const wrapper = await mountPage();
    const twentyFour = create(QualityStatusCountsSchema, { total: 1 });
    mocks.getQualityOverview.mockResolvedValue(
      overviewFixture({
        trendDays: 30,
        lastDayCounts: twentyFour,
      })
    );

    const button = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("30"));
    expect(button).toBeTruthy();
    await button?.trigger("click");
    await flushPromises();

    expect(mocks.getQualityOverview).toHaveBeenLastCalledWith({
      trendDays: 30,
    });
    expect(
      wrapper
        .findAll("button")
        .find((candidate) => candidate.text().includes("30"))
        ?.attributes("aria-pressed")
    ).toBe("true");
  });

  it("guides a workspace with no rules yet instead of showing empty charts", async () => {
    mocks.getQualityOverview.mockResolvedValue(
      overviewFixture({
        ruleCount: 0,
        enabledRuleCount: 0,
        pausedRuleCount: 0,
        monitoredTableCount: 0,
        trend: [],
      })
    );
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.overview.emptyTitle")
    );
    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.newRule"));
    expect(wrapper.find("svg[role='img']").exists()).toBe(false);
  });
});
