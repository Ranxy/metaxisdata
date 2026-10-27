import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { NavigationGuardNext, RouteLocationNormalized } from "vue-router";
import router, { authGuard } from "@/router";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";

// The guard is called directly rather than through `router.push`. The pair
// covered by the pending-reset case below bounces `Login` ↔ `Home`, and that
// loop is pure microtasks: it never yields, so the timer a test timeout would
// need never fires and the suite hangs instead of failing.
const next: NavigationGuardNext = () => {};

function guard(path: string) {
  // `resolve` hands back the looser `RouteLocationResolved`, whose name may be
  // null; every path here matches a real record, which is the shape the router
  // passes to a guard.
  const to = router.resolve(path) as RouteLocationNormalized;
  return authGuard(to, router.currentRoute.value, next);
}

function setAuth(
  options: {
    authenticated?: boolean;
    reset?: boolean;
    expired?: boolean;
    permissions?: string[];
  } = {}
) {
  setActivePinia(createPinia());
  const auth = useAuthStore();
  auth.isAuthenticated = options.authenticated ?? true;
  auth.requireResetPassword = options.reset ?? false;
  auth.sessionExpired = options.expired ?? false;
  auth.user = create(UserSchema, { permissions: options.permissions ?? [] });
  // Stubbed: the profile request itself is the store's business, not the
  // guard's, which only has to ask for it at the right moment.
  auth.ensurePermissionsLoaded = vi.fn().mockResolvedValue(undefined);
  return auth;
}

describe("auth guard", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("sends a signed-out caller to the login form with the target it wanted", async () => {
    setAuth({ authenticated: false });

    await expect(guard("/settings/general")).resolves.toEqual({
      name: "Login",
      query: { redirect: "/settings/general" },
    });
  });

  it("marks a rejected session on the login form", async () => {
    setAuth({ authenticated: false, expired: true });

    await expect(guard("/settings/general")).resolves.toEqual({
      name: "Login",
      query: { redirect: "/settings/general", expired: "1" },
    });
  });

  it("lets a signed-in caller reach the page", async () => {
    setAuth();

    await expect(guard("/settings/general")).resolves.toBeUndefined();
  });

  it("turns a signed-in caller away from the login form", async () => {
    setAuth();

    await expect(guard("/login")).resolves.toEqual({ name: "Home" });
  });

  // The reported bug: a saved sign-in link that names a destination used to
  // stop on the form even though the browser still had a session, because the
  // guard never asked who the caller was on a public route.
  it("resumes the page a signed-in caller asked for at the login form", async () => {
    setAuth();

    await expect(guard("/login?redirect=/settings/general")).resolves.toEqual({
      path: "/settings/general",
      query: {},
      hash: "",
    });
  });

  it("keeps the target's own query string", async () => {
    setAuth();

    await expect(
      guard(
        "/login?redirect=/metadata/test-pg-1/dataforge/public/llm_provider_profile?metaType=4"
      )
    ).resolves.toEqual({
      path: "/metadata/test-pg-1/dataforge/public/llm_provider_profile",
      query: { metaType: "4" },
      hash: "",
    });
  });

  // The MCP client's browser sign-in is a server route: only a full page load
  // resumes the authorization, so it must not be handed to the router.
  it("hands a server route back to the browser", async () => {
    setAuth();
    const replace = vi.fn();
    vi.stubGlobal("window", { location: { replace } });

    await expect(
      guard("/login?redirect=%2Foauth%2Fauthorize%3Fclient_id%3Dabc")
    ).resolves.toBe(false);
    expect(replace).toHaveBeenCalledWith("/oauth/authorize?client_id=abc");
  });

  it("does not bounce a redirect that names the login form itself", async () => {
    setAuth();

    await expect(
      guard("/login?redirect=/login?redirect=/settings/general")
    ).resolves.toEqual({ name: "Home" });
  });

  // A repeated parameter arrives as an array; only a single string is a target.
  it("ignores a redirect parameter that repeats", async () => {
    setAuth();

    await expect(
      guard("/login?redirect=/settings/general&redirect=/settings/users")
    ).resolves.toEqual({ name: "Home" });
  });

  // The URL parser strips tab and newlines, so this value would reach
  // `location.replace` as `//evil.example.com` — an off-origin URL.
  it("rejects a redirect that hides an origin behind a stripped control character", async () => {
    setAuth();

    await expect(
      guard("/login?redirect=/%09/evil.example.com")
    ).resolves.toEqual({ name: "Home" });
  });

  it("sends a caller missing the page permission home", async () => {
    setAuth({ permissions: [] });

    await expect(guard("/settings/roles")).resolves.toEqual({ name: "Home" });
  });

  it("lets a caller holding the page permission through", async () => {
    setAuth({ permissions: ["metaxisdata.roles.list"] });

    await expect(guard("/settings/roles")).resolves.toBeUndefined();
  });

  // The administration surface is permission-gated; the self-service one is
  // deliberately not, because every signed-in user manages their own connections.
  it("guards the connected-apps administration page by permission", async () => {
    setAuth({ permissions: [] });
    await expect(guard("/settings/connected-apps")).resolves.toEqual({
      name: "Home",
    });

    setAuth({ permissions: ["metaxisdata.connectedApps.list"] });
    await expect(guard("/settings/connected-apps")).resolves.toBeUndefined();
  });

  it("lets any signed-in caller reach their own connected apps", async () => {
    setAuth({ permissions: [] });

    await expect(guard("/connected-apps")).resolves.toBeUndefined();
  });

  it("keeps a pending password reset on the login form", async () => {
    setAuth({ reset: true, permissions: ["metaxisdata.roles.list"] });

    // Every other target goes to the login form...
    await expect(guard("/settings/roles")).resolves.toEqual({ name: "Login" });
    // ...and the login form keeps the caller: sending them Home here is what
    // used to bounce these two branches against each other without end.
    await expect(guard("/login")).resolves.toBeUndefined();
  });

  it("loads the profile before a protected page", async () => {
    const auth = setAuth();

    await guard("/settings/general");
    expect(auth.ensurePermissionsLoaded).toHaveBeenCalledTimes(1);
  });

  it("probes the session before showing the login form", async () => {
    const auth = setAuth();

    await guard("/login");
    expect(auth.ensurePermissionsLoaded).toHaveBeenCalledTimes(1);

    // The store records the answer, so the form's next arrival must not ask again.
    auth.sessionChecked = true;
    await guard("/login");
    expect(auth.ensurePermissionsLoaded).toHaveBeenCalledTimes(1);
  });
});
