import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityIssueSchema,
  QualityRuleSchema,
  QualityStatus,
  QualityStatusCountsSchema,
  QualityTargetOrder,
  QualityTargetPathSchema,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
  QualityTrendPointSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityOverviewPage from "./DataQualityOverviewPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityOverview: vi.fn(),
  listAllQualityRules: vi.fn(),
  listQualityIssues: vi.fn(),
  listQualityTargets: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityOverview: mocks.getQualityOverview,
  listAllQualityRules: mocks.listAllQualityRules,
  listQualityIssues: mocks.listQualityIssues,
  listQualityTargets: mocks.listQualityTargets,
}));

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.get",
  "metaxisdata.dataQuality.issues.list",
];

const Dummy = { template: "<div />" };

const RULE = "workspaces/ws/qualityRules/rule-1";
const ISSUE = "workspaces/ws/qualityIssues/issue-1";
const TARGET_GUID = "inst1;shop;public;orders";

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

/** One directory row as the attention card receives it. */
function targetFixture(
  overrides: {
    guid?: string;
    object?: string;
    worstStatus?: QualityStatus;
    openIssueCount?: number;
    ruleCount?: number;
  } = {}
): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: overrides.guid ?? TARGET_GUID,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object: overrides.object ?? "orders",
      }),
    }),
    ruleCount: overrides.ruleCount ?? 3,
    enabledRuleCount: overrides.ruleCount ?? 3,
    pausedRuleCount: 0,
    columnCount: 1,
    openIssueCount: overrides.openIssueCount ?? 1,
    pendingDraftCount: 0,
    pendingLabelCount: 0,
    worstStatus: overrides.worstStatus ?? QualityStatus.FAIL,
    schemaChanged: false,
  });
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
        path: "/data-quality/checks",
        name: "DataQualityChecks",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleRedirect",
        component: Dummy,
      },
      {
        path: "/data-quality/executions",
        name: "DataQualityExecutions",
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
    mocks.listQualityIssues.mockResolvedValue({
      items: [
        create(QualityIssueSchema, {
          name: ISSUE,
          rule: RULE,
          status: 1,
          severity: 2,
          occurrenceCount: 3,
          target: create(QualityTargetSchema, {
            metaGuid: TARGET_GUID,
            metaType: MetaType.TABLE,
            path: create(QualityTargetPathSchema, {
              instance: "prod",
              database: "shop",
              schema: "public",
              object: "orders",
            }),
          }),
        }),
      ],
      nextPageToken: "",
    });
    mocks.listQualityTargets.mockResolvedValue({
      items: [
        targetFixture(),
        // A healthy target is not an attention row even when the page returns it.
        targetFixture({
          guid: "inst1;shop;public;customers",
          object: "customers",
          worstStatus: QualityStatus.PASS,
          openIssueCount: 0,
          ruleCount: 1,
        }),
      ],
      nextPageToken: "",
    });
  });

  it("renders the counts, the trend and the targets needing attention", async () => {
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
    expect(text).toContain(
      i18n.global.t("dataQuality.overview.attentionTitle")
    );
    expect(text).toContain(
      i18n.global.t("dataQuality.overview.attentionDescription")
    );
    // The failing target is the row; a healthy one from the same page is not.
    expect(text).toContain("orders");
    expect(text).not.toContain("customers");
    expect(text).toContain(
      i18n.global.t("dataQuality.targets.rulesCount", { count: 3 })
    );
    expect(text).toContain(i18n.global.t("dataQuality.status.fail"));
    expect(text).toContain(
      i18n.global.t("dataQuality.workspace.summaryIssues", { count: 1 })
    );

    // The attention read asks for the worst rows first, then filters.
    expect(mocks.listQualityTargets).toHaveBeenCalledWith({
      pageSize: 5,
      orderBy: QualityTargetOrder.ATTENTION,
    });

    const attentionLink = wrapper
      .findAll("a")
      .find((link) =>
        (link.attributes("href") ?? "").startsWith(
          "/data-quality/targets/inst1/shop/public/orders"
        )
      );
    expect(attentionLink).toBeDefined();
    expect(attentionLink?.attributes("href")).toBe(
      "/data-quality/targets/inst1/shop/public/orders?metaType=4"
    );

    // The open-issues block stays beside the attention card.
    expect(text).toContain(
      i18n.global.t("dataQuality.overview.openIssuesTitle")
    );
    expect(text).toContain("Null rate on orders.id");

    // The chart is a labelled inline SVG, not an <img>.
    const svg = wrapper.find("svg[role='img']");
    expect(svg.exists()).toBe(true);
    expect(svg.attributes("aria-label")).toContain("7");
    expect(wrapper.findAll("rect").length).toBeGreaterThan(0);
  });

  it("links the enabled-check tile to the audit directory", async () => {
    const wrapper = await mountPage();

    const checks = wrapper.find('a[href="/data-quality/checks"]');
    expect(checks.exists()).toBe(true);
    expect(wrapper.find('a[href="/data-quality/targets"]').exists()).toBe(true);
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

  it("says every target is healthy when none needs attention", async () => {
    mocks.listQualityTargets.mockResolvedValue({
      items: [
        targetFixture({
          worstStatus: QualityStatus.PASS,
          openIssueCount: 0,
        }),
      ],
      nextPageToken: "",
    });
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.overview.attentionEmpty")
    );
    wrapper.unmount();
  });

  it("degrades the attention card without losing the rest of the page", async () => {
    mocks.listQualityTargets.mockRejectedValue(new Error("targets down"));
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.overview.attentionEmpty")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.overview.openIssuesTitle")
    );
    expect(wrapper.find("svg[role='img']").exists()).toBe(true);
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.overview.fetchError")
    );
    wrapper.unmount();
  });

  it("guides a workspace with no rules yet and never offers to create one here", async () => {
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
    expect(wrapper.text()).not.toContain(i18n.global.t("dataQuality.newRule"));
    expect(wrapper.find('a[href="/data-quality/rules/new"]').exists()).toBe(
      false
    );
    const targets = wrapper
      .findAll("a")
      .find(
        (link) =>
          link.text().trim() ===
          i18n.global.t("dataQuality.targets.startMonitoring")
      );
    expect(targets?.attributes("href")).toBe("/data-quality/targets?new=1");
    expect(wrapper.find("svg[role='img']").exists()).toBe(false);
  });
});
