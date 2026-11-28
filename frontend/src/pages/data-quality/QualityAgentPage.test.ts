import { create } from "@bufbuild/protobuf";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { Select } from "@/components/ui/select";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  type QualityAgent,
  QualityAgentAskKind,
  QualityAgentAskSchema,
  QualityAgentAskStatus,
  QualityAgentMessageIntent,
  QualityAgentMessageRole,
  QualityAgentQuestionOptionSchema,
  QualityAgentQuestionSchema,
  QualityAgentSchema,
  QualityAgentSessionMessageSchema,
  QualityAgentSessionMode,
  QualityAgentSessionRole,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import QualityAgentPage from "./QualityAgentPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityAgent: vi.fn(),
  listQualityAgentConversationMessages: vi.fn(),
  listQualityAgentReports: vi.fn(),
  listQualityAgentAsksPage: vi.fn(),
  sendQualityAgentMessage: vi.fn(),
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
  sendQualityAgentMessage: mocks.sendQualityAgentMessage,
  listQualityAgentCases: mocks.listQualityAgentCases,
  listQualityAgentSessionsPage: mocks.listQualityAgentSessionsPage,
  listQualityAgentCaseActions: mocks.listQualityAgentCaseActions,
}));

let pinia = createPinia();

const Dummy = { template: "<div />" };
const AGENT = "workspaces/ws/qualityAgents/agent-1";

