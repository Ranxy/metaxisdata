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
  type QualityExecution,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityObservedKind,
  QualityRuleSchema,
  type QualityRun,
  QualityRunSchema,
  QualityStatus,
  QualityStatusCountsSchema,
  QualityTargetOrder,
  QualityTargetPathSchema,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityExecutionsPage from "./DataQualityExecutionsPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityTargets: vi.fn(),
  listQualityExecutions: vi.fn(),
  listQualityRuns: vi.fn(),
  listAllQualityRules: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityTargets: mocks.listQualityTargets,
  listQualityExecutions: mocks.listQualityExecutions,
  listQualityRuns: mocks.listQualityRuns,
  listAllQualityRules: mocks.listAllQualityRules,
}));

const TARGET_ORDERS = "inst1;shop;public;orders";
const TARGET_CUSTOMERS = "inst1;shop;public;customers";
const RUN = "workspaces/ws/qualityRuns/run-1";
const RULE = "workspaces/ws/qualityRules/rule-1";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const START = new Date("2026-10-07T09:30:00Z");
const FINISH = new Date(START.getTime() + 2_000);
const Dummy = { template: "<div />" };
const labels = () => i18n.global.t;

const PERMISSIONS = [
  "metaxisdata.dataQuality.executions.list",
  "metaxisdata.dataQuality.executions.get",
  "metaxisdata.dataQuality.rules.get",
];

function page<T>(items: T[]) {
  return { items, nextPageToken: "" };
}

function targetFor(guid: string, object: string) {
  return create(QualityTargetSchema, {
    metaGuid: guid,
    metaType: MetaType.TABLE,
    path: create(QualityTargetPathSchema, {
      instance: "prod",
      database: "shop",
      schema: "public",
      object,
    }),
  });
}

/** The latest batch of one target: counts and revisions live on the run. */
function runFor(
  guid: string,
  object: string,
  options: {
    counts?: Record<string, number>;
    outcome?: QualityStatus;
    state?: QualityExecutionState;
    trigger?: QualityExecutionTrigger;
  } = {}
): QualityRun {
  return create(QualityRunSchema, {
    name: RUN,
    instance: "instances/prod",
    trigger: options.trigger ?? QualityExecutionTrigger.MANUAL,
    state: options.state ?? QualityExecutionState.COMPLETED,
    outcome: options.outcome ?? QualityStatus.FAIL,
    counts: create(QualityStatusCountsSchema, {
      total: 2,
      pass: 1,
      fail: 1,
      ...options.counts,
    }),
    startTime: timestampFromDate(START),
    finishTime: timestampFromDate(FINISH),
    target: targetFor(guid, object),
    ruleRevisionMin: 3,
    ruleRevisionMax: 3,
  });
}

function runFixture(targetGuid: string): QualityRun {
  const object = targetGuid === TARGET_ORDERS ? "orders" : "customers";
  return runFor(targetGuid, object, {
    counts: { total: 9, pass: 5, fail: 4 },
  });
}

function targetSummary(options: {
  guid: string;
  object: string;
  latestRun?: QualityTargetSummary["latestRun"];
  runCount?: number;
  orphaned?: boolean;
  ruleCount?: number;
}): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: targetFor(options.guid, options.object),
    ruleCount: options.ruleCount ?? 2,
    enabledRuleCount: options.ruleCount ?? 2,
    pausedRuleCount: 0,
    columnCount: 1,
    openIssueCount: 0,
    pendingDraftCount: 0,
    pendingLabelCount: 0,
    worstStatus: QualityStatus.FAIL,
    lastRunTime: timestampFromDate(FINISH),
    schemaChanged: false,
    orphaned: options.orphaned ?? false,
    latestRun: options.latestRun,
    runCount: options.runCount ?? 1,
  });
}

