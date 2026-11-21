import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import {
  QualityAgentMessageOrigin,
  QualityAgentMessageRole,
  type QualityAgentSession,
  type QualityAgentSessionContextUsage,
  QualityAgentSessionContextUsageSchema,
  type QualityAgentSessionMessage,
  QualityAgentSessionMessageSchema,
  QualityAgentSessionMode,
  QualityAgentSessionSchema,
  QualityAgentSessionState,
  QualityTargetPathSchema,
  QualityTargetSchema,
  QualityToolCallSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import { MetaType } from "@/types/proto-es/v1/database_service_pb";
import {
  AGENT_SESSION_STALE_AFTER_MS,
  agentEventExecutionReference,
  agentMessageOriginKey,
  agentMessageRoleKey,
  agentSessionContextLevel,
  agentSessionContextRatio,
  agentSessionHeartbeat,
  agentSessionModeKey,
  agentSessionModeVariant,
  agentSessionStateKey,
  agentSessionStateVariant,
  agentSessionTargetName,
  agentSessionTargetPath,
  countMessagesAfterSeq,
  groupMessagesByWake,
  lastTranscriptSeq,
  mergeTranscriptMessages,
  orphanToolResults,
  toolCallArgumentsPreview,
  toolResultForCall,
  truncateText,
} from "./qualityAgentSession";

const TARGET = "inst1;db1;public;orders";
const START = new Date("2026-11-07T10:00:00Z");

function sessionFixture(
  overrides: MessageInitShape<typeof QualityAgentSessionSchema> = {}
): QualityAgentSession {
  return create(QualityAgentSessionSchema, {
    name: "workspaces/ws/qualityAgentSessions/1",
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
    database: "instances/inst1/databases/db1",
    state: QualityAgentSessionState.ENABLED,
    running: true,
    lastActivityTime: timestampFromDate(START),
    ...overrides,
  });
}

let seq = 0;
function messageFixture(
  overrides: MessageInitShape<typeof QualityAgentSessionMessageSchema> = {}
): QualityAgentSessionMessage {
  seq += 1;
  return create(QualityAgentSessionMessageSchema, {
    name: `workspaces/ws/qualityAgentSessions/1/messages/${seq}`,
    seq: BigInt(seq),
    wake: 1,
    turn: 1,
    role: QualityAgentMessageRole.ASSISTANT,
    origin: QualityAgentMessageOrigin.MODEL,
    content: "answer",
    createTime: timestampFromDate(START),
    ...overrides,
  });
}

describe("session vocabulary", () => {
  it("maps every state to a label key and a badge variant", () => {
    expect(agentSessionStateKey(QualityAgentSessionState.ENABLED)).toBe(
      "qualityAgent.stateEnabled"
    );
    expect(agentSessionStateKey(QualityAgentSessionState.BLOCKED)).toBe(
      "qualityAgent.stateBlocked"
    );
    expect(agentSessionStateKey(QualityAgentSessionState.ARCHIVED)).toBe(
      "qualityAgent.stateArchived"
    );
    expect(agentSessionStateKey(QualityAgentSessionState.UNSPECIFIED)).toBe(
      "qualityAgent.stateUnspecified"
    );
    expect(agentSessionStateVariant(QualityAgentSessionState.ENABLED)).toBe(
      "success"
    );
    expect(agentSessionStateVariant(QualityAgentSessionState.BLOCKED)).toBe(
      "warning"
    );
    expect(agentSessionStateVariant(QualityAgentSessionState.ARCHIVED)).toBe(
      "neutral"
    );
    expect(agentSessionStateVariant(QualityAgentSessionState.UNSPECIFIED)).toBe(
      "outline"
    );
  });

  it("maps every mode to a label key and a badge variant", () => {
    expect(agentSessionModeKey(QualityAgentSessionMode.MONITOR)).toBe(
      "qualityAgent.modeMonitor"
    );
    expect(agentSessionModeKey(QualityAgentSessionMode.TRIAGE)).toBe(
      "qualityAgent.modeTriage"
    );
    expect(agentSessionModeKey(QualityAgentSessionMode.UNSPECIFIED)).toBe(
      "qualityAgent.modeUnspecified"
    );
    expect(agentSessionModeVariant(QualityAgentSessionMode.MONITOR)).toBe(
      "info"
    );
    expect(agentSessionModeVariant(QualityAgentSessionMode.TRIAGE)).toBe(
      "warning"
    );
    expect(agentSessionModeVariant(QualityAgentSessionMode.UNSPECIFIED)).toBe(
      "outline"
    );
  });

  it("maps every transcript role and origin", () => {
    expect(agentMessageRoleKey(QualityAgentMessageRole.SYSTEM)).toBe(
      "qualityAgent.roleSystem"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.USER)).toBe(
      "qualityAgent.roleUser"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.ASSISTANT)).toBe(
      "qualityAgent.roleAssistant"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.TOOL)).toBe(
      "qualityAgent.roleTool"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.ENGINE)).toBe(
      "qualityAgent.roleEngine"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.COMPACTION)).toBe(
      "qualityAgent.roleCompaction"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.UNSPECIFIED)).toBe(
      "qualityAgent.roleUnspecified"
    );

    expect(agentMessageOriginKey(QualityAgentMessageOrigin.WAKE)).toBe(
      "qualityAgent.originWake"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.EVENT)).toBe(
      "qualityAgent.originEvent"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.ASK)).toBe(
      "qualityAgent.originAsk"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.RUNNER)).toBe(
      "qualityAgent.originRunner"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.MODEL)).toBe(
      "qualityAgent.originModel"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.COMPACTION)).toBe(
      "qualityAgent.originCompaction"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.UNSPECIFIED)).toBe(
      "qualityAgent.originUnspecified"
    );
  });
});

