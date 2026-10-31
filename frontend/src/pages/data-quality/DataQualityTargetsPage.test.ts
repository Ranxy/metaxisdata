import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
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
  exportQualityTargets: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityTargets: mocks.listQualityTargets,
  exportQualityTargets: mocks.exportQualityTargets,
}));

// jsdom has no object-URL download path; the download itself is covered by the
// CSV helper's own test, so this test only asserts what it was handed.
const csvMocks = vi.hoisted(() => ({ downloadBlob: vi.fn() }));

vi.mock("@/utils/csv", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/utils/csv")>()),
  downloadBlob: csvMocks.downloadBlob,
}));

const TARGET_ORDERS = "instance-1;shop;public;orders";
const TARGET_CUSTOMERS = "instance-1;shop;public;customers";

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.create",
];
const READ_ONLY_PERMISSIONS = ["metaxisdata.dataQuality.rules.list"];

const Dummy = { template: "<div />" };
const TargetPickerStub = {
  name: "TargetPicker",
  props: ["open"],
  template: '<div data-test="target-picker" />',
};

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
  schemaChanged?: boolean;
  orphaned?: boolean;
  qualityScore?: number;
  scoredRuleCount?: number;
  recentPassRate?: number;
  recentExecutionCount?: number;
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
    schemaChanged: options.schemaChanged ?? false,
    orphaned: options.orphaned ?? false,
    qualityScore: options.qualityScore,
    scoredRuleCount: options.scoredRuleCount ?? 0,
    recentPassRate: options.recentPassRate,
    recentExecutionCount: options.recentExecutionCount ?? 0,
  });
}

function directoryRows() {
  return [
    // Server order is authoritative: the failing target comes first.
    summary({
      guid: TARGET_CUSTOMERS,
      object: "customers",
      ruleCount: 1,
      worstStatus: QualityStatus.FAIL,
      pendingDraftCount: 1,
      qualityScore: 40,
      scoredRuleCount: 1,
      recentPassRate: 0.25,
      recentExecutionCount: 20,
    }),
    summary({
      guid: TARGET_ORDERS,
      object: "orders",
      ruleCount: 2,
      enabledRuleCount: 1,
      pausedRuleCount: 1,
      columnCount: 2,
      openIssueCount: 1,
      worstStatus: QualityStatus.PASS,
      qualityScore: 92.5,
      scoredRuleCount: 1,
      recentPassRate: 0.9,
      recentExecutionCount: 10,
    }),
  ];
}

async function mountPage(
  options: { path?: string; permissions?: string[] } = {}
) {
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
        path: "/data-quality/checks",
        name: "DataQualityChecks",
        component: Dummy,
      },
      {
        path: "/metadata",
        name: "MetadataBrowser",
        component: Dummy,
      },
    ],
  });
  await router.push(options.path ?? "/data-quality/targets");
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, {
    permissions: options.permissions ?? PERMISSIONS,
  });
  const wrapper = mount(DataQualityTargetsPage, {
    global: {
      plugins: [router, i18n, pinia],
      stubs: { TargetPicker: TargetPickerStub },
    },
  });
  await flushPromises();
  return { wrapper, router };
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

/** Let the 300ms search debounce fire. */
async function settleSearch() {
  await new Promise((resolve) => setTimeout(resolve, 350));
  await flushPromises();
}

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  mocks.listQualityTargets.mockResolvedValue({
    items: directoryRows(),
    nextPageToken: "",
  });
  mocks.exportQualityTargets.mockResolvedValue({
    blob: new Blob([]),
    rowCount: 2n,
  });
});