/** The directory as the aggregate read returns it, newest scan first. */
function directoryRows(): QualityTargetSummary[] {
  return [
    targetSummary({
      guid: TARGET_CUSTOMERS,
      object: "customers",
      latestRun: runFor(TARGET_CUSTOMERS, "customers", {
        counts: { total: 2, pass: 1, fail: 1 },
        outcome: QualityStatus.FAIL,
      }),
      runCount: 3,
    }),
    targetSummary({
      guid: TARGET_ORDERS,
      object: "orders",
      latestRun: runFor(TARGET_ORDERS, "orders", {
        counts: { total: 3, pass: 2, error: 1, fail: 0 },
        outcome: QualityStatus.ERROR,
      }),
      runCount: 1,
    }),
  ];
}

function executionFixture(
  overrides: Record<string, unknown> = {}
): QualityExecution {
  return create(QualityExecutionSchema, {
    name: EXECUTION,
    rule: RULE,
    state: QualityExecutionState.COMPLETED,
    outcome: QualityStatus.FAIL,
    trigger: QualityExecutionTrigger.MANUAL,
    target: targetFor(TARGET_ORDERS, "orders"),
    observedValue: 0.04,
    observedKind: QualityObservedKind.RATIO,
    failedRows: 40n,
    totalRows: 1000n,
    startTime: timestampFromDate(START),
    finishTime: timestampFromDate(FINISH),
    ...overrides,
  });
}

async function mountPage(path = "/data-quality/executions") {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/executions",
        name: "DataQualityExecutions",
        component: Dummy,
      },
      {
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: Dummy,
      },
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
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleRedirect",
        component: Dummy,
      },
    ],
  });
  await router.push(path);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: PERMISSIONS });
  const wrapper = mount(DataQualityExecutionsPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

function rows(wrapper: ReturnType<typeof mount>) {
  return wrapper.findAll("tbody tr");
}

function expanderButton(row: ReturnType<typeof rows>[number]) {
  return row.find(
    `button[aria-label="${labels()("dataQuality.executions.historyTitle")}"]`
  );
}

function viewButton(wrapper: ReturnType<typeof mount>, text: string) {
  return wrapper
    .findAll("button")
    .find((button) => button.text().trim() === text);
}

/** Let the 300ms search debounce fire. */
async function settleSearch() {
  await new Promise((resolve) => setTimeout(resolve, 350));
  await flushPromises();
}

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  mocks.listQualityTargets.mockResolvedValue(page(directoryRows()));
  mocks.listQualityExecutions.mockResolvedValue(page([executionFixture()]));
  mocks.listQualityRuns.mockImplementation((options: { targetGuid: string }) =>
    Promise.resolve(page([runFixture(options.targetGuid)]))
  );
  mocks.listAllQualityRules.mockResolvedValue([
    create(QualityRuleSchema, { name: RULE, title: "Null rate on orders.id" }),
  ]);
});

