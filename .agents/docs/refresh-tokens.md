# Refresh tokens — Reference

> Status: **implemented**. Maintenance reference for `backend/api/oauth`, `backend/api/v1/auth_service_refresh.go`, `backend/store/oauth_refresh_token.go`, `backend/store/web_refresh_token.go`. Related: `docs/security-posture.md:28-29`, `docs/mcp.md`, `AGENTS.md` (the OAuth pending-state/refresh-token bullet) — that posture text is not repeated here.

## What it is

Two independent single-use rotating flows: the MCP OAuth 2.1 server at `/oauth/*` and the SPA web session. Both store only the SHA-256 hex digest of an opaque token and rotate with `DELETE … RETURNING`, so two concurrent refreshes race and exactly one wins; a replay is `invalid_grant` (MCP) or `Unauthenticated` (web). Refresh tokens live 30 days (`backend/api/auth/auth.go:41`) and access tokens keep their seven-day lifetime (`:36`). The schema is additive; no legacy-token fallback exists.

## Decisions

- **A refresh token is a digest, never a secret at rest** — a database read hands out no usable credential, and a 256-bit random string needs no slow KDF, so a lookup stays one indexed read.
- **Web sessions are keyed by `user_id`** — an account is its principal id; the administrative, mutable email address would let a change orphan or adopt a session.
- **Every refresh re-validates the principal from persisted state** — a deactivated account, and a grant issued before the last password change, are refused, exactly as `TokenAuthenticator.Resolve` refuses an access token. The rotated row therefore carries the original issue time forward, so the rule survives rotation.
- **Web rotation keeps an absolute session lifetime; the MCP grant slides** — the web token inherits the original issue time and deadline, so refreshing cannot extend a session indefinitely; an MCP client in use has no natural point to re-authorize, so its grant's expiry moves on activity.
- **Revocation covers what could replace the credential** — `Logout` deletes the web row its cookie names, and for an MCP token (whose `cid` claim names its registration) every grant that client holds for that user, so revoking one token ends the connection rather than leaving it one refresh from a new pair.
- **The MCP grant set is validated but not persisted** — one client shape (public, authorization code + PKCE), so `grant_types` is only checked against the supported set at registration (`backend/api/oauth/register.go:176`) and advertised back in the metadata (`backend/api/oauth/metadata.go:152`).

## Persisted state

| Table | Record | Pinned to | Reclaimed |
| --- | --- | --- | --- |
| `oauth_refresh_token` | `token_hash` (unique), `client_id`, `user_id → principal(id)`, `resource`, `scope`, `expires_at`, `created_at` | the `<external_url>/mcp` resource it was consented for, so moving the external URL invalidates it | expiry prune, `backend/runner/maintenance/maintenance.go:96` |
| `web_refresh_token` | `token_hash` (unique), `user_id → principal(id)`, `expires_at`, `created_at` | nothing else — single-tenant workspace | expiry prune, `maintenance.go:101` |

## Invariants

1. **MCP refresh checks before it consumes, then re-checks the live principal** (`backend/api/oauth/token.go:147`–`:222`): client binding, row presence, expiry, `resource` equals the current endpoint, and a requested scope identical to the consented one all run first, so a client-correctable refusal does not burn the credential; only then does the atomic consume decide the winner. A principal that no longer exists, is deactivated, or has a password change newer than the grant ends the grant.
2. **Web refresh consumes first, then re-reads the principal** (`backend/api/v1/auth_service_refresh.go:63`), refuses deactivation and a pre-password-change session, and re-stamps the rotated token with the original `IssuedAt` **and** `ExpiresAt`; the MCP rotation also keeps `IssuedAt` while re-stamping its expiry from now (`token.go:270`), which is what lets the password-change cutoff still order it.
3. **Refusal codes are part of the contract:** replay/unknown/other-client → `invalid_grant`; a differing requested scope → `invalid_scope`; an expired grant is refused **and** its row dropped in the same path (`token.go:172`); web refusals are `Unauthenticated`.

## Where things live

- DDL: `backend/migrator/migration/0.1/0014##refresh_tokens.sql`, mirrored at `backend/migrator/migration/LATEST.sql:637`, `:659`; query shapes and digest-only rule in `backend/store/oauth_refresh_token.go`, `backend/store/web_refresh_token.go`, guarded by `backend/store/refresh_token_test.go`.
- MCP flow: `backend/api/oauth/token.go` (`TokenHandler`, `exchangeRefreshToken`, `storeRefreshToken`) and `backend/api/oauth/metadata.go`; hermetic tests `backend/api/oauth/*_test.go`. Web flow: `backend/api/v1/auth_service_refresh.go` (`setWebRefreshCookie`, `storeWebSession`, `Refresh`), with `Login`/`Logout` in `backend/api/v1/auth_service.go`; cookie/hash tests `backend/api/auth/refresh_token_test.go`.
- SPA: `frontend/src/api/session.ts` (`sessionInterceptor`, single-flight refresh), `frontend/src/api/session.test.ts`, wired in `frontend/src/main.ts`.
- Integration: `backend/test/integration/runner/refresh_token_service_test.go`, `mcp_service_test.go` (full grant, rotation, replay refusal, web refresh over the real server).
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`, build; `make test-integration-smoke`; SPA `biome:check`, `type-check`, `test run`.
- Out of scope: the `mxd` CLI's device-login token stays a plain access token — no cookie jar, and re-running `mxd login` is the ordinary recovery.
