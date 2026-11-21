import { create } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import {
  QualityAgentMessageOrigin,
  QualityAgentMessageRole,
  QualityAgentSchema,
  type QualityAgentSession,
  type QualityAgentSessionContextUsage,
  QualityAgentSessionContextUsageSchema,
  QualityAgentSessionMessageSchema,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityToolCallSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import QualityAgentSessionPage from "./QualityAgentSessionPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityAgent: vi.fn(),
  getQualityAgentSession: vi.fn(),
  listQualityAgentSessionMessages: vi.fn(),
  resumeQualityAgentSession: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityAgent: mocks.getQualityAgent,
  getQualityAgentSession: mocks.getQualityAgentSession,
  listQualityAgentSessionMessages: mocks.listQualityAgentSessionMessages,
  resumeQualityAgentSession: mocks.resumeQualityAgentSession,
}));

// Keep the lazy Markdown renderer (and its ~1MB chunk) out of the test process.
vi.mock("@/components/common/AsyncMarkdown", () => ({
  AsyncMarkdown: {
    props: ["content"],
    template: '<div class="markdown">{{ content }}</div>',
  },
  preloadMarkdown: () => Promise.resolve(),
}));

/** Captures the observer so a test can report the top sentinel coming into view. */
let intersectionCallback: IntersectionObserverCallback | undefined;

class FakeIntersectionObserver {
  readonly root = null;
  readonly rootMargin = "";
  readonly thresholds = [];
  constructor(callback: IntersectionObserverCallback) {
    intersectionCallback = callback;
  }
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
  takeRecords = () => [];
}

const Dummy = { template: "<div />" };
const SESSION = "workspaces/ws/qualityAgentSessions/session-1";
const START = new Date("2026-11-07T10:00:00Z");

function sessionFixture(
  contextUsage?: QualityAgentSessionContextUsage
): QualityAgentSession {
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
    state: QualityAgentSessionState.ARCHIVED,
    running: false,
    wakeCount: 4,
    lastActivityTime: timestampFromDate(START),
    contextUsage,
  });
}

function messagesFixture() {
  const call = create(QualityToolCallSchema, {
    id: "call-1",
    name: "get_target_summary",
    arguments: '{"target":"inst-1;shop;public;orders"}',
  });
  return [
    create(QualityAgentSessionMessageSchema, {
      name: `${SESSION}/messages/1`,
      seq: 1n,
      wake: 1,
      role: QualityAgentMessageRole.USER,
      origin: QualityAgentMessageOrigin.WAKE,
      content: "scheduled wake",
      createTime: timestampFromDate(START),
    }),
    create(QualityAgentSessionMessageSchema, {
      name: `${SESSION}/messages/2`,
      seq: 2n,
      wake: 1,
      role: QualityAgentMessageRole.ASSISTANT,
      origin: QualityAgentMessageOrigin.MODEL,
      content: "The table is healthy.",
      reasoning: "look at the latest execution",
      toolCalls: [call],
      createTime: timestampFromDate(START),
    }),
    create(QualityAgentSessionMessageSchema, {
      name: `${SESSION}/messages/3`,
      seq: 3n,
      wake: 1,
      role: QualityAgentMessageRole.TOOL,
      origin: QualityAgentMessageOrigin.MODEL,
      toolCallId: "call-1",
      toolName: "get_target_summary",
      content: '{"ruleCount":2}',
      createTime: timestampFromDate(START),
    }),
  ];
}

async function mountPage() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: "/data-quality/agents",
        name: "QualityAgents",
        component: Dummy,
      },
      {
        path: "/data-quality/agents/:workspace/:agentId",
        name: "QualityAgent",
        component: Dummy,
      },
      {
        path: "/data-quality/agents/:workspace/:agentId/sessions",
        name: "QualityAgentSessions",
        component: Dummy,
      },
      {
        path: "/data-quality/agents/:workspace/:agentId/sessions/:sessionId",
        name: "QualityAgentSession",
        component: QualityAgentSessionPage,
      },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
    ],
  });
  await router.push("/data-quality/agents/ws/agent-1/sessions/session-1");
  await router.isReady();
  const wrapper = mount(QualityAgentSessionPage, {
    global: {
      plugins: [router, i18n],
    },
  });
  await flushPromises();
  return wrapper;
}

