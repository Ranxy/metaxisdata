import { create, type MessageInitShape } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
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
import {
  AGENT_RUNNER_STALE_AFTER_MS,
  AGENT_SESSION_STALE_AFTER_MS,
  agentMessageOriginKey,
  agentMessageRoleKey,
  agentSessionHeartbeat,
  agentSessionMatchesSearch,
  agentSessionStateKey,
  agentSessionStateVariant,
  agentSessionTargetName,
  agentSessionTargetPath,
  formatTokens,
  groupMessagesByWake,
  lastTranscriptSeq,
  mergeTranscriptMessages,
  orphanToolResults,
  runnerIsTicking,
  toolCallArgumentsPreview,
  toolResultForCall,
  truncateText,
} from "./agentSession";

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
    role: QualityAgentMessageRole.USER,
    origin: QualityAgentMessageOrigin.WAKE,
    content: `message ${seq}`,
    createTime: timestampFromDate(START),
    ...overrides,
  });
}

describe("agentSession state vocabulary", () => {
  it("maps every durable state to its label key and badge variant", () => {
    expect(agentSessionStateKey(QualityAgentSessionState.ENABLED)).toBe(
      "dataQuality.agent.state.enabled"
    );
    expect(agentSessionStateKey(QualityAgentSessionState.PAUSED)).toBe(
      "dataQuality.agent.state.paused"
    );
    expect(agentSessionStateKey(QualityAgentSessionState.BLOCKED)).toBe(
      "dataQuality.agent.state.blocked"
    );
    expect(agentSessionStateKey(QualityAgentSessionState.ARCHIVED)).toBe(
      "dataQuality.agent.state.archived"
    );
    expect(agentSessionStateKey(QualityAgentSessionState.UNSPECIFIED)).toBe(
      "dataQuality.agent.state.unspecified"
    );

    expect(agentSessionStateVariant(QualityAgentSessionState.ENABLED)).toBe(
      "success"
    );
    expect(agentSessionStateVariant(QualityAgentSessionState.BLOCKED)).toBe(
      "warning"
    );
    expect(agentSessionStateVariant(QualityAgentSessionState.PAUSED)).toBe(
      "neutral"
    );
    expect(agentSessionStateVariant(QualityAgentSessionState.ARCHIVED)).toBe(
      "neutral"
    );
    expect(agentSessionStateVariant(QualityAgentSessionState.UNSPECIFIED)).toBe(
      "outline"
    );
  });
});

describe("agentSession heartbeat", () => {
  it("reports a running session whose transcript is fresh as not stale", () => {
    const session = sessionFixture();
    const now = START.getTime() + 60_000;

    expect(agentSessionHeartbeat(session, now)).toEqual({
      running: true,
      stale: false,
    });
  });

  it("flags a live lease that has not written anything for ten minutes", () => {
    const session = sessionFixture();
    const now = START.getTime() + AGENT_SESSION_STALE_AFTER_MS + 1;

    expect(agentSessionHeartbeat(session, now)).toEqual({
      running: true,
      stale: true,
    });
  });

  it("never calls a stopped session stale", () => {
    const session = sessionFixture({
      running: false,
      lastActivityTime: timestampFromDate(new Date(0)),
    });

    expect(agentSessionHeartbeat(session, START.getTime())).toEqual({
      running: false,
      stale: false,
    });
  });

  it("treats a session without a recorded heartbeat as not stale", () => {
    const session = sessionFixture({ lastActivityTime: undefined });

    expect(agentSessionHeartbeat(session, START.getTime()).stale).toBe(false);
  });

  it("reads a timestamp at or before the epoch as missing", () => {
    const epoch = create(QualityAgentSessionSchema, {
      lastActivityTime: { seconds: 0n, nanos: 0 },
      running: true,
    });

    expect(agentSessionHeartbeat(epoch, START.getTime()).stale).toBe(false);
  });

  it("calls a runner tick newer than a minute live", () => {
    const now = START.getTime() + AGENT_RUNNER_STALE_AFTER_MS - 1;

    expect(runnerIsTicking(timestampFromDate(START), now)).toBe(true);
  });

  it("calls a missing or old runner tick stopped", () => {
    const now = START.getTime() + AGENT_RUNNER_STALE_AFTER_MS;

    expect(runnerIsTicking(timestampFromDate(START), now)).toBe(false);
    expect(runnerIsTicking(undefined, now)).toBe(false);
  });
});

