import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { nextTick } from "vue";
import { createMemoryHistory, createRouter } from "vue-router";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityExecutionSummarySchema,
  QualityIssueSchema,
  QualityIssueStatus,
  QualityRuleSchema,
  QualityRuleState,
  QualityStatus,
  QualitySuggestionKind,
  QualitySuggestionSchema,
  QualitySuggestionStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityTargetsPage from "./DataQualityTargetsPage.vue";

const mocks = vi.hoisted(() => ({
  listAllQualityRules: vi.fn(),
  listQualityIssues: vi.fn(),
  listQualitySuggestions: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listAllQualityRules: mocks.listAllQualityRules,
  listQualityIssues: mocks.listQualityIssues,
  listQualitySuggestions: mocks.listQualitySuggestions,
}));

const TARGET_ORDERS = "instance-1;shop;public;orders";
const TARGET_CUSTOMERS = "instance-1;shop;public;customers";

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.create",
];

const Dummy = { template: "<div />" };

function target(guid: string, object: string, column = "") {
  return create(QualityTargetSchema, {
    metaGuid: guid,
    metaType: MetaType.TABLE,
    path: create(QualityTargetPathSchema, {
      instance: "instance-1",
      database: "shop",
      schema: "public",
      object,
      column,
    }),
    columns: column ? [column] : [],
  });
}

function rule(options: {
  guid: string;
  object: string;
  column?: string;
  state?: QualityRuleState;
  status?: QualityStatus;
}) {
  return create(QualityRuleSchema, {
    name: `workspaces/ws/qualityRules/${options.guid}-${options.column || "table"}`,
    title: `rule on ${options.object}`,
    target: target(options.guid, options.object, options.column),
    state: options.state ?? QualityRuleState.ENABLED,
    lastResult: options.status
      ? create(QualityExecutionSummarySchema, {
          status: options.status,
          finishTime: timestampFromDate(new Date(Date.now() - 5 * 60_000)),
        })
      : undefined,
  });
}

function issue(guid: string, object: string, status: QualityIssueStatus) {
  return create(QualityIssueSchema, {
    name: `workspaces/ws/qualityIssues/${guid}-${status}`,
    rule: `workspaces/ws/qualityRules/${guid}-id`,
    target: target(guid, object, "id"),
    status,
  });
}

function draft(guid: string, object: string) {
  return create(QualitySuggestionSchema, {
    name: `workspaces/ws/qualitySuggestions/${guid}`,
    kind: QualitySuggestionKind.RULE_DRAFT,
    target: target(guid, object),
    status: QualitySuggestionStatus.PENDING,
  });
}

async function mountPage() {
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
        path: "/data-quality/rules",
        name: "DataQualityRules",
        component: Dummy,
      },
      {
        path: "/data-quality/rules/new",
        name: "DataQualityRuleNew",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/targets");
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const wrapper = mount(DataQualityTargetsPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

/** The table body rows, one per target. */
function rows(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll("tbody tr");
}

function buttonWithLabel(wrapper: ReturnType<typeof mount>, label: string) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().trim() === label);
}

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  mocks.listAllQualityRules.mockResolvedValue([
    // Orders watches two columns; one rule has run and passed.
    rule({
      guid: TARGET_ORDERS,
      object: "orders",
      column: "id",
      status: QualityStatus.PASS,
    }),
    rule({ guid: TARGET_ORDERS, object: "orders", column: "amount" }),
    // Customers has one table-level rule whose latest run failed.
    rule({
      guid: TARGET_CUSTOMERS,
      object: "customers",
      status: QualityStatus.FAIL,
    }),
  ]);
  // The page reads open and acknowledged issues separately; the resolved row
  // rides along in the open page to prove the summarizer still drops it.
  mocks.listQualityIssues.mockImplementation(
    ({ status }: { status: QualityIssueStatus }) =>
      Promise.resolve({
        items:
          status === QualityIssueStatus.OPEN
            ? [
                issue(TARGET_ORDERS, "orders", QualityIssueStatus.OPEN),
                issue(TARGET_ORDERS, "orders", QualityIssueStatus.RESOLVED),
              ]
            : [],
        nextPageToken: "",
      })
  );
  mocks.listQualitySuggestions.mockResolvedValue({
    items: [draft(TARGET_CUSTOMERS, "customers")],
    nextPageToken: "",
  });
});

describe("DataQualityTargetsPage grouping", () => {
  it("renders one row per target, with its counts and latest result", async () => {
    const wrapper = await mountPage();
    const all = rows(wrapper);

    expect(all).toHaveLength(2);
    // Attention first: the failing target outranks the passing one.
    expect(all[0]?.text()).toContain("customers");
    expect(all[1]?.text()).toContain("orders");

    // The two orders rules are one row, not one row each.
    expect(all[0]?.text()).toContain(
      i18n.global.t("dataQuality.targets.rulesCount", { count: 1 })
    );
    expect(all[1]?.text()).toContain(
      i18n.global.t("dataQuality.targets.rulesCount", { count: 2 })
    );
    expect(all[0]?.text()).toContain(i18n.global.t("dataQuality.status.fail"));
    expect(all[1]?.text()).toContain(i18n.global.t("dataQuality.status.pass"));

    // The coarse location and the metadata type are the row's second line.
    expect(all[1]?.text()).toContain("instance-1 · shop");
    expect(all[1]?.text()).toContain(i18n.global.t("metadataBrowser.tables"));
  });

  it("counts only unresolved issues and pending drafts", async () => {
    const wrapper = await mountPage();
    const [customers, orders] = rows(wrapper);

    const customersCells = customers?.findAll("td") ?? [];
    const ordersCells = orders?.findAll("td") ?? [];
    // Columns: target, rules, columns, latest result, open issues, drafts, actions.
    expect(ordersCells[2]?.text()).toBe("2");
    expect(customersCells[2]?.text()).toBe("-");
    expect(ordersCells[4]?.text()).toBe("1");
    expect(customersCells[4]?.text()).toBe("-");
    expect(ordersCells[5]?.text()).toBe("-");
    expect(customersCells[5]?.text()).toBe("1");
  });

  it("re-sorts by rule count from the sort select", async () => {
    const wrapper = await mountPage();
    expect(rows(wrapper)[0]?.text()).toContain("customers");

    const sort = wrapper.findComponent(Select);
    await sort.vm.$emit("update:modelValue", "rules");
    await nextTick();

    expect(rows(wrapper)[0]?.text()).toContain("orders");
  });

  it("keeps the page when the pending-draft block fails", async () => {
    mocks.listQualitySuggestions.mockRejectedValue(
      new Error("suggestions unavailable")
    );
    const wrapper = await mountPage();

    expect(rows(wrapper)).toHaveLength(2);
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.targets.loadFailed")
    );
    // The draft counts degrade to zero rather than inventing a draft.
    expect(rows(wrapper)[0]?.findAll("td")[5]?.text()).toBe("-");
  });
});