describe("session heartbeat", () => {
  it("reports a live lease as running and only calls it stale after the threshold", () => {
    const now = START.getTime() + 1000;
    expect(
      agentSessionHeartbeat(
        sessionFixture({ lastActivityTime: timestampFromDate(START) }),
        now
      )
    ).toEqual({ running: true, stale: false });
    expect(
      agentSessionHeartbeat(
        sessionFixture({ lastActivityTime: timestampFromDate(START) }),
        START.getTime() + AGENT_SESSION_STALE_AFTER_MS
      )
    ).toEqual({ running: true, stale: true });
    expect(
      agentSessionHeartbeat(sessionFixture({ running: false }), now)
    ).toEqual({ running: false, stale: false });
    expect(
      agentSessionHeartbeat(
        sessionFixture({ lastActivityTime: undefined }),
        now
      )
    ).toEqual({ running: true, stale: false });
  });
});

describe("wake grouping", () => {
  it("orders by seq, groups by wake and reads the runner's conclusion", () => {
    const system = messageFixture({
      seq: 1n,
      wake: 0,
      role: QualityAgentMessageRole.SYSTEM,
      origin: QualityAgentMessageOrigin.RUNNER,
      content: "system prompt",
    });
    const wakeUser = messageFixture({
      seq: 2n,
      wake: 1,
      role: QualityAgentMessageRole.USER,
      origin: QualityAgentMessageOrigin.EVENT,
      content: "failure event",
    });
    const answer = messageFixture({
      seq: 3n,
      wake: 1,
      content: "looking",
    });
    const engine = messageFixture({
      seq: 4n,
      wake: 1,
      role: QualityAgentMessageRole.ENGINE,
      origin: QualityAgentMessageOrigin.RUNNER,
      content: "wake finished: reviewed the event",
    });
    const groups = groupMessagesByWake([engine, answer, system, wakeUser]);
    expect(groups.map((group) => group.wake)).toEqual([0, 1]);
    expect(groups[0].messages).toHaveLength(1);
    expect(groups[1].origin).toBe(QualityAgentMessageOrigin.EVENT);
    expect(groups[1].summary).toBe("reviewed the event");
    expect(groups[1].startTime).toBeDefined();
  });

  it("keeps a non-prefixed runner note and ignores an empty one", () => {
    const engine = messageFixture({
      wake: 2,
      role: QualityAgentMessageRole.ENGINE,
      origin: QualityAgentMessageOrigin.RUNNER,
      content: "model turn 2 started",
    });
    const empty = messageFixture({
      wake: 2,
      role: QualityAgentMessageRole.ENGINE,
      origin: QualityAgentMessageOrigin.RUNNER,
      content: "  ",
    });
    expect(groupMessagesByWake([empty, engine])[0].summary).toBe(
      "model turn 2 started"
    );
  });
});