describe("DataQualityExecutionsPage by target", () => {
  it("defaults to the target directory with each target's latest scan", async () => {
    const { wrapper } = await mountPage();

    expect(mocks.listQualityTargets).toHaveBeenCalledWith(
      expect.objectContaining({
        pageSize: 50,
        pageToken: "",
        search: "",
        orderBy: QualityTargetOrder.LAST_RUN,
      })
    );
    // The flat list is not fetched at all in this view.
    expect(mocks.listQualityExecutions).not.toHaveBeenCalled();
    expect(mocks.listQualityRuns).not.toHaveBeenCalled();

    const all = rows(wrapper);
    expect(all).toHaveLength(2);
    expect(all[0]?.text()).toContain("customers");
    expect(all[1]?.text()).toContain("orders");

    // The latest-scan cell carries the trigger; the result cell the verdict.
    expect(all[0]?.text()).toContain(
      labels()("dataQuality.executionTrigger.manual")
    );
    expect(all[0]?.text()).toContain(labels()("dataQuality.status.fail"));
    expect(all[1]?.text()).toContain(labels()("dataQuality.status.error"));
    // Compact counts come from that target's own batch.
    expect(all[1]?.text()).toContain(
      `2 ${labels()("dataQuality.status.pass")}`
    );
    expect(all[0]?.text()).toContain(
      labels()("dataQuality.executions.runCountValue", { count: 3 })
    );

    const customersCells = all[0]?.findAll("td") ?? [];
    // Columns: target, latest scan, batch result, duration, failed checks, scans, expander.
    expect(customersCells[4]?.text()).toBe("1");
    expect(customersCells[3]?.text()).toBe("2 s");
    expect(customersCells[5]?.text()).toBe(
      labels()("dataQuality.executions.runCountValue", { count: 3 })
    );
  });

  it("expands a target's scan history and writes it to the URL", async () => {
    const { wrapper, router } = await mountPage();

    await expanderButton(rows(wrapper)[0]).trigger("click");
    await flushPromises();

    expect(mocks.listQualityRuns).toHaveBeenCalledWith({
      targetGuid: TARGET_CUSTOMERS,
      metaType: MetaType.TABLE,
      pageSize: 20,
    });
    expect(router.currentRoute.value.query.target).toBe(TARGET_CUSTOMERS);
    // The expanded row renders the run card for the batch.
    expect(wrapper.text()).toContain(
      labels()("dataQuality.executions.runOpenInList")
    );

    await expanderButton(rows(wrapper)[0]).trigger("click");
    await flushPromises();
    expect(router.currentRoute.value.query.target).toBeUndefined();
    expect(rows(wrapper)).toHaveLength(2);
  });

  it("auto-expands a target and run carried by the URL", async () => {
    const { wrapper, router } = await mountPage(
      `/data-quality/executions?target=${encodeURIComponent(TARGET_ORDERS)}&run=${encodeURIComponent(RUN)}`
    );

    expect(mocks.listQualityRuns).toHaveBeenCalledWith({
      targetGuid: TARGET_ORDERS,
      metaType: MetaType.TABLE,
      pageSize: 20,
    });
    // The run named by the URL opens its checks without another click.
    expect(mocks.listQualityExecutions).toHaveBeenCalledWith({
      run: RUN,
      pageSize: 200,
    });
    expect(wrapper.text()).toContain("Null rate on orders.id");
    expect(router.currentRoute.value.query.run).toBe(RUN);
  });

  it("searches and sorts targets on the server", async () => {
    const { wrapper } = await mountPage();
    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);

    await wrapper.find("#data-quality-target-search").setValue("customers");
    // Typing does not query until the debounce fires.
    expect(mocks.listQualityTargets).toHaveBeenCalledTimes(1);
    await settleSearch();
    expect(mocks.listQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "customers" })
    );

    await wrapper
      .findComponent(Select)
      .vm.$emit("update:modelValue", "attention");
    await flushPromises();
    expect(mocks.listQualityTargets).toHaveBeenLastCalledWith(
      expect.objectContaining({ orderBy: QualityTargetOrder.ATTENTION })
    );
  });

  it("shows the empty directory with a link to the targets page", async () => {
    mocks.listQualityTargets.mockResolvedValue(page([]));
    const { wrapper } = await mountPage();

    expect(rows(wrapper)).toHaveLength(0);
    expect(wrapper.text()).toContain(labels()("dataQuality.noExecutions"));
    expect(wrapper.text()).toContain(labels()("dataQuality.noExecutionsHint"));
    expect(
      wrapper
        .findAll("a")
        .some((link) => link.attributes("href") === "/data-quality/targets")
    ).toBe(true);
  });

  it("retries a failed directory load", async () => {
    mocks.listQualityTargets.mockRejectedValueOnce(new Error("boom"));
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      labels()("dataQuality.targets.loadFailed")
    );

    const retry = wrapper
      .findAll("button")
      .filter((button) => button.text().trim() === labels()("common.refresh"));
    await retry[retry.length - 1]?.trigger("click");
    await flushPromises();

    expect(rows(wrapper)).toHaveLength(2);
  });
});