describe("agentSession message vocabulary", () => {
  it("maps every role and origin to its label key", () => {
    expect(agentMessageRoleKey(QualityAgentMessageRole.SYSTEM)).toContain(
      "role.system"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.USER)).toContain(
      "role.user"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.ASSISTANT)).toContain(
      "role.assistant"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.TOOL)).toContain(
      "role.tool"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.ENGINE)).toContain(
      "role.engine"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.COMPACTION)).toContain(
      "role.compaction"
    );
    expect(agentMessageRoleKey(QualityAgentMessageRole.UNSPECIFIED)).toContain(
      "role.unspecified"
    );

    expect(agentMessageOriginKey(QualityAgentMessageOrigin.WAKE)).toContain(
      "origin.wake"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.EVENT)).toContain(
      "origin.event"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.ASK)).toContain(
      "origin.ask"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.RUNNER)).toContain(
      "origin.runner"
    );
    expect(agentMessageOriginKey(QualityAgentMessageOrigin.MODEL)).toContain(
      "origin.model"
    );
    expect(
      agentMessageOriginKey(QualityAgentMessageOrigin.COMPACTION)
    ).toContain("origin.compaction");
    expect(
      agentMessageOriginKey(QualityAgentMessageOrigin.UNSPECIFIED)
    ).toContain("origin.unspecified");
  });
});

describe("groupMessagesByWake", () => {
  it("groups by wake, orders groups and messages, and keeps the header data", () => {
    const system = messageFixture({
      wake: 0,
      role: QualityAgentMessageRole.SYSTEM,
      origin: QualityAgentMessageOrigin.UNSPECIFIED,
      content: "system prompt",
    });
    const first = messageFixture({
      wake: 1,
      role: QualityAgentMessageRole.ASSISTANT,
      origin: QualityAgentMessageOrigin.MODEL,
      content: "checking",
    });
    const wake = messageFixture({
      wake: 1,
      role: QualityAgentMessageRole.USER,
      origin: QualityAgentMessageOrigin.EVENT,
      content: "[event] a check failed",
    });

    // Arrival order is not transcript order; the helper must sort by seq.
    const groups = groupMessagesByWake([first, system, wake]);

    expect(groups.map((group) => group.wake)).toEqual([0, 1]);
    expect(groups[0].messages.map((message) => message.seq)).toEqual([
      system.seq,
    ]);
    expect(groups[1].messages.map((message) => message.seq)).toEqual([
      first.seq,
      wake.seq,
    ]);
    expect(groups[1].origin).toBe(QualityAgentMessageOrigin.EVENT);
    expect(groups[1].startTime).toEqual(wake.createTime);
    // The system group has no user message, so its origin stays unspecified.
    expect(groups[0].origin).toBe(QualityAgentMessageOrigin.UNSPECIFIED);
  });

  it("derives the wake summary from the last runner note", () => {
    const early = messageFixture({
      wake: 2,
      role: QualityAgentMessageRole.ENGINE,
      content: "model turn 1 started",
    });
    const result = messageFixture({
      wake: 2,
      role: QualityAgentMessageRole.ENGINE,
      content:
        "wake finished: recorded an investigation and will check again tomorrow",
    });
    const last = messageFixture({
      wake: 2,
      role: QualityAgentMessageRole.ASSISTANT,
      origin: QualityAgentMessageOrigin.MODEL,
      content: "done",
    });

    const [group] = groupMessagesByWake([early, result, last]);

    expect(group.summary).toBe(
      "recorded an investigation and will check again tomorrow"
    );
  });

  it("keeps a runner note that has no outcome prefix", () => {
    const note = messageFixture({
      role: QualityAgentMessageRole.ENGINE,
      content: "deterministic survey: no coverage gap",
    });

    expect(groupMessagesByWake([note])[0].summary).toBe(
      "deterministic survey: no coverage gap"
    );
  });

  it("skips empty runner notes and tolerates a wake without one", () => {
    const empty = messageFixture({
      wake: 3,
      role: QualityAgentMessageRole.ENGINE,
      content: "  ",
    });
    const text = messageFixture({
      wake: 3,
      role: QualityAgentMessageRole.ASSISTANT,
      content: "hello",
    });

    expect(groupMessagesByWake([empty, text])[0].summary).toBe("");
    expect(groupMessagesByWake([empty])[0].summary).toBe("");
  });

  it("tolerates duplicate seq values while grouping", () => {
    const first = messageFixture({ wake: 1 });
    const duplicate = messageFixture({ wake: 1, seq: first.seq });

    const [group] = groupMessagesByWake([duplicate, first]);

    expect(group.messages).toHaveLength(2);
  });
});

describe("mergeTranscriptMessages", () => {
  it("deduplicates pages by seq and keeps them chronological", () => {
    const first = messageFixture();
    const second = messageFixture();
    const third = messageFixture();

    const merged = mergeTranscriptMessages([second, first], [third, second]);

    expect(merged.map((message) => message.seq)).toEqual([
      first.seq,
      second.seq,
      third.seq,
    ]);
  });

  it("reports the newest seq, or zero for an empty transcript", () => {
    expect(lastTranscriptSeq([])).toBe(0n);
    const first = messageFixture();
    const second = messageFixture();
    expect(lastTranscriptSeq([second, first])).toBe(second.seq);
  });
});

