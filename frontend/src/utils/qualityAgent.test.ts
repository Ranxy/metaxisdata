import { describe, expect, it } from "vitest";
import {
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
