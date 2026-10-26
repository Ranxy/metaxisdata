import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityIssueSchema,
  QualityIssueStatus,
  QualityRuleSchema,
  QualitySeverity,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { type User, UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityIssueDetailPage from "./DataQualityIssueDetailPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityIssue: vi.fn(),
  getQualityRule: vi.fn(),
  getUser: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityIssue: mocks.getQualityIssue,
  getQualityRule: mocks.getQualityRule,
}));

vi.mock("@/api/user", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/user")>()),
  getUser: mocks.getUser,
}));

const ISSUE = "workspaces/ws/qualityIssues/issue-1";
const RULE = "workspaces/ws/qualityRules/rule-1";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const ACKNOWLEDGED_BY = "users/9";
const Dummy = { template: "<div />" };

const PERMISSIONS = [
  "metaxisdata.dataQuality.issues.get",
  "metaxisdata.dataQuality.rules.get",
  "metaxisdata.dataQuality.executions.get",
];

function issueFixture(
  acknowledgedBy: string,
  target: { guid?: string; metaType?: MetaType } = {}
) {
  return create(QualityIssueSchema, {
    name: ISSUE,
    rule: RULE,
    status: QualityIssueStatus.ACKNOWLEDGED,
    severity: QualitySeverity.WARNING,
    acknowledgedBy,
    occurrenceCount: 1,
    firstSeenExecution: EXECUTION,
    lastSeenExecution: EXECUTION,
    // An issue carries the target identity (GUID and type), not a path
    // snapshot, because the path can change while the issue stays open.
    target: create(QualityTargetSchema, {
      metaGuid: target.guid ?? "",
      metaType: target.metaType ?? MetaType.TABLE,
    }),
  });
}

async function mountPage(permissions: string[]) {
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
        path: "/data-quality/issues",
        name: "DataQualityIssues",
        component: Dummy,
      },
      {
        path: "/data-quality/issues/:issueId+",
        name: "DataQualityIssueDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleDetail",
        component: Dummy,
      },
      {
        path: "/metadata/:guid+",
        name: "MetadataDetail",
        component: Dummy,
      },
    ],
  });
  await router.push(`/data-quality/issues/${ISSUE}`);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualityIssueDetailPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

describe("DataQualityIssueDetailPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getQualityIssue.mockResolvedValue(
      issueFixture(ACKNOWLEDGED_BY, {
        guid: "instance_1;shop;public;orders",
      })
    );
    mocks.getQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: RULE,
        title: "Orders should have rows",
      })
    );
    mocks.getUser.mockResolvedValue(
      create(UserSchema, {
        name: ACKNOWLEDGED_BY,
        title: "Ada Lovelace",
        email: "ada@example.com",
      })
    );
  });

  it("links the first and last seen executions", async () => {
    const { wrapper } = await mountPage(PERMISSIONS);

    const links = wrapper.findAll("a");
    expect(
      links.some((link) =>
        link.attributes("href")?.includes("qualityExecutions")
      )
    ).toBe(true);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.viewExecution")
    );
  });

  it("links the target to its metadata page", async () => {
    const { wrapper, router } = await mountPage(PERMISSIONS);

    const link = wrapper.find(
      `a[title="${i18n.global.t("dataQuality.viewMetadata")}"]`
    );
    expect(link.exists()).toBe(true);
    expect(link.text()).toContain("instance_1;shop;public;orders");
    expect(link.attributes("href")).toBe(
      "/metadata/instance_1/shop/public/orders?metaType=4"
    );

    await link.trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.name).toBe("MetadataDetail");
    expect(router.currentRoute.value.params.guid).toEqual([
      "instance_1",
      "shop",
      "public",
      "orders",
    ]);
  });

  it("keeps the target as plain text without a usable meta type", async () => {
    mocks.getQualityIssue.mockResolvedValue(
      issueFixture(ACKNOWLEDGED_BY, {
        guid: "instance_1;shop;public;orders",
        metaType: MetaType.UNSPECIFIED,
      })
    );

    const { wrapper } = await mountPage(PERMISSIONS);

    const viewMetadata = i18n.global.t("dataQuality.viewMetadata");
    expect(wrapper.findAll(`a[title="${viewMetadata}"]`)).toHaveLength(0);
    expect(wrapper.text()).toContain("instance_1;shop;public;orders");
  });

  it("shows the acknowledging user's display name when the caller may read users", async () => {
    const { wrapper } = await mountPage([
      ...PERMISSIONS,
      "metaxisdata.users.get",
    ]);

    expect(mocks.getUser).toHaveBeenCalledWith(ACKNOWLEDGED_BY);
    expect(wrapper.text()).toContain("Ada Lovelace");
    expect(wrapper.text()).not.toContain("9");
  });

  it("falls back to the trailing id without the user read permission", async () => {
    const { wrapper } = await mountPage(PERMISSIONS);

    expect(mocks.getUser).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("9");
    expect(wrapper.text()).not.toContain("Ada Lovelace");
  });

  it("keeps the id, silently, when the user lookup fails", async () => {
    mocks.getUser.mockRejectedValue(new Error("permission denied"));

    const { wrapper } = await mountPage([
      ...PERMISSIONS,
      "metaxisdata.users.get",
    ]);

    expect(wrapper.text()).toContain("9");
    expect(wrapper.text()).not.toContain("Ada Lovelace");
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.issueActionFailed")
    );
  });

  it("ignores a stale lookup that resolves after a newer issue is shown", async () => {
    let resolveStale: ((user: User) => void) | undefined;
    mocks.getUser.mockImplementation((name: string) => {
      if (name === ACKNOWLEDGED_BY) {
        return new Promise<User>((resolve) => {
          resolveStale = resolve;
        });
      }
      return Promise.resolve(
        create(UserSchema, { name, title: "Grace Hopper" })
      );
    });
    const { wrapper } = await mountPage([
      ...PERMISSIONS,
      "metaxisdata.users.get",
    ]);
    expect(wrapper.text()).toContain("9");

    mocks.getQualityIssue.mockResolvedValue(issueFixture("users/10"));
    const refresh = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("common.refresh"))
      );
    expect(refresh).toBeTruthy();
    await refresh?.trigger("click");
    await flushPromises();
    expect(wrapper.text()).toContain("Grace Hopper");

    resolveStale?.(
      create(UserSchema, { name: ACKNOWLEDGED_BY, title: "Ada Lovelace" })
    );
    await flushPromises();

    expect(wrapper.text()).toContain("Grace Hopper");
    expect(wrapper.text()).not.toContain("Ada Lovelace");
  });
});
