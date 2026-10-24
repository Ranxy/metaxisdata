import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { notify } from "@/lib/notify";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  ColumnProfileSchema,
  ColumnProfileStatsSchema,
  ColumnSemanticLabelSchema,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityIssueSchema,
  QualityIssueStatus,
  QualityRuleSchema,
  QualityRuleType,
  QualitySemanticType,
  QualitySeverity,
  QualityStatus,
  QualitySuggestionQueueSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import MetadataQualitySection from "./MetadataQualitySection.vue";

const mocks = vi.hoisted(() => ({
  listQualityRules: vi.fn(),
  listQualityExecutions: vi.fn(),
  listQualityIssues: vi.fn(),
  listColumnLabels: vi.fn(),
  listColumnProfiles: vi.fn(),
  profileColumns: vi.fn(),
  suggestColumnLabels: vi.fn(),
  updateColumnLabel: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityRules: mocks.listQualityRules,
  listQualityExecutions: mocks.listQualityExecutions,
  listQualityIssues: mocks.listQualityIssues,
  listColumnLabels: mocks.listColumnLabels,
  listColumnProfiles: mocks.listColumnProfiles,
  profileColumns: mocks.profileColumns,
  suggestColumnLabels: mocks.suggestColumnLabels,
  updateColumnLabel: mocks.updateColumnLabel,
}));

const TABLE = "inst1;db1;public;orders";
const RULE = "workspaces/ws/qualityRules/rule-1";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const SUGGEST_PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.profile.run",
  "metaxisdata.dataQuality.suggest",
  "metaxisdata.dataQuality.labels.update",
];

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

async function mountSection(permissions: string[]) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", component: { template: "<div />" } },
      {
        path: "/data-quality/rules/new",
        name: "DataQualityRuleNew",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleDetail",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/executions",
        name: "DataQualityExecutions",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/issues",
        name: "DataQualityIssues",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/issues/:issueId+",
        name: "DataQualityIssueDetail",
        component: { template: "<div />" },
      },
    ],
  });
  await router.push("/");
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(MetadataQualitySection, {
    props: {
      tableGuid: TABLE,
      metaType: MetaType.TABLE,
      columns: [
        {
          name: "id",
          type: "bigint",
          nullable: false,
          position: 1,
          $typeName: "metaxisdata.v1.ColumnMetadata",
        } as never,
        {
          name: "email",
          type: "text",
          nullable: true,
          position: 2,
          $typeName: "metaxisdata.v1.ColumnMetadata",
        } as never,
      ],
    },
    global: { plugins: [i18n, pinia, router] },
  });
  await flushPromises();
  return wrapper;
}

