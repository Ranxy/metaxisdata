# Connected apps (MCP OAuth authorizations and CLI device logins) — Reference

> Maintenance reference for `backend/api/v1/connected_app_service.go`, `backend/store/oauth_grant.go`, the rotation paths in `backend/api/oauth/`, the grant-liveness check in `backend/api/auth/authenticator.go`, and the MCP tool-activity path. Related: [docs/mcp.md](../../docs/mcp.md) (operator guide), [docs/security-posture.md](../../docs/security-posture.md) (accepted trade-offs), [mcp-server.md](mcp-server.md), [refresh-tokens.md](refresh-tokens.md), [iam-and-permissions.md](iam-and-permissions.md).

## What it is

One authorization is one **connected app**: a user × one client registration × one consent, stored as a row in `oauth_refresh_token` (historical table name; Go type `store.OAuthGrant`). Two issuance ceremonies share the row shape, the rotation and the revocation path, distinguished by `kind`:

- **MCP client** — browser OAuth 2.1 authorization (code + PKCE), `kind = 'mcp'`, access-token audience is the MCP resource.
- **`mxd` CLI** — device login, `kind = 'cli'`, access-token audience is the user API.

`ConnectedAppService` lists the existing rows, derives their state and revokes them one at a time. It does not manage browser sessions (`web_refresh_token` is a separate model) or MCP transport sessions (`/mcp` is stateless; only the authorization is durable and revocable). CLI sessions appear here because a user needs to see which machines the CLI is signed in on and cut one off without signing out the others.

```
user --browser consent--> registered OAuth client --+
                                                     +--> grant row (id stable; kind mcp|cli)
user --device approval--> built-in client `mxd` -----+         | `gid` claim
                                                               v
MCP access token --> /mcp checks grant liveness per request
CLI access token --> user API checks grant liveness per request
```

The row carries `created_at` (first issuance), `expires_at` (sliding 30 days), `last_tool_call_at` (MCP tool-call attempts), `client_version` and `request_ip` (CLI device facts, display only), and `revoked_at` / `revoked_by` (soft revocation).

## Decisions

| # | Decision | Why |
| --- | --- | --- |
| D1 | Scope is MCP OAuth authorizations plus CLI device-login sessions | Both already are one authorization on one rotating grant; browser sessions stay a separate model |
| D2 | An administrator sees the workspace; a user sees and revokes their own | Single-tenant posture; no per-resource IAM, so each handler narrows first and `connectedApps.list` / `.revoke` express the widen |
| D3 | Revocation reaches already-issued access tokens — immediately on the revoking replica, within 30 s elsewhere | The access token carries the grant id and every entry point re-checks grant liveness |
| D4 | `last_tool_call_at` records tool-call **attempts**; issuance and refresh never write it | "Is the app still used" differs from "the token was just rotated"; the conditional update writes at most once per 5 minutes per grant |
| D5 | Canonical name Connected app; name `connectedApps/{id}`; permissions `connectedApps.{list,revoke}` | One concept, one resource name, one permission family; no MCP tool or CLI command, because management is a human action |
| D6 | Rows survive 7 days past `expires_at` before the maintenance prune | The page can show what was revoked and by whom; permanent history belongs to the audit ledger |
| D7 | One row = one authorization, revoked per row, never batched by user or client | The same (user, client) pair may hold several authorizations; the connection is the unit |
| D8 | Reuse the grant row and rotate in place; the CLI reuses the same table and rotation, distinguished by `kind` | The id stays the stable handle for `connectedApps/{id}` and `gid`; a second table or rotation would need a mapping and could drift |
| D9 | CLI `Logout` revokes only its own grant; MCP `Logout` revokes every grant of that client | One CLI row is one machine, so signing out of one must not sign out the others; one MCP row is one client instance, so cutting it ends the whole connection |
| D10 | `client_version` / `request_ip` are display-only, outside the trust chain | "Which machine was this" must be recognizable; the values are self-reported metadata, never authorization input |

## Core mechanics

