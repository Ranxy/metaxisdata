import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import {
  type QualityAgent,
  QualityAgentSchema,
  type QualityAgentSession,
  QualityAgentSessionMode,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
  QualityTargetPathSchema,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import QualityAgentSessionsPage from "./QualityAgentSessionsPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityAgent: vi.fn(),
  listQualityAgentSessionsPage: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityAgent: mocks.getQualityAgent,
  listQualityAgentSessionsPage: mocks.listQualityAgentSessionsPage,
}));

const Dummy = { template: "<div />" };
const AGENT = "workspaces/ws/qualityAgents/agent-1";
const SESSION = "workspaces/ws/qualityAgentSessions/session-1";

function agentFixture(): QualityAgent {
  return create(QualityAgentSchema, {
    name: AGENT,
    displayName: "Quality agent",
    databases: ["instances/inst-1/databases/shop"],
  });
}

function sessionFixture(): QualityAgentSession {
  return create(QualityAgentSessionSchema, {
    name: SESSION,
    target: create(QualityTargetSchema, {
      metaGuid: "inst-1;shop;public;orders",
      metaType: MetaType.TABLE,
      path: create(QualityTargetPathSchema, {
        instance: "inst-1",
        database: "shop",
        schema: "public",
        object: "orders",
      }),
    }),
    database: "instances/inst-1/databases/shop",
    state: QualityAgentSessionState.ENABLED,
    mode: QualityAgentSessionMode.MONITOR,
    running: true,
    wakeCount: 3,
    lastActivityTime: timestampFromDate(new Date("2026-11-07T10:00:00Z")),
  });
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/data-quality/agents/:workspace/:agentId",
        name: "QualityAgent",
        component: Dummy,
      },
      {
        path: "/data-quality/agents/:workspace/:agentId/sessions",
        name: "QualityAgentSessions",
        component: QualityAgentSessionsPage,
      },
      {
        path: "/data-quality/agents/:workspace/:agentId/sessions/:sessionId",
        name: "QualityAgentSession",
        component: Dummy,
      },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/agents/ws/agent-1/sessions");
  await router.isReady();
  const wrapper = mount(QualityAgentSessionsPage, {
    global: { plugins: [router, i18n] },
  });
  await flushPromises();
  return wrapper;
}

describe("QualityAgentSessionsPage", () => {
  beforeEach(() => {
    i18n.global.locale.value = "en-US";
    mocks.getQualityAgent.mockReset();
    mocks.listQualityAgentSessionsPage.mockReset();
    mocks.getQualityAgent.mockResolvedValue(agentFixture());
    mocks.listQualityAgentSessionsPage.mockResolvedValue({
      sessions: [sessionFixture()],
      nextPageToken: "",
    });
  });

  it("lists the agent's work sessions with their target and state", async () => {
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("public.orders");
    expect(wrapper.text()).toContain("Monitor");
    expect(wrapper.text()).toContain("Enabled");
    expect(wrapper.text()).toContain("Running");
    const link = wrapper
      .findAll("a")
      .find((anchor) => anchor.text().includes("orders"));
    expect(link?.attributes("href")).toContain(
      "/data-quality/agents/ws/agent-1/sessions/session-1"
    );
    expect(mocks.listQualityAgentSessionsPage).toHaveBeenCalledWith(
      expect.objectContaining({ agent: AGENT, pageSize: 50 })
    );
    wrapper.unmount();
  });

  it("shows the enrollment hint when the agent has no work sessions", async () => {
    mocks.listQualityAgentSessionsPage.mockResolvedValue({
      sessions: [],
      nextPageToken: "",
    });
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("No work sessions yet");
    expect(wrapper.text()).toContain("enrollment");
    wrapper.unmount();
  });
});
