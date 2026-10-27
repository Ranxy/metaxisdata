import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import {
  QualityExecutionState,
  QualityExecutionTrigger,
  type QualityRun,
  QualityRunSchema,
  QualityStatus,
  QualityTargetSchema,
  type QualityTargetSummary,
  QualityTargetSummarySchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import TargetRecentScanCard from "./TargetRecentScanCard.vue";

const TABLE = "inst1;db1;public;orders";
const START = new Date("2026-10-07T09:30:00Z");
const Dummy = { template: "<div />" };

function summaryFixture(
  options: {
    latestRun?: QualityRun;
    runCount?: number;
    ruleCount?: number;
    enabledRuleCount?: number;
  } = {}
): QualityTargetSummary {
  return create(QualityTargetSummarySchema, {
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
    }),
    ruleCount: options.ruleCount ?? 0,
    enabledRuleCount: options.enabledRuleCount ?? options.ruleCount ?? 0,
    runCount: options.runCount ?? 0,
    latestRun: options.latestRun,
  });
}

function runFixture(
  options: {
    total?: number;
    pass?: number;
    warn?: number;
    fail?: number;
    error?: number;
    skipped?: number;
    notEvaluated?: number;
    outcome?: QualityStatus;
    trigger?: QualityExecutionTrigger;
    state?: QualityExecutionState;
    revisionMin?: number;
    revisionMax?: number;
    running?: boolean;
  } = {}
): QualityRun {
  const start = timestampFromDate(START);
  const finish = timestampFromDate(new Date(START.getTime() + 90_000));
  return create(QualityRunSchema, {
    name: "workspaces/ws/qualityRuns/run-1",
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
    }),
    outcome: options.outcome ?? QualityStatus.FAIL,
    trigger: options.trigger ?? QualityExecutionTrigger.MANUAL,
    state: options.state ?? QualityExecutionState.COMPLETED,
    counts: {
      total: options.total ?? 6,
      pass: options.pass ?? 2,
      warn: options.warn ?? 1,
      fail: options.fail ?? 1,
      error: options.error ?? 1,
      skipped: options.skipped ?? 0,
      notEvaluated: options.notEvaluated ?? 1,
    },
    ruleRevisionMin: options.revisionMin ?? 3,
    ruleRevisionMax: options.revisionMax ?? 5,
    startTime: start,
    finishTime: options.running ? undefined : finish,
  });
}

async function mountCard(summary: QualityTargetSummary) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/executions",
        name: "DataQualityExecutions",
        component: Dummy,
      },
    ],
  });
  await router.push("/");
  await router.isReady();
  return mount(TargetRecentScanCard, {
    props: { summary, targetGuid: TABLE },
    global: { plugins: [router, i18n] },
  });
}

function t(key: string, params: Record<string, unknown> = {}) {
  return i18n.global.t(key, params);
}

