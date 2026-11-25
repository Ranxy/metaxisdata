import { create } from "@bufbuild/protobuf";
import { createPinia, setActivePinia } from "pinia";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { refresh } from "@/api/auth";
import { setSessionRefresher, setUnauthenticatedHandler } from "@/api/session";
import router, { handleSessionExpired } from "@/router";
import { useAuthStore } from "@/store/modules/auth";
import { UserSchema } from "@/types/proto-es/v1/user_service_pb";

// The scenario these tests cover starts from a full page load: the browser has
// a session cookie, the SPA's store is empty. Resolving that session is what the
// transport interceptor (refresh on a refused request) and the router guard do
// together, so this exercises the real client rather than mocking the API.

const currentUser = {
  name: "users/1",
  email: "dev@example.com",
  permissions: ["metaxisdata.instances.list"],
};

function ok(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json" },
  });
}

function unauthenticated() {
  return new Response(JSON.stringify({ code: "unauthenticated" }), {
    status: 401,
    headers: { "content-type": "application/json" },
  });
}

// The fake server's session state: the access token can be expired while the
// refresh token is still usable, which is the routine case a browser reload hits.
let accessTokenAlive = true;
let refreshTokenAlive = true;
let fetchMock: ReturnType<typeof vi.fn>;

describe("a full load of the sign-in page", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setActivePinia(createPinia());
    accessTokenAlive = true;
    refreshTokenAlive = true;
    fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/metaxisdata.v1.UserService/GetCurrentUser")) {
        return accessTokenAlive ? ok(currentUser) : unauthenticated();
      }
      if (url.endsWith("/metaxisdata.v1.AuthService/Refresh")) {
        if (!refreshTokenAlive) {
          return unauthenticated();
        }
        accessTokenAlive = true;
        return ok({});
      }
      if (url.endsWith("/metaxisdata.v1.UserService/UpdateUser")) {
        return ok(currentUser);
      }
      return unauthenticated();
    });
    vi.stubGlobal("fetch", fetchMock);
    setUnauthenticatedHandler(handleSessionExpired);
    setSessionRefresher(refresh);
  });

  it("continues to the saved target when the session is valid", async () => {
    // The reported URL: a signed-in caller following a saved sign-in link used
    // to stay on the form because the guard never resolved the session there.
    await router.push(
      "/login?redirect=/metadata/test-pg-1/dataforge/public/llm_provider_profile?metaType=4"
    );

    expect(router.currentRoute.value.name).toBe("MetadataDetail");
    expect(router.currentRoute.value.query.metaType).toBe("4");
  }, 10_000);

  it("renews an expired access token before continuing to the target", async () => {
    accessTokenAlive = false;

    await router.push("/login?redirect=/settings/general");

    const authStore = useAuthStore();
    expect(authStore.isAuthenticated).toBe(true);
    expect(authStore.sessionExpired).toBe(false);
    expect(router.currentRoute.value.name).toBe("GeneralSettings");
  });

  it("keeps a visitor with no session on the form, with the target it wanted", async () => {
    accessTokenAlive = false;
    refreshTokenAlive = false;

    await router.push("/login?redirect=/settings/general");

    expect(router.currentRoute.value.name).toBe("Login");
    expect(router.currentRoute.value.query).toMatchObject({
      redirect: "/settings/general",
    });
    // The probe found no cookie, it did not expire one.
    expect(router.currentRoute.value.query).not.toHaveProperty("expired");
    expect(useAuthStore().sessionExpired).toBe(false);
  });

  it("does not claim an expiry when a guarded page is refused before sign-in", async () => {
    accessTokenAlive = false;
    refreshTokenAlive = false;

    await router.push("/databases");

    expect(router.currentRoute.value.name).toBe("Login");
    expect(router.currentRoute.value.query).toMatchObject({
      redirect: "/databases",
    });
    expect(router.currentRoute.value.query).not.toHaveProperty("expired");
  });

  // The login response deliberately carries the user without permissions; the
  // first guarded navigation is what loads the permission-bearing profile, once.
  it("loads the profile once after a login before the first page", async () => {
    const authStore = useAuthStore();
    authStore.user = create(UserSchema, { name: "users/1" });
    authStore.isAuthenticated = true;
    expect(authStore.permissions).toEqual([]);

    await router.push("/");
    expect(authStore.permissions).toEqual(["metaxisdata.instances.list"]);

    await router.push("/databases");
    expect(
      fetchMock.mock.calls.filter(([input]) =>
        String(input).endsWith("/metaxisdata.v1.UserService/GetCurrentUser")
      )
    ).toHaveLength(1);
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
    accessTokenAlive = false;
    refreshTokenAlive = false;
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
