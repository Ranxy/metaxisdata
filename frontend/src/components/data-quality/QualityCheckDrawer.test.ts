import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { notify } from "@/lib/notify";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityComparison,
  QualityDimension,
  QualityEmptyBehavior,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityFilterOperator,
  QualityFrequencyTier,
  QualityObservedKind,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleTrendPointSchema,
  QualityRuleTrendSchema,
  QualityRuleType,
  QualitySeverity,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityThresholdMetric,
  QualityTrendUnit,
  RowCountChangeConfigSchema,
  RowCountConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { formatDateTime } from "@/utils/datetime";
import QualityCheckDrawer from "./QualityCheckDrawer.vue";
import QualityObservedTrendChart from "./QualityObservedTrendChart.vue";

const mocks = vi.hoisted(() => ({
  getQualityRule: vi.fn(),
  getQualityExecution: vi.fn(),
  listQualityExecutions: vi.fn(),
  getQualityRuleTrend: vi.fn(),
  runQualityRules: vi.fn(),
  updateQualityRule: vi.fn(),
  deleteQualityRule: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityRule: mocks.getQualityRule,
  getQualityExecution: mocks.getQualityExecution,
  listQualityExecutions: mocks.listQualityExecutions,
  getQualityRuleTrend: mocks.getQualityRuleTrend,
  runQualityRules: mocks.runQualityRules,
  updateQualityRule: mocks.updateQualityRule,
  deleteQualityRule: mocks.deleteQualityRule,
}));

vi.mock("@/api/user", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/user")>()),
  listAllUsers: vi.fn().mockResolvedValue([]),
}));

const RULE = "workspaces/ws/qualityRules/rule-1";
const TABLE = "inst1;db1;public;orders";
const START = new Date("2026-10-07T09:30:00Z");

const ALL_PERMISSIONS = [
  "metaxisdata.dataQuality.rules.get",
  "metaxisdata.dataQuality.rules.run",
  "metaxisdata.dataQuality.rules.update",
  "metaxisdata.dataQuality.rules.delete",
  "metaxisdata.dataQuality.rules.create",
  "metaxisdata.dataQuality.executions.list",
  "metaxisdata.dataQuality.executions.get",
  "metaxisdata.dataQuality.explain",
];

const Dummy = { template: "<div />" };

function ruleFixture(overrides: Record<string, unknown> = {}) {
  return create(QualityRuleSchema, {
    name: RULE,
    title: "Orders should have rows",
    description: "The fact table must never be empty.",
    dimension: QualityDimension.COMPLETENESS,
    ruleType: QualityRuleType.ROW_COUNT,
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object: "orders",
      }),
      columns: ["id"],
    }),
    severity: QualitySeverity.ERROR,
    state: QualityRuleState.ENABLED,
    revision: 3,
    createTime: timestampFromDate(new Date(START.getTime() - 86_400_000)),
    updateTime: timestampFromDate(START),
    tags: ["fact"],
    config: create(QualityRuleConfigSchema, {
      thresholdMetric: QualityThresholdMetric.COUNT,
      onEmpty: QualityEmptyBehavior.FAIL,
      filter: {
        clauses: [
          { column: "region", op: QualityFilterOperator.EQ, values: ["us"] },
        ],
      },
      config: {
        case: "rowCount",
        value: create(RowCountConfigSchema, { minRows: 1n }),
      },
    }),
    ...overrides,
  });
}

/** Executions arrive newest-first, exactly as the API returns them. */
function executionFixture(
  index: number,
  outcome: "PASS" | "FAIL" | "ERROR",
  trigger = QualityExecutionTrigger.SCHEDULED
) {
  const at = timestampFromDate(new Date(START.getTime() - index * 60_000));
  return create(QualityExecutionSchema, {
    name: `workspaces/ws/qualityExecutions/${index + 1}`,
    rule: RULE,
    state: QualityExecutionState.COMPLETED,
    outcome:
      outcome === "PASS"
        ? QualityStatus.PASS
        : outcome === "FAIL"
          ? QualityStatus.FAIL
          : QualityStatus.ERROR,
    observedValue: outcome === "PASS" ? 10 : 0,
    observedKind: QualityObservedKind.COUNT,
    failedRows: outcome === "PASS" ? 0n : 3n,
    totalRows: 10n,
    thresholds: {
      comparison: QualityComparison.GREATER_THAN,
      failValue: 0,
    },
    trigger,
    startTime: at,
    finishTime: at,
  });
}

