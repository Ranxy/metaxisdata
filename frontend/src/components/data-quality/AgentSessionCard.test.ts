import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityAgentSession,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import AgentSessionCard from "./AgentSessionCard.vue";

const mocks = vi.hoisted(() => ({
  listQualityAgentSessions: vi.fn(),
  createQualityAgentSession: vi.fn(),
  pauseQualityAgentSession: vi.fn(),
  resumeQualityAgentSession: vi.fn(),
  archiveQualityAgentSession: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  listQualityAgentSessions: mocks.listQualityAgentSessions,
  createQualityAgentSession: mocks.createQualityAgentSession,
  pauseQualityAgentSession: mocks.pauseQualityAgentSession,
  resumeQualityAgentSession: mocks.resumeQualityAgentSession,
  archiveQualityAgentSession: mocks.archiveQualityAgentSession,
}));

const TARGET = "inst1;db1;public;orders";
const SESSION = "workspaces/ws/qualityAgentSessions/7";
const NOW = new Date("2026-11-07T10:00:00Z");
const Dummy = { template: "<div />" };

function sessionFixture(
  overrides: MessageInitShape<typeof QualityAgentSessionSchema> = {}
): QualityAgentSession {
  return create(QualityAgentSessionSchema, {
    name: SESSION,
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
    state: QualityAgentSessionState.ENABLED,
    running: true,
    revision: 3,
    wakeCount: 4,
    tokensUsed: 12_000n,
    lastActivityTime: timestampFromDate(NOW),
    ...overrides,
  });
}

async function mountCard(canUpdate = true) {
  const pinia = createPinia();
  setActivePinia(pinia);
  useAuthStore().user = create(UserSchema, { permissions: [] });
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/data-quality/agent/:sessionId+",
        name: "DataQualityAgentSession",
        component: Dummy,
      },
      {
        path: "/data-quality/agent",
        name: "DataQualityAgent",
        component: Dummy,
      },
      { path: "/:pathMatch(.*)*", component: Dummy },
    ],
  });
  await router.push("/");
  await router.isReady();
  const wrapper = mount(AgentSessionCard, {
    props: {
      targetGuid: TARGET,
      metaType: MetaType.TABLE,
      canUpdate,
    },
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

describe("AgentSessionCard", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    i18n.global.locale.value = "en-US";
    mocks.listQualityAgentSessions.mockResolvedValue([]);
  });

  it("invites a session when the target has none", async () => {
    const { wrapper } = await mountCard();

    expect(mocks.listQualityAgentSessions).toHaveBeenCalledWith({
      targetGuid: TARGET,
    });
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.cardTitle")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.cardEmpty")
    );
    wrapper.unmount();
  });

  it("starts a session for the target and shows it", async () => {
    const created = sessionFixture();
    mocks.createQualityAgentSession.mockResolvedValue(created);
    const { wrapper } = await mountCard();

    const start = wrapper
      .findAll("button")
      .find(
        (button) => button.text() === i18n.global.t("dataQuality.agent.start")
      );
    expect(start).toBeDefined();
    await start?.trigger("click");
    await flushPromises();

    expect(mocks.createQualityAgentSession).toHaveBeenCalledWith({
      targetGuid: TARGET,
      metaType: MetaType.TABLE,
    });
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.state.enabled")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.viewConversation")
    );
    wrapper.unmount();
  });

  it("shows the existing session and links to its conversation", async () => {
    mocks.listQualityAgentSessions.mockResolvedValue([sessionFixture()]);
    const { wrapper } = await mountCard();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.cardTitle")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.running")
    );
    const link = wrapper
      .findAll("a")
      .find(
        (anchor) =>
          anchor.text() === i18n.global.t("dataQuality.agent.viewConversation")
      );
    expect(link?.attributes("href")).toContain(
      "/data-quality/agent/workspaces/ws/qualityAgentSessions/7"
    );
    wrapper.unmount();
  });

  it("pauses the session and renders the updated state", async () => {
    mocks.listQualityAgentSessions.mockResolvedValue([sessionFixture()]);
    mocks.pauseQualityAgentSession.mockResolvedValue(
      sessionFixture({ state: QualityAgentSessionState.PAUSED, revision: 4 })
    );
    const { wrapper } = await mountCard();

    const pause = wrapper
      .findAll("button")
      .find((button) =>
        button.text().includes(i18n.global.t("dataQuality.agent.pause"))
      );
    await pause?.trigger("click");
    await flushPromises();

    expect(mocks.pauseQualityAgentSession).toHaveBeenCalledWith(SESSION, 3);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.state.paused")
    );
    wrapper.unmount();
  });

  it("hides the controls from a reader who cannot update", async () => {
    mocks.listQualityAgentSessions.mockResolvedValue([sessionFixture()]);
    const { wrapper } = await mountCard(false);

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.viewConversation")
    );
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.agent.pause")
    );
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.agent.archive")
    );
    wrapper.unmount();
  });
});
