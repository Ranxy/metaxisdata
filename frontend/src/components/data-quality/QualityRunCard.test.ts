import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityExecution,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityObservedKind,
  type QualityRun,
  QualityRunSchema,
  QualityRunTargetSchema,
  QualityStatus,
  QualityStatusCountsSchema,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import QualityRunCard from "./QualityRunCard.vue";

const mocks = vi.hoisted(() => ({
  listQualityExecutions: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  listQualityExecutions: mocks.listQualityExecutions,
}));

const TARGET = "inst1;shop;public;orders";
const RUN = "workspaces/ws/qualityRuns/run-1";
const RULE = "workspaces/ws/qualityRules/rule-1";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const START = new Date("2026-10-07T09:30:00Z");
const FINISH = new Date(START.getTime() + 2_000);
const Dummy = { template: "<div />" };

const labels = () => i18n.global.t;

function target() {
  return create(QualityTargetSchema, {
    metaGuid: TARGET,
    metaType: MetaType.TABLE,
    path: create(QualityTargetPathSchema, {
      instance: "prod",
      database: "shop",
      schema: "public",
      object: "orders",
    }),
  });
}

/** Batch-wide counts: deliberately larger than the target slice below. */
function batchCounts() {
  return create(QualityStatusCountsSchema, {
    total: 10,
    pass: 6,
    fail: 2,
    error: 1,
    skipped: 1,
  });
}

/** This target's slice of the batch. */
function sliceCounts(overrides: Record<string, number> = {}) {
  return create(QualityStatusCountsSchema, {
    total: 6,
    pass: 3,
    fail: 1,
    error: 1,
    skipped: 1,
    ...overrides,
  });
}

function runFixture(revision: { min?: number; max?: number } = {}): QualityRun {
  return create(QualityRunSchema, {
    name: RUN,
    instance: "instances/prod",
    trigger: QualityExecutionTrigger.MANUAL,
    state: QualityExecutionState.COMPLETED,
    outcome: QualityStatus.FAIL,
    counts: batchCounts(),
    startTime: timestampFromDate(START),
    finishTime: timestampFromDate(FINISH),
    targets: [
      create(QualityRunTargetSchema, {
        target: target(),
        counts: sliceCounts(),
        outcome: QualityStatus.FAIL,
        ruleRevisionMin: revision.min ?? 7,
        ruleRevisionMax: revision.max ?? 7,
        startTime: timestampFromDate(START),
        finishTime: timestampFromDate(FINISH),
        run: RUN,
        state: QualityExecutionState.COMPLETED,
        trigger: QualityExecutionTrigger.MANUAL,
      }),
    ],
  });
}

function executionFixture(): QualityExecution {
  return create(QualityExecutionSchema, {
    name: EXECUTION,
    rule: RULE,
    state: QualityExecutionState.COMPLETED,
    outcome: QualityStatus.FAIL,
    trigger: QualityExecutionTrigger.MANUAL,
    observedValue: 0.04,
    observedKind: QualityObservedKind.RATIO,
    failedRows: 40n,
    totalRows: 1000n,
    startTime: timestampFromDate(START),
    finishTime: timestampFromDate(FINISH),
  });
}