describe("MetadataQualitySection", () => {
  beforeEach(() => {
    localStorage.clear();
    document.body.innerHTML = "";
    vi.clearAllMocks();
    mocks.listQualityRules.mockResolvedValue({
      items: [
        create(QualityRuleSchema, {
          name: RULE,
          title: "Null rate on orders.id",
          ruleType: QualityRuleType.NULL_PERCENT,
          severity: QualitySeverity.ERROR,
          state: 1,
          lastResult: { status: QualityStatus.FAIL },
        }),
      ],
      nextPageToken: "",
    });
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
          name: "workspaces/ws/qualityIssues/i1",
          rule: RULE,
          status: QualityIssueStatus.ACKNOWLEDGED,
          severity: QualitySeverity.ERROR,
        }),
      ],
      nextPageToken: "",
    });
    mocks.listColumnLabels.mockResolvedValue([
      create(ColumnSemanticLabelSchema, {
        tableGuid: TABLE,
        metaType: MetaType.TABLE,
        column: "id",
        label: QualitySemanticType.ID,
        status: 2,
        confidence: 0.9,
      }),
    ]);
    mocks.listColumnProfiles.mockResolvedValue([
      create(ColumnProfileSchema, {
        tableGuid: TABLE,
        metaType: MetaType.TABLE,
        column: "id",
        sampleMethod: "full",
        sampleRows: 1000n,
        profile: create(ColumnProfileStatsSchema, {
          totalRows: 1000n,
          nullCount: 20n,
          distinctCount: 990n,
        }),
      }),
    ]);
    mocks.suggestColumnLabels.mockResolvedValue({
      qualitySuggestions: [],
      summary: create(QualitySuggestionQueueSummarySchema, { created: 2 }),
    });
  });

  it("renders the table's rules, executions, issues and column labels", async () => {
    const wrapper = await mountSection(["metaxisdata.dataQuality.rules.list"]);

    expect(wrapper.text()).toContain("Null rate on orders.id");
    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.status.fail"));
    // The accepted label and the profile facts show up per column, and the
    // unlabelled column says so explicitly.
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.semanticType.id")
    );
    expect(wrapper.text()).toContain("990");
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.asset.noLabel")
    );
    // Read-only caller: no action buttons.
    expect(
      wrapper.text().includes(i18n.global.t("dataQuality.asset.createRule"))
    ).toBe(false);
    expect(
      wrapper.text().includes(i18n.global.t("dataQuality.asset.analyzeColumns"))
    ).toBe(false);
  });

  it("offers the actions its permissions allow", async () => {
    const wrapper = await mountSection([
      "metaxisdata.dataQuality.rules.list",
      "metaxisdata.dataQuality.rules.create",
      "metaxisdata.dataQuality.profile.run",
      "metaxisdata.dataQuality.suggest",
      "metaxisdata.dataQuality.labels.update",
    ]);

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.asset.createRule")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.asset.analyzeColumns")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.asset.suggestLabels")
    );
    expect(wrapper.find('[aria-label="Clear label"]').exists()).toBe(true);
  });

  it("stays out of the page for a caller without rules.list", async () => {
    const wrapper = await mountSection([]);
    expect(wrapper.text()).toBe("");
    expect(mocks.listQualityRules).not.toHaveBeenCalled();
  });

  it("toasts what the label pass created, refreshed and skipped", async () => {
    mocks.suggestColumnLabels.mockResolvedValue({
      qualitySuggestions: [],
      summary: create(QualitySuggestionQueueSummarySchema, {
        created: 2,
        refreshed: 1,
        skippedExistingFact: 3,
      }),
    });
    const success = vi.spyOn(notify, "success");
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    await wrapper
      .findAll("button")
      .find((item) =>
        item.text().includes(label("dataQuality.asset.suggestLabels"))
      )
      ?.trigger("click");
    await flushPromises();

    // The default action skips columns that already have a fact or a proposal.
    expect(mocks.suggestColumnLabels).toHaveBeenCalledWith({
      tableGuid: TABLE,
      metaType: MetaType.TABLE,
      columns: ["id", "email"],
      language: "en-US",
      forceRegenerate: false,
    });
    expect(success).toHaveBeenCalledWith(
      [
        label("dataQuality.suggestionSummary.created", { count: 2 }),
        label("dataQuality.suggestionSummary.refreshed", { count: 1 }),
        label("dataQuality.suggestionSummary.skipped", { count: 3 }),
      ].join(" · ")
    );
    success.mockRestore();
    wrapper.unmount();
  });

  it("explains why a label pass queued nothing", async () => {
    mocks.suggestColumnLabels.mockResolvedValue({
      qualitySuggestions: [],
      summary: create(QualitySuggestionQueueSummarySchema, {
        skippedExistingFact: 2,
      }),
    });
    const info = vi.spyOn(notify, "info");
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    await wrapper
      .findAll("button")
      .find((item) =>
        item.text().includes(label("dataQuality.asset.suggestLabels"))
      )
      ?.trigger("click");
    await flushPromises();

    expect(info).toHaveBeenCalledWith(
      label("dataQuality.suggestionSummary.emptyLabelExistingFact", {
        count: 2,
      })
    );
    info.mockRestore();
    wrapper.unmount();
  });

  it("offers a forced regeneration as the split action's second choice", async () => {
    const wrapper = await mountSection(SUGGEST_PERMISSIONS);

    const trigger = wrapper.find(
      `[aria-label="${label("dataQuality.asset.suggestRegenerate")}"]`
    );
    expect(trigger.exists()).toBe(true);
    await trigger.trigger("click");
    await flushPromises();

    const item = document.body.querySelector<HTMLElement>('[role="menuitem"]');
    // The secondary action says it may refresh existing proposals.
    expect(item?.textContent).toContain(
      label("dataQuality.asset.suggestRegenerateHint")
    );
    item?.click();
    await flushPromises();

    expect(mocks.suggestColumnLabels).toHaveBeenCalledWith(
      expect.objectContaining({ forceRegenerate: true })
    );
    wrapper.unmount();
  });
});
