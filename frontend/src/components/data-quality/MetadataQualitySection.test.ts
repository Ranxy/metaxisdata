import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  ColumnProfileSchema,
  ColumnProfileStatsSchema,
  ColumnSemanticLabelSchema,
  QualityIssueSchema,
  QualityIssueStatus,
  QualityRuleResultSchema,
  QualityRuleSchema,
  QualityRuleType,
  QualitySemanticType,
  QualitySeverity,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import MetadataQualitySection from "./MetadataQualitySection.vue";

const mocks = vi.hoisted(() => ({
  listQualityRules: vi.fn(),
  listQualityRuleResults: vi.fn(),
  listQualityIssues: vi.fn(),
  listColumnLabels: vi.fn(),
  listColumnProfiles: vi.fn(),
  profileColumns: vi.fn(),
  suggestColumnLabels: vi.fn(),
  updateColumnLabel: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityRules: mocks.listQualityRules,
  listQualityRuleResults: mocks.listQualityRuleResults,
  listQualityIssues: mocks.listQualityIssues,
  listColumnLabels: mocks.listColumnLabels,
  listColumnProfiles: mocks.listColumnProfiles,
  profileColumns: mocks.profileColumns,
  suggestColumnLabels: mocks.suggestColumnLabels,
  updateColumnLabel: mocks.updateColumnLabel,
}));

const TABLE = "inst1;db1;public;orders";
const RULE = "workspaces/ws/qualityRules/rule-1";
const RUN = "workspaces/ws/qualityRuns/run-1";

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
        path: "/data-quality/runs",
        name: "DataQualityRuns",
        component: { template: "<div />" },
      },
      {
        path: "/data-quality/runs/:runId+",
        name: "DataQualityRunDetail",
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
    mocks.listQualityRuleResults.mockResolvedValue({
      items: [
        create(QualityRuleResultSchema, {
          name: "result-1",
          rule: RULE,
          run: RUN,
          status: QualityStatus.FAIL,
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
  });

  it("renders the table's rules, results, issues and column labels", async () => {
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
});
