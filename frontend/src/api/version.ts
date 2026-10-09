/**
 * The build metadata `GET /api/version` reports. It describes the server that
 * served the SPA, which is what an operator comparing a running deployment
 * against a release needs — not the bundle's own build.
 */
export interface BuildInfo {
  version: string;
  git_commit: string;
  build_time: string;
}

/**
 * Reads the server's build metadata, or `null` when it is unavailable.
 *
 * `/api/version` is the one endpoint that is not a ConnectRPC service — the
 * server serves it next to `/healthz` (`backend/server/echo_routes.go`) — so it
 * is a plain fetch rather than a client from `./client`, but it still belongs
 * to this layer instead of a component (see `frontend/AGENTS.md`).
 *
 * It never throws: the value is decorative, so an older server, a proxy that
 * strips the path, or a network failure should leave the caller blank rather
 * than surface an error nobody can act on.
 */
export async function fetchBuildInfo(): Promise<BuildInfo | null> {
  const baseUrl = import.meta.env.VITE_API_BASE_URL || "";
  try {
    const response = await fetch(`${baseUrl}/api/version`);
    if (!response.ok) {
      return null;
    }
    return (await response.json()) as BuildInfo;
  } catch {
    return null;
  }
}
