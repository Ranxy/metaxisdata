import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";
import {
  type QualityAgentAsk,
  QualityAgentAskKind,
  QualityAgentAskSchema,
  QualityAgentAskStatus,
  QualityTargetSchema,
} from "@/types/proto-es/v1/data_quality_service_pb";
import {
  qualityAgentAskIsApproval,
  qualityAgentAskKindKey,
  qualityAgentAskStatusKey,
  qualityAgentAskTargetGuid,
  qualityAgentName,
  qualityAgentPath,
  qualityAgentRoute,
  qualityAgentRouteParams,
  qualityAgentSessionId,
  qualityAgentSessionName,
  qualityAgentSessionRoute,
  qualityAgentSessionsRoute,
} from "./qualityAgent";

describe("quality agent route helpers", () => {
  const name = "workspaces/ws-1/qualityAgents/agent-1";

  it("splits a full resource name into the real workspace and id", () => {
    expect(qualityAgentRouteParams(name)).toEqual({
      workspace: "ws-1",
      agentId: "agent-1",
    });
    expect(qualityAgentRoute(name)).toEqual({
      name: "QualityAgent",
      params: { workspace: "ws-1", agentId: "agent-1" },
    });
  });

  it("builds the resource name and product path", () => {
    expect(qualityAgentName("ws-1", "agent-1")).toBe(name);
    expect(qualityAgentPath(name)).toBe("/data-quality/agents/ws-1/agent-1");
  });

  it("falls back to the agent list for a malformed name", () => {
    expect(qualityAgentRouteParams("qualityAgents/agent-1")).toBeUndefined();
    expect(qualityAgentRoute("qualityAgents/agent-1")).toEqual({
      name: "QualityAgents",
    });
    expect(qualityAgentPath("qualityAgents/agent-1")).toBe(
      "/data-quality/agents"
    );
  });

  it("builds the work-session routes and resource names", () => {
    expect(qualityAgentSessionsRoute(name)).toEqual({
      name: "QualityAgentSessions",
      params: { workspace: "ws-1", agentId: "agent-1" },
    });
    expect(
      qualityAgentSessionId("workspaces/ws-1/qualityAgentSessions/session-1")
    ).toBe("session-1");
    expect(qualityAgentSessionName("ws-1", "session-1")).toBe(
      "workspaces/ws-1/qualityAgentSessions/session-1"
    );
    expect(
      qualityAgentSessionRoute(
        name,
        "workspaces/ws-1/qualityAgentSessions/session-1"
      )
    ).toEqual({
      name: "QualityAgentSession",
      params: {
        workspace: "ws-1",
        agentId: "agent-1",
        sessionId: "session-1",
      },
    });
    expect(qualityAgentSessionId("workspaces/ws-1/qualityAgentSessions")).toBe(
      ""
    );
    expect(qualityAgentSessionRoute(name, "bad")).toEqual({
      name: "QualityAgents",
    });
    expect(qualityAgentSessionsRoute("bad")).toEqual({ name: "QualityAgents" });
  });

  it("rejects names with the wrong prefix or an empty segment", () => {
    expect(
      qualityAgentRouteParams("projects/ws/qualityAgents/agent-1")
    ).toBeUndefined();
    expect(
      qualityAgentRouteParams("workspaces//qualityAgents/agent-1")
    ).toBeUndefined();
    expect(
      qualityAgentRouteParams("workspaces/ws/qualityAgents/")
    ).toBeUndefined();
    expect(
      qualityAgentRouteParams("workspaces/ws/qualityAgents")
    ).toBeUndefined();
  });
});

describe("quality agent ask helpers", () => {
  function ask(
    kind: QualityAgentAskKind,
    status: QualityAgentAskStatus
  ): QualityAgentAsk {
    return {
      kind,
      status,
    } as QualityAgentAsk;
  }

  it("maps every kind and status to a catalog key", () => {
    expect(qualityAgentAskKindKey(QualityAgentAskKind.QUESTION)).toBe(
      "qualityAgent.askKindQuestion"
    );
    expect(qualityAgentAskKindKey(QualityAgentAskKind.ANOMALY)).toBe(
      "qualityAgent.askKindAnomaly"
    );
    expect(qualityAgentAskKindKey(QualityAgentAskKind.APPROVAL)).toBe(
      "qualityAgent.askKindApproval"
    );
    expect(qualityAgentAskStatusKey(QualityAgentAskStatus.OPEN)).toBe(
      "qualityAgent.askStatusOpen"
    );
    expect(qualityAgentAskStatusKey(QualityAgentAskStatus.DISMISSED)).toBe(
      "qualityAgent.askStatusDismissed"
    );
    expect(qualityAgentAskKindKey(QualityAgentAskKind.UNSPECIFIED)).toBe(
      "qualityAgent.askKindUnspecified"
    );
    expect(qualityAgentAskStatusKey(QualityAgentAskStatus.ANSWERED)).toBe(
      "qualityAgent.askStatusAnswered"
    );
    expect(qualityAgentAskStatusKey(QualityAgentAskStatus.RESOLVED)).toBe(
      "qualityAgent.askStatusResolved"
    );
    expect(qualityAgentAskStatusKey(QualityAgentAskStatus.UNSPECIFIED)).toBe(
      "qualityAgent.askStatusUnspecified"
    );
  });

  it("only an open approval ask reuses the adoption path", () => {
    expect(
      qualityAgentAskIsApproval(
        ask(QualityAgentAskKind.APPROVAL, QualityAgentAskStatus.OPEN)
      )
    ).toBe(true);
    expect(
      qualityAgentAskIsApproval(
        ask(QualityAgentAskKind.APPROVAL, QualityAgentAskStatus.ANSWERED)
      )
    ).toBe(false);
    expect(
      qualityAgentAskIsApproval(
        ask(QualityAgentAskKind.ANOMALY, QualityAgentAskStatus.OPEN)
      )
    ).toBe(false);
  });
});

describe("quality agent ask target", () => {
  it("returns the one target an approval ask covers", () => {
    const ask = create(QualityAgentAskSchema, {
      targets: [create(QualityTargetSchema, { metaGuid: "inst;db;;orders" })],
    });
    expect(qualityAgentAskTargetGuid(ask)).toBe("inst;db;;orders");
  });

  it("refuses an ask with no target or several targets", () => {
    expect(qualityAgentAskTargetGuid(create(QualityAgentAskSchema, {}))).toBe(
      ""
    );
    const many = create(QualityAgentAskSchema, {
      targets: [
        create(QualityTargetSchema, { metaGuid: "a" }),
        create(QualityTargetSchema, { metaGuid: "b" }),
      ],
    });
    expect(qualityAgentAskTargetGuid(many)).toBe("");
  });
});
