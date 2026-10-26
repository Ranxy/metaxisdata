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
  QualityStatus,
  QualityTargetOrder,
  QualityTargetPathSchema,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityTargetsPage from "./DataQualityTargetsPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityTargets: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityTargets: mocks.listQualityTargets,
}));

const TARGET_ORDERS = "instance-1;shop;public;orders";
const TARGET_CUSTOMERS = "instance-1;shop;public;customers";

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.create",
];

const Dummy = { template: "<div />" };

/** One directory row as the aggregate read returns it. */
function summary(options: {
  guid: string;
  object: string;
  ruleCount?: number;
  enabledRuleCount?: number;
  pausedRuleCount?: number;
  columnCount?: number;
  openIssueCount?: number;
  pendingDraftCount?: number;
  worstStatus?: QualityStatus;
  lastRunTime?: boolean;
}): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: options.guid,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "instance-1",
        database: "shop",
        schema: "public",
        object: options.object,
      }),
    }),
    ruleCount: options.ruleCount ?? 1,
    enabledRuleCount: options.enabledRuleCount ?? options.ruleCount ?? 1,
    pausedRuleCount: options.pausedRuleCount ?? 0,
    columnCount: options.columnCount ?? 0,
    openIssueCount: options.openIssueCount ?? 0,
    pendingDraftCount: options.pendingDraftCount ?? 0,
    worstStatus: options.worstStatus ?? QualityStatus.UNSPECIFIED,
    lastRunTime:
      options.lastRunTime === false
        ? undefined
        : timestampFromDate(new Date(Date.now() - 5 * 60_000)),
  });
}

function directoryRows() {
  return [
    // Orders watches two columns; one rule has run and passed. One of its two
    // rules is paused, so the row carries the enabled/total hint.
    summary({
      guid: TARGET_ORDERS,
      object: "orders",
      ruleCount: 2,
      enabledRuleCount: 1,
      pausedRuleCount: 1,
      columnCount: 2,
      openIssueCount: 1,
      worstStatus: QualityStatus.PASS,
    }),
    // Customers has one table-level rule whose latest run failed, and a draft.
    summary({
      guid: TARGET_CUSTOMERS,
      object: "customers",
      ruleCount: 1,
      worstStatus: QualityStatus.FAIL,
      pendingDraftCount: 1,
    }),
  ];
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
  mocks.listQualityTargets.mockResolvedValue({
    items: directoryRows(),
    nextPageToken: "",
  });
});

describe("DataQualityTargetsPage directory", () => {
  it("renders one row per target, with its counts and latest result", async () => {
    const wrapper = await mountPage();

    // One aggregate read, in attention order, for the whole directory.
    expect(mocks.listQualityTargets).toHaveBeenCalledWith({
      pageSize: 1000,
      pageToken: "",
      orderBy: QualityTargetOrder.ATTENTION,
    });

    const all = rows(wrapper);
    expect(all).toHaveLength(2);
    // Attention first: the failing target outranks the passing one.
    expect(all[0]?.text()).toContain("customers");
    expect(all[1]?.text()).toContain("orders");

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

  it("reads its counts and column count from the summary", async () => {
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

    // The paused rule narrows the enabled/total hint without hiding the row.
    expect(ordersCells[1]?.text()).toContain("1 / 2");
  });

  it("walks the cursor so a directory past one page stays complete", async () => {
    mocks.listQualityTargets.mockImplementation(
      ({ pageToken }: { pageToken: string }) =>
        Promise.resolve(
          pageToken === ""
            ? { items: [directoryRows()[1]], nextPageToken: "next" }
            : { items: [directoryRows()[0]], nextPageToken: "" }
        )
    );
    const wrapper = await mountPage();

    expect(rows(wrapper)).toHaveLength(2);
    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "next" })
    );
  });

  it("re-sorts by rule count from the sort select", async () => {
    const wrapper = await mountPage();
    expect(rows(wrapper)[0]?.text()).toContain("customers");

    const sort = wrapper.findComponent(Select);
    await sort.vm.$emit("update:modelValue", "rules");
    await nextTick();

    expect(rows(wrapper)[0]?.text()).toContain("orders");
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
  it("guides an empty directory to create its first rule", async () => {
    mocks.listQualityTargets.mockResolvedValue({
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
    mocks.listQualityTargets.mockRejectedValue(new Error("boom"));
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.targets.loadFailed")
    );

    mocks.listQualityTargets.mockResolvedValue({
      items: directoryRows(),
      nextPageToken: "",
    });
    // The header and the error block both offer the retry; either one loads.
    const retry = wrapper
      .findAll("button")
      .filter(
        (button) => button.text().trim() === i18n.global.t("common.refresh")
      );
    expect(retry).toHaveLength(2);
    await retry[1]?.trigger("click");
    await flushPromises();

    expect(rows(wrapper)).toHaveLength(2);
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