describe("transcript merge", () => {
  it("deduplicates by seq and keeps ascending order", () => {
    const first = messageFixture({ seq: 1n, content: "first" });
    const second = messageFixture({ seq: 2n, content: "second" });
    const replacement = messageFixture({ seq: 2n, content: "replacement" });
    const merged = mergeTranscriptMessages([second, first], [replacement]);
    expect(merged.map((message) => message.seq)).toEqual([1n, 2n]);
    expect(merged[1].content).toBe("replacement");
    expect(lastTranscriptSeq(merged)).toBe(2n);
    expect(lastTranscriptSeq([])).toBe(0n);
  });

  it("counts the messages newer than the watermark", () => {
    const messages = [
      messageFixture({ seq: 1n }),
      messageFixture({ seq: 4n }),
      messageFixture({ seq: 7n }),
    ];
    expect(countMessagesAfterSeq(messages, 1n)).toBe(2);
    expect(countMessagesAfterSeq(messages, 7n)).toBe(0);
    expect(countMessagesAfterSeq(messages, 6n)).toBe(1);
    expect(countMessagesAfterSeq(messages, 0n)).toBe(3);
    expect(countMessagesAfterSeq([], 0n)).toBe(0);
  });
});

describe("tool pairing", () => {
  it("finds a result by call id and shows results whose call is gone", () => {
    const call = create(QualityToolCallSchema, {
      id: "call-1",
      name: "get_target_summary",
      arguments: "{}",
    });
    const assistant = messageFixture({ toolCalls: [call] });
    const paired = messageFixture({
      role: QualityAgentMessageRole.TOOL,
      toolCallId: "call-1",
      toolName: "get_target_summary",
      content: "{}",
    });
    const orphan = messageFixture({
      role: QualityAgentMessageRole.TOOL,
      toolCallId: "call-missing",
      toolName: "get_memory",
      content: "{}",
    });
    const messages = [assistant, paired, orphan];
    expect(toolResultForCall(messages, "call-1")).toBe(paired);
    expect(toolResultForCall(messages, "")).toBeUndefined();
    expect(toolResultForCall(messages, "call-missing")).toBe(orphan);
    expect(orphanToolResults(messages)).toEqual([orphan]);

    const emptyID = messageFixture({
      role: QualityAgentMessageRole.TOOL,
      toolCallId: "",
      toolName: "get_memory",
      content: "no id",
    });
    const callWithoutID = messageFixture({
      toolCalls: [
        create(QualityToolCallSchema, { id: "", name: "get_memory" }),
      ],
    });
    expect(orphanToolResults([callWithoutID, emptyID])).toEqual([emptyID]);
  });
});

describe("tool arguments", () => {
  it("flattens a JSON object, keeps plain text, and handles empty arguments", () => {
    expect(
      toolCallArgumentsPreview(
        create(QualityToolCallSchema, {
          arguments: '{"target":"inst1;db1","limit":5}',
        })
      )
    ).toBe("target: inst1;db1, limit: 5");
    expect(
      toolCallArgumentsPreview(
        create(QualityToolCallSchema, { arguments: "plain text" })
      )
    ).toBe("plain text");
    expect(
      toolCallArgumentsPreview(
        create(QualityToolCallSchema, { arguments: "{}" })
      )
    ).toBe("");
    expect(
      toolCallArgumentsPreview(
        create(QualityToolCallSchema, { arguments: "  " })
      )
    ).toBe("");
  });

  it("truncates on rune boundaries", () => {
    expect(truncateText("abcdef", 4)).toBe("abc…");
    expect(truncateText("你好世界", 3)).toBe("你好…");
    expect(truncateText("abc", 3)).toBe("abc");
    expect(truncateText("abc", 0)).toBe("…");
  });
});

