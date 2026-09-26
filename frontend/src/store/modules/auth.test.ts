import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
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

  it("clears the session when the server rejects the cookie", async () => {
    mocks.getCurrentUser.mockRejectedValue(
      new ConnectError("no cookie", Code.Unauthenticated)
    );

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

  it("keeps a valid session when the request fails for another reason", async () => {
    const store = useAuthStore();
    store.user = create(UserSchema, { name: "users/1" });
    store.isAuthenticated = true;
    mocks.getCurrentUser.mockRejectedValue(
      new ConnectError("upstream down", Code.Unavailable)
    );

    await store.ensurePermissionsLoaded();

    // A 5xx or a dropped connection is not a logout: the next navigation retries
    // with the same cookie instead of bouncing the user to the login page.
    expect(store.isAuthenticated).toBe(true);
    expect(store.user?.name).toBe("users/1");
    expect(store.permissionsLoaded).toBe(false);
  });

  it("shares one request between two navigations in the same tick", async () => {
    let resolveProfile: (user: unknown) => void = () => {};
    mocks.getCurrentUser.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveProfile = resolve;
        })
    );

    const store = useAuthStore();
    const first = store.ensurePermissionsLoaded();
    const second = store.ensurePermissionsLoaded();
    resolveProfile(create(UserSchema, { name: "users/1" }));
    await Promise.all([first, second]);

    expect(mocks.getCurrentUser).toHaveBeenCalledTimes(1);
    expect(store.permissionsLoaded).toBe(true);
  });
});

describe("clearSession", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
  });

  it("drops the user, the reset flag and the loaded permissions together", async () => {
    mocks.login.mockResolvedValue(loginResponse(true));

    const store = useAuthStore();
    await store.login("dev@example.com", "pw");
    store.clearSession();

    expect(store.user).toBeNull();
    expect(store.isAuthenticated).toBe(false);
    expect(store.requireResetPassword).toBe(false);
    expect(store.permissionsLoaded).toBe(false);
  });

  it("does not call the rejection of a session that never existed an expiry", async () => {
    const store = useAuthStore();

    store.handleUnauthenticated();

    expect(store.isAuthenticated).toBe(false);
    // The first GetCurrentUser of a visitor with no cookie answers the same way,
    // and the login page must not claim that a session expired.
    expect(store.sessionExpired).toBe(false);
  });

  it("remembers that the server rejected a session the app believed in", async () => {
    mocks.login.mockResolvedValue(loginResponse(false));
    const store = useAuthStore();
    await store.login("dev@example.com", "pw");

    store.handleUnauthenticated();

    expect(store.isAuthenticated).toBe(false);
    expect(store.sessionExpired).toBe(true);
  });

  it("clears the expiry flag on the next successful login", async () => {
    mocks.login.mockResolvedValue(loginResponse(false));
    const store = useAuthStore();
    await store.login("dev@example.com", "pw");
    store.handleUnauthenticated();
    expect(store.sessionExpired).toBe(true);

    await store.login("dev@example.com", "pw");

    expect(store.sessionExpired).toBe(false);
  });

  it("does not call a deliberate sign-out an expiry", async () => {
    mocks.login.mockResolvedValue(loginResponse(false));
    mocks.logout.mockResolvedValue(undefined);
    const store = useAuthStore();
    await store.login("dev@example.com", "pw");

    await store.logout();

    expect(store.sessionExpired).toBe(false);
  });

  it("is what a failed logout leaves behind", async () => {
    mocks.login.mockResolvedValue(loginResponse(false));
    mocks.logout.mockRejectedValue(new Error("network"));

    const store = useAuthStore();
    await store.login("dev@example.com", "pw");
    await expect(store.logout()).rejects.toThrow("network");

    expect(store.isAuthenticated).toBe(false);
    expect(store.user).toBeNull();
  });
});