function agentFixture(): QualityAgent {
  return create(QualityAgentSchema, {
    name: AGENT,
    displayName: "Quality agent",
    owner: "users/owner",
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
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return wrapper;
}

describe("QualityAgentPage", () => {
  beforeEach(() => {
    pinia = createPinia();
    setActivePinia(pinia);
    useAuthStore().user = create(UserSchema, { name: "users/owner" });
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
    mocks.sendQualityAgentMessage.mockResolvedValue({});
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
    expect(mocks.listQualityAgentAsksPage).toHaveBeenCalledWith(AGENT, {
      status: QualityAgentAskStatus.OPEN,
      pageSize: 50,
    });
    expect(mocks.listQualityAgentAsksPage).toHaveBeenCalledWith(AGENT, {
      statuses: [
        QualityAgentAskStatus.ANSWERED,
        QualityAgentAskStatus.RESOLVED,
        QualityAgentAskStatus.DISMISSED,
      ],
      pageSize: 20,
    });
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

  it("answers the ask whose option was clicked", async () => {
    const ask = create(QualityAgentAskSchema, {
      name: "workspaces/ws/qualityAgentAsks/a-open",
      kind: QualityAgentAskKind.QUESTION,
      status: QualityAgentAskStatus.OPEN,
      title: "A decision is needed",
      question: "Keep the check?",
      questions: [
        create(QualityAgentQuestionSchema, {
          id: "q1",
          question: "Keep the check?",
          options: [
            create(QualityAgentQuestionOptionSchema, { label: "Keep it" }),
            create(QualityAgentQuestionOptionSchema, { label: "Pause it" }),
          ],
        }),
      ],
    });
    mocks.listQualityAgentAsksPage.mockResolvedValue({
      asks: [ask],
      nextPageToken: "",
    });
    const wrapper = await mountPage();
    const keep = wrapper
      .findAll("button")
      .find((candidate) => candidate.text().includes("Keep it"));
    await keep?.trigger("click");
    const send = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Send answer");
    await send?.trigger("click");
    await flushPromises();

    expect(mocks.sendQualityAgentMessage).toHaveBeenCalledTimes(1);
    const [, body, options] = mocks.sendQualityAgentMessage.mock.calls[0];
    expect(body).toBe("");
    expect(options.replyTo).toBe(ask.name);
    expect(options.answers[0].id).toBe("q1");
    expect(options.answers[0].selected).toEqual(["Keep it"]);
    wrapper.unmount();
  });

  it("marks the agent's question message and jumps to its ask", async () => {
    const ask = create(QualityAgentAskSchema, {
      name: "workspaces/ws/qualityAgentAsks/a-open",
      kind: QualityAgentAskKind.QUESTION,
      status: QualityAgentAskStatus.OPEN,
      title: "A decision is needed",
      question: "Keep the check?",
      messageSeq: 7n,
      questions: [
        create(QualityAgentQuestionSchema, {
          id: "q1",
          question: "Keep the check?",
        }),
      ],
    });
    mocks.listQualityAgentAsksPage.mockResolvedValue({
      asks: [ask],
      nextPageToken: "",
    });
    mocks.listQualityAgentConversationMessages.mockResolvedValue({
      qualityAgentConversationMessages: [
        create(QualityAgentSessionMessageSchema, {
          name: "workspaces/ws/qualityAgentSessions/s1/messages/7",
          seq: 7n,
          role: QualityAgentMessageRole.ASSISTANT,
          intent: QualityAgentMessageIntent.QUESTION,
          content: "Keep the check?",
        }),
        create(QualityAgentSessionMessageSchema, {
          name: "workspaces/ws/qualityAgentSessions/s1/messages/8",
          seq: 8n,
          role: QualityAgentMessageRole.ASSISTANT,
          intent: QualityAgentMessageIntent.QUESTION,
          content: "A question the ask list does not know.",
        }),
      ],
    });
    const scrollIntoView = vi.fn();
    Object.defineProperty(window.HTMLElement.prototype, "scrollIntoView", {
      configurable: true,
      value: scrollIntoView,
    });

    const wrapper = await mountPage();
    const messages = wrapper
      .findAll("div.bg-muted")
      .filter((candidate) => candidate.text().includes("Keep the check?"));
    const questionMessage = messages.find((candidate) =>
      candidate.text().includes("Answer in Needs you")
    );
    expect(questionMessage?.text()).toContain("Question");
    const jumps = wrapper
      .findAll("button")
      .filter((candidate) => candidate.text() === "Answer in Needs you");
    expect(jumps).toHaveLength(1);
    await jumps[0]?.trigger("click");
    expect(scrollIntoView).toHaveBeenCalled();
    wrapper.unmount();
  });

  it("answers a single-question ask from the composer when replying", async () => {
    const ask = create(QualityAgentAskSchema, {
      name: "workspaces/ws/qualityAgentAsks/a-open",
      kind: QualityAgentAskKind.QUESTION,
      status: QualityAgentAskStatus.OPEN,
      title: "A decision is needed",
      question: "Keep the check?",
      questions: [
        create(QualityAgentQuestionSchema, {
          id: "q1",
          question: "Keep the check?",
        }),
      ],
    });
    mocks.listQualityAgentAsksPage.mockResolvedValue({
      asks: [ask],
      nextPageToken: "",
    });
    const wrapper = await mountPage();
    const select = wrapper.findComponent(Select);
    expect(select.exists()).toBe(true);
    await select.vm.$emit("update:modelValue", ask.name);
    await wrapper.find("textarea").setValue("Keep it for now");
    const send = wrapper
      .findAll("button")
      .find((candidate) => candidate.text() === "Send");
    await send?.trigger("click");
    await flushPromises();

    expect(mocks.sendQualityAgentMessage).toHaveBeenCalledTimes(1);
    const [, body, options] = mocks.sendQualityAgentMessage.mock.calls[0];
    expect(body).toBe("");
    expect(options.replyTo).toBe(ask.name);
    expect(options.answers[0].id).toBe("q1");
    expect(options.answers[0].custom).toBe("Keep it for now");
    wrapper.unmount();
  });

  it("does not offer a batch ask in the composer selector", async () => {
    const ask = create(QualityAgentAskSchema, {
      name: "workspaces/ws/qualityAgentAsks/a-batch",
      kind: QualityAgentAskKind.QUESTION,
      status: QualityAgentAskStatus.OPEN,
      title: "Two decisions",
      question: "First? Second?",
      questions: [
        create(QualityAgentQuestionSchema, { id: "first", question: "First?" }),
        create(QualityAgentQuestionSchema, {
          id: "second",
          question: "Second?",
        }),
      ],
    });
    mocks.listQualityAgentAsksPage.mockResolvedValue({
      asks: [ask],
      nextPageToken: "",
    });
    const wrapper = await mountPage();
    expect(wrapper.findComponent(Select).exists()).toBe(false);
    wrapper.unmount();
  });

  it("renders a dismissal as a decision, not ordinary chat", async () => {
    const envelope = JSON.stringify({
      kind: "dismiss_ask",
      ask: "workspaces/ws/qualityAgentAsks/a-dismissed",
      questions: [{ id: "q1", question: "Which mail rule should apply?" }],
    });
    mocks.listQualityAgentConversationMessages.mockResolvedValue({
      qualityAgentConversationMessages: [
        create(QualityAgentSessionMessageSchema, {
          name: "workspaces/ws/qualityAgentSessions/s1/messages/10",
          seq: 10n,
          role: QualityAgentMessageRole.USER,
          intent: QualityAgentMessageIntent.DISMISS,
          content: envelope,
          replyTo: "workspaces/ws/qualityAgentAsks/a-dismissed",
        }),
      ],
    });

    const wrapper = await mountPage();
    expect(wrapper.text()).toContain("Ask dismissed");
    expect(wrapper.text()).toContain("Which mail rule should apply?");
    expect(wrapper.text()).not.toContain(envelope);
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