describe("DataQualityExecutionsPage view switch", () => {
  it("switches to the flat execution list and back", async () => {
    const { wrapper, router } = await mountPage();
    expect(rows(wrapper)[0]?.text()).toContain("customers");

    await viewButton(
      wrapper,
      labels()("dataQuality.executions.viewExecutions")
    )?.trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.query.view).toBe("executions");
    expect(mocks.listQualityExecutions).toHaveBeenCalled();
    expect(wrapper.text()).toContain("Null rate on orders.id");
    expect(rows(wrapper)).toHaveLength(1);

    await viewButton(
      wrapper,
      labels()("dataQuality.executions.viewTargets")
    )?.trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.query.view).toBeUndefined();
    expect(rows(wrapper)[0]?.text()).toContain("customers");
  });

  it("opens the flat list from a run deep link and keeps the run chip", async () => {
    const { wrapper, router } = await mountPage(
      `/data-quality/executions?run=${encodeURIComponent(RUN)}`
    );

    // A run alone is an execution list, not the target view.
    expect(mocks.listQualityTargets).not.toHaveBeenCalled();
    expect(mocks.listQualityExecutions).toHaveBeenCalledWith(
      expect.objectContaining({ run: RUN })
    );
    expect(wrapper.text()).toContain(
      labels()("dataQuality.filteredByRun", { run: "run-1" })
    );

    await wrapper
      .find(`[aria-label="${labels()("dataQuality.clearRunFilter")}"]`)
      .trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.query.run).toBeUndefined();
    // Clearing the chip keeps the reader in the flat list.
    expect(router.currentRoute.value.query.view).toBe("executions");
    expect(mocks.listQualityExecutions).toHaveBeenLastCalledWith(
      expect.objectContaining({ run: "" })
    );
  });
});