describe("DataQualityTargetsPage export", () => {
  /** The header's CSV action, found by its label. */
  function exportButton(wrapper: ReturnType<typeof mount>) {
    return wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("dataQuality.exportCsv"))
      );
  }

  it("exports every target the directory filters select", async () => {
    const { wrapper } = await mountPage({
      path: "/data-quality/targets?q=orders&issues=1&failing=1",
    });

    await exportButton(wrapper)?.trigger("click");
    await flushPromises();

    // The failing switch is its own flag; the export never sets worstStatus.
    expect(mocks.exportQualityTargets).toHaveBeenCalledWith({
      search: "orders",
      failingOnly: true,
      hasOpenIssue: true,
      hasPendingDraft: false,
      orderBy: QualityTargetOrder.ATTENTION,
      signal: expect.any(AbortSignal),
    });
    expect(csvMocks.downloadBlob).toHaveBeenCalledTimes(1);
    expect(csvMocks.downloadBlob.mock.calls[0]?.[1]).toMatch(
      /^quality-targets-\d{8}-\d{6}\.csv$/
    );
  });

  it("exports the applied search, not the text still being typed", async () => {
    const { wrapper } = await mountPage();
    await exportButton(wrapper)?.trigger("click");
    await flushPromises();
    expect(mocks.exportQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "" })
    );

    // Typing alone does not change what the directory (or its export) selects;
    // the debounce applies the term once the reader pauses.
    await wrapper.find("#data-quality-target-search").setValue("orders");
    await exportButton(wrapper)?.trigger("click");
    await flushPromises();
    expect(mocks.exportQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "" })
    );

    await settleSearch();
    await exportButton(wrapper)?.trigger("click");
    await flushPromises();
    expect(mocks.exportQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "orders" })
    );
  });
});