function executionsFixture() {
  return [
    executionFixture(0, "FAIL"),
    executionFixture(1, "PASS"),
    executionFixture(2, "PASS"),
  ];
}

/** The check's measured history, oldest first, as the trend RPC returns it. */
function trendFixture() {
  return create(QualityRuleTrendSchema, {
    rule: RULE,
    unit: QualityTrendUnit.COUNT,
    points: [
      create(QualityRuleTrendPointSchema, {
        execution: "workspaces/ws/qualityExecutions/3",
        status: QualityStatus.PASS,
        value: 12,
        failedRows: 0n,
        totalRows: 12n,
        startTime: timestampFromDate(new Date(START.getTime() - 120_000)),
      }),
      create(QualityRuleTrendPointSchema, {
        execution: "workspaces/ws/qualityExecutions/2",
        status: QualityStatus.FAIL,
        value: 0,
        failedRows: 10n,
        totalRows: 10n,
        startTime: timestampFromDate(new Date(START.getTime() - 60_000)),
      }),
      create(QualityRuleTrendPointSchema, {
        execution: "workspaces/ws/qualityExecutions/1",
        status: QualityStatus.PASS,
        value: 11,
        failedRows: 0n,
        totalRows: 11n,
        startTime: timestampFromDate(START),
      }),
    ],
  });
}

/** A count-change rule, whose verdict rests on a baseline. */
function countChangeRuleFixture() {
  return ruleFixture({
    ruleType: QualityRuleType.ROW_COUNT_CHANGE,
    config: create(QualityRuleConfigSchema, {
      onEmpty: QualityEmptyBehavior.PASS,
      config: {
        case: "rowCountChange",
        value: create(RowCountChangeConfigSchema, { maxIncreasePct: 10 }),
      },
    }),
  });
}

/** The execution `runQualityRules` plans: it starts without a verdict. */
function plannedExecution(state: QualityExecutionState) {
  return create(QualityExecutionSchema, {
    name: "workspaces/ws/qualityExecutions/run-1",
    rule: RULE,
    state,
  });
}

async function mountDrawer(
  options: {
    permissions?: string[];
    open?: boolean;
    rule?: ReturnType<typeof ruleFixture>;
  } = {}
) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, {
    permissions: options.permissions ?? ALL_PERMISSIONS,
  });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: Dummy,
      },
      {
        path: "/metadata/:guid+",
        name: "MetadataDetail",
        component: Dummy,
      },
    ],
  });
  await router.push("/");
  await router.isReady();
  const wrapper = mount(QualityCheckDrawer, {
    attachTo: document.body,
    props: {
      open: options.open ?? true,
      ruleName: RULE,
      targetGuid: TABLE,
      metaType: MetaType.TABLE,
    },
    global: {
      plugins: [router, i18n, pinia],
      stubs: { QualityExplainDrawer: true },
    },
  });
  await flushPromises();
  return { wrapper, router, rule: options.rule };
}

function label(key: string, params: Record<string, unknown> = {}): string {
  return i18n.global.t(key, params);
}

function buttonByText(text: string): HTMLElement | undefined {
  return [...document.body.querySelectorAll<HTMLElement>("button")].find(
    (button) => button.textContent?.trim() === text
  );
}

function confirmDialog(): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"]'),
  ].find((dialog) =>
    dialog.textContent?.includes(label("dataQuality.deleteRuleTitle"))
  );
}

