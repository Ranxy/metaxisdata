import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { flushPromises, mount } from "@vue/test-utils";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createMemoryHistory, createRouter } from "vue-router";
import { i18n } from "@/locales";
import { useAuthStore } from "@/store/modules/auth";
import {
  QualityAgentMessageOrigin,
  QualityAgentMessageRole,
  type QualityAgentSession,
  type QualityAgentSessionMessage,
  QualityAgentSessionMessageSchema,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityToolCallSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import DataQualityAgentSessionPage from "./DataQualityAgentSessionPage.vue";

const mocks = vi.hoisted(() => ({
  getQualityAgentSession: vi.fn(),
  listQualityAgentSessionMessages: vi.fn(),
  wakeQualityAgentSession: vi.fn(),
  pauseQualityAgentSession: vi.fn(),
  resumeQualityAgentSession: vi.fn(),
  archiveQualityAgentSession: vi.fn(),
  notifySuccess: vi.fn(),
  notifyError: vi.fn(),
}));

vi.mock("@/api/data-quality", () => ({
  getQualityAgentSession: mocks.getQualityAgentSession,
  listQualityAgentSessionMessages: mocks.listQualityAgentSessionMessages,
  wakeQualityAgentSession: mocks.wakeQualityAgentSession,
  pauseQualityAgentSession: mocks.pauseQualityAgentSession,
  resumeQualityAgentSession: mocks.resumeQualityAgentSession,
  archiveQualityAgentSession: mocks.archiveQualityAgentSession,
}));

vi.mock("@/lib/notify", () => ({
  notify: {
    success: mocks.notifySuccess,
    error: mocks.notifyError,
    warning: vi.fn(),
    info: vi.fn(),
  },
}));

const SESSION = "workspaces/ws/qualityAgentSessions/7";
const NOW = new Date("2026-11-07T10:00:00Z");
const Dummy = { template: "<div />" };
const ALL_PERMISSIONS = [
  "metaxisdata.dataQuality.rules.list",
  "metaxisdata.dataQuality.rules.update",
];
const READ_ONLY = ["metaxisdata.dataQuality.rules.list"];

function sessionFixture(
  overrides: MessageInitShape<typeof QualityAgentSessionSchema> = {}
): QualityAgentSession {
  return create(QualityAgentSessionSchema, {
    name: SESSION,
    target: create(QualityTargetSchema, {
      metaGuid: "inst1;db1;public;orders",
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
    revision: 1,
    wakeCount: 2,
    tokensUsed: 5000n,
    contextTokens: 1200,
    lastActivityTime: timestampFromDate(NOW),
    nextWakeTime: timestampFromDate(new Date(NOW.getTime() + 3_600_000)),
    ...overrides,
  });
}

let seq = 0;
function messageFixture(
  overrides: MessageInitShape<typeof QualityAgentSessionMessageSchema> = {}
): QualityAgentSessionMessage {
  seq += 1;
  return create(QualityAgentSessionMessageSchema, {
    name: `${SESSION}/messages/${seq}`,
    seq: BigInt(seq),
    wake: 1,
    role: QualityAgentMessageRole.USER,
    origin: QualityAgentMessageOrigin.WAKE,
    content: `message ${seq}`,
    createTime: timestampFromDate(NOW),
    ...overrides,
  });
}

const SYSTEM = messageFixture({
  wake: 0,
  seq: 1n,
  role: QualityAgentMessageRole.SYSTEM,
  origin: QualityAgentMessageOrigin.UNSPECIFIED,
  content: "you are the quality agent",
});
const WAKE = messageFixture({
  wake: 1,
  seq: 2n,
  role: QualityAgentMessageRole.USER,
  origin: QualityAgentMessageOrigin.WAKE,
  content: "scheduled wake 1",
});
const ASSISTANT = messageFixture({
  wake: 1,
  seq: 3n,
  role: QualityAgentMessageRole.ASSISTANT,
  origin: QualityAgentMessageOrigin.MODEL,
  content: "looking at the table",
  toolCalls: [
    create(QualityToolCallSchema, {
      id: "call-1",
      name: "get_target_summary",
      arguments: JSON.stringify({ targetGuid: "inst1;db1;public;orders" }),
    }),
  ],
});
const TOOL = messageFixture({
  wake: 1,
  seq: 4n,
  role: QualityAgentMessageRole.TOOL,
  toolCallId: "call-1",
  toolName: "get_target_summary",
  content: "rows: 10",
});

async function mountPage(permissions = ALL_PERMISSIONS) {
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
        path: "/data-quality/executions/:executionId+",
        name: "DataQualityExecutionDetail",
        component: Dummy,
      },
      {
        path: "/data-quality/targets/:targetGuid+",
        name: "DataQualityTarget",
        component: Dummy,
      },
    ],
  });
  await router.push({
    name: "DataQualityAgentSession",
    params: {
      sessionId: ["workspaces", "ws", "qualityAgentSessions", "7"],
    },
  });
  await router.isReady();
  const wrapper = mount(DataQualityAgentSessionPage, {
    global: { plugins: [router, i18n, pinia] },
  });
  await flushPromises();
  return { wrapper, router };
}