describe("QualityAgentSessionPage", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    i18n.global.locale.value = "en-US";
    intersectionCallback = undefined;
    vi.stubGlobal("IntersectionObserver", FakeIntersectionObserver);
    mocks.getQualityAgent.mockReset();
    mocks.getQualityAgentSession.mockReset();
    mocks.listQualityAgentSessionMessages.mockReset();
    mocks.getQualityAgent.mockResolvedValue(
      create(QualityAgentSchema, {
        name: "workspaces/ws/qualityAgents/agent-1",
        displayName: "rece",
      })
    );
    mocks.getQualityAgentSession.mockResolvedValue(sessionFixture());
    mocks.listQualityAgentSessionMessages.mockResolvedValue({
      messages: messagesFixture(),
      nextPageToken: "",
    });
  });

  it("loads older history by seq anchor rather than a shifting page token", async () => {
    const newer = create(QualityAgentSessionMessageSchema, {
      name: `${SESSION}/messages/100`,
      seq: 100n,
      wake: 1,
      role: QualityAgentMessageRole.ASSISTANT,
      origin: QualityAgentMessageOrigin.MODEL,
      content: "newest message",
      createTime: timestampFromDate(START),
    });
    mocks.listQualityAgentSessionMessages
      .mockResolvedValueOnce({ messages: [newer], nextPageToken: "offset:1" })
      .mockResolvedValueOnce({ messages: [], nextPageToken: "" });
    const wrapper = await mountPage();

    const loadEarlier = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Load earlier"));
    expect(loadEarlier).toBeDefined();
    await loadEarlier?.trigger("click");
    await flushPromises();

    expect(mocks.listQualityAgentSessionMessages).toHaveBeenLastCalledWith(
      SESSION,
      expect.objectContaining({ beforeSeq: 100n })
    );
    wrapper.unmount();
  });

  it("renders the archived session, its transcript and the folded reasoning", async () => {
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("Archived; the transcript is read-only.");
    expect(wrapper.text()).toContain("get_target_summary");
    expect(wrapper.text()).toContain("Thinking");
    expect(wrapper.text()).not.toContain("look at the latest execution");
    expect(wrapper.text()).not.toContain("Load earlier");
    expect(wrapper.text()).toContain("Start of the transcript");
    wrapper.unmount();
  });

  it("renders the model context sample in the run state", async () => {
    mocks.getQualityAgent.mockResolvedValue(
      create(QualityAgentSchema, {
        name: "workspaces/ws/qualityAgents/agent-1",
        displayName: "rece",
        providerModel: "new-model",
      })
    );
    mocks.getQualityAgentSession.mockResolvedValue(
      sessionFixture(
        create(QualityAgentSessionContextUsageSchema, {
          usedTokens: 1000,
          contextWindow: 4000,
          maxOutputTokens: 1000,
          compactionTriggerTokens: 3000,
          model: "old-model",
          wake: 2,
          measuredAt: timestampFromDate(START),
          estimated: false,
        })
      )
    );
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("1,000 / 4,000");
    expect(wrapper.text()).toContain("wake 2");
    expect(wrapper.text()).toContain("The agent model changed to new-model");
    expect(wrapper.text()).not.toContain("No model input measured yet.");
    wrapper.unmount();
  });

  it("loads older history when the reader reaches the top of the pane", async () => {
    const older = create(QualityAgentSessionMessageSchema, {
      name: `${SESSION}/messages/0`,
      seq: 0n,
      wake: 0,
      role: QualityAgentMessageRole.ASSISTANT,
      origin: QualityAgentMessageOrigin.MODEL,
      content: "an older answer",
      createTime: timestampFromDate(START),
    });
    mocks.listQualityAgentSessionMessages
      .mockResolvedValueOnce({
        messages: messagesFixture(),
        nextPageToken: "offset:1",
      })
      .mockResolvedValueOnce({ messages: [older], nextPageToken: "" });
    const wrapper = await mountPage();
    // Let the initial landing finish before scrolling away from it.
    await new Promise((resolve) => setTimeout(resolve, 250));
    expect(intersectionCallback).toBeDefined();
    // The reader has scrolled back from the newest message.
    const pane = wrapper.get('[role="region"]').element as HTMLElement;
    Object.defineProperty(pane, "scrollHeight", {
      configurable: true,
      value: 1000,
    });
    Object.defineProperty(pane, "clientHeight", {
      configurable: true,
      value: 400,
    });
    pane.scrollTop = 100;
    await wrapper.get('[role="region"]').trigger("scroll");

    intersectionCallback?.(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver
    );
    await flushPromises();

    expect(mocks.listQualityAgentSessionMessages).toHaveBeenLastCalledWith(
      SESSION,
      expect.objectContaining({ beforeSeq: 1n })
    );
    expect(wrapper.text()).toContain("an older answer");
    wrapper.unmount();
  });

  it("offers a jump back to the newest message after scrolling up", async () => {
    const wrapper = await mountPage();
    // Let the initial landing loop finish; it owns the scroll position until then.
    await new Promise((resolve) => setTimeout(resolve, 250));
    const region = wrapper.get('[role="region"]');
    const pane = region.element as HTMLElement;
    Object.defineProperty(pane, "scrollHeight", {
      configurable: true,
      value: 1000,
    });
    Object.defineProperty(pane, "clientHeight", {
      configurable: true,
      value: 400,
    });
    pane.scrollTop = 0;

    await region.trigger("scroll");
    expect(wrapper.text()).toContain("Jump to latest");

    const jump = wrapper
      .findAll("button")
      .find((button) => button.text().includes("Jump to latest"));
    await jump?.trigger("click");
    await flushPromises();

    expect(pane.scrollTop).toBe(1000);
    expect(wrapper.text()).not.toContain("Jump to latest");
    wrapper.unmount();
  });
});
