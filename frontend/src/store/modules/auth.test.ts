import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { useAuthStore } from "./auth";

const mocks = vi.hoisted(() => ({
  login: vi.fn(),
  logout: vi.fn(),
  getCurrentUser: vi.fn(),
  updateUser: vi.fn(),
}));

vi.mock("@/api/auth", () => ({
  login: mocks.login,
  logout: mocks.logout,
}));

vi.mock("@/api/user", () => ({
  getCurrentUser: mocks.getCurrentUser,
  updateUser: mocks.updateUser,
}));

function loginResponse(requireResetPassword: boolean) {
  // The server's login response deliberately carries the user without
  // permissions; only GetCurrentUser resolves them.
  return {
    user: create(UserSchema, { name: "users/1", email: "dev@example.com" }),
    requireResetPassword,
  };
}

describe("ensurePermissionsLoaded", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
  });

  it("loads the permissions the login response left out", async () => {
    mocks.login.mockResolvedValue(loginResponse(false));
    mocks.getCurrentUser.mockResolvedValue(
      create(UserSchema, {
        name: "users/1",
        permissions: ["metaxisdata.instances.list"],
      })
    );

    const store = useAuthStore();
    await store.login("dev@example.com", "pw");
    expect(store.permissions).toEqual([]);

    await store.ensurePermissionsLoaded();
    expect(store.permissions).toEqual(["metaxisdata.instances.list"]);
    expect(mocks.getCurrentUser).toHaveBeenCalledTimes(1);
  });

  it("is a no-op on every later navigation", async () => {
    mocks.getCurrentUser.mockResolvedValue(
      create(UserSchema, { name: "users/1", permissions: ["metaxisdata.read"] })
    );

    const store = useAuthStore();
    await store.ensurePermissionsLoaded();
    await store.ensurePermissionsLoaded();

    expect(mocks.getCurrentUser).toHaveBeenCalledTimes(1);
  });

  it("keeps the restricted-token session intact for the password reset", async () => {
    mocks.login.mockResolvedValue(loginResponse(true));

    const store = useAuthStore();
    await store.login("dev@example.com", "pw");
    await store.ensurePermissionsLoaded();

    // The restricted token may not call GetCurrentUser, and the pending user
    // has to survive for changePassword().
    expect(mocks.getCurrentUser).not.toHaveBeenCalled();
    expect(store.user?.name).toBe("users/1");
    expect(store.permissionsLoaded).toBe(false);
  });

  it("clears the session when the profile cannot be loaded", async () => {
    mocks.getCurrentUser.mockRejectedValue(new Error("unauthenticated"));

    const store = useAuthStore();
    await store.ensurePermissionsLoaded();

    expect(store.isAuthenticated).toBe(false);
    expect(store.permissionsLoaded).toBe(false);

    // A later navigation retries instead of trusting the empty state.
    mocks.getCurrentUser.mockResolvedValue(
      create(UserSchema, { name: "users/1", permissions: ["metaxisdata.read"] })
    );
    await store.ensurePermissionsLoaded();
    expect(store.permissionsLoaded).toBe(true);
  });
});