describe("event execution references", () => {
  const session = "workspaces/ws/qualityAgentSessions/1";

  it("resolves a short reference through the session workspace", () => {
    expect(
      agentEventExecutionReference("qualityExecutions/123", session)
    ).toEqual({
      id: "123",
      route: {
        name: "DataQualityExecutionDetail",
        params: {
          executionId: ["workspaces", "ws", "qualityExecutions", "123"],
        },
      },
    });
  });

  it("accepts a full resource name and rejects malformed references", () => {
    expect(
      agentEventExecutionReference("workspaces/ws/qualityExecutions/9", session)
        ?.id
    ).toBe("9");
    expect(agentEventExecutionReference("", session)).toBeNull();
    expect(
      agentEventExecutionReference("qualityExecutions/", session)
    ).toBeNull();
    expect(
      agentEventExecutionReference("qualityExecutions/a/b", session)
    ).toBeNull();
    expect(agentEventExecutionReference("other/9", session)).toBeNull();
    expect(
      agentEventExecutionReference("workspaces/ws/qualityExecutions/9", "bad")
    ).not.toBeNull();
    expect(
      agentEventExecutionReference("qualityExecutions/9", "bad")
    ).toBeNull();
    expect(
      agentEventExecutionReference("workspaces/ws/qualityRules/9", session)
    ).toBeNull();
  });
});

describe("target display", () => {
  it("uses the object name with a path subtitle and falls back to the GUID", () => {
    const session = sessionFixture();
    expect(agentSessionTargetName(session)).toBe("orders");
    expect(agentSessionTargetPath(session)).toBe("public.orders");
    expect(
      agentSessionTargetName(
        sessionFixture({
          target: create(QualityTargetSchema, { metaGuid: "fallback" }),
        })
      )
    ).toBe("fallback");
    expect(
      agentSessionTargetName(
        sessionFixture({ target: create(QualityTargetSchema, {}) })
      )
    ).toBe("-");
    expect(agentSessionTargetPath(sessionFixture({ target: undefined }))).toBe(
      "-"
    );
  });
});

function contextUsageFixture(
  overrides: MessageInitShape<typeof QualityAgentSessionContextUsageSchema> = {}
): QualityAgentSessionContextUsage {
  return create(QualityAgentSessionContextUsageSchema, {
    usedTokens: 1000,
    contextWindow: 4000,
    maxOutputTokens: 1000,
    compactionTriggerTokens: 3000,
    model: "test-model",
    wake: 2,
    estimated: false,
    ...overrides,
  });
}

describe("context usage", () => {
  it("measures the sample against the window and clamps the bar", () => {
    expect(agentSessionContextRatio(contextUsageFixture())).toBe(0.25);
    expect(
      agentSessionContextRatio(contextUsageFixture({ usedTokens: 5000 }))
    ).toBe(1);
    expect(
      agentSessionContextRatio(contextUsageFixture({ contextWindow: 0 }))
    ).toBe(0);
    expect(agentSessionContextRatio(undefined)).toBe(0);
  });

  it("warns at the compaction trigger and is critical at the window", () => {
    expect(agentSessionContextLevel(contextUsageFixture())).toBe("normal");
    expect(
      agentSessionContextLevel(contextUsageFixture({ usedTokens: 3000 }))
    ).toBe("warning");
    expect(
      agentSessionContextLevel(contextUsageFixture({ usedTokens: 4000 }))
    ).toBe("critical");
    // Without a trigger, only the window crosses the boundary.
    expect(
      agentSessionContextLevel(
        contextUsageFixture({ usedTokens: 3500, compactionTriggerTokens: 0 })
      )
    ).toBe("normal");
    expect(agentSessionContextLevel(undefined)).toBe("normal");
  });
});
