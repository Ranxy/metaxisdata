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
  QualityAgentMetricsSchema,
  type QualityAgentRunnerStatus,
  QualityAgentRunnerStatusSchema,
  type QualityAgentSession,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
  QualityAgentSessionStateCountSchema,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityAgentSessionsPage from "./DataQualityAgentSessionsPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityAgentSessions: vi.fn(),
  getQualityAgentRunnerStatus: vi.fn(),
  getQualityAgentMetrics: vi.fn(),
  createQualityAgentSession: vi.fn(),
  pauseQualityAgentSession: vi.fn(),
  resumeQualityAgentSession: vi.fn(),
  archiveQualityAgentSession: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityAgentSessions: mocks.listQualityAgentSessions,
  getQualityAgentRunnerStatus: mocks.getQualityAgentRunnerStatus,
  getQualityAgentMetrics: mocks.getQualityAgentMetrics,
  createQualityAgentSession: mocks.createQualityAgentSession,
  pauseQualityAgentSession: mocks.pauseQualityAgentSession,
  resumeQualityAgentSession: mocks.resumeQualityAgentSession,
  archiveQualityAgentSession: mocks.archiveQualityAgentSession,
}));

const NOW = new Date("2026-11-07T10:00:00Z");
const Dummy = { template: "<div />" };
const TABLE = "inst1;db1;public;orders";

function sessionFixture(
  overrides: MessageInitShape<typeof QualityAgentSessionSchema> = {}
): QualityAgentSession {
  return create(QualityAgentSessionSchema, {
    name: "workspaces/ws/qualityAgentSessions/1",
    target: create(QualityTargetSchema, {
      metaGuid: TABLE,
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "inst1",
        database: "db1",
        schema: "public",
        object: "orders",
      }),
    }),
    state: QualityAgentSessionState.ENABLED,
    revision: 2,
    wakeCount: 3,
    tokensUsed: 1234n,
    lastActivityTime: timestampFromDate(NOW),
    nextWakeTime: timestampFromDate(new Date(NOW.getTime() + 86_400_000)),
    ...overrides,
  });
}

function runnerFixture(
  overrides: MessageInitShape<typeof QualityAgentRunnerStatusSchema> = {}
): QualityAgentRunnerStatus {
  return create(QualityAgentRunnerStatusSchema, {
    enabled: true,
    autonomyEnabled: true,
    runningSessions: 1,
    idleSessions: 2,
    blockedSessions: 0,
    tokensLastHour: 1200n,
    tokensPerHourLimit: 100_000n,
    wakesLastHour: 3,
    wakesPerHourLimit: 50,
    lastActivityTime: timestampFromDate(NOW),
    runnerLastTickTime: timestampFromDate(new Date()),
    ...overrides,
  });
}

function metricsFixture(
  counts: MessageInitShape<typeof QualityAgentSessionStateCountSchema>[] = []
) {
  return create(QualityAgentMetricsSchema, {
    sessions: counts.map((count) =>
      create(QualityAgentSessionStateCountSchema, count)
    ),
  });
}

async function mountPage(permissions = PERMISSIONS) {
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
        path: "/data-quality/targets",
        name: "DataQualityTargets",
        component: Dummy,
      },
      {
        path: "/settings/data-quality",
        name: "DataQualitySettings",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/agent");
  await router.isReady();
  const wrapper = mount(DataQualityAgentSessionsPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

const PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.update",
];
const READ_ONLY = ["metaxisdata.dataQuality.rules.list"];

describe("DataQualityAgentSessionsPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    i18n.global.locale.value = "en-US";
    mocks.listQualityAgentSessions.mockResolvedValue([]);
    mocks.getQualityAgentRunnerStatus.mockResolvedValue(runnerFixture());
    mocks.getQualityAgentMetrics.mockResolvedValue(metricsFixture());
  });

  it("explains that the runner is off and links to its settings", async () => {
    mocks.getQualityAgentRunnerStatus.mockResolvedValue(
      runnerFixture({ enabled: false })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.emptyDisabledTitle")
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

  it("renders the runner status strip and the session rows", async () => {
    mocks.listQualityAgentSessions.mockResolvedValue([sessionFixture()]);
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.runnerTicking")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.autonomyOn")
    );
    expect(wrapper.text()).toContain("1.2K");
    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("public.orders");
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.state.enabled")
    );
    expect(wrapper.text()).toContain("3");
    wrapper.unmount();
  });

  it("says the runner is enabled but not ticking when its heartbeat is stale", async () => {
    mocks.getQualityAgentRunnerStatus.mockResolvedValue(
      runnerFixture({
        runnerLastTickTime: timestampFromDate(
          new Date(Date.now() - 10 * 60 * 1000)
        ),
      })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.runnerStopped")
    );
    wrapper.unmount();
  });

  it("re-reads the directory with the selected state filter", async () => {
    const { wrapper } = await mountPage();

    const select = wrapper.findComponent(Select);
    select.vm.$emit("update:modelValue", "blocked");
    await flushPromises();

    expect(mocks.listQualityAgentSessions).toHaveBeenLastCalledWith({
      state: QualityAgentSessionState.BLOCKED,
      signal: undefined,
    });
    wrapper.unmount();
  });

  it("filters rows by target on Enter", async () => {
    mocks.listQualityAgentSessions.mockResolvedValue([
      sessionFixture(),
      sessionFixture({
        name: "workspaces/ws/qualityAgentSessions/2",
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
    ]);
    const { wrapper } = await mountPage();

    const input = wrapper.find("#quality-agent-search");
    await input.setValue("orders");
    await input.trigger("keyup.enter");
    await flushPromises();

    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).not.toContain("customers");
    wrapper.unmount();
  });

  it("pauses a session from its row", async () => {
    mocks.listQualityAgentSessions.mockResolvedValue([sessionFixture()]);
    mocks.pauseQualityAgentSession.mockResolvedValue(
      sessionFixture({ state: QualityAgentSessionState.PAUSED, revision: 3 })
    );
    const { wrapper } = await mountPage();

    const pause = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("dataQuality.agent.pause"))
      );
    await pause?.trigger("click");
    await flushPromises();

    expect(mocks.pauseQualityAgentSession).toHaveBeenCalledWith(
      "workspaces/ws/qualityAgentSessions/1",
      2
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.state.paused")
    );
    wrapper.unmount();
  });

  it("opens the conversation when a row is clicked", async () => {
    mocks.listQualityAgentSessions.mockResolvedValue([sessionFixture()]);
    const { wrapper, router } = await mountPage();

    const row = wrapper.find("tbody tr");
    await row.trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.name).toBe("DataQualityAgentSession");
    wrapper.unmount();
  });

  it("hides every write control from a read-only reader", async () => {
    mocks.listQualityAgentSessions.mockResolvedValue([sessionFixture()]);
    const { wrapper } = await mountPage(READ_ONLY);

    expect(wrapper.text()).toContain("orders");
    const labels = wrapper.findAll("button").map((button) => button.text());
    expect(labels).not.toContain(i18n.global.t("dataQuality.agent.pause"));
    expect(labels).not.toContain(
      i18n.global.t("dataQuality.agent.startFromTable")
    );
    wrapper.unmount();
  });
});
