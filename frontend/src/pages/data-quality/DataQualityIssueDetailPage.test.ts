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
} from "@/types/proto-es/v1/data_quality_service_pb";
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
const ACKNOWLEDGED_BY = "users/9";
const Dummy = { template: "<div />" };

const PERMISSIONS = [
  "metaxisdata.dataQuality.issues.get",
  "metaxisdata.dataQuality.rules.get",
];

function issueFixture(acknowledgedBy: string) {
  return create(QualityIssueSchema, {
    name: ISSUE,
    rule: RULE,
    status: QualityIssueStatus.ACKNOWLEDGED,
    severity: QualitySeverity.WARNING,
    acknowledgedBy,
    occurrenceCount: 1,
  });
}

async function mountPage(permissions: string[]) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
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
        path: "/data-quality/runs/:runId+",
        name: "DataQualityRunDetail",
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
  return wrapper;
}

describe("DataQualityIssueDetailPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getQualityIssue.mockResolvedValue(issueFixture(ACKNOWLEDGED_BY));
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

  it("shows the acknowledging user's display name when the caller may read users", async () => {
    const wrapper = await mountPage([...PERMISSIONS, "metaxisdata.users.get"]);

    expect(mocks.getUser).toHaveBeenCalledWith(ACKNOWLEDGED_BY);
    expect(wrapper.text()).toContain("Ada Lovelace");
    expect(wrapper.text()).not.toContain("9");
  });

  it("falls back to the trailing id without the user read permission", async () => {
    const wrapper = await mountPage(PERMISSIONS);

    expect(mocks.getUser).not.toHaveBeenCalled();
    expect(wrapper.text()).toContain("9");
    expect(wrapper.text()).not.toContain("Ada Lovelace");
  });

  it("keeps the id, silently, when the user lookup fails", async () => {
    mocks.getUser.mockRejectedValue(new Error("permission denied"));

    const wrapper = await mountPage([...PERMISSIONS, "metaxisdata.users.get"]);

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
    const wrapper = await mountPage([...PERMISSIONS, "metaxisdata.users.get"]);
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
