import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import StatCard from "@/components/common/StatCard.vue";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityAgentMetricsSchema,
  type QualityAgentRunnerStatus,
  QualityAgentRunnerStatusSchema,
  type QualityAgentSession,
  QualityAgentSessionMode,
  QualityAgentSessionSchema,
  QualityAgentSessionSource,
  QualityAgentSessionState,
  QualityAgentSessionStateCountSchema,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityAgentSessionsPage from "./DataQualityAgentSessionsPage.vue";

const mocks = vi.hoisted(() => ({
  listQualityAgentSessionsPage: vi.fn(),
  getQualityAgentRunnerStatus: vi.fn(),
  getQualityAgentMetrics: vi.fn(),
  createQualityAgentSession: vi.fn(),
  pauseQualityAgentSession: vi.fn(),
  resumeQualityAgentSession: vi.fn(),
  archiveQualityAgentSession: vi.fn(),
  listDatabases: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityAgentSessionsPage: mocks.listQualityAgentSessionsPage,
  getQualityAgentRunnerStatus: mocks.getQualityAgentRunnerStatus,
  getQualityAgentMetrics: mocks.getQualityAgentMetrics,
  createQualityAgentSession: mocks.createQualityAgentSession,
  pauseQualityAgentSession: mocks.pauseQualityAgentSession,
  resumeQualityAgentSession: mocks.resumeQualityAgentSession,
  archiveQualityAgentSession: mocks.archiveQualityAgentSession,
}));

vi.mock("@/api/database", () => ({
  listDatabases: mocks.listDatabases,
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
    source: QualityAgentSessionSource.AUTO,
    mode: QualityAgentSessionMode.TRIAGE,
    revision: 2,
    wakeCount: 3,
    tokensUsed: 1234n,
    lastActivityTime: timestampFromDate(NOW),
    nextWakeTime: timestampFromDate(new Date(NOW.getTime() + 86_400_000)),
    ...overrides,
  });
}

/** One page of the sessions list, as the server-paged wrapper returns it. */
function pageOf(items: QualityAgentSession[], nextPageToken = "") {
  return { items, nextPageToken };
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
    mocks.listQualityAgentSessionsPage.mockResolvedValue(pageOf([]));
    mocks.getQualityAgentRunnerStatus.mockResolvedValue(runnerFixture());
    mocks.getQualityAgentMetrics.mockResolvedValue(metricsFixture());
    mocks.listDatabases.mockResolvedValue({ databases: [] });
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
    mocks.listQualityAgentSessionsPage.mockResolvedValue(
      pageOf([sessionFixture()])
    );
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
    // The source and mode badges distinguish an automatic triage session.
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.source.auto")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.mode.triage")
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

  it("shows no counts when the metrics read fails and keeps the list", async () => {
    mocks.getQualityAgentMetrics.mockRejectedValue(new Error("metrics down"));
    mocks.listQualityAgentSessionsPage.mockResolvedValue(
      pageOf([sessionFixture()])
    );
    const { wrapper } = await mountPage();

    const values = wrapper
      .findAllComponents(StatCard)
      .map((card) => card.props("value"));
    expect(values).toEqual(["—", "—", "—", "—"]);
    expect(wrapper.text()).toContain("orders");
    wrapper.unmount();
  });

  it("re-reads the directory with the selected state filter", async () => {
    const { wrapper } = await mountPage();

    const select = wrapper
      .findAllComponents(Select)
      .find((candidate) => candidate.find("#quality-agent-state").exists());
    select?.vm.$emit("update:modelValue", "blocked");
    await flushPromises();

    expect(mocks.listQualityAgentSessionsPage).toHaveBeenLastCalledWith(
      expect.objectContaining({
        pageSize: 50,
        state: QualityAgentSessionState.BLOCKED,
        database: "",
        source: QualityAgentSessionSource.UNSPECIFIED,
        mode: QualityAgentSessionMode.UNSPECIFIED,
      })
    );
    wrapper.unmount();
  });

  it("requests the selected database from the server", async () => {
    mocks.listDatabases.mockResolvedValue({
      databases: [
        { name: "instances/i1/databases/warehouse" },
        { name: "instances/i2/databases/archive" },
      ],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();
    await flushPromises();

    const select = wrapper
      .findAllComponents(Select)
      .find((candidate) => candidate.find("#quality-agent-database").exists());
    select?.vm.$emit("update:modelValue", "instances/i2/databases/archive");
    await flushPromises();

    expect(mocks.listQualityAgentSessionsPage).toHaveBeenLastCalledWith(
      expect.objectContaining({
        database: "instances/i2/databases/archive",
      })
    );
    wrapper.unmount();
  });

  it("pages forward with the server's cursor", async () => {
    mocks.listQualityAgentSessionsPage
      .mockResolvedValueOnce(pageOf([sessionFixture()], "cursor-2"))
      .mockResolvedValueOnce(
        pageOf([
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
        ])
      );
    const { wrapper } = await mountPage();

    const next = wrapper
      .findAll("button")
      .find((button) => button.text().includes(i18n.global.t("common.next")));
    await next?.trigger("click");
    await flushPromises();

    expect(mocks.listQualityAgentSessionsPage).toHaveBeenLastCalledWith(
      expect.objectContaining({ pageToken: "cursor-2" })
    );
    expect(wrapper.text()).toContain("customers");
    wrapper.unmount();
  });

  it("filters rows by target on Enter", async () => {
    mocks.listQualityAgentSessionsPage.mockResolvedValue(
      pageOf([
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
      ])
    );
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
    mocks.listQualityAgentSessionsPage.mockResolvedValue(
      pageOf([sessionFixture()])
    );
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
    mocks.listQualityAgentSessionsPage.mockResolvedValue(
      pageOf([sessionFixture()])
    );
    const { wrapper, router } = await mountPage();

    const row = wrapper.find("tbody tr");
    await row.trigger("click");
    await flushPromises();

    expect(router.currentRoute.value.name).toBe("DataQualityAgentSession");
    wrapper.unmount();
  });

  it("hides every write control from a read-only reader", async () => {
    mocks.listQualityAgentSessionsPage.mockResolvedValue(
      pageOf([sessionFixture()])
    );
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