async function mountCard(
  options: {
    expanded?: boolean;
    run?: QualityRun;
    targetRuleCount?: number;
  } = {}
) {
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
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/executions");
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: [] });
  const wrapper = mount(QualityRunCard, {
    props: {
      run: options.run ?? runFixture(),
      targetGuid: TARGET,
      ruleTitles: { [RULE]: "Null rate on orders.id" },
      targetRuleCount: options.targetRuleCount ?? 0,
      modelValue: options.expanded ?? false,
    },
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

function expander(wrapper: ReturnType<typeof mount>) {
  return wrapper.find(
    `button[aria-label="${labels()("dataQuality.executions.runExpand")}"]`
  );
}

beforeEach(() => {
  localStorage.clear();
  vi.clearAllMocks();
  mocks.listQualityExecutions.mockResolvedValue({
    items: [executionFixture()],
    nextPageToken: "",
  });
});

describe("QualityRunCard", () => {
  it("reports the target's slice, not the batch-wide counts", async () => {
    const { wrapper } = await mountCard();
    const text = wrapper.text();

    // Slice verdicts: 3 pass, 1 fail, 1 error, 1 skipped, 6 planned.
    expect(text).toContain("3 Pass");
    expect(text).toContain("1 Fail");
    expect(text).toContain(
      labels()("dataQuality.executions.runErrors", { count: 1 })
    );
    expect(text).toContain(
      labels()("dataQuality.executions.runChecks", { count: 6 })
    );
    expect(text).toContain(
      labels()("dataQuality.executions.runFailed", { count: 2 })
    );
    // The batch-wide tally (6 pass, 10 planned) is not this target's story.
    expect(text).not.toContain("6 Pass");
    expect(text).not.toContain(
      labels()("dataQuality.executions.runChecks", { count: 10 })
    );

    expect(text).toContain(
      labels()("dataQuality.executions.revisionSingle", { revision: 7 })
    );
    // Two seconds between the slice's start and finish.
    expect(text).toContain("2 s");
  });

  it("names how many targets a multi-target batch covered", async () => {
    const single = await mountCard();
    expect(single.wrapper.text()).not.toContain(
      labels()("dataQuality.executions.runTargets", { count: 1 })
    );

    const run = runFixture();
    run.targets = [
      ...run.targets,
      create(QualityRunTargetSchema, {
        target: create(QualityTargetSchema, {
          metaGuid: "inst1;shop;public;customers",
          metaType: MetaType.TABLE,
        }),
      }),
    ];
    const { wrapper } = await mountCard({ run });
    expect(wrapper.text()).toContain(
      labels()("dataQuality.executions.runTargets", { count: 2 })
    );
  });

  it("says how many of the target's checks this batch covered", async () => {
    const { wrapper } = await mountCard({ targetRuleCount: 10 });
    expect(wrapper.text()).toContain(
      labels()("dataQuality.executions.runPartial", { done: 6, all: 10 })
    );

    const complete = await mountCard({ targetRuleCount: 6 });
    expect(complete.wrapper.text()).not.toContain(
      labels()("dataQuality.executions.runPartial", { done: 6, all: 6 })
    );
  });

  it("shows the rule revision range when the slice judged several revisions", async () => {
    const { wrapper } = await mountCard({
      run: runFixture({ min: 3, max: 5 }),
    });

    expect(wrapper.text()).toContain(
      labels()("dataQuality.executions.revisionRange", { min: 3, max: 5 })
    );
  });

  it("loads the batch's checks on first expand", async () => {
    const { wrapper } = await mountCard();

    await expander(wrapper).trigger("click");
    expect(wrapper.emitted("update:modelValue")).toContainEqual([true]);
    // The parent owns the prop: v-model flips it and the card loads the checks.
    await wrapper.setProps({ modelValue: true });
    await flushPromises();

    expect(mocks.listQualityExecutions).toHaveBeenCalledTimes(1);
    expect(mocks.listQualityExecutions).toHaveBeenCalledWith({
      run: RUN,
      pageSize: 200,
    });

    const text = wrapper.text();
    expect(text).toContain("Null rate on orders.id");
    expect(text).toContain(labels()("dataQuality.status.fail"));
    expect(text).toContain("4%");
    expect(text).toContain("40");
    expect(text).toContain("2 s");

    // The check title links into the target workspace with the check selected.
    const checkLink = wrapper
      .findAll("a")
      .find((link) => link.text().trim() === "Null rate on orders.id");
    const href = checkLink?.attributes("href") ?? "";
    expect(href).toContain("/data-quality/targets/inst1/shop/public/orders");
    expect(href).toContain("metaType=4");
    expect(href).toContain(`check=${RULE}`);

    // The row keeps a link to the execution itself.
    expect(
      wrapper
        .findAll("a")
        .some((link) =>
          (link.attributes("href") ?? "").includes(
            "/data-quality/executions/workspaces/ws/qualityExecutions/exec-1"
          )
        )
    ).toBe(true);
  });

  it("links the batch into the flat executions list", async () => {
    const { wrapper } = await mountCard();

    const link = wrapper
      .findAll("a")
      .find(
        (candidate) =>
          candidate.text().trim() ===
          labels()("dataQuality.executions.runOpenInList")
      );
    expect(link).toBeDefined();
    const href = decodeURIComponent(link?.attributes("href") ?? "");
    expect(href).toContain("/data-quality/executions");
    expect(href).toContain("view=executions");
    expect(href).toContain(`run=${RUN}`);
  });

  it("shows the load failure with a retry instead of an empty table", async () => {
    mocks.listQualityExecutions.mockRejectedValueOnce(new Error("boom"));
    const { wrapper } = await mountCard();

    await expander(wrapper).trigger("click");
    await wrapper.setProps({ modelValue: true });
    await flushPromises();

    expect(wrapper.text()).toContain(
      labels()("dataQuality.executions.runExecutionsFailed")
    );

    const retry = wrapper
      .findAll("button")
      .find((button) => button.text().trim() === labels()("common.refresh"));
    expect(retry).toBeTruthy();
    await retry?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("Null rate on orders.id");
  });
});