describe("DataQualityTargetsPage directory", () => {
  it("asks the server for the whole directory and renders its order", async () => {
    const { wrapper } = await mountPage();

    // One aggregate read; no cursor walk and no client sort.
    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);
    expect(mocks.listQualityTargets).toHaveBeenCalledWith({
      pageSize: 1000,
      search: "",
      hasOpenIssue: false,
      hasPendingDraft: false,
      orderBy: QualityTargetOrder.ATTENTION,
    });

    const all = rows(wrapper);
    expect(all).toHaveLength(2);
    // The rows keep the server's attention order.
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
    const { wrapper } = await mountPage();
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

  it("shows the quality score and recent pass rate beside the latest result", async () => {
    const { wrapper } = await mountPage();
    const [customers, orders] = rows(wrapper);

    const customersResult = customers?.findAll("td")[3];
    expect(customersResult?.text()).toContain(
      i18n.global.t("dataQuality.targets.scoreLine", {
        score: "40.0",
        passRate: "25.0%",
      })
    );
    const ordersResult = orders?.findAll("td")[3];
    expect(ordersResult?.text()).toContain(
      i18n.global.t("dataQuality.targets.scoreLine", {
        score: "92.5",
        passRate: "90.0%",
      })
    );
    // The pass rate's window (the last N completed executions) is the tooltip.
    expect(
      ordersResult
        ?.find(
          `[title="${i18n.global.t("dataQuality.targets.scoreLineHint", { count: 10 })}"]`
        )
        .exists()
    ).toBe(true);
  });

  it("says '-' for a target whose checks were never judged", async () => {
    mocks.listQualityTargets.mockResolvedValue({
      items: [
        summary({
          guid: TARGET_ORDERS,
          object: "orders",
          lastRunTime: false,
        }),
      ],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();

    const result = rows(wrapper)[0]?.findAll("td")[3];
    expect(result?.text()).toContain(
      i18n.global.t("dataQuality.targets.scoreLine", {
        score: "-",
        passRate: "-",
      })
    );
  });

  it("shows the schema-changed and orphaned badges on a row", async () => {
    mocks.listQualityTargets.mockResolvedValue({
      items: [
        summary({
          guid: TARGET_CUSTOMERS,
          object: "customers",
          schemaChanged: true,
        }),
        summary({
          guid: TARGET_ORDERS,
          object: "orders",
          orphaned: true,
        }),
      ],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();
    const [customers, orders] = rows(wrapper);

    expect(customers?.text()).toContain(
      i18n.global.t("dataQuality.targetChanged")
    );
    expect(orders?.text()).toContain(i18n.global.t("dataQuality.orphaned"));
    expect(
      orders
        ?.find(
          `[title="${i18n.global.t("dataQuality.workspace.orphanedHint")}"]`
        )
        .exists()
    ).toBe(true);
  });
});

describe("DataQualityTargetsPage server filters", () => {
  it("reads the filters from the URL and sends them on the first request", async () => {
    const { wrapper } = await mountPage({
      path: "/data-quality/targets?q=orders&issues=1&drafts=1&sort=rules",
    });

    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);
    expect(mocks.listQualityTargets).toHaveBeenCalledWith({
      pageSize: 1000,
      search: "orders",
      hasOpenIssue: true,
      hasPendingDraft: true,
      orderBy: QualityTargetOrder.RULE_COUNT,
    });
    expect(
      (wrapper.find("#data-quality-target-search").element as HTMLInputElement)
        .value
    ).toBe("orders");
    expect(
      buttonWithLabel(
        wrapper,
        i18n.global.t("dataQuality.targets.onlyIssues")
      )?.attributes("aria-pressed")
    ).toBe("true");
    expect(
      buttonWithLabel(
        wrapper,
        i18n.global.t("dataQuality.targets.onlyDrafts")
      )?.attributes("aria-pressed")
    ).toBe("true");
    expect(wrapper.findComponent(Select).props("modelValue")).toBe("rules");
  });

  it("re-queries once the search stops changing", async () => {
    const { wrapper, router } = await mountPage();
    const search = wrapper.find("#data-quality-target-search");

    await search.setValue("customers");
    // Typing alone does not hit the server; the debounce does.
    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);

    await settleSearch();

    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "customers" })
    );
    expect(router.currentRoute.value.query).toEqual({ q: "customers" });
  });

  it("writes the issue, draft and sort changes to the URL", async () => {
    const { wrapper, router } = await mountPage({
      path: "/data-quality/targets?keep=1",
    });

    await buttonWithLabel(
      wrapper,
      i18n.global.t("dataQuality.targets.onlyIssues")
    )?.trigger("click");
    await flushPromises();
    expect(mocks.listQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ hasOpenIssue: true })
    );
    // A parameter this page does not own survives the rewrite.
    expect(router.currentRoute.value.query).toEqual({
      keep: "1",
      issues: "1",
    });

    await buttonWithLabel(
      wrapper,
      i18n.global.t("dataQuality.targets.onlyDrafts")
    )?.trigger("click");
    await flushPromises();
    expect(mocks.listQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ hasPendingDraft: true })
    );

    await wrapper
      .findComponent(Select)
      .vm.$emit("update:modelValue", "lastRun");
    await flushPromises();
    expect(mocks.listQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ orderBy: QualityTargetOrder.LAST_RUN })
    );
    expect(router.currentRoute.value.query).toMatchObject({
      keep: "1",
      issues: "1",
      drafts: "1",
      sort: "lastRun",
    });
  });

  it("filters failing on the client without another request", async () => {
    const { wrapper, router } = await mountPage();
    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);

    const failing = buttonWithLabel(
      wrapper,
      i18n.global.t("dataQuality.targets.onlyFailing")
    );
    await failing?.trigger("click");
    await flushPromises();

    // FAIL *or* ERROR cannot be one API filter; the rows are narrowed in place.
    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);
    expect(rows(wrapper)).toHaveLength(1);
    expect(rows(wrapper)[0]?.text()).toContain("customers");
    expect(failing?.attributes("aria-pressed")).toBe("true");
    expect(router.currentRoute.value.query).toEqual({ failing: "1" });

    await failing?.trigger("click");
    await flushPromises();
    expect(rows(wrapper)).toHaveLength(2);
    expect(router.currentRoute.value.query).toEqual({});
  });

  it("clears the filters and the URL from the filtered empty state", async () => {
    mocks.listQualityTargets.mockImplementation(
      (options: { search?: string }) =>
        Promise.resolve({
          items: options.search === "nothing" ? [] : directoryRows(),
          nextPageToken: "",
        })
    );
    const { wrapper, router } = await mountPage();

    await wrapper.find("#data-quality-target-search").setValue("nothing");
    await settleSearch();
    expect(rows(wrapper)).toHaveLength(0);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.targets.emptyFilteredTitle")
    );

    const clear = buttonWithLabel(
      wrapper,
      i18n.global.t("dataQuality.targets.clearFilters")
    );
    expect(clear).toBeTruthy();
    await clear?.trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.query).toEqual({});
    expect(mocks.listQualityTargets).toHaveBeenLastCalledWith({
      pageSize: 1000,
      search: "",
      hasOpenIssue: false,
      hasPendingDraft: false,
      orderBy: QualityTargetOrder.ATTENTION,
    });
    expect(rows(wrapper)).toHaveLength(2);
  });
});

