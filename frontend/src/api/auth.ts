import { create } from "@bufbuild/protobuf";
import { LoginRequestSchema } from "@/types/proto-es/v1/auth_service_pb";
import { authClient } from "./client";

/**
 * The OAuth2 half of a login request. `state` is the nonce the server issued
 * through `createSSOState` and consumes here; `codeVerifier` is the PKCE verifier
 * matching the challenge the browser sent to the provider.
 */
export interface SSOLoginContext {
  idpName: string;
  code: string;
  state: string;
  codeVerifier: string;
}

export async function login(
  email: string,
  password: string,
  idp?: SSOLoginContext
) {
  const request = create(LoginRequestSchema, {
    email,
    password,
    web: true,
    ...(idp
      ? {
          idpName: idp.idpName,
          idpContext: {
            context: {
              case: "oauth2Context" as const,
              value: {
                code: idp.code,
                state: idp.state,
                codeVerifier: idp.codeVerifier,
              },
            },
          },
        }
      : {}),
  });
  return await authClient.login(request);
}

/**
 * Issues the one-time nonce a login flow is bound to. It prevents an attacker
 * from completing an authorization-code exchange in someone else's browser: the
 * server rejects a login whose nonce it did not issue, or already consumed.
 */
export async function createSSOState(): Promise<string> {
  // The RPC takes google.protobuf.Empty, so there is no request message to build.
  const response = await authClient.createSSOState({});
  return response.state;
}

export async function logout() {
  return await authClient.logout({});
}

/**
 * Renews the session from the refresh cookie. The transport calls it when a
 * request is refused with `Unauthenticated`; it needs no arguments because both
 * credentials travel in HttpOnly cookies.
 */
export async function refresh(): Promise<void> {
  await authClient.refresh({});
}
