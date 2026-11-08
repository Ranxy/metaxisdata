import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityAgentDatabaseEnrollmentSummarySchema,
  type QualityAgentEnrollment,
  QualityAgentEnrollmentSchema,
  QualityAgentEnrollmentState,
  QualityAgentEnrollmentStateCountSchema,
  type QualityAgentEnrollmentSummary,
  QualityAgentEnrollmentSummarySchema,
  type QualityAgentRunnerStatus,
  QualityAgentRunnerStatusSchema,
  QualityAgentSessionMode,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { formatDateTime } from "@/utils/datetime";
import DataQualityAgentEnrollmentPage from "./DataQualityAgentEnrollmentPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityAgentEnrollments: vi.fn(),
  getQualityAgentEnrollmentSummary: vi.fn(),
  getQualityAgentRunnerStatus: vi.fn(),
  updateQualityAgentEnrollment: vi.fn(),
  retryQualityAgentEnrollment: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityAgentEnrollments: mocks.listQualityAgentEnrollments,
  getQualityAgentEnrollmentSummary: mocks.getQualityAgentEnrollmentSummary,
  getQualityAgentRunnerStatus: mocks.getQualityAgentRunnerStatus,
  updateQualityAgentEnrollment: mocks.updateQualityAgentEnrollment,
  retryQualityAgentEnrollment: mocks.retryQualityAgentEnrollment,
}));

const NOW = new Date("2026-11-07T10:00:00Z");
const Dummy = { template: "<div />" };
const TARGET = "inst1;db1;public;orders";
const DATABASE = "instances/inst1/databases/db1";
const ENROLLMENT = "workspaces/ws/qualityAgentEnrollments/1";

function enrollmentFixture(
  overrides: MessageInitShape<typeof QualityAgentEnrollmentSchema> = {}
): QualityAgentEnrollment {
  return create(QualityAgentEnrollmentSchema, {
    name: ENROLLMENT,
    target: create(QualityTargetSchema, {
      metaGuid: TARGET,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "inst1",
        database: "db1",
        schema: "public",
        object: "orders",
      }),
    }),
    database: DATABASE,
    state: QualityAgentEnrollmentState.PENDING,
    mode: QualityAgentSessionMode.TRIAGE,
    priority: 640,
    priorityReason: "rules,failing,consumers:12,critical",
    revision: 4,
    ...overrides,
  });
}

function summaryFixture(
  overrides: MessageInitShape<typeof QualityAgentEnrollmentSummarySchema> = {}
): QualityAgentEnrollmentSummary {
  return create(QualityAgentEnrollmentSummarySchema, {
    states: [
      create(QualityAgentEnrollmentStateCountSchema, {
        state: QualityAgentEnrollmentState.PENDING,
        count: 3,
      }),
      create(QualityAgentEnrollmentStateCountSchema, {
        state: QualityAgentEnrollmentState.ACTIVE,
        count: 2,
      }),
      create(QualityAgentEnrollmentStateCountSchema, {
        state: QualityAgentEnrollmentState.DORMANT,
        count: 1,
      }),
    ],
    databases: [
      create(QualityAgentDatabaseEnrollmentSummarySchema, {
        database: DATABASE,
        pending: 3,
        active: 2,
        dormant: 1,
      }),
    ],
    lastScanTime: timestampFromDate(NOW),
    ...overrides,
  });
}

function runnerFixture(
  overrides: MessageInitShape<typeof QualityAgentRunnerStatusSchema> = {}
): QualityAgentRunnerStatus {
  return create(QualityAgentRunnerStatusSchema, {
    enabled: true,
    autoEnrollEnabled: true,
    runningSessions: 0,
    idleSessions: 3,
    tokensLastHour: 0n,
    tokensPerHourLimit: 100_000n,
    wakesLastHour: 0,
    wakesPerHourLimit: 50,
    maxNewSessionsPerHour: 10,
    maxActiveSessions: 100,
    runnerLastTickTime: timestampFromDate(new Date()),
    ...overrides,
  });
}

