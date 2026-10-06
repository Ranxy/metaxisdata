import { Code, ConnectError, type Interceptor } from "@connectrpc/connect";

/**
 * Called once when the server rejects an established session. `main.ts` wires it
 * to the auth store and the router: `api/` deliberately knows neither, so a
 * session failure cannot drag the whole store graph into the transport module.
 */
export type UnauthenticatedHandler = () => void;

/**
 * Renews the session from the refresh cookie. `main.ts` wires it to the auth
 * API, which keeps the transport free of a dependency on any particular client.
 */
export type SessionRefresher = () => Promise<void>;

let handler: UnauthenticatedHandler | undefined;
let refresher: SessionRefresher | undefined;
// The in-flight refresh, kept outside the closure: a promise is not state, and
// concurrent 401s must share one rotation rather than race for the same token.
let inFlightRefresh: Promise<void> | undefined;

export function setUnauthenticatedHandler(
  next: UnauthenticatedHandler | undefined
) {
  handler = next;
}

export function setSessionRefresher(next: SessionRefresher | undefined) {
  refresher = next;
}

const AUTH_SERVICE = "metaxisdata.v1.AuthService";

/**
 * Attempts one renewal, shared by every request that was refused while it runs.
 *
 * A refresh token is single-use, so two concurrent refreshes would consume each
 * other's: the second would be refused and would end a perfectly valid session.
 * One promise per burst is therefore a correctness requirement, not an
 * optimization.
 */
async function refreshSession(): Promise<boolean> {
  if (!refresher) {
    return false;
  }
  if (!inFlightRefresh) {
    inFlightRefresh = refresher().finally(() => {
      inFlightRefresh = undefined;
    });
  }
  try {
    await inFlightRefresh;
    return true;
  } catch {
    return false;
  }
}

/**
 * Answers a mid-session `Unauthenticated` with a renewal and one retry instead
 * of an immediate logout: the access token expiring is routine, and the refresh
 * cookie outlives it.
 *
 * The replay is safe — the server refused the request before doing any work —
 * and it happens at most once, so a session that is genuinely gone cannot put
 * the client in a refresh loop.
 *
 * The AuthService is exempt from the handler: a wrong password on Login also
 * answers `Unauthenticated`, and that belongs to the form, not to a session
 * logout. The refresh call itself goes through this interceptor too, and that
 * exemption is what keeps it from recursing.
 */
export const sessionInterceptor: Interceptor = (next) => async (request) => {
  try {
    return await next(request);
  } catch (error) {
    if (
      error instanceof ConnectError &&
      error.code === Code.Unauthenticated &&
      request.service.typeName !== AUTH_SERVICE
    ) {
      if (await refreshSession()) {
        try {
          return await next(request);
        } catch (retryError) {
          // The renewal succeeded but the replay was still refused, so the
          // session is genuinely gone. Ending it here saves the next request the
          // same round trip.
          if (
            retryError instanceof ConnectError &&
            retryError.code === Code.Unauthenticated
          ) {
            handler?.();
          }
          throw retryError;
        }
      }
      handler?.();
    }
    throw error;
  }
};
