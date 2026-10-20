import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityRuleResultSchema,
  QualityRuleSchema,
  QualityRunCountsSchema,
  QualityRunSchema,
  QualityRunStatus,
  QualityStatus,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityRunDetailPage from "./DataQualityRunDetailPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityRun: vi.fn(),
  listAllQualityRuleResults: vi.fn(),
  listAllQualityRules: vi.fn(),
  getWorkspaceProfileSetting: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityRun: mocks.getQualityRun,
  listAllQualityRuleResults: mocks.listAllQualityRuleResults,
  listAllQualityRules: mocks.listAllQualityRules,
}));

vi.mock("@/api/setting", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/setting")>()),
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
}));

const RUN = "workspaces/ws/qualityRuns/run-1";
const RULE = "workspaces/ws/qualityRules/rule-1";
const START = new Date("2026-10-07T09:30:00Z");
const Dummy = { template: "<div />" };

const PERMISSIONS = [
  "metaxisdata.dataQuality.runs.get",
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.update",
];

async function mountPage(permissions: string[]) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: Dummy,
      },
      { path: "/data-quality/runs", name: "DataQualityRuns", component: Dummy },
      {
        path: "/data-quality/runs/:runId+",
        name: "DataQualityRunDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+/edit",
        name: "DataQualityRuleEdit",
        component: Dummy,
      },
    ],
  });
  await router.push(`/data-quality/runs/${RUN}`);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualityRunDetailPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("DataQualityRunDetailPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getQualityRun.mockResolvedValue(
      create(QualityRunSchema, {
        name: RUN,
        status: QualityRunStatus.COMPLETED,
        outcome: QualityStatus.FAIL,
        instance: "instances/inst-1",
        startTime: timestampFromDate(START),
        finishTime: timestampFromDate(new Date(START.getTime() + 3000)),
        counts: create(QualityRunCountsSchema, { total: 1, fail: 1 }),
      })
    );
    mocks.listAllQualityRuleResults.mockResolvedValue([
      create(QualityRuleResultSchema, {
        name: "workspaces/ws/qualityRuleResults/1",
        run: RUN,
        rule: RULE,
        status: QualityStatus.FAIL,
      }),
    ]);
    mocks.listAllQualityRules.mockResolvedValue([
      create(QualityRuleSchema, {
        name: RULE,
        title: "Orders should have rows",
      }),
    ]);
    mocks.getWorkspaceProfileSetting.mockResolvedValue({});
  });

  it("links a result's rule to the rule editor", async () => {
    const wrapper = await mountPage(PERMISSIONS);

    const link = wrapper.find(
      'a[href="/data-quality/rules/workspaces/ws/qualityRules/rule-1/edit"]'
    );
    expect(link.exists()).toBe(true);
    expect(link.text()).toBe("Orders should have rows");
  });

  it("shows the rule name without a link when the caller cannot edit it", async () => {
    const wrapper = await mountPage([
      "metaxisdata.dataQuality.runs.get",
      "metaxisdata.dataQuality.rules.list",
    ]);

    expect(wrapper.text()).toContain("Orders should have rows");
    expect(wrapper.find('a[href$="/edit"]').exists()).toBe(false);
  });
});