function pageOf(items: QualityAgentEnrollment[], nextPageToken = "") {
  return { items, nextPageToken };
}

async function mountPageRaw(permissions = PERMISSIONS) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/", name: "Home", component: Dummy },
      {
        path: "/data-quality/agent",
        name: "DataQualityAgent",
        component: Dummy,
      },
      {
        path: "/data-quality/agent/enrollment",
        name: "DataQualityAgentEnrollment",
        component: Dummy,
      },
      {
        path: "/data-quality/agent/:sessionId+",
        name: "DataQualityAgentSession",
        component: Dummy,
      },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
      {
        path: "/settings/data-quality",
        name: "DataQualitySettings",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/agent/enrollment");
  await router.isReady();
  const wrapper = mount(DataQualityAgentEnrollmentPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  return { wrapper, router };
}

async function mountPage(permissions = PERMISSIONS) {
  const mounted = await mountPageRaw(permissions);
  await flushPromises();
  return mounted;
}

/** A button inside one of the page's (teleported) dialogs, by its text. */
function dialogButton(text: string): HTMLElement | undefined {
  return [
    ...document.body.querySelectorAll<HTMLElement>('[role="dialog"] button'),
  ].find((button) => button.textContent?.trim() === text);
}

function selectByID(wrapper: ReturnType<typeof mount>, id: string) {
  return wrapper
    .findAllComponents(Select)
    .find((candidate) => candidate.find(`#${id}`).exists());
}

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.update",
];
const READ_ONLY = ["metaxisdata.dataQuality.rules.list"];

describe("DataQualityAgentEnrollmentPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    vi.useRealTimers();
    i18n.global.locale.value = "en-US";
    mocks.listQualityAgentEnrollments.mockResolvedValue(pageOf([]));
    mocks.getQualityAgentEnrollmentSummary.mockResolvedValue(summaryFixture());
    mocks.getQualityAgentRunnerStatus.mockResolvedValue(runnerFixture());
    mocks.retryQualityAgentEnrollment.mockImplementation(async (name: string) =>
      enrollmentFixture({ name, priorityReason: "rules", priority: 400 })
    );
    mocks.updateQualityAgentEnrollment.mockImplementation(
      async ({ enrollment }: { enrollment: QualityAgentEnrollment }) =>
        create(QualityAgentEnrollmentSchema, enrollment)
    );
  });

  it("renders the summary, the database grouping and the queue rows", async () => {
    mocks.listQualityAgentEnrollments.mockResolvedValue(
      pageOf([
        enrollmentFixture({
          blockedReason: "no-model",
          lastTriagedTime: timestampFromDate(NOW),
          session: "workspaces/ws/qualityAgentSessions/9",
        }),
      ])
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.enrollment.title")
    );
    // The summary cards count the queue rather than the current page.
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.enrollment.state.pending")
    );
    expect(wrapper.text()).toContain("inst1 · db1");
    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("public.orders");
    // Each priority signal is labelled, including the counted consumers.
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.enrollment.prioritySignal.rules")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.enrollment.prioritySignal.consumers", {
        count: 12,
      })
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.enrollment.blocked.noModel")
    );
    expect(
      wrapper
        .find(`[title="${formatDateTime(timestampFromDate(NOW), "en-US")}"]`)
        .exists()
    ).toBe(true);
    wrapper.unmount();
  });

  it("filters the queue by state, database and mode on the server", async () => {
    const { wrapper } = await mountPage();

    selectByID(wrapper, "quality-enrollment-state")?.vm.$emit(
      "update:modelValue",
      String(QualityAgentEnrollmentState.ACTIVE)
    );
    await flushPromises();
    expect(mocks.listQualityAgentEnrollments).toHaveBeenLastCalledWith(
      expect.objectContaining({ state: QualityAgentEnrollmentState.ACTIVE })
    );

    selectByID(wrapper, "quality-enrollment-database")?.vm.$emit(
      "update:modelValue",
      DATABASE
    );
    await flushPromises();
    expect(mocks.listQualityAgentEnrollments).toHaveBeenLastCalledWith(
      expect.objectContaining({ database: DATABASE })
    );

    selectByID(wrapper, "quality-enrollment-mode")?.vm.$emit(
      "update:modelValue",
      String(QualityAgentSessionMode.TRIAGE)
    );
    await flushPromises();
    expect(mocks.listQualityAgentEnrollments).toHaveBeenLastCalledWith(
      expect.objectContaining({ mode: QualityAgentSessionMode.TRIAGE })
    );
    wrapper.unmount();
  });

  it("filters by a state card's link", async () => {
    const { wrapper, router } = await mountPage();

    const link = wrapper
      .findAll("a")
      .find((anchor) =>
        (anchor.attributes("href") ?? "").includes(
          `state=${QualityAgentEnrollmentState.PENDING}`
        )
      );
    expect(link).toBeTruthy();
    await link?.trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.query.state).toBe(
      String(QualityAgentEnrollmentState.PENDING)
    );
    expect(mocks.listQualityAgentEnrollments).toHaveBeenLastCalledWith(
      expect.objectContaining({ state: QualityAgentEnrollmentState.PENDING })
    );
    wrapper.unmount();
  });

  it("searches on the server when Enter is pressed", async () => {
    const { wrapper } = await mountPage();

    const input = wrapper.find("#quality-enrollment-search");
    await input.setValue("orders");
    await input.trigger("keyup.enter");
    await flushPromises();

    expect(mocks.listQualityAgentEnrollments).toHaveBeenLastCalledWith(
      expect.objectContaining({ search: "orders" })
    );
    wrapper.unmount();
  });

  it("pages forward with the server's cursor", async () => {
    mocks.listQualityAgentEnrollments
      .mockResolvedValueOnce(pageOf([enrollmentFixture()], "cursor-2"))
      .mockResolvedValueOnce(
        pageOf([
          enrollmentFixture({
            name: "workspaces/ws/qualityAgentEnrollments/2",
            target: create(QualityTargetSchema, {
              metaGuid: "inst1;db1;public;customers",
              metaType: MetaType.TABLE,
              path: create(QualityTargetPathSchema, {
                instance: "inst1",
                database: "db1",
                schema: "public",
                object: "customers",
              }),
            }),
          }),
        ])
      );
    const { wrapper } = await mountPage();

    const next = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.next")));
    await next?.trigger("click");
    await flushPromises();

    expect(mocks.listQualityAgentEnrollments).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "cursor-2" })
    );
    expect(wrapper.text()).toContain("customers");
    wrapper.unmount();
  });

  it("retries an enrollment with the revision it saw", async () => {
    mocks.listQualityAgentEnrollments.mockResolvedValue(
      pageOf([
        enrollmentFixture({
          state: QualityAgentEnrollmentState.DORMANT,
          nextReviewTime: timestampFromDate(NOW),
        }),
      ])
    );
    const { wrapper } = await mountPage();

    await wrapper
      .find(
        `button[title="${i18n.global.t("dataQuality.agent.enrollment.retry")}"]`
      )
      .trigger("click");
    await flushPromises();

    expect(mocks.retryQualityAgentEnrollment).toHaveBeenCalledWith(
      ENROLLMENT,
      4
    );
    wrapper.unmount();
  });

  it("excludes a target after a confirmation and with the seen revision", async () => {
    mocks.listQualityAgentEnrollments.mockResolvedValue(
      pageOf([enrollmentFixture()])
    );
    const { wrapper } = await mountPage();

    await wrapper
      .find(
        `button[title="${i18n.global.t("dataQuality.agent.enrollment.exclude")}"]`
      )
      .trigger("click");
    await flushPromises();

    expect(document.body.textContent).toContain(
      i18n.global.t("dataQuality.agent.enrollment.excludeTitle")
    );
    dialogButton(
      i18n.global.t("dataQuality.agent.enrollment.exclude")
    )?.click();
    await flushPromises();

    expect(mocks.updateQualityAgentEnrollment).toHaveBeenCalledTimes(1);
    const request = mocks.updateQualityAgentEnrollment.mock.calls[0]?.[0] as {
      enrollment: QualityAgentEnrollment;
      updateMask: string[];
      expectedRevision: number;
    };
    expect(request.updateMask).toEqual(["state"]);
    expect(request.expectedRevision).toBe(4);
    expect(request.enrollment.name).toBe(ENROLLMENT);
    expect(request.enrollment.state).toBe(QualityAgentEnrollmentState.EXCLUDED);
    wrapper.unmount();
  });

  it("re-includes an excluded target", async () => {
    mocks.listQualityAgentEnrollments.mockResolvedValue(
      pageOf([
        enrollmentFixture({ state: QualityAgentEnrollmentState.EXCLUDED }),
      ])
    );
    const { wrapper } = await mountPage();

    await wrapper
      .find(
        `button[title="${i18n.global.t("dataQuality.agent.enrollment.include")}"]`
      )
      .trigger("click");
    await flushPromises();

    expect(mocks.updateQualityAgentEnrollment).toHaveBeenCalledTimes(1);
    const request = mocks.updateQualityAgentEnrollment.mock.calls[0]?.[0] as {
      enrollment: QualityAgentEnrollment;
      updateMask: string[];
    };
    expect(request.updateMask).toEqual(["state"]);
    expect(request.enrollment.state).toBe(QualityAgentEnrollmentState.PENDING);
    wrapper.unmount();
  });

  it("hides the write actions from a read-only reader", async () => {
    mocks.listQualityAgentEnrollments.mockResolvedValue(
      pageOf([enrollmentFixture()])
    );
    const { wrapper } = await mountPage(READ_ONLY);

    expect(wrapper.text()).toContain("orders");
    expect(
      wrapper
        .find(
          `button[title="${i18n.global.t("dataQuality.agent.enrollment.retry")}"]`
        )
        .exists()
    ).toBe(false);
    expect(
      wrapper
        .find(
          `button[title="${i18n.global.t("dataQuality.agent.enrollment.exclude")}"]`
        )
        .exists()
    ).toBe(false);
    wrapper.unmount();
  });

  it("explains that the runner is off", async () => {
    mocks.getQualityAgentRunnerStatus.mockResolvedValue(
      runnerFixture({ enabled: false })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.enrollment.runnerOffTitle")
    );
    const link = wrapper
      .findAll("a")
      .find(
        (anchor) =>
          anchor.text() === i18n.global.t("dataQuality.agent.openSettings")
      );
    expect(link?.attributes("href")).toBe("/settings/data-quality");
    wrapper.unmount();
  });

  it("explains that automatic enrollment is off", async () => {
    mocks.getQualityAgentRunnerStatus.mockResolvedValue(
      runnerFixture({ autoEnrollEnabled: false })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.enrollment.autoEnrollOffTitle")
    );
    wrapper.unmount();
  });

  it("waits for the first discovery scan", async () => {
    mocks.getQualityAgentEnrollmentSummary.mockResolvedValue(
      summaryFixture({ lastScanTime: undefined })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.enrollment.waitingTitle")
    );
    wrapper.unmount();
  });

  it("polls while the queue has pending work", async () => {
    vi.useFakeTimers();
    try {
      const { wrapper } = await mountPageRaw();
      await vi.advanceTimersByTimeAsync(0);
      const initialCalls = mocks.listQualityAgentEnrollments.mock.calls.length;
      expect(initialCalls).toBe(1);

      await vi.advanceTimersByTimeAsync(10_000);

      expect(
        mocks.listQualityAgentEnrollments.mock.calls.length
      ).toBeGreaterThan(initialCalls);
      wrapper.unmount();
    } finally {
      vi.useRealTimers();
    }
  });
});
