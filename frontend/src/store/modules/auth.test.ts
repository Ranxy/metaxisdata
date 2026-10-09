import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/locales";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";
import { useAppStore } from "./app";
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

describe("language", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // clearAllMocks keeps implementations, so a rejection mocked by one test
    // would otherwise leak into the next.
    mocks.updateUser.mockResolvedValue(undefined);
    setActivePinia(createPinia());
    localStorage.clear();
    i18n.global.locale.value = "en-US";
  });

  it("switches the UI and records the language on the profile", async () => {
    const store = useAuthStore();
    store.user = create(UserSchema, { name: "users/1", language: "en-US" });

    await store.changeLanguage("zh-CN");

    // The server answers Explain SQL in the language stored here, so the switch
    // has to reach the profile, not just the browser.
    expect(mocks.updateUser).toHaveBeenCalledWith(
      { name: "users/1", language: "zh-CN" },
      ["language"]
    );
    expect(store.user?.language).toBe("zh-CN");
    expect(useAppStore().locale).toBe("zh-CN");
    expect(i18n.global.locale.value).toBe("zh-CN");
  });

  it("does not write a language the profile already carries", async () => {
    const store = useAuthStore();
    store.user = create(UserSchema, { name: "users/1", language: "zh-CN" });

    await store.changeLanguage("zh-CN");

    expect(mocks.updateUser).not.toHaveBeenCalled();
    expect(useAppStore().locale).toBe("zh-CN");
  });

  it("sends rapid switches in the order they were made", async () => {
    const store = useAuthStore();
    store.user = create(UserSchema, { name: "users/1", language: "en-US" });

    const written: string[] = [];
    let releaseFirst: () => void = () => {};
    mocks.updateUser.mockImplementation((user: { language?: string }) => {
      written.push(user.language ?? "");
      if (written.length === 1) {
        return new Promise<void>((resolve) => {
          releaseFirst = resolve;
        });
      }
      return Promise.resolve();
    });

    const first = store.changeLanguage("zh-CN");
    const second = store.changeLanguage("en-US");
    await Promise.resolve();
    // The second write waits for the first: fired concurrently, the first could
    // land last and leave the profile in a language the user has already left.
    expect(written).toEqual(["zh-CN"]);

    releaseFirst();
    await Promise.all([first, second]);

    expect(written).toEqual(["zh-CN", "en-US"]);
    expect(store.user?.language).toBe("en-US");
  });

  it("retries a failed write when an answer is about to depend on it", async () => {
    const store = useAuthStore();
    store.user = create(UserSchema, { name: "users/1", language: "en-US" });
    mocks.updateUser.mockRejectedValueOnce(new Error("network"));

    await store.changeLanguage("zh-CN");
    expect(store.user?.language).toBe("en-US");

    // Explain SQL reads the stored language, so the retry happens before it.
    await store.ensureLanguagePersisted();

    expect(mocks.updateUser).toHaveBeenCalledTimes(2);
    expect(store.user?.language).toBe("zh-CN");
  });

  it("writes nothing before an answer when the profile is already in step", async () => {
    const store = useAuthStore();
    store.user = create(UserSchema, { name: "users/1", language: "en-US" });

    await store.ensureLanguagePersisted();

    expect(mocks.updateUser).not.toHaveBeenCalled();
  });

  it("switches the UI before there is a session to write to", async () => {
    const store = useAuthStore();

    await store.changeLanguage("zh-CN");

    // The login page has a language menu and no profile to save it to.
    expect(useAppStore().locale).toBe("zh-CN");
    expect(mocks.updateUser).not.toHaveBeenCalled();
  });

  it("keeps the chosen language when the write fails", async () => {
    const store = useAuthStore();
    store.user = create(UserSchema, { name: "users/1", language: "en-US" });
    mocks.updateUser.mockRejectedValue(new Error("network"));

    await store.changeLanguage("zh-CN");

    // A preference that could not be saved must not undo the UI the user chose.
    expect(useAppStore().locale).toBe("zh-CN");
    expect(store.user?.language).toBe("en-US");
  });

  it("adopts the language stored on the profile, which wins over this browser", async () => {
    mocks.getCurrentUser.mockResolvedValue(
      create(UserSchema, { name: "users/1", language: "zh-CN" })
    );

    const store = useAuthStore();
    await store.fetchCurrentUser();

    expect(useAppStore().locale).toBe("zh-CN");
    expect(mocks.updateUser).not.toHaveBeenCalled();
  });

  it("leaves a stored language this SPA does not ship alone", async () => {
    mocks.getCurrentUser.mockResolvedValue(
      create(UserSchema, { name: "users/1", language: "ja-JP" })
    );

    const store = useAuthStore();
    await store.fetchCurrentUser();

    // Overwriting it with this browser's locale would discard the user's choice
    // for nothing: this interface cannot render ja-JP either way.
    expect(mocks.updateUser).not.toHaveBeenCalled();
    expect(store.user?.language).toBe("ja-JP");
    expect(useAppStore().locale).toBe("en-US");
  });

  it("fills an unset profile with the locale this browser already reads", async () => {
    useAppStore().setLocale("zh-CN");
    mocks.getCurrentUser.mockResolvedValue(
      create(UserSchema, { name: "users/1" })
    );
    const store = useAuthStore();
    await store.fetchCurrentUser();

    // Otherwise the UI would be Chinese while Explain SQL answered in English.
    expect(mocks.updateUser).toHaveBeenCalledWith(
      { name: "users/1", language: "zh-CN" },
      ["language"]
    );
    expect(store.user?.language).toBe("zh-CN");
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
