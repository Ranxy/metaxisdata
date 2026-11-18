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