describe("tool call helpers", () => {
  it("finds the result answering a call and ignores an empty id", () => {
    const result = messageFixture({
      role: QualityAgentMessageRole.TOOL,
      toolCallId: "call-1",
      toolName: "get_target_summary",
      content: "{}",
    });

    expect(toolResultForCall([result], "call-1")).toBe(result);
    expect(toolResultForCall([result], "call-2")).toBeUndefined();
    expect(toolResultForCall([result], "")).toBeUndefined();
  });

  it("returns results whose tool call is absent", () => {
    const assistant = messageFixture({
      role: QualityAgentMessageRole.ASSISTANT,
      toolCalls: [
        create(QualityToolCallSchema, {
          id: "call-1",
          name: "get_target_summary",
        }),
      ],
    });
    const paired = messageFixture({
      role: QualityAgentMessageRole.TOOL,
      toolCallId: "call-1",
      content: "paired",
    });
    const orphan = messageFixture({
      role: QualityAgentMessageRole.TOOL,
      toolCallId: "call-2",
      content: "orphan",
    });
    const unkeyed = messageFixture({
      role: QualityAgentMessageRole.TOOL,
      content: "no id",
    });

    expect(orphanToolResults([assistant, paired, orphan, unkeyed])).toEqual([
      orphan,
      unkeyed,
    ]);
    expect(orphanToolResults([paired])).toEqual([paired]);
  });

  it("previews JSON arguments as readable pairs", () => {
    const call = create(QualityToolCallSchema, {
      id: "call-1",
      name: "list_recent_executions",
      arguments: JSON.stringify({ targetGuid: TARGET, limit: 5 }),
    });

    expect(toolCallArgumentsPreview(call)).toBe(
      `targetGuid: ${TARGET}, limit: 5`
    );
  });

  it("keeps arrays and non-JSON arguments as written", () => {
    expect(
      toolCallArgumentsPreview(
        create(QualityToolCallSchema, { arguments: '["a","b"]' })
      )
    ).toBe('["a","b"]');
    expect(
      toolCallArgumentsPreview(
        create(QualityToolCallSchema, { arguments: "target=orders" })
      )
    ).toBe("target=orders");
  });

  it("shows nothing for a call without arguments", () => {
    expect(toolCallArgumentsPreview(create(QualityToolCallSchema, {}))).toBe(
      ""
    );
    expect(
      toolCallArgumentsPreview(
        create(QualityToolCallSchema, { arguments: "{}" })
      )
    ).toBe("");
  });

  it("serializes nested argument values and truncates long previews", () => {
    const nested = create(QualityToolCallSchema, {
      arguments: JSON.stringify({ filter: { status: "FAIL" } }),
    });
    expect(toolCallArgumentsPreview(nested)).toBe('filter: {"status":"FAIL"}');

    const long = create(QualityToolCallSchema, {
      arguments: JSON.stringify({ note: "x".repeat(400) }),
    });
    const preview = toolCallArgumentsPreview(long, 20);
    expect(Array.from(preview)).toHaveLength(20);
    expect(preview.endsWith("…")).toBe(true);
  });

  it("truncates text rune-safely", () => {
    expect(truncateText("orders", 10)).toBe("orders");
    expect(truncateText("orders", 6)).toBe("orders");
    expect(truncateText("订单明细", 2)).toBe("订…");
    expect(truncateText("orders", 0)).toBe("…");
  });
});

describe("formatTokens", () => {
  it("shows exact numbers below a thousand", () => {
    expect(formatTokens(0)).toBe("0");
    expect(formatTokens(999)).toBe("999");
    expect(formatTokens(-5)).toBe("-5");
    expect(formatTokens(999n)).toBe("999");
  });

  it("compacts thousands and millions", () => {
    expect(formatTokens(1500, "en-US")).toBe("1.5K");
    expect(formatTokens(2_500_000n, "en-US")).toBe("2.5M");
  });

  it("falls back to zero for a non-finite value", () => {
    expect(formatTokens(Number.NaN)).toBe("0");
  });
});

describe("target display", () => {
  it("names the target from its path and falls back to the GUID", () => {
    expect(agentSessionTargetName(sessionFixture())).toBe("orders");
    expect(
      agentSessionTargetName(
        sessionFixture({
          target: create(QualityTargetSchema, { metaGuid: TARGET }),
        })
      )
    ).toBe(TARGET);
    expect(
      agentSessionTargetName(
        sessionFixture({ target: create(QualityTargetSchema, {}) })
      )
    ).toBe("-");
  });

  it("renders the qualified path", () => {
    expect(agentSessionTargetPath(sessionFixture())).toBe("public.orders");
    expect(
      agentSessionTargetPath(
        sessionFixture({ target: create(QualityTargetSchema, {}) })
      )
    ).toBe("-");
  });

  it("matches a search over the path, GUID and database", () => {
    const session = sessionFixture();

    expect(agentSessionMatchesSearch(session, " ")).toBe(true);
    expect(agentSessionMatchesSearch(session, "ORDERS")).toBe(true);
    expect(agentSessionMatchesSearch(session, "public")).toBe(true);
    expect(agentSessionMatchesSearch(session, "instances/inst1")).toBe(true);
    expect(agentSessionMatchesSearch(session, "missing")).toBe(false);
  });
});
