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
  QualityObservedKind,
  QualityRuleConfigSchema,
  QualityRuleSchema,
  QualityRuleState,
  QualityRuleType,
  QualitySeverity,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityThresholdMetric,
  RowCountConfigSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import QualityCheckDrawer from "./QualityCheckDrawer.vue";

const mocks = vi.hoisted(() => ({
  getQualityRule: vi.fn(),
  getQualityExecution: vi.fn(),
  listQualityExecutions: vi.fn(),
  runQualityRules: vi.fn(),
  updateQualityRule: vi.fn(),
  deleteQualityRule: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityRule: mocks.getQualityRule,
  getQualityExecution: mocks.getQualityExecution,
  listQualityExecutions: mocks.listQualityExecutions,
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
    scheduleInterval: { seconds: 3600n, nanos: 0 },
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

    // The definition carries the pre-filter and the empty-table behaviour too.
    expect(text).toContain(label("dataQuality.editor.filter"));
    expect(text).toContain("region");
    expect(text).toContain(label("dataQuality.editor.onEmpty"));
    expect(text).toContain(label("dataQuality.emptyBehavior.fail"));
    expect(text).toContain("fact");
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
    expect(mocks.getQualityExecution).toHaveBeenCalledWith(planned.name);
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
});
