/**
 * Where a successful sign-in should land.
 *
 * `redirect` reaches us from the router guard (which records the path that was
 * asked for) or from the stored SSO state record, so it is caller-supplied and
 * has to be treated as such.
 */

/**
 * Only a same-origin absolute path is a redirect target. Anything else — an
 * absolute URL, a protocol-relative `//host` path, or a value the provider echoed
 * back — would turn the sign-in into an open redirect.
 */
export function sanitizeRedirect(
  redirect: string | null | undefined
): string | undefined {
  if (!redirect) {
    return undefined;
  }
  if (!redirect.startsWith("/") || redirect.startsWith("//")) {
    return undefined;
  }
  // A backslash is a path separator to the URL parser, so `/\evil.com` passes the
  // two checks above and still resolves off-origin. Rejecting the whole value is
  // simpler than trying to normalise it.
  if (redirect.includes("\\")) {
    return undefined;
  }
  // The URL parser strips ASCII tab, LF and CR before parsing, so `/\t/evil.com`
  // passes every check above and then resolves to `//evil.com` — an off-origin
  // URL. Reject every control character rather than enumerate the ones the
  // browser strips.
  if (/[\u0000-\u001f\u007f]/.test(redirect)) {
    return undefined;
  }
  return redirect;
}

/**
 * Whether the router owns a real route for the name `router.resolve(path)` gave
 * back. An unmatched path answers no record at all (`undefined`), and this app's
 * catch-all is named `NotFound`; neither is an in-app destination.
 */
export function isKnownSpaRouteName(
  name: string | symbol | null | undefined
): boolean {
  return typeof name === "string" && name !== "NotFound";
}

export type SignInTarget =
  | { kind: "home" }
  | { kind: "spa"; path: string }
  | { kind: "server"; path: string };

/**
 * Decides between an in-app navigation and a full page load.
 *
 * `isKnownSpaPath` answers whether the router owns a real route for the path,
 * and the answer cannot come from `router.resolve(...).matched.length`: this app
 * registers a catch-all `/:pathMatch(.*)*`, so that is never zero and every
 * unknown path would be treated as an SPA route.
 *
 * The case that matters is the MCP client's browser sign-in, which bounces a
 * signed-out user through `/login?redirect=%2Foauth%2Fauthorize%3F...`. That is a
 * server route, and loading it from the server is what resumes the authorization;
 * handing it to the router lands the user on the 404 page instead.
 */
export function resolveSignInTarget(
  redirect: string | null | undefined,
  isKnownSpaPath: (path: string) => boolean
): SignInTarget {
  const path = sanitizeRedirect(redirect);
  if (!path) {
    return { kind: "home" };
  }
  return isKnownSpaPath(path)
    ? { kind: "spa", path }
    : { kind: "server", path };
}
