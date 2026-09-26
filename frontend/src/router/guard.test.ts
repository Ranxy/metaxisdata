import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import router from "@/router";
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
    // GetCurrentUser is the first call a booted app makes, so it is where an
    // expired cookie is usually discovered.
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
});
