# Refresh tokens — Reference

> Status: **implemented**. Maintenance reference for `backend/api/oauth` (grant rotation and the CLI session), `backend/api/v1/auth_service_refresh.go` (web session), `backend/api/v1/auth_service_exchange_refresh_token.go` (CLI session), `backend/api/v1/connected_app_service.go`, `backend/store/oauth_grant.go`, `backend/store/web_refresh_token.go`. Related: `docs/security-posture.md`, `docs/mcp.md`, `.agents/docs/connected-apps.md`, `.agents/docs/cli-mxd.md`.

## What it is

Two rotation **store shapes** behind three flows:

| Flow | Stored rotation state | Rotation | Lifetime | Refresh transport |
| --- | --- | --- | --- | --- |
| MCP OAuth grant | `oauth_refresh_token` row, `kind = 'mcp'` | in-place `UPDATE … RETURNING id` | 30 days, **slides** | `POST /oauth/token` (`grant_type=refresh_token`) |
| CLI session | the same row, `kind = 'cli'` | the same in-place update | 30 days, **slides** | `AuthService.ExchangeRefreshToken` (token in the body) |
| web session | `web_refresh_token` row | `DELETE … RETURNING` + insert | 30 days, **absolute** | `AuthService.Refresh` (HttpOnly cookie) |

The CLI is the same kind of **connected app** as an MCP client: one grant row that appears on Connected apps, carries the device facts of the login, and is revocable by the user or an administrator. A CLI access token is a user-API token that carries the client and grant ids (`cid` / `gid`), so `TokenAuthenticator.Resolve` applies the same grant-liveness check an MCP token gets: revoking the connection refuses outstanding CLI tokens (immediately on the revoking replica, within the 30-second cache window elsewhere) and its refresh credential mints nothing. The CLI refreshes through ConnectRPC rather than `/oauth/token` because the OAuth surface is switched off unless `mcp_enabled` and `external_url` are both usable, while the CLI must work in any workspace; the rotation and principal-recheck helpers are shared (`backend/api/oauth/grant.go`), so the two grant paths cannot drift.

Only the SHA-256 hex digest of an opaque token is stored, and rotation is single-use: two concurrent refreshes race and exactly one wins. Refresh credentials live 30 days; access tokens keep their seven-day lifetime. The schema is additive.

## Decisions

