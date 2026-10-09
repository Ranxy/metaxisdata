import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
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

  it("sends a caller missing the page permission home", async () => {
    setAuth({ permissions: [] });

    await expect(guard("/settings/roles")).resolves.toEqual({ name: "Home" });
  });

  it("lets a caller holding the page permission through", async () => {
    setAuth({ permissions: ["metaxisdata.roles.list"] });

    await expect(guard("/settings/roles")).resolves.toBeUndefined();
  });

  it("keeps a pending password reset on the login form", async () => {
    setAuth({ reset: true, permissions: ["metaxisdata.roles.list"] });

    // Every other target goes to the login form...
    await expect(guard("/settings/roles")).resolves.toEqual({ name: "Login" });
    // ...and the login form keeps the caller: sending them Home here is what
    // used to bounce these two branches against each other without end.
    await expect(guard("/login")).resolves.toBeUndefined();
  });

  it("loads the profile before a protected page, but not for the login form", async () => {
    const auth = setAuth();

    await guard("/settings/general");
    expect(auth.ensurePermissionsLoaded).toHaveBeenCalledTimes(1);

    auth.ensurePermissionsLoaded = vi.fn().mockResolvedValue(undefined);
    await guard("/login");
    expect(auth.ensurePermissionsLoaded).not.toHaveBeenCalled();
  });
});
