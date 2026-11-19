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
  type QualityAgentSession,
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
  getQualityAgentSession: vi.fn(),
  listQualityAgentSessionMessages: vi.fn(),
  resumeQualityAgentSession: vi.fn(),
}));

vi.mock("@/api/data-quality", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api/data-quality")>()),
  getQualityAgentSession: mocks.getQualityAgentSession,
  listQualityAgentSessionMessages: mocks.listQualityAgentSessionMessages,
  resumeQualityAgentSession: mocks.resumeQualityAgentSession,
}));

const Dummy = { template: "<div />" };
const SESSION = "workspaces/ws/qualityAgentSessions/session-1";
const START = new Date("2026-11-07T10:00:00Z");

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
    state: QualityAgentSessionState.ARCHIVED,
    running: false,
    wakeCount: 4,
    lastActivityTime: timestampFromDate(START),
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
      stubs: {
        AsyncMarkdown: {
          props: ["content"],
          template: '<div class="markdown">{{ content }}</div>',
        },
      },
    },
  });
  await flushPromises();
  return wrapper;
}

describe("QualityAgentSessionPage", () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    i18n.global.locale.value = "en-US";
    mocks.getQualityAgentSession.mockReset();
    mocks.listQualityAgentSessionMessages.mockReset();
    mocks.getQualityAgentSession.mockResolvedValue(sessionFixture());
    mocks.listQualityAgentSessionMessages.mockResolvedValue({
      messages: messagesFixture(),
      nextPageToken: "",
    });
  });

  it("renders the archived session, its transcript and the folded reasoning", async () => {
    const wrapper = await mountPage();

    expect(wrapper.text()).toContain("orders");
    expect(wrapper.text()).toContain("Archived; the transcript is read-only.");
    expect(wrapper.text()).toContain("get_target_summary");
    expect(wrapper.text()).toContain("Thinking");
    expect(wrapper.text()).not.toContain("look at the latest execution");
    expect(wrapper.text()).not.toContain("Load earlier");
    wrapper.unmount();
  });
});
