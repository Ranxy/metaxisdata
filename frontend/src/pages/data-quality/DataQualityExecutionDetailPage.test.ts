import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  GetQualityTargetImpactResponseSchema,
  QualityComparison,
  type QualityExecution,
  QualityExecutionSchema,
  QualityExecutionState,
  QualityExecutionTrigger,
  QualityObservedKind,
  QualityRuleSchema,
  QualityRuleType,
  QualityStatus,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityThresholdsSchema,
  SegmentObservationSchema,
  SegmentRecordSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { formatDateTime } from "@/utils/datetime";
import DataQualityExecutionDetailPage from "./DataQualityExecutionDetailPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityExecution: vi.fn(),
  getQualityRule: vi.fn(),
  getQualityTargetImpact: vi.fn(),
  getWorkspaceProfileSetting: vi.fn(),
  listAllUsers: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityExecution: mocks.getQualityExecution,
  getQualityRule: mocks.getQualityRule,
  getQualityTargetImpact: mocks.getQualityTargetImpact,
}));

vi.mock("@/api/setting", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/setting")>()),
  getWorkspaceProfileSetting: mocks.getWorkspaceProfileSetting,
}));

vi.mock("@/api/user", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/user")>()),
  listAllUsers: mocks.listAllUsers,
}));

const RULE = "workspaces/ws/qualityRules/rule-1";
const EXECUTION = "workspaces/ws/qualityExecutions/exec-1";
const START = new Date("2026-10-07T09:30:00Z");
const Dummy = { template: "<div />" };

const PERMISSIONS = [
  "metaxisdata.dataQuality.executions.get",
  "metaxisdata.dataQuality.executions.list",
  "metaxisdata.dataQuality.rules.get",
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.explain",
  "metaxisdata.users.list",
];

function executionFixture(
  overrides: Record<string, unknown> = {}
): QualityExecution {
  return create(QualityExecutionSchema, {
    name: EXECUTION,
    rule: RULE,
    ruleRevision: 3,
    ruleEngineVersion: "2026.10",
    state: QualityExecutionState.COMPLETED,
    outcome: QualityStatus.FAIL,
    trigger: QualityExecutionTrigger.MANUAL,
    initiator: "users/7",
    instance: "instances/inst1",
    target: create(QualityTargetSchema, {
      metaGuid: "inst1;shop;public;orders",
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "prod",
        database: "shop",
        schema: "public",
        object: "orders",
        column: "id",
      }),
    }),
    observedValue: 0.04,
    observedKind: QualityObservedKind.RATIO,
    thresholds: create(QualityThresholdsSchema, {
      comparison: QualityComparison.GREATER_THAN,
      failValue: 0,
    }),
    failedRows: 40n,
    totalRows: 1000n,
    samples: [
      {
        columns: [
          { column: "id", value: "1" },
          { column: "email", value: "a@example.com" },
          { column: "note", value: "", isNull: true },
          { column: "title", value: "", isNull: false },
          { column: "id_card", value: "110105********002X", masked: true },
          { column: "legacy", value: "" },
        ],
      },
      {
        columns: [
          { column: "id", value: "2" },
          { column: "email", value: "b@example.com" },
        ],
      },
    ],
    executedSql: "SELECT COUNT(*) FROM public.orders WHERE id IS NULL",
    startTime: timestampFromDate(START),
    finishTime: timestampFromDate(new Date(START.getTime() + 2_000)),
    ...overrides,
  });
}

async function mountPage(permissions = PERMISSIONS) {
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
        path: "/data-quality/rules/:ruleId+",
        name: "DataQualityRuleRedirect",
        component: Dummy,
      },
    ],
  });
  await router.push(`/data-quality/executions/${EXECUTION}`);
  await router.isReady();
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const wrapper = mount(DataQualityExecutionDetailPage, {
    global: {
      plugins: [router, i18n, pinia],
      stubs: { QualityExplainDrawer: true },
    },
  });
  await flushPromises();
  return { wrapper, router };
}

describe("DataQualityExecutionDetailPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    mocks.getQualityExecution.mockResolvedValue(executionFixture());
    mocks.getQualityTargetImpact.mockResolvedValue(
      create(GetQualityTargetImpactResponseSchema, {})
    );
    mocks.getQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: RULE,
        title: "Null rate on orders.id",
      })
    );
    mocks.getWorkspaceProfileSetting.mockResolvedValue({
      dataQuality: { enableLlmAnalysis: true },
    });
    mocks.listAllUsers.mockResolvedValue([
      create(UserSchema, { name: "users/7", email: "owner@example.com" }),
    ]);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders the verdict and its evidence", async () => {
    const { wrapper } = await mountPage();
    const text = wrapper.text();

    expect(text).toContain("Null rate on orders.id");
    expect(text).toContain("prod · shop · public.orders.id");
    expect(text).toContain(i18n.global.t("dataQuality.status.fail"));
    expect(text).toContain("4%");
    expect(text).toContain("> 0%");
    expect(text).toContain("40 / 1,000");
    expect(text).toContain("2 s");
    expect(text).toContain(
      i18n.global.t("dataQuality.executionTrigger.manual")
    );
    expect(text).toContain("inst1");
    // The initiator is a principal resource name; the page resolves it through
    // the workspace directory rather than printing the numeric id.
    expect(text).toContain("owner@example.com");
    expect(wrapper.text()).toContain("a@example.com");
    expect(wrapper.text()).toContain("b@example.com");
    // Every sampled row is numbered, so two failures on the same column do not
    // read as one row repeated.
    expect(text).toContain(
      i18n.global.t("dataQuality.sampleNumber", { index: 1 })
    );
    expect(text).toContain(
      i18n.global.t("dataQuality.sampleNumber", { index: 2 })
    );
    // A NULL and an empty string are both empty on the wire; the page says which
    // one it was instead of printing a blank cell.
    expect(text).toContain(i18n.global.t("dataQuality.sampleNull"));
    expect(text).toContain(i18n.global.t("dataQuality.sampleEmpty"));
    // A redacted value is shown as the display form with the masked marker.
    expect(text).toContain("110105********002X");
    expect(text).toContain(i18n.global.t("dataQuality.sampleMasked"));
    // A sample recorded before the marker existed cannot be either: it says so,
    // with the explanation on the cell.
    expect(text).toContain(i18n.global.t("dataQuality.sampleAmbiguous"));
    expect(wrapper.html()).toContain(
      i18n.global.t("dataQuality.sampleLegacyHint")
    );
    expect(wrapper.text()).toContain("SELECT COUNT(*) FROM public.orders");
    // A finished execution does not poll.
    expect(text).not.toContain(i18n.global.t("dataQuality.polling"));

    // A failed verdict explains its lineage; the root-cause window ends at the
    // run's start time, so the evidence predates the failure.
    expect(text).toContain(i18n.global.t("dataQuality.impact.title"));
    expect(mocks.getQualityTargetImpact).toHaveBeenCalledWith(
      expect.objectContaining({
        targetGuid: "inst1;shop;public;orders",
        metaType: MetaType.TABLE,
        depth: 2,
        windowHours: 24,
        asOf: timestampFromDate(START),
      })
    );
  });

  it("shows the impact card only for a failed or warned verdict", async () => {
    mocks.getQualityExecution.mockResolvedValue(
      executionFixture({ outcome: QualityStatus.PASS })
    );
    const passed = await mountPage();
    expect(passed.wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.impact.title")
    );
    expect(mocks.getQualityTargetImpact).not.toHaveBeenCalled();
    passed.wrapper.unmount();

    mocks.getQualityExecution.mockResolvedValue(
      executionFixture({ outcome: QualityStatus.WARN })
    );
    const warned = await mountPage();
    expect(warned.wrapper.text()).toContain(
      i18n.global.t("dataQuality.impact.title")
    );
    warned.wrapper.unmount();
  });

  it("polls a running execution until it reaches a verdict", async () => {
    vi.useFakeTimers();
    mocks.getQualityExecution.mockResolvedValue(
      executionFixture({
        state: QualityExecutionState.RUNNING,
        outcome: QualityStatus.UNSPECIFIED,
        finishTime: undefined,
      })
    );
    const { wrapper } = await mountPage();
    await flushPromises();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.executionState.running")
    );
    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.polling"));

    mocks.getQualityExecution.mockResolvedValue(executionFixture());
    await vi.advanceTimersByTimeAsync(3_000);
    await flushPromises();

    expect(wrapper.text()).toContain(i18n.global.t("dataQuality.status.fail"));
    expect(wrapper.text()).not.toContain(i18n.global.t("dataQuality.polling"));
    wrapper.unmount();
  });

  it("offers the model explanation only for a failed verdict", async () => {
    const { wrapper } = await mountPage();

    const explain = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("dataQuality.llm.explain.action"))
      );
    expect(explain).toBeTruthy();

    mocks.getWorkspaceProfileSetting.mockResolvedValue({
      dataQuality: { enableLlmAnalysis: false },
    });
    const disabled = await mountPage();
    expect(
      disabled.wrapper
        .findAll("button")
        .some((button) =>
          button
            .text()
            .includes(i18n.global.t("dataQuality.llm.explain.action"))
        )
    ).toBe(false);
    disabled.wrapper.unmount();
    wrapper.unmount();
  });

  it("surfaces a load failure instead of an empty page", async () => {
    mocks.getQualityExecution.mockRejectedValue(new Error("not found"));

    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.fetchExecutionError")
    );
  });
  it("names the baseline a count-change execution compared with", async () => {
    mocks.getQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: RULE,
        title: "Row count stays stable",
        ruleType: QualityRuleType.ROW_COUNT_CHANGE,
      })
    );
    const baselineAt = new Date(START.getTime() - 86_400_000);
    mocks.getQualityExecution.mockResolvedValue(
      executionFixture({
        baselineValue: 900,
        baselineTime: timestampFromDate(baselineAt),
      })
    );
    const withBaseline = await mountPage();
    const text = withBaseline.wrapper.text();
    expect(text).toContain(i18n.global.t("dataQuality.baseline.label"));
    // The baseline is a row count with the time it was measured.
    expect(text).toContain(
      i18n.global.t("dataQuality.baseline.value", {
        value: "900",
        time: formatDateTime(timestampFromDate(baselineAt), "en-US"),
      })
    );
    withBaseline.wrapper.unmount();

    // A first observation has no baseline to compare with, and says so.
    mocks.getQualityExecution.mockResolvedValue(executionFixture());
    const withoutBaseline = await mountPage();
    expect(withoutBaseline.wrapper.text()).toContain(
      i18n.global.t("dataQuality.baseline.missing")
    );
    withoutBaseline.wrapper.unmount();
  });

  it("shows the reference statement only when the reconciliation used one", async () => {
    const withoutReference = await mountPage();
    expect(withoutReference.wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.referenceExecutedSql")
    );
    withoutReference.wrapper.unmount();

    // A reference in another database or instance runs over its own connection,
    // so the execution carries a second statement beside the target's.
    mocks.getQualityExecution.mockResolvedValue(
      executionFixture({
        referenceExecutedSql:
          "SELECT COUNT(*) AS right_value FROM other.customers",
      })
    );
    const withReference = await mountPage();
    const text = withReference.wrapper.text();
    expect(text).toContain(i18n.global.t("dataQuality.referenceExecutedSql"));
    expect(text).toContain(
      "SELECT COUNT(*) AS right_value FROM other.customers"
    );
    withReference.wrapper.unmount();
  });

  it("renders the passing rows beside the failed rows when it kept them", async () => {
    mocks.getQualityExecution.mockResolvedValue(
      executionFixture({
        passingSamples: [
          {
            columns: [
              { column: "id", value: "9" },
              { column: "email", value: "ok@example.com" },
            ],
          },
        ],
      })
    );
    const { wrapper } = await mountPage();
    const text = wrapper.text();
    expect(text).toContain(i18n.global.t("dataQuality.passingSamples"));
    expect(text).toContain(i18n.global.t("dataQuality.passingSamplesHint"));
    expect(text).toContain("ok@example.com");
    // The failed rows card is still there: the two populations sit together.
    expect(text).toContain(i18n.global.t("dataQuality.failureSamples"));
    wrapper.unmount();
  });

  it("does not render an empty passing-rows card", async () => {
    const { wrapper } = await mountPage();
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.passingSamples")
    );
    wrapper.unmount();
  });

  it("renders a segment check's evidence instead of failed rows", async () => {
    mocks.getQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: RULE,
        title: "Every region keeps reporting",
        ruleType: QualityRuleType.SEGMENT_CHECK,
      })
    );
    const lastSeen = new Date(START.getTime() - 3_600_000);
    mocks.getQualityExecution.mockResolvedValue(
      executionFixture({
        samples: [],
        observedValue: 2,
        observedKind: QualityObservedKind.COUNT,
        segmentEvidence: create(SegmentObservationSchema, {
          observed: [
            create(SegmentRecordSchema, {
              values: ["emea", "web"],
              rowCount: 10n,
            }),
          ],
          newSegments: [
            create(SegmentRecordSchema, {
              values: ["latam", "app"],
              rowCount: 4n,
            }),
          ],
          disappeared: [
            create(SegmentRecordSchema, {
              values: ["apac", "web"],
              rowCount: 7n,
              lastSeen: timestampFromDate(lastSeen),
            }),
          ],
          baselineRuns: 3,
          expectedCount: 2n,
        }),
      })
    );
    const { wrapper } = await mountPage();
    const text = wrapper.text();

    expect(text).toContain(i18n.global.t("dataQuality.segmentEvidence.title"));
    // The disappeared segment is the failure: its values, row count and last run.
    expect(text).toContain(
      i18n.global.t("dataQuality.segmentEvidence.disappeared")
    );
    expect(text).toContain("apac · web");
    expect(text).toContain(
      i18n.global.t("dataQuality.segmentEvidence.rowCount", { count: "7" })
    );
    expect(text).toContain(
      i18n.global.t("dataQuality.segmentEvidence.lastSeen", { time: "" }).trim()
    );
    // The new segment is evidence only.
    expect(text).toContain(
      i18n.global.t("dataQuality.segmentEvidence.newSegments")
    );
    expect(text).toContain("latam · app");
    expect(text).toContain(
      i18n.global.t("dataQuality.segmentEvidence.baseline", {
        runs: "3",
        expected: "2",
      })
    );
    // A segment check keeps no failed-row samples, so it shows no such card.
    expect(text).not.toContain(i18n.global.t("dataQuality.failureSamples"));
    expect(text).not.toContain(i18n.global.t("dataQuality.noSamples"));
    wrapper.unmount();
  });

  it("says when a segment observation was truncated and not judged", async () => {
    mocks.getQualityRule.mockResolvedValue(
      create(QualityRuleSchema, {
        name: RULE,
        title: "Segment set",
        ruleType: QualityRuleType.SEGMENT_CHECK,
      })
    );
    mocks.getQualityExecution.mockResolvedValue(
      executionFixture({
        samples: [],
        outcome: QualityStatus.NOT_EVALUATED,
        segmentEvidence: create(SegmentObservationSchema, {
          observed: [
            create(SegmentRecordSchema, {
              values: ["emea"],
              rowCount: 10n,
            }),
          ],
          truncated: true,
        }),
      })
    );
    const { wrapper } = await mountPage();
    const text = wrapper.text();
    expect(text).toContain(
      i18n.global.t("dataQuality.segmentEvidence.truncated")
    );
    expect(text).toContain(
      i18n.global.t("dataQuality.segmentEvidence.noNewSegments")
    );
    wrapper.unmount();
  });
});