- **Issuance and rotation.** `exchangeAuthorizationCode` creates the row first (`CreateOAuthGrant`, `INSERT … RETURNING id`) and only then mints the access token carrying that id (`writeGrantResponse`); `created_at` is the first issuance time, not the consent moment (they differ by at most the authorization-code TTL). A refresh runs every check decidable from the row first (client binding, existence, expiry, `revoked_at`, resource, scope), then claims the credential with one atomic `RotateOAuthGrant`: an `UPDATE` of `token_hash` and `expires_at` gated on the presented hash, the client, `revoked_at IS NULL` and `expires_at > now`, returning the row id. Zero rows means a lost race, a revocation, an expiry or a wrong client → `invalid_grant`. The id survives, so `gid` and the resource name survive every rotation; expired rows are kept for the `EXPIRED` state; a principal re-check that fails afterwards (missing or deactivated account, or grant older than the last password change) ends the grant explicitly through `revokeUnusableGrant`. Under READ COMMITTED the losing `UPDATE` re-evaluates its predicate after the lock, so exactly one concurrent refresh wins, and a revocation that commits first serializes against the rotation on the same row.
- **CLI sessions.** The approval branch of `ExchangeDeviceLogin` calls `EnsureCLIClient` (an idempotent insert of the built-in `mxd` registration, only so the list can show a name) and `CreateCLIGrant`, then mints a user-API-audience access token carrying that grant id. Refresh is `AuthService.ExchangeRefreshToken` → `RotateCLIGrant`: the row must exist, be `kind = 'cli'`, unexpired and not revoked; then the same atomic rotation and the same principal re-check as MCP, with the expiry recomputed from now, so the session slides. The audiences keep the surfaces apart — an MCP token is refused on the user API and a CLI token at `/mcp` — and both carry `cid`/`gid`, so revocation reaches tokens minted before it.
- **Grant-liveness enforcement.** Access tokens carry a `gid` claim (`AccessTokenIdentity.GrantID`); web and API tokens carry 0. `TokenAuthenticator.Resolve` checks the `jti` revocation record first and, only when `GrantID != 0`, grant liveness: the process-local 30-second `state.GrantRevocationCache` (a separate instance from the token cache, so token traffic cannot evict connection decisions), then `IsOAuthGrantLive`, whose predicate binds the token's own `id`, `user_id` and `client_id` with `revoked_at IS NULL AND expires_at > NOW()`. A missing row is not live; a store read failure fails closed. The `/mcp` verifier refuses an MCP access token without `gid` and puts the grant id into `TokenInfo.Extra` so the tool path can record activity.
- **Revocation and `Logout`.** `RevokeOAuthGrant` is a conditional update: zero changed rows on an existing row is idempotent success and keeps the first `revoked_by`, a missing row is `NotFound`, and revoking another user's connection without permission is also `NotFound` so the call cannot probe existence. `revoked_by` is NULL only on the system path. After the write commits, the replica marks `GrantRevocationCache`, so its next requests refuse immediately; another replica sees the row within one cache TTL. `Logout` follows the token's claims: an MCP token revokes every grant of `(user, client)`, a CLI token only the grant its `gid` names, and a web session deletes its refresh row. Pending in-process authorization requests are not cleaned up; a new consent creates a new row and a new id, which is the intended semantics.
- **Tool activity.** `dispatch` calls `touchConnectedApp` before invoking the tool, so a call refused for permission or budget still counts as a use; `TouchOAuthGrant` is one conditional update (only when `last_tool_call_at` is older than 5 minutes), so a throttled write is a no-op. Issuance and refresh never touch it, and a write failure never fails the tool call.
- **Listing, filtering and state.** `ListConnectedApps` defaults to 50 rows (maximum 1000; a page token keeps the size it was issued with), ordered `created_at DESC, id DESC`. Handler scoping: with `connectedApps.list` the whole workspace, narrowed by `user`; without it only the caller's own rows, and naming another user is `PermissionDenied`. Revoking one's own row needs no permission; another user's needs `connectedApps.revoke`, otherwise `NotFound`. `parseConnectedAppFilter` supports `user ==`, `state ==` / `in`, `client` / `client_id` / `user_email` (exact or `matches`), and `create_time >=` / `<=`, translating `state` to the same `CASE` the server derives. State precedence: `REVOKED` → `EXPIRED` → `INACTIVE` (deactivated account; restoring the account revives the connection, it is not auto-revoked) → `REAUTH_REQUIRED` (an MCP grant whose resource differs from the resolvable current MCP resource, or any grant created before the user's last password change) → `ACTIVE`. With `mcp_enabled` off or `external_url` unresolvable, the resource clause is dropped rather than reporting an anomaly. Rows carry `kind`; CLI rows also carry `client_version` and `request_ip` and have no `last_tool_call_time`. `user_email` is filled only for the caller's own row or with `users.get`; `client_name` is display text.
- **Retention.** The maintenance runner prunes grants 7 days after `expires_at` (`connectedAppRetention`). A revoked row stops sliding at its last rotation, so it can stay visible for about 37 days.