describe("DataQualityTargetsPage states and links", () => {
  it("starts monitoring from the empty directory", async () => {
    mocks.listQualityTargets.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();

    expect(rows(wrapper)).toHaveLength(0);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.targets.emptyTitle")
    );
    const action = buttonWithLabel(
      wrapper,
      i18n.global.t("dataQuality.targets.startMonitoring")
    );
    expect(action).toBeTruthy();
    await action?.trigger("click");
    expect(wrapper.findComponent(TargetPickerStub).props("open")).toBe(true);
  });

  it("hides start monitoring and browses metadata without the create permission", async () => {
    mocks.listQualityTargets.mockResolvedValue({
      items: [],
      nextPageToken: "",
    });
    const { wrapper, router } = await mountPage({
      path: "/data-quality/targets?new=1",
      permissions: READ_ONLY_PERMISSIONS,
    });

    expect(
      buttonWithLabel(
        wrapper,
        i18n.global.t("dataQuality.targets.startMonitoring")
      )
    ).toBeUndefined();
    expect(wrapper.findComponent(TargetPickerStub).props("open")).toBe(false);
    // `new=1` is consumed even when it cannot be honored.
    expect(router.currentRoute.value.query.new).toBeUndefined();

    const browse = wrapper
      .findAll("a")
      .find(
        (link) =>
          link.text().trim() ===
          i18n.global.t("dataQuality.targets.browseMetadata")
      );
    expect(browse?.attributes("href")).toBe("/metadata");
    // The header still offers refresh and the flat checks list.
    expect(
      buttonWithLabel(wrapper, i18n.global.t("common.refresh"))
    ).toBeTruthy();
    expect(
      wrapper
        .findAll("a")
        .some(
          (link) =>
            link.text().trim() === i18n.global.t("dataQuality.targets.allRules")
        )
    ).toBe(true);
  });

  it("opens the picker from new=1 and keeps the rest of the query", async () => {
    const { wrapper, router } = await mountPage({
      path: "/data-quality/targets?new=1&q=orders",
    });

    expect(wrapper.findComponent(TargetPickerStub).props("open")).toBe(true);
    expect(router.currentRoute.value.query).toEqual({ q: "orders" });
    // The flag is not a filter: the read applies only what remains.
    expect(mocks.listQualityTargets).toHaveBeenCalledWith(
      expect.objectContaining({ search: "orders" })
    );
  });

  it("opens the picked target's workspace ready to add its first check", async () => {
    const { wrapper } = await mountPage();
    const picker = wrapper.findComponent(TargetPickerStub);
    picker.vm.$emit("select", {
      guid: TARGET_ORDERS,
      metaType: MetaType.TABLE,
      path: {
        instance: "instance-1",
        database: "shop",
        schema: "public",
        object: "orders",
      },
      columns: [],
    });
    await flushPromises();
    expect(wrapper.vm.$route.name).toBe("DataQualityTarget");
    expect(wrapper.vm.$route.query).toMatchObject({
      metaType: String(MetaType.TABLE),
      add: "1",
    });
  });

  it("shows the error state and retries the load", async () => {
    mocks.listQualityTargets.mockRejectedValue(new Error("boom"));
    const { wrapper } = await mountPage();

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
    const { wrapper } = await mountPage();
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
