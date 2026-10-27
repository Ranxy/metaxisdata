import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityIssueSchema,
  QualityIssueStatus,
  QualityRuleSchema,
  QualitySeverity,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityIssuesPage from "./DataQualityIssuesPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityIssues: vi.fn(),
  listAllQualityRules: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityIssues: mocks.listQualityIssues,
  listAllQualityRules: mocks.listAllQualityRules,
}));

const PERMISSIONS = [
  "metaxisdata.dataQuality.issues.list",
  "metaxisdata.dataQuality.rules.list",
];

const Dummy = { template: "<div />" };

let activeRouter: ReturnType<typeof createRouter> | undefined;

async function mountPage(path = "/data-quality/issues") {
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
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
    ],
  });
  await router.push(path);
  await router.isReady();
  activeRouter = router;
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const wrapper = mount(DataQualityIssuesPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

/** The query of the current URL, i.e. what a reload or a share would carry. */
function currentQuery(): Record<string, unknown> {
  return (activeRouter?.currentRoute.value.query ?? {}) as Record<
    string,
    unknown
  >;
}

function applyButton(wrapper: ReturnType<typeof mount>) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().trim() === i18n.global.t("common.apply"));
}

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  mocks.listQualityIssues.mockResolvedValue({
    items: [],
    nextPageToken: "",
  });
  mocks.listAllQualityRules.mockResolvedValue([]);
});

describe("DataQualityIssuesPage URL filters", () => {
  it("writes the applied filters to the URL and queries them", async () => {
    const wrapper = await mountPage();
    expect(currentQuery()).toEqual({});

    const selects = wrapper.findAllComponents(Select);
    await selects[0]?.vm.$emit(
      "update:modelValue",
      String(QualityIssueStatus.OPEN)
    );
    await selects[1]?.vm.$emit(
      "update:modelValue",
      String(QualitySeverity.ERROR)
    );
    await nextTick();

    await applyButton(wrapper)?.trigger("click");
    await flushPromises();

    expect(currentQuery()).toEqual({
      status: String(QualityIssueStatus.OPEN),
      severity: String(QualitySeverity.ERROR),
    });
    expect(mocks.listQualityIssues).toHaveBeenLastCalledWith(
      expect.objectContaining({
        status: QualityIssueStatus.OPEN,
        severity: QualitySeverity.ERROR,
      })
    );
    // Applying writes the URL without triggering a second request.
    expect(mocks.listQualityIssues).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("reads the filters from the URL on entry", async () => {
    const wrapper = await mountPage("/data-quality/issues?status=1&severity=2");

    expect(mocks.listQualityIssues).toHaveBeenLastCalledWith(
      expect.objectContaining({
        status: QualityIssueStatus.OPEN,
        severity: QualitySeverity.ERROR,
      })
    );
    const selects = wrapper.findAllComponents(Select);
    expect(selects[0]?.props("modelValue")).toBe(
      String(QualityIssueStatus.OPEN)
    );
    expect(selects[1]?.props("modelValue")).toBe(String(QualitySeverity.ERROR));
    wrapper.unmount();
  });

  it("clears the URL with the filters", async () => {
    const wrapper = await mountPage("/data-quality/issues?status=1");

    const clear = wrapper
      .findAll("button")
      .find(
        (button) =>
          button.text().trim() === i18n.global.t("common.filter.clear")
      );
    expect(clear).toBeTruthy();
    await clear?.trigger("click");
    await flushPromises();

    expect(currentQuery()).toEqual({});
    expect(mocks.listQualityIssues).toHaveBeenLastCalledWith(
      expect.objectContaining({
        status: QualityIssueStatus.UNSPECIFIED,
        severity: QualitySeverity.UNSPECIFIED,
      })
    );
    wrapper.unmount();
  });

  it("adopts a filter change that arrives from outside", async () => {
    const wrapper = await mountPage();
    expect(mocks.listQualityIssues).toHaveBeenCalledTimes(1);

    await activeRouter?.replace({ query: { severity: "2" } });
    await flushPromises();

    expect(mocks.listQualityIssues).toHaveBeenLastCalledWith(
      expect.objectContaining({ severity: QualitySeverity.ERROR })
    );
    expect(wrapper.findAllComponents(Select)[1]?.props("modelValue")).toBe(
      String(QualitySeverity.ERROR)
    );
    expect(mocks.listQualityIssues).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("links the rule and the target into the target workspace", async () => {
    mocks.listQualityIssues.mockResolvedValue({
      items: [issueFixture()],
      nextPageToken: "",
    });
    mocks.listAllQualityRules.mockResolvedValue([
      create(QualityRuleSchema, {
        name: "workspaces/ws/qualityRules/rule-1",
        title: "Null rate on orders",
      }),
    ]);
    const wrapper = await mountPage();

    const rule = wrapper
      .findAll("a")
      .find((link) => link.text().trim() === "Null rate on orders");
    expect(rule).toBeDefined();
    const ruleHref = rule?.attributes("href") ?? "";
    expect(ruleHref).toContain(
      "/data-quality/targets/inst1/shop/public/orders"
    );
    expect(ruleHref).toContain("metaType=4");
    expect(ruleHref).toContain("check=workspaces/ws/qualityRules/rule-1");

    const target = wrapper
      .findAll("a")
      .find(
        (link) =>
          link.text().trim() === "prod · shop · public.orders" &&
          (link.attributes("href") ?? "").startsWith("/data-quality/targets/")
      );
    expect(target).toBeDefined();
    const targetHref = target?.attributes("href") ?? "";
    expect(targetHref).toContain(
      "/data-quality/targets/inst1/shop/public/orders"
    );
    expect(targetHref).toContain("metaType=4");
    expect(targetHref).not.toContain("check=");
    wrapper.unmount();
  });

  it("keeps the issue detail behavior when an issue carries no target", async () => {
    mocks.listQualityIssues.mockResolvedValue({
      items: [issueFixture({ withTarget: false })],
      nextPageToken: "",
    });
    mocks.listAllQualityRules.mockResolvedValue([
      create(QualityRuleSchema, {
        name: "workspaces/ws/qualityRules/rule-1",
        title: "Null rate on orders",
      }),
    ]);
    const wrapper = await mountPage();

    // No target to link to, so the rule stays a button and no target link shows.
    expect(
      wrapper
        .findAll("a")
        .some((link) =>
          (link.attributes("href") ?? "").startsWith("/data-quality/targets/")
        )
    ).toBe(false);
    const ruleButton = wrapper
      .findAll("button")
      .find((button) => button.text().trim() === "Null rate on orders");
    expect(ruleButton).toBeDefined();
    wrapper.unmount();
  });
});

const RULE = "workspaces/ws/qualityRules/rule-1";
const TARGET_GUID = "inst1;shop;public;orders";

function issueFixture(options: { withTarget?: boolean } = {}) {
  return create(QualityIssueSchema, {
    name: "workspaces/ws/qualityIssues/issue-1",
    rule: RULE,
    status: QualityIssueStatus.OPEN,
    severity: QualitySeverity.ERROR,
    target:
      options.withTarget === false
        ? undefined
        : create(QualityTargetSchema, {
            metaGuid: TARGET_GUID,
            metaType: MetaType.TABLE,
            path: create(QualityTargetPathSchema, {
              instance: "prod",
              database: "shop",
              schema: "public",
              object: "orders",
            }),
          }),
  });
}
