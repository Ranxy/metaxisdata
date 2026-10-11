import { createHash } from "node:crypto";
import { create } from "@bufbuild/protobuf";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  beginSSOLogin,
  buildAuthorizeUrl,
  consumeSSOState,
  createPKCEPair,
  SSO_STATE_TTL_MS,
  sanitizeRedirect,
  ssoCallbackUrl,
} from "@/lib/oauth";
import {
  IdentityProviderSchema,
  IdentityProviderType,
  OAuth2AuthStyle,
} from "@/types/proto-es/v1/identity_provider_service_pb";

function oauth2Provider(
  overrides: {
    name?: string;
    authUrl?: string;
    clientId?: string;
    scopes?: string[];
  } = {}
) {
  return create(IdentityProviderSchema, {
    name: overrides.name ?? "idps/acme",
    title: "Acme",
    type: IdentityProviderType.OAUTH2,
    config: {
      config: {
        case: "oauth2Config",
        value: {
          authUrl: overrides.authUrl ?? "https://idp.example.com/authorize",
          tokenUrl: "https://idp.example.com/token",
          userInfoUrl: "https://idp.example.com/userinfo",
          clientId: overrides.clientId ?? "client-id",
          scopes: overrides.scopes ?? ["openid", "email"],
          authStyle: OAuth2AuthStyle.IN_PARAMS,
        },
      },
    },
  });
}

