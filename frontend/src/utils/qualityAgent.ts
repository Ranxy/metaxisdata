/**
 * Resource-name and route helpers for quality agents. A single-resource RPC needs
 * the real workspace segment, so a page opened from an agent name carries both the
 * workspace and the resource id in its route instead of rebuilding `workspaces/-`.
 */
export interface QualityAgentRouteParams {
  [key: string]: string;
  workspace: string;
  agentId: string;
}

/** Splits `workspaces/{workspace}/qualityAgents/{id}` into route params. */
export function qualityAgentRouteParams(
  name: string
): QualityAgentRouteParams | undefined {
  const parts = name.split("/");
  if (
    parts.length < 4 ||
    parts[0] !== "workspaces" ||
    parts[2] !== "qualityAgents"
  ) {
    return undefined;
  }
  if (parts[1] === "" || parts[3] === "") {
    return undefined;
  }
  return { workspace: parts[1], agentId: parts[3] };
}

/** Builds the agent home route for a full resource name. */
export function qualityAgentRoute(name: string) {
  const params = qualityAgentRouteParams(name);
  return params === undefined
    ? { name: "QualityAgents" }
    : { name: "QualityAgent", params };
}

/** Builds the full resource name from route params. */
export function qualityAgentName(workspace: string, agentId: string): string {
  return `workspaces/${workspace}/qualityAgents/${agentId}`;
}

/** Builds the product URL of an agent from its full resource name. */
export function qualityAgentPath(name: string): string {
  const params = qualityAgentRouteParams(name);
  return params === undefined
    ? "/data-quality/agents"
    : `/data-quality/agents/${params.workspace}/${params.agentId}`;
}

/** Builds the agent's work-session list route from the agent resource name. */
export function qualityAgentSessionsRoute(name: string) {
  const params = qualityAgentRouteParams(name);
  return params === undefined
    ? { name: "QualityAgents" }
    : { name: "QualityAgentSessions", params };
}

/** The resource id inside `workspaces/{workspace}/qualityAgentSessions/{id}`. */
export function qualityAgentSessionId(name: string): string {
  const parts = name.split("/");
  if (
    parts.length < 4 ||
    parts[0] !== "workspaces" ||
    parts[2] !== "qualityAgentSessions" ||
    parts[1] === "" ||
    parts[3] === ""
  ) {
    return "";
  }
  return parts[3];
}

/** Builds the full session resource name from the route's workspace and id. */
export function qualityAgentSessionName(
  workspace: string,
  sessionId: string
): string {
  return `workspaces/${workspace}/qualityAgentSessions/${sessionId}`;
}

/** Builds one work-session detail route from the agent and session names. */
export function qualityAgentSessionRoute(
  agentName: string,
  sessionName: string
) {
  const params = qualityAgentRouteParams(agentName);
  const sessionId = qualityAgentSessionId(sessionName);
  if (params === undefined || sessionId === "") {
    return { name: "QualityAgents" };
  }
  return {
    name: "QualityAgentSession",
    params: { ...params, sessionId },
  };
}
