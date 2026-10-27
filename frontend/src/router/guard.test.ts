import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import router, { handleSessionExpired } from "@/router";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";

const mocks = vi.hoisted(() => ({
  getCurrentUser: vi.fn(),
  updateUser: vi.fn(),
  login: vi.fn(),
  logout: vi.fn(),
}));

vi.mock("@/api/user", () => ({
  getCurrentUser: mocks.getCurrentUser,
  updateUser: mocks.updateUser,
}));

vi.mock("@/api/auth", () => ({
  login: mocks.login,
  logout: mocks.logout,
}));

describe("router auth guard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    mocks.getCurrentUser.mockResolvedValue(
      create(UserSchema, {
        name: "users/1",
        permissions: ["metaxisdata.instances.list"],
      })
    );
  });

  it("sends an unauthenticated visitor to the login page", async () => {
    mocks.getCurrentUser.mockRejectedValue(
      new ConnectError("no cookie", Code.Unauthenticated)
    );

    await router.push("/databases");

    expect(useAuthStore().isAuthenticated).toBe(false);
    expect(router.currentRoute.value.name).toBe("Login");
  });

  it("explains an expired session instead of showing a bare form", async () => {
    // GetCurrentUser is where an expired cookie is discovered, but only a
    // session the app already believed in can expire — so arrive signed in.
    const authStore = useAuthStore();
    authStore.user = create(UserSchema, { name: "users/1" });
    authStore.isAuthenticated = true;
    authStore.permissionsLoaded = false;
    mocks.getCurrentUser.mockRejectedValue(
      new ConnectError("no cookie", Code.Unauthenticated)
    );

    await router.push("/databases");

    expect(useAuthStore().sessionExpired).toBe(true);
    expect(router.currentRoute.value.name).toBe("Login");
    expect(router.currentRoute.value.query).toMatchObject({
      expired: "1",
      redirect: "/databases",
    });
  });

  it("does not claim an expiry for a visitor who never signed in", async () => {
    // The same Unauthenticated the previous test uses, but with no session
    // behind it: landing on /login must not say a session expired.
    mocks.getCurrentUser.mockRejectedValue(
      new ConnectError("no cookie", Code.Unauthenticated)
    );

    await router.push("/databases");

    expect(useAuthStore().sessionExpired).toBe(false);
    expect(router.currentRoute.value.name).toBe("Login");
    expect(router.currentRoute.value.query).not.toHaveProperty("expired");
  });

  it("does not claim an expiry when the profile call itself failed", async () => {
    mocks.getCurrentUser.mockRejectedValue(
      new ConnectError("upstream down", Code.Unavailable)
    );

    await router.push("/databases");

    expect(router.currentRoute.value.name).toBe("Login");
    expect(router.currentRoute.value.query).not.toHaveProperty("expired");
  });

  it("loads the permissions before entering a page right after login", async () => {
    const authStore = useAuthStore();
    // Exactly what login() leaves behind: a user without permissions.
    authStore.user = create(UserSchema, { name: "users/1" });
    authStore.isAuthenticated = true;
    expect(authStore.permissions).toEqual([]);

    await router.push("/");

    expect(mocks.getCurrentUser).toHaveBeenCalledTimes(1);
    expect(authStore.permissions).toEqual(["metaxisdata.instances.list"]);

    // The permission-bearing profile is loaded once, not on every navigation.
    await router.push("/databases");
    expect(mocks.getCurrentUser).toHaveBeenCalledTimes(1);
  });

  // The reported bug: following a saved sign-in link with a valid session used
  // to leave the caller on the form.
  it("continues to the requested page when the session is already valid", async () => {
    await router.push("/login?redirect=/databases");

    expect(router.currentRoute.value.name).toBe("DatabaseManagement");
  });

  it("does not land a signed-in caller on a page they cannot open", async () => {
    mocks.getCurrentUser.mockResolvedValue(
      create(UserSchema, { name: "users/1", permissions: [] })
    );

    await router.push("/login?redirect=/settings/roles");

    // The redirect is not a permission grant: the role page's own guard turns the
    // caller away exactly as it does for any other navigation.
    expect(router.currentRoute.value.name).toBe("Home");
  });

  it("sends an expired session to the login form with its path", async () => {
    await router.push("/instances");
    const authStore = useAuthStore();
    expect(authStore.isAuthenticated).toBe(true);

    await handleSessionExpired();

    expect(authStore.isAuthenticated).toBe(false);
    expect(authStore.sessionExpired).toBe(true);
    expect(router.currentRoute.value.name).toBe("Login");
    expect(router.currentRoute.value.query).toMatchObject({
      expired: "1",
      redirect: "/instances",
    });
  });

  it("does not navigate for a refusal before there was ever a session", async () => {
    mocks.getCurrentUser.mockRejectedValue(
      new ConnectError("no cookie", Code.Unauthenticated)
    );
    await router.push("/login?redirect=/databases");
    const authStore = useAuthStore();
    expect(authStore.isAuthenticated).toBe(false);

    // A guarded page's first profile request is refused before the caller ever
    // signed in; the guard is already sending them to the form, so a push here
    // would race it and invent an expiry.
    await handleSessionExpired();

    expect(authStore.sessionExpired).toBe(false);
    expect(router.currentRoute.value.name).toBe("Login");
    expect(router.currentRoute.value.query).toMatchObject({
      redirect: "/databases",
    });
  });
});
