import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import {
  QualityRuleSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { guidToRouteParams } from "@/utils/guid";
import RedirectQualityRulePage from "./RedirectQualityRulePage.vue";

const mocks = vi.hoisted(() => ({
  getQualityRule: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityRule: mocks.getQualityRule,
}));

const RULE = "workspaces/ws/qualityRules/rule-1";
const TARGET = "inst1;db1;public;orders";
const Dummy = { template: "<div />" };

function ruleFixture(target: boolean = true) {
  return create(QualityRuleSchema, {
    name: RULE,
    title: "Orders should have rows",
    target: target
      ? create(QualityTargetSchema, {
          metaGuid: TARGET,
          metaType: MetaType.TABLE,
        })
      : undefined,
  });
}

async function mountRedirect(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/rules/:ruleId+/edit",
        name: "DataQualityRuleEditRedirect",
        component: RedirectQualityRulePage,
      },
      {
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleRedirect",
        component: RedirectQualityRulePage,
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
    ],
  });
  await router.push(path);
  await router.isReady();
  const wrapper = mount(RedirectQualityRulePage, {
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return { wrapper, router };
}

describe("RedirectQualityRulePage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getQualityRule.mockResolvedValue(ruleFixture());
  });

  it("lands the old check URL on its target workspace with the sheet selected", async () => {
    const { wrapper, router } = await mountRedirect(
      `/data-quality/rules/${RULE.split("/").join("/")}`
    );

    expect(router.currentRoute.value.name).toBe("DataQualityTarget");
    expect(router.currentRoute.value.params.targetGuid).toEqual(
      guidToRouteParams(TARGET)
    );
    expect(router.currentRoute.value.query).toMatchObject({
      metaType: String(MetaType.TABLE),
      check: RULE,
    });
    // The plain page was not an edit request.
    expect(router.currentRoute.value.query.edit).toBeUndefined();
    wrapper.unmount();
  });

  it("keeps the edit intent of the old edit URL", async () => {
    const { wrapper, router } = await mountRedirect(
      `/data-quality/rules/${RULE}/edit`
    );

    expect(router.currentRoute.value.name).toBe("DataQualityTarget");
    expect(router.currentRoute.value.query).toMatchObject({
      metaType: String(MetaType.TABLE),
      check: RULE,
      edit: "1",
    });
    wrapper.unmount();
  });

  it("falls back to the checks directory for a missing check", async () => {
    mocks.getQualityRule.mockRejectedValue(
      new ConnectError("gone", Code.NotFound)
    );
    const { wrapper, router } = await mountRedirect(
      `/data-quality/rules/${RULE}`
    );

    expect(router.currentRoute.value.name).toBe("DataQualityChecks");
    wrapper.unmount();
  });

  it("falls back to the checks directory for a check without a target", async () => {
    mocks.getQualityRule.mockResolvedValue(ruleFixture(false));
    const { wrapper, router } = await mountRedirect(
      `/data-quality/rules/${RULE}/edit`
    );

    expect(router.currentRoute.value.name).toBe("DataQualityChecks");
    wrapper.unmount();
  });
});
