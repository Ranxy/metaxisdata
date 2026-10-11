import {
  type IdentityProvider,
  IdentityProviderType,
  type OAuth2IdentityProviderConfig,
} from "@/types/proto-es/v1/identity_provider_service_pb";

/**
 * The client half of the OAuth2 authorization-code flow.
 *
 * Everything here is a pure function over values the caller supplies, so it can
 * be unit tested without a DOM or a server. The one thing it does reach outside
 * for is `sessionStorage` and `window.location`, both injected through the
 * callbacks a caller can omit in a test.
 *
 * The nonce has two purposes and one value. The server issues it through
 * `CreateSSOState` and consumes it when the login lands, which is what binds the
 * callback to this browser (login CSRF). The same value travels to the provider
 * as the `state` parameter, so the provider's answer can be matched to the record
 * this client stored. Using one nonce for both keeps them from disagreeing.
 */

/** How long a stored flow may be resumed before it is treated as abandoned. */
export const SSO_STATE_TTL_MS = 10 * 60 * 1000;

const SSO_STATE_PREFIX = "metaxisdata.sso.state.";

export type SSOFlowMode = "login" | "test";

export interface SSOStateRecord {
  /** The server-issued nonce, which is also the OAuth `state` parameter. */
  token: string;
  /** The identity provider resource name, `idps/{idp}`. */
  idpName: string;
  mode: SSOFlowMode;
  /** Where to land after a successful login. Absent for a test. */
  redirect?: string;
  /** The PKCE verifier matching the challenge sent to the provider. */
  codeVerifier: string;
  createdAt: number;
}

/**
 * Only a same-origin absolute path is a redirect target. Anything else — an
 * absolute URL, a protocol-relative `//host` path, or a value the provider
 * echoed back — would turn the callback into an open redirect.
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
  return redirect;
}

/**
 * The redirect URI the provider sends the browser back to. It must be the value
 * the server builds for the token exchange, which is derived from the workspace
 * `external_url`; falling back to the current origin keeps an instance that
 * serves the SPA and the API from one address working without configuration.
 */
export function ssoCallbackUrl(externalUrl?: string | null): string {
  const base = (externalUrl ?? "").replace(/\/+$/, "");
  return `${base || window.location.origin}/oauth/callback`;
}

function base64Url(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return btoa(binary)
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}

function randomToken(): string {
  return base64Url(crypto.getRandomValues(new Uint8Array(32)));
}

/**
 * A PKCE verifier and its S256 challenge. It returns null when the platform has
 * no WebCrypto — a non-secure context — because sending the flow without PKCE
 * would silently downgrade it rather than fail.
 */
export async function createPKCEPair(): Promise<{
  verifier: string;
  challenge: string;
} | null> {
  if (!globalThis.crypto?.subtle) {
    return null;
  }
  const verifier = randomToken();
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(verifier)
  );
  return { verifier, challenge: base64Url(new Uint8Array(digest)) };
}

function oauth2Config(
  provider: IdentityProvider
): OAuth2IdentityProviderConfig | undefined {
  const config = provider.config?.config;
  if (config?.case !== "oauth2Config") {
    return undefined;
  }
  return config.value;
}

function isAbsoluteHttpUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

/**
 * The provider's authorization URL. It returns null when the provider cannot
 * start a flow — a non-OAuth2 type, a missing config, or an authorization URL
 * that is not absolute — so the caller reports a broken configuration instead of
 * navigating somewhere unexpected.
 */
export function buildAuthorizeUrl(
  provider: IdentityProvider,
  stateToken: string,
  redirectUri: string,
  codeChallenge: string
): string | null {
  if (provider.type !== IdentityProviderType.OAUTH2) {
    return null;
  }
  const config = oauth2Config(provider);
  const authUrl = config?.authUrl.trim() ?? "";
  if (!config || !isAbsoluteHttpUrl(authUrl)) {
    return null;
  }
  const url = new URL(authUrl);
  url.searchParams.set("client_id", config.clientId);
  url.searchParams.set("response_type", "code");
  url.searchParams.set("redirect_uri", redirectUri);
  url.searchParams.set("state", stateToken);
  if (config.scopes.length > 0) {
    url.searchParams.set("scope", config.scopes.join(" "));
  }
  // PKCE is always sent: the server already accepts a verifier, and a
  // confidential client that also proves possession of the code is strictly
  // better than one that only presents its secret.
  url.searchParams.set("code_challenge", codeChallenge);
  url.searchParams.set("code_challenge_method", "S256");
  return url.toString();
}

function storeSSOState(
  record: SSOStateRecord,
  storage: Pick<Storage, "setItem"> = sessionStorage
): void {
  try {
    storage.setItem(SSO_STATE_PREFIX + record.token, JSON.stringify(record));
  } catch {
    // A browser that refuses session storage (privacy mode, a full quota) still
    // completes the flow; only the redirect target and the mode are lost, and
    // the callback then reports an unknown state rather than signing anyone in
    // twice.
  }
}

/**
 * Resumes a flow by its nonce. The record is removed whether or not it is still
 * usable, so a replayed callback finds nothing: one authorization code is
 * exchanged once.
 */
export function consumeSSOState(
  token: string,
  storage: Pick<Storage, "getItem" | "removeItem"> = sessionStorage
): SSOStateRecord | null {
  if (!token) {
    return null;
  }
  const key = SSO_STATE_PREFIX + token;
  let raw: string | null = null;
  try {
    raw = storage.getItem(key);
    storage.removeItem(key);
  } catch {
    return null;
  }
  if (!raw) {
    return null;
  }
  let record: SSOStateRecord;
  try {
    record = JSON.parse(raw) as SSOStateRecord;
  } catch {
    return null;
  }
  if (record?.token !== token) {
    return null;
  }
  if (Date.now() - (record.createdAt ?? 0) > SSO_STATE_TTL_MS) {
    return null;
  }
  return record;
}

export interface BeginSSOOptions {
  /** The nonce from `AuthService.CreateSSOState`. */
  serverState: string;
  mode?: SSOFlowMode;
  redirect?: string;
  externalUrl?: string | null;
  /** Injected by tests; defaults to the real browser location. */
  navigate?: (url: string) => void;
}

/**
 * Starts the flow and navigates the browser to the provider. It reports false
 * instead of navigating when the provider is misconfigured or PKCE is
 * unavailable; the caller turns that into an error message.
 *
 * The record is stored only after the URL is known to be good, so a failed start
 * leaves nothing behind to be resumed later.
 */
export async function beginSSOLogin(
  provider: IdentityProvider,
  options: BeginSSOOptions
): Promise<boolean> {
  const pkce = await createPKCEPair();
  if (!pkce) {
    return false;
  }
  const url = buildAuthorizeUrl(
    provider,
    options.serverState,
    ssoCallbackUrl(options.externalUrl),
    pkce.challenge
  );
  if (!url) {
    return false;
  }
  storeSSOState({
    token: options.serverState,
    idpName: provider.name,
    mode: options.mode ?? "login",
    redirect: sanitizeRedirect(options.redirect),
    codeVerifier: pkce.verifier,
    createdAt: Date.now(),
  });
  (options.navigate ?? ((target: string) => window.location.assign(target)))(
    url
  );
  return true;
}