describe("DataQualityTargetsPage filters", () => {
  it("filters by the search box on the client", async () => {
    const wrapper = await mountPage();
    const search = wrapper.find("#data-quality-target-search");

    await search.setValue("customers");
    expect(rows(wrapper)).toHaveLength(1);
    expect(rows(wrapper)[0]?.text()).toContain("customers");

    // The instance and the GUID are searchable too, not just the object name.
    await search.setValue("instance-1");
    expect(rows(wrapper)).toHaveLength(2);

    await search.setValue("nothing-matches");
    expect(rows(wrapper)).toHaveLength(0);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.targets.emptyFilteredTitle")
    );
  });

  it("filters by each attention toggle, independently", async () => {
    const wrapper = await mountPage();

    const failing = buttonWithLabel(
      wrapper,
      i18n.global.t("dataQuality.targets.onlyFailing")
    );
    await failing?.trigger("click");
    expect(rows(wrapper)).toHaveLength(1);
    expect(rows(wrapper)[0]?.text()).toContain("customers");
    expect(failing?.attributes("aria-pressed")).toBe("true");

    // Turning it off restores every target.
    await failing?.trigger("click");
    expect(rows(wrapper)).toHaveLength(2);

    const issues = buttonWithLabel(
      wrapper,
      i18n.global.t("dataQuality.targets.onlyIssues")
    );
    await issues?.trigger("click");
    expect(rows(wrapper)).toHaveLength(1);
    expect(rows(wrapper)[0]?.text()).toContain("orders");
    await issues?.trigger("click");

    const drafts = buttonWithLabel(
      wrapper,
      i18n.global.t("dataQuality.targets.onlyDrafts")
    );
    await drafts?.trigger("click");
    expect(rows(wrapper)).toHaveLength(1);
    expect(rows(wrapper)[0]?.text()).toContain("customers");
  });

  it("clears the filters from the filtered empty state", async () => {
    const wrapper = await mountPage();

    await wrapper.find("#data-quality-target-search").setValue("nothing");
    expect(rows(wrapper)).toHaveLength(0);

    const clear = buttonWithLabel(
      wrapper,
      i18n.global.t("dataQuality.targets.clearFilters")
    );
    expect(clear).toBeTruthy();
    await clear?.trigger("click");

    expect(rows(wrapper)).toHaveLength(2);
  });
});

describe("DataQualityTargetsPage states and links", () => {
  it("guides an empty workspace to create its first rule", async () => {
    mocks.listAllQualityRules.mockResolvedValue([]);
    mocks.listQualityIssues.mockResolvedValue({ items: [], nextPageToken: "" });
    mocks.listQualitySuggestions.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    const wrapper = await mountPage();

    expect(rows(wrapper)).toHaveLength(0);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.targets.emptyTitle")
    );
    const action = wrapper
      .findAll("a")
      .find(
        (link) =>
          link.text().trim() ===
          i18n.global.t("dataQuality.targets.emptyAction")
      );
    expect(action?.attributes("href")).toBe("/data-quality/rules/new");
  });

  it("shows the error state and retries the load", async () => {
    mocks.listAllQualityRules.mockRejectedValue(new Error("boom"));
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.targets.loadFailed")
    );

    mocks.listAllQualityRules.mockResolvedValue([
      rule({ guid: TARGET_CUSTOMERS, object: "customers" }),
    ]);
    // The header and the error block both offer the retry; either one loads.
    const retry = wrapper
      .findAll("button")
      .filter(
        (button) => button.text().trim() === i18n.global.t("common.refresh")
      );
    expect(retry).toHaveLength(2);
    await retry[1]?.trigger("click");
    await flushPromises();

    expect(rows(wrapper)).toHaveLength(1);
  });

  it("links every row to its workspace with the GUID and meta type", async () => {
    const wrapper = await mountPage();
    const hrefs = wrapper
      .findAll("a")
      .map((link) => link.attributes("href") ?? "");

    expect(hrefs).toContain(
      "/data-quality/targets/instance-1/shop/public/orders?metaType=4"
    );
    expect(hrefs).toContain(
      "/data-quality/targets/instance-1/shop/public/customers?metaType=4"
    );

    const openLinks = wrapper
      .findAll("a")
      .filter(
        (link) =>
          link.attributes("aria-label") ===
          i18n.global.t("dataQuality.targets.openWorkspace")
      );
    expect(openLinks).toHaveLength(2);
    expect(openLinks[0]?.attributes("href")).toBe(
      "/data-quality/targets/instance-1/shop/public/customers?metaType=4"
    );
  });
});