## Invariants

1. **Rotation and revocation land on the same row and serialize.** The rotation predicate must include `revoked_at IS NULL AND expires_at > now`, and revocation must include `revoked_at IS NULL`; otherwise a concurrent refresh revives a revoked authorization.
2. **`revoked_at` is terminal.** No path clears it; authorizing again creates a new row and a new id. `expires_at` slides on rotation; `created_at` is never rewritten to the rotation time.
3. **The row id is the stable identity** — the resource name, the `gid` claim and the handle the tool path records activity against, all at once. Never reuse, renumber or recreate the row on rotation.
4. **A missing row is not live, and a read failure fails closed.** The liveness query must bind `id + user_id + client_id`; the cache must stay a separate instance, and an MCP token without `gid` is refused outright.
5. **The refresh credential outlives the access token (30 days vs 7 days).** That relation is what makes "row pruned ⇒ token already expired" safe; making the lifetimes configurable must preserve it.
6. **One row is one authorization.** The same (user, client) may hold several rows, and revoking one does not affect another; MCP `Logout` is the only per-(user, client) bulk path.
7. **A mixed-audience RPC narrows before it widens.** List and revoke are unannotated and on the allowlist; each handler scopes to the caller first and widens only on the permission, and a checker error fails closed. `TestEveryMethodIsPermissionGated` only checks allowlist membership, so the matrix test `connected_app_test.go` is the real guard.
8. **Revocation is audited.** `connectedApps.revoke` carries the audit annotation, so the row and the permanent ledger both name what was revoked; listing is neither audited nor budgeted. MCP tool-ledger rows do not attribute the client or grant.
9. **The management surface is independent of `mcp_enabled`.** Turning the MCP surface off leaves Connected apps as the way to clean up existing authorizations.
10. **`connectedApps.*` is not in the workspace-member baseline.** It is reachable through `workspaceAdmin` or a custom role, while self-service still works without it.

## Failure modes

| Scenario | Behavior |
| --- | --- |
| Invalid `name`, `user` not `users/{id}`, invalid `page_token` or filter | `InvalidArgument` |
| Naming another user's connections without `connectedApps.list` | `PermissionDenied` |
| Missing row, or another user's row without `connectedApps.revoke` | `NotFound`, uniformly (no existence probe) |
| Revoking an already-revoked row | Idempotent success with the current snapshot; audited; the first `revoked_by` is kept |
| Store or permission-checker failure | `Internal`; a checker failure does not widen the scope |
| Access token of a revoked connection | `/mcp` 401 with `invalid_token`; user API `Unauthenticated` (immediate on the revoking replica, within 30 s elsewhere) |
| Revoked, expired, replayed, other-client or resource-mismatched MCP refresh | `invalid_grant`; the expired row is kept and shown as `EXPIRED` until the prune |
| Revoked, expired, replayed or wrong-kind CLI refresh | `Unauthenticated`; the CLI exits 2 and asks for a new `mxd auth login` |
| Revoking a CLI session from Connected apps | That machine's access token is refused and its refresh chain mints nothing; other machines are unaffected |
| Access token minted before `gid` existed | 401 at `/mcp`; one refresh recovers, and only an expired refresh token needs the browser again |
| Deactivated account | State `INACTIVE`; both access and refresh are refused by the existing chain; restoring the account revives the connection without revoking it |
| `mcp_enabled` off or `external_url` unresolvable | The page still lists and revokes; state degrades to stored facts and skips only the resource clause |
| Cache marking fails after a successful revoke | This replica refuses after at most one TTL; the database remains authoritative |
| `last_tool_call_at` write fails or is throttled | The tool call proceeds (best effort); the column lags by at most one 5-minute window |

## Known residuals (accepted or tracked)

