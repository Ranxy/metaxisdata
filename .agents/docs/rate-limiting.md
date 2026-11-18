# Rate limiting

Maintenance reference for the request budgets in the server: where each is enforced and why the pieces are shaped this way. The posture — which holes are deliberate, and what each costs — is stated in [docs/security-posture.md](../../docs/security-posture.md) and not repeated here.

## The two dimensions

- **Anonymous entry points** have no principal. Their key is the client address resolved by the trusted-proxy rules — `audit.ClientAddress`, the same value the audit row records — so a caller cannot reset its bucket with a forwarding header. Each also carries a global counter, so a deployment whose callers all appear to share one address (an unlisted reverse proxy) is still bounded.
- **A signed-in caller's work** is keyed by principal. Its buckets are `(principal, procedure)` plus one deployment-wide counter shared across every covered method, so one loud method cannot spend another method's quota while the deployment as a whole still has a ceiling.

Both are fixed windows: a key's window opens with its first allowed request and lasts one minute, and a refused request does not extend or spend anything.

## What is budgeted, and where

| Where | Methods | Key | Per key | Global |
| --- | --- | --- | --- | --- |
| `ThrottleInterceptor` | `Login`, `Refresh`, `ExchangeRefreshToken` | resolved address | 120/min | 300/min |
| `ThrottleInterceptor` | `CreateUser` (signed-in callers exempt) | resolved address | 20/min | 100/min |
| `ThrottleInterceptor` | `Logout` | resolved address | 120/min | 300/min |
| `ThrottleInterceptor` | `CreateSSOState` | resolved address | 120/min | 300/min |
| `PrincipalThrottleInterceptor` | every authenticated audited method, except `ListAuditLogs`, plus `ExplainSQL` and `FetchLLMModels` (includes `ConnectedAppService.RevokeConnectedApp`) | `user:{id}\|{procedure}` | 3000/min | 30000/min |
| `LoginLimiter` (handler) | `Login` failures | account, source | 10/5min, 20/5min | — |
| `ThrottleInterceptor` | `CreateDeviceLogin` | resolved address | 10/min | 100/min |
| Device login (handler) | `GetDeviceLogin`, `ApproveDeviceLogin` | caller (`user:` or `ip:`) | 60/min | 600/min |
| MCP (`Server.invoke`) | tool calls | principal | 600/min | 6000/min |
| MCP (`Server.Handler`) | every request, before the bearer check | resolved address | 6000/min | 60000/min |
| *(none)* | Application SQL reporting, `POST /api/v1/applicationSql/report` | — | **no budget** | **no budget** |
| `boundedRateLimiterStore` | OpenLineage ingestion | ingestion-key digest, else resolved address | 50/s, burst 100 | — |
| `boundedRateLimiterStore` | OpenLineage ingestion | resolved address, whatever key presented | 2000/s, burst 4000 | — |
| `boundedRateLimiterStore` | each `/oauth/*` route | resolved address | 10/s, burst 20 | — |

`CreateUser` exempts a signed-in caller on the anonymous interceptor because an administrator may create members in bulk; those calls are counted by the principal budget instead. The numbers are constants in `backend/component/state/window_limiter.go`, `backend/component/state/login_limiter.go`, `backend/server/openlineage_ingestion.go` and `backend/server/oauth_endpoints.go`; the two allowlists are `principalLimitedProcedures` and `auditedReadExemptProcedures` in `backend/api/v1/principal_throttle_interceptor.go`.

## Rows, not calls

Every budget above counts **calls**. What grows permanently is **rows**, and the two are different numbers: one accepted call can write many rows into a table no budget looks at. The bound that matters for each table:

| Table | Rows per call | What bounds it | Reclaimed? |
| --- | --- | --- | --- |
| `audit_log` | 1 | the call budgets above | never (decision) |
| `meta_registry_resource_history` | 1 per changed object (a schema sync's whole diff; up to 1000 instances per `BatchSyncInstances` call) | the call budgets only | never (decision) |
| `openlineage_run` / `_run_dataset` / `_dataset_member` | 1 per event, plus up to 1000 dataset refs per event | the ingestion buckets, then `openlineage_retention_days` | yes, with retention |
| `column_lineage` (OpenLineage edges) | the edges one event derives | the ingestion buckets | yes — the prune deletes them with their run |
| `external_dataset` | 1 per distinct referenced dataset | the ingestion buckets | never (no delete path) |
| `manual_sql_tag` / `_attribute` | 1 per tag / attribute, capped at 100 each | the cap plus the 4 MiB body | with the entry |
| `oauth_client` | 1 row + 1 audit row (each up to ~20 KiB) | one per-address bucket, 600/min | never (no caller of `DeleteOAuthClient`) |
| `revoked_token` | 1 per logout | 300/min plus the token's 7-day life | yes, by the maintenance prune |
| `oauth_refresh_token` (one connected app per authorization) | 1 per authorization exchange / device login; rotation and revocation UPDATE the row in place, they never append | the authorization exchange bucket; CLI rows mint inside `ExchangeDeviceLogin`, which can only follow an anonymous budgeted `CreateDeviceLogin` plus the signed-in `ApproveDeviceLogin`; `RevokeConnectedApp` is audited and therefore principal-budgeted; `last_tool_call_at` is a conditional UPDATE at most once per 5 minutes per grant | yes — 7 days after `expires_at` |
| `web_refresh_token` | 1 per login / refresh (DELETE + INSERT) | the login/refresh buckets | yes, after 30 days |
| `principal` | 1 per signup | the signup budget, 100/min | never |

Two consequences worth keeping in mind when adding a table or a bulk write: a budget on a method is not a budget on its rows, and "kept forever" tables need their per-call amplification reviewed rather than their call rate.

## Design decisions

- **A refused request leaves no ledger row — if its budget is on the chain.** Both Connect interceptors run before `AuditInterceptor`, so a request they stop never runs and never records. That is what makes the budget backpressure on the caller rather than a silent drop from a ledger that is kept forever. A budget *inside* a handler is wrapped by the audit interceptor and does not get this property; the Login failure lockout and the device-login lookup budget are the two that remain, and both only ever refuse a request that already passed a chain budget, so their rows are bounded by it.
- **The budget covers ledger writes, not reads, and the LLM calls.** The audit annotation decides the set, so a method that gains `audit = true` later is budgeted without touching the interceptor; `ListAuditLogs` is exempt as the one audited method that only reads, and `docs/security-posture.md` states why that exemption is unbounded.
- **The connected-app matrix is priced the same way as any other method.** `RevokeConnectedApp` is audited, so it joins the principal budget with no separate allowlist entry, and the audit row keeps the revoked snapshot after the row is pruned. `ListConnectedApps` carries no audit annotation and is an unbudgeted read: its `page_size` is capped at 1000, and its filter only narrows rows the caller may already see.
- **The CLI's refresh shares the login budget.** `ExchangeRefreshToken` is anonymous and spends one indexed lookup on a caller-supplied hash, exactly like the web `Refresh`, so both sit behind `LoginRequestLimiter`: a caller cannot spend the deployment's database on guessed token hashes, and a user with several machines is unaffected.
- **`Logout` is budgeted even though it is idempotent.** Revoking the same token twice changes nothing, but every call still writes an audit row, and a token this server signed can be replayed for as long as the caller likes. It gets its own bucket rather than sharing Login's, so a token replay cannot spend the sign-in budget.
- **A tool call writes `last_tool_call_at` at most once per five minutes per connection.** The write is one conditional UPDATE, so a call inside the window is a no-op rather than a row rewrite, and the value records attempts — including a call the permission layer later refuses — because dispatch touches it before the tool runs.
- **The MCP source budget is counted before the bearer check.** The per-principal tool budget cannot see a request that carries no or an invalid token, so without the address budget an unauthenticated caller can drive token verification and a revocation lookup per invented token without limit. Its numbers are ten times the per-principal ones so it bounds probing without pacing a fleet of agents behind one NAT.
- **OpenLineage ingestion counts the resolved address as well as the key.** An unknown ingestion key is rejected without a password check, so forged keys are cheap and each one opens a fresh bucket; the address is the one identifier the caller cannot choose. Its number is sized for a real fan-in (a NAT, a shared Spark gateway) and is a choice rather than a measurement: raising it gives a rotating caller more room before its own address refuses.
- **OpenLineage ingestion and each OAuth route stay separate budgets.** Sharing one ceiling across the four anonymous OAuth routes would tighten that surface four-fold; that remains an open question in [.agents/docs/security-open-items.md](security-open-items.md), not a decision this reference makes.

## Tests and gate

- `backend/api/v1/principal_throttle_interceptor_test.go` walks the protobuf registry and requires every audited method to be bounded either per source or per principal; the runtime allowlists must match the test's copies exactly, so adding an exemption is a deliberate two-file change.
- `backend/server/openlineage_ingestion_test.go` pins the key and address dimensions, that an invented key cannot outrun the address budget, and that one caller spending its own address budget does not refuse a producer on another address.
- `backend/server/rate_limiter_test.go` pins the identifier ceiling and the property it deliberately does not provide: a rotating caller always gets a fresh bucket. `backend/api/v1/auth_service_device_login_test.go` pins that `CreateDeviceLogin` is bounded on the chain, not in its handler. `backend/mcp/source_limiter_test.go` pins the address budget and the bearer-check ordering.
- `backend/test/integration/runner/rate_limit_service_test.go` runs the real server: a `Logout` replay past its budget is refused, an authenticated `FetchLLMModels` loop hits the principal budget, and 40 anonymous `CreateDeviceLogin` calls from one address leave exactly the ten accepted calls' rows in the ledger.
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`, build; `make test-integration-smoke`.

## Where things live

Connect interceptors: `backend/api/v1/throttle_interceptor.go` (anonymous) and `backend/api/v1/principal_throttle_interceptor.go` (per principal, with the two allowlists). Budget constants: `backend/component/state/window_limiter.go` and `backend/component/state/login_limiter.go`. Plain-HTTP routes: `backend/server/openlineage_ingestion.go`, `backend/server/oauth_endpoints.go` and the bounded store in `backend/server/rate_limiter.go`. MCP: `backend/mcp/server.go` and `backend/mcp/source_limiter.go`. Enablement: nothing — budgets are constants applied unconditionally, so a changed number is a code change with its test.