- **A refresh token is a digest, never a secret at rest** — a database read hands out no usable credential, and a 256-bit random string needs no slow KDF, so a lookup stays one indexed read.
- **Grant rows rotate in place; web sessions are consume-and-insert** — the grant row's id is the connected app's stable handle (`gid`, `connectedApps/{id}`), so it must survive rotation; the web session has no such handle, and consuming its row first is what makes rotation single-use there.
- **Grant lifetimes slide; the web session's does not** — a machine client (MCP or CLI) has no natural moment to re-authorize, so activity moves the expiry; a browser session does, so refreshing cannot extend it past its absolute deadline. `created_at` is carried across every rotation so the last-password-change rule still orders it.
- **The CLI's OAuth client is built-in, not dynamically registered** — `CLIClientID = "mxd"`, `CLIClientName = "Metaxisdata CLI"`, no redirect URIs, `token_endpoint_auth_method=none`. `EnsureCLIClient` inserts the row idempotently on every device-login exchange; the row exists so the connected-apps list can name the connection, while the grant row is the app itself.
- **A CLI grant is not bound to the MCP endpoint** — its `resource` and `scope` are empty, because its access token is addressed to the user API. `/oauth/token` therefore refuses to refresh it (`resource` mismatch), which is deliberate: `ExchangeRefreshToken` is its only refresh path.
- **The two audiences keep the surfaces apart** — an MCP token carries the MCP resource as `aud` and is refused on the user API; a CLI token carries the user-API audience and is refused at `/mcp`. Both carry `cid`/`gid` so revocation reaches tokens minted before it.
- **Device facts are display-only** — `client_version` and `request_ip` on a CLI grant let the management page say where the session was opened. They are not trusted and are not credentials.
- **Rotation re-validates the live principal** — a missing or deactivated account, or a grant issued before the user's last password change, ends the grant: the row is revoked (kept as a tombstone) and the refresh is refused. A mere read failure is not a verdict: the credential is already burned, so it fails the issuance without revoking a connection that may still be healthy. Both grant paths share this so they cannot disagree.
- **`Logout` ends what could replace the presented credential** — an MCP token names its registration, so every grant that client holds for the user ends; a CLI token names one session (one machine), so only its own grant ends, or signing out on one machine would sign the user out of every other one. The web session deletes the row its cookie names. Every path revokes the access token's `jti`.
- **The CLI refresh endpoint is anonymous, throttled and unaudited** — like `Refresh`, it spends one database round trip on caller-supplied input, so it shares the login request budget; it writes no audit row (the device login's create and approve are the audited events, as with the web session).
- **A replayed token is refused, not traced as a family** — like the MCP flow, there is no reuse-detection that revokes a rotated descendant. A stolen copy used first forces the real holder to sign in again, while the attacker keeps their rotated copy until the sliding expiry or a revocation from Connected apps. This is the accepted posture, not an open item.

## Persisted state

| Table | Record | Pinned to | Reclaimed |
| --- | --- | --- | --- |
| `oauth_refresh_token` (historical name; Go type `store.OAuthGrant`) | One connected app: `token_hash` (unique), `client_id`, `user_id → principal(id)`, `kind` (`mcp`/`cli`), `resource` (the `<external_url>/mcp` for an MCP grant, empty for a CLI session), `scope`, `expires_at`, `created_at` (first issuance, carried across rotations), `last_tool_call_at` (MCP tool-call attempts only, NULL until the first one), `client_version` / `request_ip` (CLI device facts, empty for MCP), `revoked_at` (soft revocation, terminal), `revoked_by` (acting principal; NULL when the system ended it on a failed principal re-check) | an MCP grant is pinned to the MCP resource it was consented for, so moving `external_url` makes it `REAUTH_REQUIRED`; a CLI grant has no such pin | expiry prune **7 days after `expires_at`**, `backend/runner/maintenance/maintenance.go` |
| `web_refresh_token` | `token_hash` (unique), `user_id → principal(id)`, `expires_at`, `created_at` | nothing else — single-tenant workspace | expiry prune, `maintenance.go` |

## Invariants

1. **The rotation gate is one atomic statement scoped to the client, with `revoked_at IS NULL` and `expires_at > now` in the predicate** (`RotateOAuthGrant`). A revocation that commits first wins over a rotation; the row id survives, so `gid` stays stable.
2. **A CLI refresh runs the row-decidable checks before rotating, then re-checks the live principal** (`RotateCLIGrant`): the row must exist, be `kind = 'cli'`, not expired and not revoked; only then does the atomic rotation decide the winner. A principal that is gone, deactivated or predates the last password change ends the grant explicitly (`revokeUnusableGrant`), because an in-place rotation can no longer end it by deleting the row.
3. **The web session consumes first, then re-reads the principal** (`AuthService.Refresh`), refuses deactivation and a pre-password-change session, and re-stamps the rotated token with the original `IssuedAt` **and** `ExpiresAt`; only a new login moves those.
4. **The access-token check is the grant's liveness, not the refresh credential's.** Any access token with `gid` is checked against `(id, user_id, client_id)` with `revoked_at IS NULL AND expires_at > NOW()` (`IsOAuthGrantLive`) behind the separate 30-second `state.GrantRevocationCache`. A missing row is not live; a read failure fails closed. MCP access tokens and CLI access tokens use the same claim and the same check; `/mcp` additionally refuses a token without `gid`.
5. **Refusal codes are part of the contract:** MCP replay/unknown/other-client/expired/revoked → `invalid_grant`; a differing requested scope → `invalid_scope`; an expired grant is refused and its row is **kept** (maintenance reclaims it, and the page shows `EXPIRED`); web and CLI refusals are `Unauthenticated` (the CLI exits 2 and asks for a new device login).
6. **`Logout` revokes, it does not delete.** An MCP token's `Logout` conditionally updates every grant of `(user, client)`; a CLI token's `Logout` updates only the grant its `gid` names; a web `Logout` deletes its cookie's row. A repeated logout is an idempotent no-op; the ledger records it.
7. **The CLI's stored pair is written by one owner.** `cli/client/session.go` holds the only path that rotates and persists: in-process refreshes are serialized, a waiting caller re-reads the rotated token instead of consuming it, and a refusal is believed only after the credentials file is re-read (another process may have won the race and written its pair). A definitive refusal leaves the file alone rather than risk clobbering a concurrent writer.
8. **Upgrade is additive and rolling-safe.** The columns default (`kind='mcp'`), old replicas ignore them and write the default, and a pre-upgrade CLI credential has no refresh token — it keeps working until the access token expires and then signs in once.

## Where things live

- Shared grant rotation: `backend/api/oauth/grant.go` (`rotateGrantToken`, `recheckGrantPrincipal`, `revokeUnusableGrant`), used by the MCP token endpoint and the CLI path.
- CLI session: `backend/api/oauth/cli.go` (`CLIClientID`, `CLIClientName`, `EnsureCLIClient`, `CreateCLIGrant`, `RotateCLIGrant`).
- MCP flow: `backend/api/oauth/token.go` (`TokenHandler`, `exchangeAuthorizationCode`, `exchangeRefreshToken`, `createGrant`, `writeGrantResponse`) and `metadata.go`; connected-app management in `backend/api/v1/connected_app_service.go`.
- CLI flow: issued in `backend/api/v1/auth_service_device_login.go` (`issueDeviceLoginToken`), refreshed in `backend/api/v1/auth_service_exchange_refresh_token.go`, ended by the CLI branch of `backend/api/v1/auth_service.go` (`Logout`).
- Enforcement: `backend/api/auth/authenticator.go` (`GrantStore`, `grantRevoked`, `ErrGrantRevoked`), `backend/component/state/state.go` (`GrantRevocationCache`), `backend/api/auth/grant_token.go` (`GenerateGrantAccessToken`, `GenerateCLIAccessToken`), `backend/mcp/auth.go`.
- Web flow: `backend/api/v1/auth_service_refresh.go`, with `Login`/`Logout` in `backend/api/v1/auth_service.go`; cookie/hash tests `backend/api/auth/refresh_token_test.go`.
- DDL: `backend/migrator/migration/0.1/0014##refresh_tokens.sql` (historical tables), `0023##connected_apps.sql`, `0040##cli_sessions.sql`, mirrored in `LATEST.sql`.
- CLI side: `cli/config/config.go` (the stored pair and expiries), `cli/client/session.go` (rotation, single flight, cross-process adoption), `cli/client/client.go` (the 401 interceptor and `RefreshSession`), `cli/cmd/auth.go` (login/status/logout), `cli/authflow/device.go`.
- Integration: `backend/test/integration/runner/refresh_token_service_test.go` (web), `mcp_service_test.go` (MCP grant), `agent_cli_service_test.go` (CLI session: rotation, replay, per-device logout, Connected apps revocation).
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`, build; `make test-integration-smoke`; SPA `biome:check`, `type-check`, `test run`.
