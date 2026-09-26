import { Code, ConnectError, type Interceptor } from "@connectrpc/connect";

/**
 * Called once when the server rejects an established session. `main.ts` wires it
 * to the auth store and the router: `api/` deliberately knows neither, so a
 * session failure cannot drag the whole store graph into the transport module.
 */
export type UnauthenticatedHandler = () => void;

let handler: UnauthenticatedHandler | undefined;

export function setUnauthenticatedHandler(
  next: UnauthenticatedHandler | undefined
) {
  handler = next;
}

const AUTH_SERVICE = "metaxisdata.v1.AuthService";

/**
 * Turns a mid-session `Unauthenticated` into one global logout instead of a
 * per-page toast: the cookie expired, so every later request would fail too.
 *
 * The AuthService is exempt — a wrong password on Login also answers
 * `Unauthenticated`, and that belongs to the form, not to a session logout.
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
      handler?.();
    }
    throw error;
  }
};