/** A storage stand-in so a test never depends on jsdom's session state. */
function memoryStorage(initial: Record<string, string> = {}) {
  const entries = new Map(Object.entries(initial));
  return {
    getItem: (key: string) => entries.get(key) ?? null,
    setItem: (key: string, value: string) => {
      entries.set(key, value);
    },
    removeItem: (key: string) => {
      entries.delete(key);
    },
    size: () => entries.size,
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("sanitizeRedirect", () => {
  it("accepts a same-origin absolute path", () => {
    expect(sanitizeRedirect("/instances/3")).toBe("/instances/3");
  });

  it.each([
    ["an absolute URL", "https://evil.example.com/"],
    ["a protocol-relative path", "//evil.example.com/"],
    ["a relative path", "instances"],
    ["an empty value", ""],
    ["nothing", undefined],
    ["nothing at all", null],
  ])("rejects %s", (_label, value) => {
    expect(sanitizeRedirect(value)).toBeUndefined();
  });
});

describe("ssoCallbackUrl", () => {
  it("uses the workspace external URL when one is configured", () => {
    expect(ssoCallbackUrl("https://metaxis.example.com")).toBe(
      "https://metaxis.example.com/oauth/callback"
    );
  });

  it("strips trailing slashes so the path is not doubled", () => {
    expect(ssoCallbackUrl("https://metaxis.example.com///")).toBe(
      "https://metaxis.example.com/oauth/callback"
    );
  });

  it("falls back to the current origin", () => {
    expect(ssoCallbackUrl("")).toBe(`${window.location.origin}/oauth/callback`);
  });
});

describe("createPKCEPair", () => {
  it("derives the S256 challenge from the verifier", async () => {
    const pair = await createPKCEPair();
    expect(pair).not.toBeNull();
    const expected = createHash("sha256")
      .update(pair?.verifier ?? "")
      .digest("base64url");
    expect(pair?.challenge).toBe(expected);
    // A verifier must carry at least 32 bytes of entropy; base64url of 32 bytes
    // is 43 characters, and the padding is stripped.
    expect(pair?.verifier).toMatch(/^[A-Za-z0-9_-]{43,}$/);
  });

  it("refuses to start a flow without WebCrypto instead of dropping PKCE", async () => {
    vi.stubGlobal("crypto", {});
    expect(await createPKCEPair()).toBeNull();
  });
});

describe("buildAuthorizeUrl", () => {
  const redirectUri = "https://metaxis.example.com/oauth/callback";

  it("builds an authorization request with PKCE and the scopes", () => {
    const url = new URL(
      buildAuthorizeUrl(oauth2Provider(), "nonce", redirectUri, "challenge") ??
        ""
    );
    expect(url.origin + url.pathname).toBe("https://idp.example.com/authorize");
    expect(url.searchParams.get("client_id")).toBe("client-id");
    expect(url.searchParams.get("response_type")).toBe("code");
    expect(url.searchParams.get("redirect_uri")).toBe(redirectUri);
    expect(url.searchParams.get("state")).toBe("nonce");
    expect(url.searchParams.get("scope")).toBe("openid email");
    expect(url.searchParams.get("code_challenge")).toBe("challenge");
    expect(url.searchParams.get("code_challenge_method")).toBe("S256");
  });

  it("omits the scope parameter when the provider declares none", () => {
    const url = new URL(
      buildAuthorizeUrl(
        oauth2Provider({ scopes: [] }),
        "nonce",
        redirectUri,
        "challenge"
      ) ?? ""
    );
    expect(url.searchParams.has("scope")).toBe(false);
  });

  it("keeps an authorization endpoint that already carries a query", () => {
    const url = new URL(
      buildAuthorizeUrl(
        oauth2Provider({
          authUrl: "https://idp.example.com/authorize?tenant=a",
        }),
        "nonce",
        redirectUri,
        "challenge"
      ) ?? ""
    );
    expect(url.searchParams.get("tenant")).toBe("a");
    expect(url.searchParams.get("state")).toBe("nonce");
  });

  it.each([
    [
      "a non-OAuth2 provider",
      create(IdentityProviderSchema, { name: "idps/x" }),
    ],
    [
      "a provider with no config",
      create(IdentityProviderSchema, {
        name: "idps/x",
        type: IdentityProviderType.OAUTH2,
      }),
    ],
    ["a relative authorization URL", oauth2Provider({ authUrl: "/authorize" })],
    ["an empty authorization URL", oauth2Provider({ authUrl: "" })],
  ])("reports a broken configuration for %s", (_label, provider) => {
    expect(
      buildAuthorizeUrl(provider, "nonce", redirectUri, "challenge")
    ).toBeNull();
  });
});

describe("consumeSSOState", () => {
  const createdAt = Date.now();
  const record = {
    token: "nonce",
    idpName: "idps/acme",
    mode: "login" as const,
    redirect: "/instances",
    codeVerifier: "verifier",
    createdAt,
  };

  it("returns a fresh record and removes it in the same call", () => {
    const storage = memoryStorage({
      "metaxisdata.sso.state.nonce": JSON.stringify(record),
    });
    vi.spyOn(Date, "now").mockReturnValue(createdAt + SSO_STATE_TTL_MS);

    expect(consumeSSOState("nonce", storage)).toEqual(record);
    expect(storage.size()).toBe(0);
  });

  it("treats a record past its TTL as abandoned and still removes it", () => {
    const storage = memoryStorage({
      "metaxisdata.sso.state.nonce": JSON.stringify(record),
    });
    vi.spyOn(Date, "now").mockReturnValue(createdAt + SSO_STATE_TTL_MS + 1);

    expect(consumeSSOState("nonce", storage)).toBeNull();
    expect(storage.size()).toBe(0);
  });

  it("rejects a replayed callback because the first consume removed it", () => {
    const storage = memoryStorage({
      "metaxisdata.sso.state.nonce": JSON.stringify(record),
    });
    expect(consumeSSOState("nonce", storage)).not.toBeNull();
    expect(consumeSSOState("nonce", storage)).toBeNull();
  });

  it.each([
    ["an unknown nonce", "other", undefined],
    ["an empty nonce", "", undefined],
    ["a malformed record", "nonce", "{not json"],
    [
      "a record whose token does not match its key",
      "nonce",
      JSON.stringify({ ...record, token: "different" }),
    ],
  ])("rejects %s", (_label, token, stored) => {
    const storage = memoryStorage(
      stored === undefined ? {} : { "metaxisdata.sso.state.nonce": stored }
    );
    expect(consumeSSOState(token, storage)).toBeNull();
  });

  it("reports nothing when the storage refuses to be read", () => {
    const storage = {
      getItem: () => {
        throw new Error("denied");
      },
      removeItem: () => {},
    };
    expect(consumeSSOState("nonce", storage)).toBeNull();
  });
});

describe("beginSSOLogin", () => {
  it("stores the flow and navigates to the provider", async () => {
    const navigate = vi.fn();
    const storage = memoryStorage();
    vi.stubGlobal("sessionStorage", storage);

    const started = await beginSSOLogin(oauth2Provider(), {
      serverState: "nonce",
      mode: "login",
      redirect: "/instances",
      externalUrl: "https://metaxis.example.com",
      navigate,
    });

    expect(started).toBe(true);
    const stored = JSON.parse(
      storage.getItem("metaxisdata.sso.state.nonce") ?? "{}"
    );
    expect(stored.idpName).toBe("idps/acme");
    expect(stored.mode).toBe("login");
    expect(stored.redirect).toBe("/instances");
    expect(stored.codeVerifier).toMatch(/^[A-Za-z0-9_-]{43,}$/);

    const target = new URL(navigate.mock.calls[0]?.[0] ?? "");
    expect(target.searchParams.get("state")).toBe("nonce");
    expect(target.searchParams.get("code_challenge_method")).toBe("S256");
    expect(target.searchParams.get("redirect_uri")).toBe(
      "https://metaxis.example.com/oauth/callback"
    );
  });

  it("drops an off-site redirect target instead of storing it", async () => {
    const storage = memoryStorage();
    vi.stubGlobal("sessionStorage", storage);

    await beginSSOLogin(oauth2Provider(), {
      serverState: "nonce",
      redirect: "https://evil.example.com/",
      navigate: vi.fn(),
    });

    const stored = JSON.parse(
      storage.getItem("metaxisdata.sso.state.nonce") ?? "{}"
    );
    expect(stored.redirect).toBeUndefined();
  });

  it("records a test flow so the callback tests instead of signing in", async () => {
    const storage = memoryStorage();
    vi.stubGlobal("sessionStorage", storage);

    await beginSSOLogin(oauth2Provider(), {
      serverState: "nonce",
      mode: "test",
      navigate: vi.fn(),
    });

    const stored = JSON.parse(
      storage.getItem("metaxisdata.sso.state.nonce") ?? "{}"
    );
    expect(stored.mode).toBe("test");
    expect(stored.redirect).toBeUndefined();
  });

  it("stores nothing when the provider cannot be used", async () => {
    const storage = memoryStorage();
    vi.stubGlobal("sessionStorage", storage);
    const navigate = vi.fn();

    const started = await beginSSOLogin(
      create(IdentityProviderSchema, { name: "idps/broken" }),
      { serverState: "nonce", navigate }
    );

    expect(started).toBe(false);
    expect(navigate).not.toHaveBeenCalled();
    expect(storage.size()).toBe(0);
  });

  it("refuses to navigate when WebCrypto is unavailable", async () => {
    vi.stubGlobal("crypto", {});
    const navigate = vi.fn();

    expect(
      await beginSSOLogin(oauth2Provider(), { serverState: "nonce", navigate })
    ).toBe(false);
    expect(navigate).not.toHaveBeenCalled();
  });

  it("survives a storage that refuses to persist", async () => {
    vi.stubGlobal("sessionStorage", {
      getItem: () => null,
      setItem: () => {
        throw new Error("quota");
      },
      removeItem: () => {},
    });
    const navigate = vi.fn();

    expect(
      await beginSSOLogin(oauth2Provider(), { serverState: "nonce", navigate })
    ).toBe(true);
    expect(navigate).toHaveBeenCalledOnce();
  });
});

// The two parameters that default to the real browser. They are exercised here
// rather than through an injected stand-in because a caller that leaves one out
// must still work.
describe("browser defaults", () => {
  it("resumes a flow from session storage", () => {
    const record = {
      token: "nonce",
      idpName: "idps/acme",
      mode: "login" as const,
      codeVerifier: "verifier",
      createdAt: Date.now(),
    };
    window.sessionStorage.setItem(
      "metaxisdata.sso.state.nonce",
      JSON.stringify(record)
    );

    expect(consumeSSOState("nonce")).toEqual(record);
    expect(
      window.sessionStorage.getItem("metaxisdata.sso.state.nonce")
    ).toBeNull();
  });

  it("assigns the real location when no navigator is injected", async () => {
    const assign = vi.fn();
    // jsdom's location is read-only, so it is replaced for the duration of the
    // call and put back afterwards.
    const original = Object.getOwnPropertyDescriptor(window, "location");
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...window.location, assign },
    });
    try {
      expect(
        await beginSSOLogin(oauth2Provider(), { serverState: "nonce" })
      ).toBe(true);
      expect(assign).toHaveBeenCalledOnce();
      expect(String(assign.mock.calls[0]?.[0])).toContain("state=nonce");
    } finally {
      if (original) {
        Object.defineProperty(window, "location", original);
      }
    }
  });
});
