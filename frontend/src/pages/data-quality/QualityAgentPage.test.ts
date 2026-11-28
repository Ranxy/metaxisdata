import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import {
  type QualityAgent,
  QualityAgentAskKind,
  QualityAgentAskSchema,
  QualityAgentAskStatus,
  QualityAgentSchema,
  QualityAgentSessionMode,
  QualityAgentSessionRole,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import QualityAgentPage from "./QualityAgentPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityAgent: vi.fn(),
  listQualityAgentConversationMessages: vi.fn(),
  listQualityAgentReports: vi.fn(),
  listQualityAgentAsksPage: vi.fn(),
  listQualityAgentCases: vi.fn(),
  listQualityAgentSessionsPage: vi.fn(),
  listQualityAgentCaseActions: vi.fn(),
}));

vi.mock("@/components/common/AsyncMarkdown", () => ({
  AsyncMarkdown: {
    props: ["content"],
    template: `<div class="markdown">{{ content }}</div>`,
  },
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityAgent: mocks.getQualityAgent,
  listQualityAgentConversationMessages:
    mocks.listQualityAgentConversationMessages,
  listQualityAgentReports: mocks.listQualityAgentReports,
  listQualityAgentAsksPage: mocks.listQualityAgentAsksPage,
  listQualityAgentCases: mocks.listQualityAgentCases,
  listQualityAgentSessionsPage: mocks.listQualityAgentSessionsPage,
  listQualityAgentCaseActions: mocks.listQualityAgentCaseActions,
}));

const Dummy = { template: "<div />" };
const AGENT = "workspaces/ws/qualityAgents/agent-1";

function agentFixture(): QualityAgent {
  return create(QualityAgentSchema, {
    name: AGENT,
    displayName: "Quality agent",
    state: 2,
    databases: ["instances/inst/databases/db"],
    capabilities: ["suggest", "patrol"],
  });
}

function askFixture(
  status = QualityAgentAskStatus.OPEN,
  question = "Keep the check?"
) {
  return create(QualityAgentAskSchema, {
    name: `workspaces/ws/qualityAgentAsks/a-${status}`,
    kind: QualityAgentAskKind.QUESTION,
    status,
    title: "A decision is needed",
    question,
  });
}

function sessionFixture() {
  return create(QualityAgentSessionSchema, {
    name: "workspaces/ws/qualityAgentSessions/s1",
    role: QualityAgentSessionRole.SUBAGENT,
    subagentName: "orders",
    state: QualityAgentSessionState.ENABLED,
    mode: QualityAgentSessionMode.MONITOR,
    target: create(QualityTargetSchema, { metaGuid: "inst;db;;orders" }),
  });
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: "/data-quality/agents", name: "QualityAgents", component: Dummy },
      {
        path: "/data-quality/agents/:workspace/:agentId",
        name: "QualityAgent",
        component: Dummy,
      },
      {
        path: "/data-quality/agents/:workspace/:agentId/subagents",
        name: "QualityAgentSessions",
        component: Dummy,
      },
      {
        path: "/data-quality/agents/:workspace/:agentId/subagents/:sessionId",
        name: "QualityAgentSession",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/agents/ws/agent-1");
  await router.isReady();
  const wrapper = mount(QualityAgentPage, {
    global: { plugins: [router, i18n, createPinia()] },
  });
  await flushPromises();
  return wrapper;
}

describe("QualityAgentPage", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    i18n.global.locale.value = "en-US";
    for (const mock of Object.values(mocks)) {
      mock.mockReset();
    }
    mocks.getQualityAgent.mockResolvedValue(agentFixture());
    mocks.listQualityAgentConversationMessages.mockResolvedValue({
      qualityAgentConversationMessages: [],
    });
    mocks.listQualityAgentReports.mockResolvedValue([]);
    mocks.listQualityAgentAsksPage.mockResolvedValue({
      asks: [askFixture()],
      nextPageToken: "",
    });
    mocks.listQualityAgentCases.mockResolvedValue([]);
    mocks.listQualityAgentSessionsPage.mockResolvedValue({
      sessions: [sessionFixture()],
      nextPageToken: "",
    });
    mocks.listQualityAgentCaseActions.mockResolvedValue({
      actions: [],
      nextPageToken: "",
    });
  });

  it("keeps open questions out of the answered history until it is opened", async () => {
    mocks.listQualityAgentAsksPage.mockResolvedValue({
      asks: [
        askFixture(QualityAgentAskStatus.OPEN, "Keep the check?"),
        askFixture(
          QualityAgentAskStatus.ANSWERED,
          "The sparse batch is expected?"
        ),
      ],
      nextPageToken: "",
    });

    const wrapper = await mountPage();
    expect(wrapper.text()).toContain("Keep the check?");
    expect(wrapper.text()).not.toContain("The sparse batch is expected?");

    const answered = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Answered"));
    expect(answered).toBeDefined();
    await answered?.trigger("click");
    expect(wrapper.text()).toContain("The sparse batch is expected?");
    wrapper.unmount();
  });

  it("switches the rail between team, reports, work items and activity", async () => {
    const wrapper = await mountPage();
    const tabs = wrapper.findAll('[role="tab"]');
    expect(tabs.map((tab) => tab.text())).toEqual([
      "Team 1",
      "Reports",
      "Work items",
      "Activity",
    ]);
    expect(wrapper.text()).toContain("Keep the check?");

    await tabs[0].trigger("click");
    expect(wrapper.text()).toContain("orders");

    await tabs[1].trigger("click");
    expect(wrapper.text()).toContain("No reports yet.");

    await tabs[2].trigger("click");
    expect(wrapper.text()).toContain("No open work item");

    await tabs[3].trigger("click");
    expect(wrapper.text()).toContain("Every write the agent made");
    wrapper.unmount();
  });
});