describe("TargetRecentScanCard", () => {
  beforeEach(() => {
    localStorage.clear();
    i18n.global.locale.value = "en-US";
  });

  it("explains the empty batch list and links to the target's scan history", async () => {
    const wrapper = await mountCard(summaryFixture());

    const text = wrapper.text();
    expect(text).toContain(t("dataQuality.workspace.recentScanTitle"));
    expect(text).toContain(t("dataQuality.workspace.recentScanEmpty"));
    expect(text).toContain(t("dataQuality.workspace.recentScanEmptyHint"));
    expect(text).toContain(t("dataQuality.workspace.recentScanViewAll"));

    // The link opens the executions page already filtered to this target.
    const href = wrapper.find("a").attributes("href") ?? "";
    expect(href).toContain("/data-quality/executions");
    expect(href).toContain("view=targets");
    expect(href).toContain("target=inst1");
    wrapper.unmount();
  });

  it("summarizes the latest batch's own counts", async () => {
    const wrapper = await mountCard(
      summaryFixture({
        latestRun: runFixture(),
        runCount: 4,
      })
    );

    const text = wrapper.text();
    // Outcome and trigger.
    expect(text).toContain(t("dataQuality.status.fail"));
    expect(text).toContain(t("dataQuality.executionTrigger.manual"));

    // Every non-zero verdict, plus the totals the reader needs to judge the batch.
    expect(text).toContain(t("dataQuality.status.pass"));
    expect(text).toContain(t("dataQuality.status.warn"));
    expect(text).toContain(t("dataQuality.status.error"));
    expect(text).toContain(t("dataQuality.status.notEvaluated"));
    expect(text).toContain(
      t("dataQuality.workspace.recentScanChecks", { count: 6 })
    );
    expect(text).toContain(
      t("dataQuality.workspace.recentScanFailed", { count: 2 })
    );
    // All six planned checks reached a verdict, so there is no unfinished line.
    expect(text).not.toContain(
      t("dataQuality.workspace.recentScanUnfinished", { count: 0 })
    );
    // A zero count is omitted, not rendered as "0".
    expect(text).not.toContain(t("dataQuality.status.skipped"));

    // Duration, revision range and the number of batches that planned checks.
    expect(text).toContain(
      t("dataQuality.workspace.recentScanDuration", { duration: "1m 30s" })
    );
    expect(text).toContain(
      t("dataQuality.executions.revisionRange", { min: 3, max: 5 })
    );
    expect(text).toContain(
      t("dataQuality.executions.runCountValue", { count: 4 })
    );

    const href = wrapper.find("a").attributes("href") ?? "";
    expect(href).toContain("view=targets");
    expect(href).toContain("target=inst1");
    wrapper.unmount();
  });

  it("names a still-running batch and its unfinished checks", async () => {
    const wrapper = await mountCard(
      summaryFixture({
        latestRun: runFixture({
          total: 3,
          pass: 1,
          warn: 0,
          fail: 0,
          error: 0,
          notEvaluated: 0,
          outcome: QualityStatus.UNSPECIFIED,
          state: QualityExecutionState.RUNNING,
          running: true,
        }),
      })
    );

    const text = wrapper.text();
    expect(text).toContain(t("dataQuality.executions.runRunningHint"));
    // No verdict yet: no outcome badge to misread as a finished batch.
    expect(text).not.toContain(t("dataQuality.status.fail"));
    expect(text).toContain(
      t("dataQuality.workspace.recentScanUnfinished", { count: 2 })
    );
    // A run without a finish time reports no duration rather than a fake one.
    expect(text).toContain(
      t("dataQuality.workspace.recentScanDuration", { duration: "-" })
    );
    wrapper.unmount();
  });

  it("names an abandoned batch and keeps the revision range singular when it never changed", async () => {
    const wrapper = await mountCard(
      summaryFixture({
        latestRun: runFixture({
          outcome: QualityStatus.UNSPECIFIED,
          state: QualityExecutionState.ABANDONED,
          revisionMin: 2,
          revisionMax: 2,
        }),
      })
    );

    const text = wrapper.text();
    expect(text).toContain(t("dataQuality.executions.runAbandonedHint"));
    expect(text).toContain(
      t("dataQuality.executions.revisionSingle", { revision: 2 })
    );
    expect(text).not.toContain(
      t("dataQuality.executions.revisionRange", { min: 2, max: 2 })
    );
    wrapper.unmount();
  });

  it("only calls a scheduled batch partial, never a manual selection", async () => {
    const scheduled = await mountCard(
      summaryFixture({
        latestRun: runFixture({
          total: 3,
          trigger: QualityExecutionTrigger.SCHEDULED,
        }),
        ruleCount: 10,
      })
    );
    expect(scheduled.text()).toContain(
      t("dataQuality.executions.runPartial", { done: 3, all: 10 })
    );
    scheduled.unmount();

    const manual = await mountCard(
      summaryFixture({
        latestRun: runFixture({ total: 3 }),
        ruleCount: 10,
      })
    );
    expect(manual.text()).not.toContain(
      t("dataQuality.executions.runPartial", { done: 3, all: 10 })
    );
    manual.unmount();
  });
});