describe("DataQualityExecutionsPage flat list", () => {
  it("renders one row per execution with its rule, target and verdict", async () => {
    const { wrapper } = await mountPage(
      "/data-quality/executions?view=executions"
    );

    const text = wrapper.text();
    expect(text).toContain("Null rate on orders.id");
    expect(text).toContain("public.orders");
    expect(text).toContain(labels()("dataQuality.executionState.completed"));
    expect(text).toContain(labels()("dataQuality.status.fail"));
    expect(text).toContain("4%");
    expect(text).toContain(labels()("dataQuality.executionTrigger.manual"));
    expect(rows(wrapper)).toHaveLength(1);
  });

  it("shows a pending execution honestly, without inventing a verdict", async () => {
    mocks.listQualityExecutions.mockResolvedValue(
      page([
        executionFixture({
          state: QualityExecutionState.PENDING,
          outcome: QualityStatus.UNSPECIFIED,
          finishTime: undefined,
        }),
      ])
    );
    const { wrapper } = await mountPage(
      "/data-quality/executions?view=executions"
    );

    const row = rows(wrapper)[0];
    expect(row?.text()).toContain(
      labels()("dataQuality.executionState.pending")
    );
    expect(row?.text()).not.toContain(labels()("dataQuality.status.fail"));
    wrapper.unmount();
  });

  it("opens the execution detail from a row", async () => {
    const { wrapper, router } = await mountPage(
      "/data-quality/executions?view=executions"
    );

    await rows(wrapper)[0]?.trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.name).toBe("DataQualityExecutionDetail");
    expect(router.currentRoute.value.params.executionId).toEqual(
      EXECUTION.split("/")
    );
  });

  it("carries the rule filter from the URL and clears it", async () => {
    const { wrapper } = await mountPage(
      `/data-quality/executions?view=executions&rule=${encodeURIComponent(RULE)}`
    );

    expect(mocks.listQualityExecutions).toHaveBeenCalledWith(
      expect.objectContaining({ rule: RULE })
    );
    expect(wrapper.text()).toContain(
      labels()("dataQuality.filteredByRule", {
        rule: "Null rate on orders.id",
      })
    );

    await wrapper
      .find(`[aria-label="${labels()("dataQuality.clearRuleFilter")}"]`)
      .trigger("click");
    await flushPromises();
    expect(mocks.listQualityExecutions).toHaveBeenLastCalledWith(
      expect.objectContaining({ rule: "" })
    );
  });

  it("links the rule into its target workspace with the check selected", async () => {
    const { wrapper } = await mountPage(
      "/data-quality/executions?view=executions"
    );

    const rule = wrapper
      .findAll("a")
      .find((link) => link.text().trim() === "Null rate on orders.id");
    const href = rule?.attributes("href") ?? "";
    expect(href).toContain("/data-quality/targets/inst1/shop/public/orders");
    expect(href).toContain("metaType=4");
    expect(href).toContain(`check=${RULE}`);

    const target = wrapper
      .findAll("a")
      .find(
        (link) =>
          link.text().trim() === "public.orders" &&
          (link.attributes("href") ?? "").startsWith("/data-quality/targets/")
      );
    expect(target?.attributes("href")).toContain(
      "/data-quality/targets/inst1/shop/public/orders"
    );
  });

  it("keeps the per-rule fallback for an execution with no target snapshot", async () => {
    mocks.listQualityExecutions.mockResolvedValue(
      page([executionFixture({ target: undefined })])
    );
    const { wrapper } = await mountPage(
      "/data-quality/executions?view=executions"
    );

    const rule = wrapper
      .findAll("a")
      .find((link) => link.text().trim() === "Null rate on orders.id");
    expect(rule?.attributes("href")).toContain("/data-quality/rules/");
    expect(
      wrapper
        .findAll("a")
        .some((link) =>
          (link.attributes("href") ?? "").startsWith("/data-quality/targets/")
        )
    ).toBe(false);
    wrapper.unmount();
  });

  it("only discloses a partial slice at the batch composition cap", async () => {
    const scheduled = runFor(TARGET_ORDERS, "orders", {
      counts: { total: 100, pass: 100, fail: 0 },
      trigger: QualityExecutionTrigger.SCHEDULED,
    });
    mocks.listQualityTargets.mockResolvedValue(
      page([
        targetSummary({
          guid: TARGET_ORDERS,
          object: "orders",
          latestRun: scheduled,
          runCount: 1,
          ruleCount: 120,
        }),
      ])
    );
    const scheduledPage = await mountPage();
    expect(scheduledPage.wrapper.text()).toContain(
      labels()("dataQuality.executions.runPartial", { done: 100, all: 120 })
    );
    scheduledPage.wrapper.unmount();

    // A scheduled batch below the cap ran fewer checks by design (a retry, or it
    // predates a check added since), so it is not a slice of the target.
    const small = runFor(TARGET_ORDERS, "orders", {
      counts: { total: 2, pass: 2, fail: 0 },
      trigger: QualityExecutionTrigger.SCHEDULED,
    });
    mocks.listQualityTargets.mockResolvedValue(
      page([
        targetSummary({
          guid: TARGET_ORDERS,
          object: "orders",
          latestRun: small,
          runCount: 1,
          ruleCount: 5,
        }),
      ])
    );
    const smallPage = await mountPage();
    expect(smallPage.wrapper.text()).not.toContain(
      labels()("dataQuality.executions.runPartial", { done: 2, all: 5 })
    );
    smallPage.wrapper.unmount();

    // A manual run planned an explicit selection, so it is never partial.
    const manual = runFor(TARGET_ORDERS, "orders", {
      counts: { total: 2, pass: 2, fail: 0 },
    });
    mocks.listQualityTargets.mockResolvedValue(
      page([
        targetSummary({
          guid: TARGET_ORDERS,
          object: "orders",
          latestRun: manual,
          runCount: 1,
          ruleCount: 5,
        }),
      ])
    );
    const manualPage = await mountPage();
    expect(manualPage.wrapper.text()).not.toContain(
      labels()("dataQuality.executions.runPartial", { done: 2, all: 5 })
    );
    manualPage.wrapper.unmount();
  });
});