describe("QualityCheckDrawer", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    document.body.innerHTML = "";
    i18n.global.locale.value = "en-US";
    mocks.getQualityRule.mockResolvedValue(ruleFixture());
    mocks.listQualityExecutions.mockResolvedValue({
      items: executionsFixture(),
      nextPageToken: "",
    });
    mocks.getQualityRuleTrend.mockResolvedValue(trendFixture());
    mocks.runQualityRules.mockResolvedValue([]);
    mocks.updateQualityRule.mockImplementation(
      async (rule: ReturnType<typeof ruleFixture>) => rule
    );
    mocks.deleteQualityRule.mockResolvedValue(undefined);
  });

  it("renders the latest result, recent executions and the definition", async () => {
    const { wrapper } = await mountDrawer();

    expect(mocks.getQualityRule).toHaveBeenCalledWith(RULE);
    expect(mocks.listQualityExecutions).toHaveBeenCalledWith({
      rule: RULE,
      pageSize: 20,
    });

    const text = document.body.textContent ?? "";
    expect(text).toContain("Orders should have rows");
    expect(text).toContain(label("dataQuality.ruleType.rowCount"));
    expect(text).toContain(label("dataQuality.severity.error"));
    expect(text).toContain(label("dataQuality.ruleState.enabled"));
    expect(text).toContain(label("dataQuality.revision"));
    expect(text).toContain("prod · shop · public.orders");

    // The latest result is the failing one, with its observed/threshold/rows.
    expect(text).toContain(label("dataQuality.status.fail"));
    expect(text).toContain(
      label("dataQuality.thresholdGreaterThan", { value: "0" })
    );
    expect(text).toContain("3 / 10");
    // Two of the three terminal executions passed.
    expect(text).toContain(label("dataQuality.passRateValue", { value: 67 }));

    // The strip is the recent history, newest last and a square per execution.
    expect(document.body.querySelector('[role="list"]')).not.toBeNull();
    expect(document.body.querySelectorAll('[role="listitem"]')).toHaveLength(3);

    // The table adds the trigger the old page omitted.
    expect(text).toContain(label("dataQuality.recentExecutions"));
    expect(text).toContain(label("dataQuality.executionTrigger.scheduled"));

    // The definition carries the pre-filter; the empty-table behaviour is
    // omitted for row count, where it cannot change the verdict either way.
    expect(text).toContain(label("dataQuality.editor.filter"));
    expect(text).toContain("region");
    expect(text).not.toContain(label("dataQuality.editor.onEmpty"));
    expect(text).toContain("fact");
    wrapper.unmount();
  });

  it("shows the run cadence only when a check runs less often than its target", async () => {
    mocks.getQualityRule.mockResolvedValue(
      ruleFixture({ frequencyTier: QualityFrequencyTier.DAILY })
    );
    const { wrapper } = await mountDrawer();

    const text = document.body.textContent ?? "";
    expect(text).toContain(label("dataQuality.frequencyTierLabel"));
    expect(text).toContain(label("dataQuality.frequencyTier.daily"));
    wrapper.unmount();
  });

  it("drops the run cadence row for a check that follows the target", async () => {
    const { wrapper } = await mountDrawer();

    expect(document.body.textContent ?? "").not.toContain(
      label("dataQuality.frequencyTierLabel")
    );
    wrapper.unmount();
  });

  it("reports a completed execution by its verdict alone", async () => {
    const { wrapper } = await mountDrawer();

    // The lifecycle state and the verdict would say the same thing twice; the
    // narrow table shows only the verdict, so its row stays one line high.
    const text = document.body.textContent ?? "";
    expect(text).toContain(label("dataQuality.status.fail"));
    expect(text).not.toContain(label("dataQuality.executionState.completed"));

    // Trigger and duration travel inside the started cell rather than as their
    // own columns, which is what used to push the sheet into a sideways scroll.
    expect(text).toContain(
      `${label("dataQuality.executionTrigger.scheduled")} · 0 ms`
    );
    const headers = [...document.body.querySelectorAll("th")].map((th) =>
      th.textContent?.trim()
    );
    expect(headers).not.toContain(label("dataQuality.trigger"));
    expect(headers).not.toContain(label("dataQuality.duration"));
    wrapper.unmount();
  });

  it("drops the history strip for a single execution", async () => {
    mocks.listQualityExecutions.mockResolvedValue({
      items: [executionFixture(0, "FAIL")],
      nextPageToken: "",
    });
    const { wrapper } = await mountDrawer();

    // One square is not a history; the table below already carries the run.
    expect(document.body.querySelector('[role="list"]')).toBeNull();
    expect(document.body.textContent).toContain(
      label("dataQuality.status.fail")
    );
    wrapper.unmount();
  });

  it("runs the check and refreshes the sheet", async () => {
    const { wrapper } = await mountDrawer();

    buttonByText(label("dataQuality.runNow"))?.click();
    await flushPromises();

    expect(mocks.runQualityRules).toHaveBeenCalledWith({ rules: [RULE] });
    // The run returns new executions, so both reads happen again, and the
    // refreshed rule goes back to the workspace that opened the sheet.
    expect(mocks.getQualityRule).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityExecutions).toHaveBeenCalledTimes(2);
    expect(wrapper.emitted("changed")).toHaveLength(1);
    wrapper.unmount();
  });

  it("pauses and resumes by writing only the state", async () => {
    const { wrapper } = await mountDrawer();

    buttonByText(label("dataQuality.pauseRule"))?.click();
    await flushPromises();

    expect(mocks.updateQualityRule).toHaveBeenCalledTimes(1);
    const [paused, mask] = mocks.updateQualityRule.mock.calls[0] as [
      ReturnType<typeof ruleFixture>,
      string[],
    ];
    expect(paused.state).toBe(QualityRuleState.PAUSED);
    expect(mask).toEqual(["state"]);
    expect(wrapper.emitted("changed")).toHaveLength(1);
    // The button now offers the inverse action.
    expect(buttonByText(label("dataQuality.resumeRule"))).toBeDefined();

    mocks.updateQualityRule.mockResolvedValue(
      ruleFixture({ state: QualityRuleState.ENABLED })
    );
    buttonByText(label("dataQuality.resumeRule"))?.click();
    await flushPromises();
    expect(
      (
        mocks.updateQualityRule.mock.calls[1]?.[0] as ReturnType<
          typeof ruleFixture
        >
      ).state
    ).toBe(QualityRuleState.ENABLED);
    wrapper.unmount();
  });

  it("edits by handing the rule back and closing the sheet", async () => {
    const { wrapper } = await mountDrawer();

    buttonByText(label("dataQuality.editRule"))?.click();
    await flushPromises();

    const edited = wrapper.emitted("edit");
    expect(edited).toHaveLength(1);
    expect((edited?.[0]?.[0] as { name?: string }).name).toBe(RULE);
    const opens = wrapper.emitted("update:open") ?? [];
    expect(opens[opens.length - 1]).toEqual([false]);
    wrapper.unmount();
  });

  it("archives the check only after the confirmation", async () => {
    const success = vi.spyOn(notify, "success");
    const { wrapper } = await mountDrawer();

    buttonByText(label("common.delete"))?.click();
    await flushPromises();
    expect(confirmDialog()).toBeDefined();
    expect(mocks.deleteQualityRule).not.toHaveBeenCalled();

    const dialog = confirmDialog();
    dialog?.querySelectorAll<HTMLElement>("button").forEach((button) => {
      if (button.textContent?.trim() === label("common.delete")) {
        button.click();
      }
    });
    await flushPromises();

    expect(mocks.deleteQualityRule).toHaveBeenCalledWith(RULE);
    expect(wrapper.emitted("deleted")).toEqual([[RULE]]);
    const opens = wrapper.emitted("update:open") ?? [];
    expect(opens[opens.length - 1]).toEqual([false]);
    expect(success).toHaveBeenCalledWith(label("dataQuality.ruleDeleted"));
    success.mockRestore();
    wrapper.unmount();
  });

  it("shows a load failure with a retry", async () => {
    mocks.getQualityRule.mockRejectedValue(
      new ConnectError("rule down", Code.Internal)
    );
    const { wrapper } = await mountDrawer();

    // A Connect failure reports its code's sentence, not the generic fallback.
    expect(document.body.textContent).toContain(label("error.internal"));
    buttonByText(label("common.refresh"))?.click();
    await flushPromises();
    expect(mocks.getQualityRule).toHaveBeenCalledTimes(2);
    wrapper.unmount();
  });

  it("hides every write action a caller may not take", async () => {
    const { wrapper } = await mountDrawer({
      permissions: [
        "metaxisdata.dataQuality.rules.get",
        "metaxisdata.dataQuality.executions.list",
      ],
    });

    // The read still renders the check's definition and history.
    expect(document.body.textContent).toContain("Orders should have rows");
    expect(mocks.listQualityExecutions).toHaveBeenCalledTimes(1);
    expect(buttonByText(label("dataQuality.runNow"))).toBeUndefined();
    expect(buttonByText(label("dataQuality.pauseRule"))).toBeUndefined();
    expect(buttonByText(label("dataQuality.editRule"))).toBeUndefined();
    expect(buttonByText(label("common.delete"))).toBeUndefined();
    expect(
      buttonByText(label("dataQuality.llm.explain.action"))
    ).toBeUndefined();
    wrapper.unmount();
  });

  it("does not read a check at all without the rules.get permission", async () => {
    const { wrapper } = await mountDrawer({ permissions: [] });

    expect(mocks.getQualityRule).not.toHaveBeenCalled();
    expect(document.body.textContent).toContain(
      label("error.permissionDenied")
    );
    wrapper.unmount();
  });

  it("waits for a manual run to settle before re-reading the check", async () => {
    const planned = plannedExecution(QualityExecutionState.PENDING);
    mocks.runQualityRules.mockResolvedValue([planned]);
    mocks.getQualityExecution
      .mockResolvedValueOnce(plannedExecution(QualityExecutionState.RUNNING))
      .mockResolvedValueOnce(plannedExecution(QualityExecutionState.COMPLETED));
    const { wrapper } = await mountDrawer();
    expect(mocks.getQualityRule).toHaveBeenCalledTimes(1);

    vi.useFakeTimers();
    buttonByText(label("dataQuality.runNow"))?.click();
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.runQualityRules).toHaveBeenCalledWith({ rules: [RULE] });

    // The first poll only sees RUNNING, so the sheet does not refresh yet.
    await vi.advanceTimersByTimeAsync(2_000);
    expect(mocks.getQualityExecution).toHaveBeenCalledWith(
      planned.name,
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    );
    expect(mocks.getQualityRule).toHaveBeenCalledTimes(1);

    // The second poll sees the verdict; the re-read now runs once and hands the
    // refreshed rule back to the workspace.
    await vi.advanceTimersByTimeAsync(2_000);
    await vi.advanceTimersByTimeAsync(0);
    expect(mocks.getQualityRule).toHaveBeenCalledTimes(2);
    expect(mocks.listQualityExecutions).toHaveBeenCalledTimes(2);
    expect(wrapper.emitted("changed")).toHaveLength(1);
    expect(
      (
        buttonByText(label("dataQuality.runNow")) as
          | HTMLButtonElement
          | undefined
      )?.disabled
    ).toBe(false);
    wrapper.unmount();
  });

  it("does not poll or refresh when the planned run is already final", async () => {
    const done = plannedExecution(QualityExecutionState.COMPLETED);
    mocks.runQualityRules.mockResolvedValue([done]);
    const { wrapper } = await mountDrawer();

    vi.useFakeTimers();
    buttonByText(label("dataQuality.runNow"))?.click();
    await vi.advanceTimersByTimeAsync(0);

    expect(mocks.getQualityExecution).not.toHaveBeenCalled();
    expect(mocks.getQualityRule).toHaveBeenCalledTimes(2);
    expect(wrapper.emitted("changed")).toHaveLength(1);
    wrapper.unmount();
  });

  it("stops polling when the sheet unmounts", async () => {
    mocks.runQualityRules.mockResolvedValue([
      plannedExecution(QualityExecutionState.PENDING),
    ]);
    mocks.getQualityExecution.mockResolvedValue(
      plannedExecution(QualityExecutionState.RUNNING)
    );
    const { wrapper } = await mountDrawer();

    vi.useFakeTimers();
    buttonByText(label("dataQuality.runNow"))?.click();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(mocks.getQualityExecution).toHaveBeenCalledTimes(1);

    wrapper.unmount();
    await vi.advanceTimersByTimeAsync(10_000);
    // A canceled poll must not keep reading (or writing) after the sheet is gone.
    expect(mocks.getQualityExecution).toHaveBeenCalledTimes(1);
  });

  it("refreshes at the deadline even if the run never settles", async () => {
    mocks.runQualityRules.mockResolvedValue([
      plannedExecution(QualityExecutionState.PENDING),
    ]);
    mocks.getQualityExecution.mockResolvedValue(
      plannedExecution(QualityExecutionState.RUNNING)
    );
    const { wrapper } = await mountDrawer();

    vi.useFakeTimers();
    buttonByText(label("dataQuality.runNow"))?.click();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(60_000);
    await vi.advanceTimersByTimeAsync(0);

    // The timeout still lets the reader see whatever the runner recorded.
    expect(mocks.getQualityRule).toHaveBeenCalledTimes(2);
    expect(
      (
        buttonByText(label("dataQuality.runNow")) as
          | HTMLButtonElement
          | undefined
      )?.disabled
    ).toBe(false);
    wrapper.unmount();
  });
  it("draws the check's measured history beside its verdict", async () => {
    const { wrapper, router } = await mountDrawer();

    expect(mocks.getQualityRuleTrend).toHaveBeenCalledWith(RULE, {
      limit: 30,
    });
    const text = document.body.textContent ?? "";
    expect(text).toContain(label("dataQuality.trend.title"));
    expect(text).toContain(label("dataQuality.trend.description"));

    const chart = wrapper.findComponent(QualityObservedTrendChart);
    expect(chart.exists()).toBe(true);
    expect(chart.props("trend").points).toHaveLength(3);

    // A point is one execution: opening it lands on the execution detail page.
    const point = chart.props("trend").points[1];
    chart.vm.$emit("select", point);
    await flushPromises();
    expect(router.currentRoute.value.name).toBe("DataQualityExecutionDetail");
    expect(router.currentRoute.value.params.executionId).toEqual([
      "workspaces",
      "ws",
      "qualityExecutions",
      "2",
    ]);
    wrapper.unmount();
  });

  it("keeps the rest of the sheet when only the trend read fails", async () => {
    mocks.getQualityRuleTrend.mockRejectedValueOnce(
      new ConnectError("trend down", Code.Internal)
    );
    const { wrapper } = await mountDrawer();

    // The verdict, the history and the definition are unaffected.
    const text = document.body.textContent ?? "";
    expect(text).toContain("Orders should have rows");
    expect(text).toContain(label("dataQuality.status.fail"));
    expect(text).toContain(label("dataQuality.trend.loadFailed"));

    buttonByText(label("common.refresh"))?.click();
    await flushPromises();
    expect(mocks.getQualityRuleTrend).toHaveBeenCalledTimes(2);
    expect(wrapper.findComponent(QualityObservedTrendChart).exists()).toBe(
      true
    );
    wrapper.unmount();
  });

  it("names the baseline a count-change check compared with", async () => {
    mocks.getQualityRule.mockResolvedValue(countChangeRuleFixture());
    mocks.listQualityExecutions.mockResolvedValue({
      items: [
        create(QualityExecutionSchema, {
          ...executionFixture(0, "PASS"),
          baselineValue: 100,
          baselineTime: timestampFromDate(
            new Date(START.getTime() - 86_400_000)
          ),
        }),
      ],
      nextPageToken: "",
    });
    const withBaseline = await mountDrawer();
    const text = document.body.textContent ?? "";
    expect(text).toContain(label("dataQuality.baseline.label"));
    expect(text).toContain(
      label("dataQuality.baseline.value", {
        value: "100",
        time: formatDateTime(
          timestampFromDate(new Date(START.getTime() - 86_400_000)),
          "en-US"
        ),
      })
    );
    withBaseline.wrapper.unmount();

    // A first observation has no baseline to compare with, and says so.
    mocks.listQualityExecutions.mockResolvedValue({
      items: [executionFixture(0, "PASS")],
      nextPageToken: "",
    });
    const withoutBaseline = await mountDrawer();
    expect(document.body.textContent).toContain(
      label("dataQuality.baseline.missing")
    );
    withoutBaseline.wrapper.unmount();
  });

  it("does not read the trend without the executions.list permission", async () => {
    const { wrapper } = await mountDrawer({
      permissions: ["metaxisdata.dataQuality.rules.get"],
    });

    expect(mocks.getQualityRuleTrend).not.toHaveBeenCalled();
    expect(document.body.textContent).not.toContain(
      label("dataQuality.trend.title")
    );
    wrapper.unmount();
  });
});