| Residual | Status and destination |
| --- | --- |
| `oauth_refresh_token.client_id` has no foreign key; wiring up `DeleteOAuthClient` would leave orphan grants | Open engineering debt, [backend-open-items.md](backend-open-items.md) |
| Revocation does not clear pending in-process authorization requests | Accepted: a new consent creates a new connection, and a pending request needs the user's own approval |
| `Logout`'s audit row does not name the target client; the MCP tool ledger has no client/grant attribution | Open attribution debt, [backend-open-items.md](backend-open-items.md) |
| `oauth_client.last_used_at` is written but never read; `ListOAuthClients` / `DeleteOAuthClient` have no callers; registrations have no total cap | Open, [security-open-items.md](security-open-items.md); Connected apps manages authorizations, not registrations |
| No concurrent "revoke vs rotate" race test | The suites cover concurrent refresh (exactly one winner) and revoke-then-401 separately; the race test is optional hardening |
| A revoked grant row remains about 37 days after revocation | Accepted: `expires_at` stops at the last rotation plus 30 days; permanent history is the audit ledger |
| Re-running `mxd auth login` does not revoke the previous CLI session | Accepted: the old row stays visible and revocable, and the login does not decide for the user whether to cut another machine off |

## Where things live

- Schema: `backend/migrator/migration/0.1/0014##refresh_tokens.sql` (tables), `0023##connected_apps.sql` (grant columns `last_tool_call_at`, `revoked_at`, `revoked_by` and the `(user_id, created_at DESC)` index), `0040##cli_sessions.sql` (`kind`, `client_version`, `request_ip`), mirrored in `LATEST.sql`; the table name remains `oauth_refresh_token`.
- Store: `backend/store/oauth_grant.go` (`Create`/`Get`/`Rotate`/`Revoke`/`RevokeByUserAndClient`/`IsLive`/`Touch`/`GetConnectedApp`/`ListConnectedApps`/`DeleteExpired`), with the query shapes guarded by `backend/store/oauth_grant_test.go`.
- Service and API: `backend/api/v1/connected_app_service.go`; filter parsing in `backend/api/v1/filter.go` (`parseConnectedAppFilter`); proto `proto/v1/v1/connected_app_service.proto`.
- Tokens and verification: `backend/api/auth/auth.go` (`gid`, `GrantID`), `backend/api/auth/mcp_token.go`, `backend/api/auth/authenticator.go` (`GrantStore`, `grantRevoked`, `ErrGrantRevoked`), `backend/component/state/state.go` (`GrantRevocationCache`), `backend/mcp/auth.go` (refuses a token without `gid`; carries the grant in `TokenInfo.Extra`).
- MCP rotation: `backend/api/oauth/token.go` (creation, rotation, failure-triggered revocation; `ServerConfig.TokenStores` is the hermetic test seam) and `backend/api/oauth/token_test.go`.
- CLI sessions: `backend/api/oauth/cli.go` (`CLIClientID`, `EnsureCLIClient`, `CreateCLIGrant`, `RotateCLIGrant`), `backend/api/oauth/grant.go` (shared rotation, principal re-check, revocation), `backend/api/v1/auth_service_device_login.go` (issuance), `backend/api/v1/auth_service_exchange_refresh_token.go` (refresh), `backend/api/auth/grant_token.go` (`GenerateCLIAccessToken`).
- Logout and retention: `backend/api/v1/auth_service.go` (MCP branch revokes all grants, CLI branch only its `gid`; both mark the cache), `backend/runner/maintenance/maintenance.go` (`connectedAppRetention`, `DeleteExpiredOAuthGrants`).
- Tool activity: `backend/mcp/server.go` (`dispatch`, `touchConnectedApp`, `lastToolCallThrottle`).
- Permissions: catalog `backend/common/permission/permission.json` (101 entries; `connectedApps.*` is outside the 29-entry `workspaceMember` baseline and comes with `workspaceAdmin`), allowlist `backend/api/v1/acl_interceptor_test.go`, matrix `backend/api/v1/connected_app_test.go`.
- Frontend: routes `frontend/src/router/index.ts` (admin `/settings/connected-apps`, self-service `/connected-apps`), pages `frontend/src/pages/settings/ConnectedAppsSettingsPage.vue` and `frontend/src/pages/ConnectedAppsPage.vue`, shared `frontend/src/components/connected-app/`, API `frontend/src/api/connected-app.ts`, permission mirror `frontend/src/lib/permissions.ts`.
- Integration: `backend/test/integration/runner/mcp_service_test.go` (authorize → list → revoke → `/mcp` 401 → refresh `invalid_grant` → repeat revoke) and `agent_cli_service_test.go` (device login → list → revoke → user API 401 and refused refresh; rotation, replay, sliding expiry, per-device logout); the migration chain is guarded by `backend/migrator/migrator_integration_test.go`.
- Gates: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`, build; `make test-integration-smoke` (and `make test-integration` for a migration change); frontend `biome:check`, `lint`, `i18n`, `type-check`, `test run`; proto changes via `buf format` / `buf lint` / `buf generate`.