function findButton(wrapper: ReturnType<typeof mount>, label: string) {
  return wrapper.findAll("button").find((button) => button.text() === label);
}

async function sendQuestion(
  wrapper: ReturnType<typeof mount>,
  question: string
) {
  await wrapper.find("#quality-agent-ask").setValue(question);
  const send = findButton(
    wrapper,
    i18n.global.t("dataQuality.session.askSend")
  );
  await send?.trigger("click");
  await flushPromises();
}

describe("DataQualityAgentSessionPage", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    i18n.global.locale.value = "en-US";
    mocks.getQualityAgentSession.mockResolvedValue(sessionFixture());
    mocks.listQualityAgentSessionMessages.mockResolvedValue({
      messages: [SYSTEM, WAKE, ASSISTANT, TOOL],
      nextPageToken: "",
    });
  });

  it("groups the transcript into wakes and folds the runner's own rows", async () => {
    const { wrapper } = await mountPage();

    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.session.wakeSystem")
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.session.wake", { wake: 1 })
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.origin.wake")
    );
    expect(wrapper.text()).toContain("get_target_summary");
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.agent.role.system")
    );
    wrapper.unmount();
  });

  it("expands a tool card to show its result", async () => {
    const { wrapper } = await mountPage();

    expect(wrapper.text()).not.toContain("rows: 10");
    const trigger = wrapper
      .findAll("button")
      .find((button) => button.text().includes("get_target_summary"));
    await trigger?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("rows: 10");
    wrapper.unmount();
  });

  it("links an event's execution reference and falls back to text otherwise", async () => {
    const event = messageFixture({
      wake: 2,
      seq: 5n,
      role: QualityAgentMessageRole.USER,
      origin: QualityAgentMessageOrigin.EVENT,
      content: "check orders_row_count failed",
      reference: "qualityExecutions/123",
    });
    const unknownReference = messageFixture({
      wake: 2,
      seq: 6n,
      role: QualityAgentMessageRole.USER,
      origin: QualityAgentMessageOrigin.EVENT,
      content: "something else changed",
      reference: "ruleTemplates/9",
    });
    mocks.listQualityAgentSessionMessages.mockResolvedValue({
      messages: [SYSTEM, WAKE, event, unknownReference],
      nextPageToken: "",
    });
    const { wrapper } = await mountPage();

    const link = wrapper
      .findAll("a")
      .find(
        (anchor) =>
          anchor.text() ===
          i18n.global.t("dataQuality.session.eventExecution", { id: "123" })
      );
    expect(link?.attributes("href")).toBe(
      "/data-quality/executions/workspaces/ws/qualityExecutions/123"
    );
    expect(wrapper.text()).toContain("ruleTemplates/9");
    wrapper.unmount();
  });

  it("sends the question with an idempotency id and keeps it across a retry", async () => {
    mocks.listQualityAgentSessionMessages.mockImplementation(
      async (_name: string, options: { afterSeq?: bigint }) => {
        if (options.afterSeq !== undefined && options.afterSeq > 0n) {
          return {
            messages: [
              messageFixture({
                seq: 5n,
                wake: 2,
                role: QualityAgentMessageRole.USER,
                origin: QualityAgentMessageOrigin.ASK,
                content: "why did the row count drop?",
              }),
            ],
            nextPageToken: "",
          };
        }
        return { messages: [SYSTEM, WAKE], nextPageToken: "" };
      }
    );
    mocks.wakeQualityAgentSession
      .mockRejectedValueOnce(
        new ConnectError("the session is not enabled", Code.FailedPrecondition)
      )
      .mockResolvedValueOnce(sessionFixture({ wakeCount: 3 }));
    const { wrapper } = await mountPage();

    await sendQuestion(wrapper, "why did the row count drop?");
    // A refused send reports the server's reason and never claims success.
    expect(notifySuccessCount()).toBe(0);
    expect(wrapper.text()).toContain("the session is not enabled");

    await sendQuestion(wrapper, "why did the row count drop?");
    await flushPromises();

    const calls = mocks.wakeQualityAgentSession.mock.calls;
    expect(calls).toHaveLength(2);
    expect(calls[0]).toEqual([
      SESSION,
      "why did the row count drop?",
      expect.objectContaining({ requestId: expect.any(String) }),
    ]);
    expect(calls[1][2]).toEqual(calls[0][2]);
    expect((calls[0][2] as { requestId: string }).requestId).not.toBe("");
    expect(mocks.listQualityAgentSessionMessages).toHaveBeenCalledWith(
      SESSION,
      expect.objectContaining({ afterSeq: 2n })
    );
    expect(wrapper.text()).toContain("why did the row count drop?");
    wrapper.unmount();
  });

  it("loads the older page through the page token", async () => {
    mocks.listQualityAgentSessionMessages.mockImplementation(
      async (_name: string, options: { pageToken?: string }) => {
        if (options.pageToken === "older") {
          return { messages: [SYSTEM], nextPageToken: "" };
        }
        return { messages: [WAKE], nextPageToken: "older" };
      }
    );
    const { wrapper } = await mountPage();

    const loadEarlier = findButton(
      wrapper,
      i18n.global.t("dataQuality.session.loadEarlier")
    );
    expect(loadEarlier).toBeDefined();
    await loadEarlier?.trigger("click");
    await flushPromises();

    expect(mocks.listQualityAgentSessionMessages).toHaveBeenCalledWith(
      SESSION,
      expect.objectContaining({ pageToken: "older" })
    );
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.session.wakeSystem")
    );
    wrapper.unmount();
  });

  it("hides the composer on a paused session and explains why", async () => {
    mocks.getQualityAgentSession.mockResolvedValue(
      sessionFixture({ state: QualityAgentSessionState.PAUSED })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.find("#quality-agent-ask").exists()).toBe(false);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.session.askPausedHint")
    );
    wrapper.unmount();
  });

  it("shows a blocked session's code and reason instead of the composer", async () => {
    mocks.getQualityAgentSession.mockResolvedValue(
      sessionFixture({
        state: QualityAgentSessionState.BLOCKED,
        blockedCode: "needs-human",
        blockedReason: "the target no longer resolves",
      })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.find("#quality-agent-ask").exists()).toBe(false);
    expect(wrapper.text()).toContain(
      i18n.global.t("dataQuality.session.askBlockedHint")
    );
    expect(wrapper.text()).toContain("needs-human");
    expect(wrapper.text()).toContain("the target no longer resolves");
    wrapper.unmount();
  });

  it("hides the composer on an archived session", async () => {
    mocks.getQualityAgentSession.mockResolvedValue(
      sessionFixture({
        state: QualityAgentSessionState.ARCHIVED,
        running: false,
      })
    );
    const { wrapper } = await mountPage();

    expect(wrapper.find("#quality-agent-ask").exists()).toBe(false);
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.session.askPausedHint")
    );
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.session.askBlockedHint")
    );
    wrapper.unmount();
  });

  it("hides the ask box from a reader who cannot update", async () => {
    const { wrapper } = await mountPage(READ_ONLY);

    expect(wrapper.find("#quality-agent-ask").exists()).toBe(false);
    expect(wrapper.text()).not.toContain(
      i18n.global.t("dataQuality.session.askTitle")
    );
    wrapper.unmount();
  });
});

function notifySuccessCount(): number {
  return mocks.notifySuccess.mock.calls.length;
}
