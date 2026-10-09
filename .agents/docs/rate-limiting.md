# Rate limiting — Reference

> Status: **implemented**. Maintenance reference for the request budgets in the server: where each is enforced and why the pieces are shaped this way. Open items live in `.agents/docs/security-open-items.md` (未决清单); `docs/security-posture.md:20-25` states the posture and is not repeated here.

## The two dimensions

Two things are bounded, and they need different keys:

- **Anonymous entry points** have no principal. Their key is the client address resolved by the trusted-proxy rules — `audit.ClientAddress`, the same value the audit row records — so a caller cannot reset its bucket with a forwarding header. Each also carries a global counter so a deployment whose callers all appear to share one address (an unlisted reverse proxy) is still bounded.
- **A signed-in caller's work** is keyed by principal. Its buckets are `(principal, procedure)` plus one deployment-wide counter shared across every covered method, so one loud method cannot spend another method's quota while the deployment as a whole still has a ceiling.

Both are fixed windows: a key's window opens with its first allowed request and lasts one minute, and a refused request does not extend or spend anything (`backend/component/state/window_limiter.go:190`).

## What is budgeted, and where

| Where | Methods | Key | Per key | Global |
| --- | --- | --- | --- | --- |
| `ThrottleInterceptor` | `Login`, `Refresh` | resolved address | 120/min | 300/min |
| `ThrottleInterceptor` | `CreateUser` (signed-in callers exempt) | resolved address | 20/min | 100/min |
| `ThrottleInterceptor` | `Logout` | resolved address | 120/min | 300/min |
| `ThrottleInterceptor` | `CreateSSOState` | resolved address | 120/min | 300/min |
| `PrincipalThrottleInterceptor` | every authenticated audited method, except `ListAuditLogs`, plus `ExplainSQL` and `FetchLLMModels` | `user:{id}\|{procedure}` | 3000/min | 30000/min |
| `LoginLimiter` (handler) | `Login` failures | account, source | 10/5min, 20/5min | — |
| `ThrottleInterceptor` | `CreateDeviceLogin` | resolved address | 10/min | 100/min |
| Device login (handler) | `GetDeviceLogin`, `ApproveDeviceLogin` | caller (`user:` or `ip:`) | 60/min | 600/min |
| MCP (`Server.invoke`) | tool calls | principal | 600/min | 6000/min |
| MCP (`Server.Handler`) | every request, before the bearer check | resolved address | 6000/min | 60000/min |
| `boundedRateLimiterStore` | OpenLineage ingestion | ingestion-key digest, else resolved address | 50/s, burst 100 | — |
| `boundedRateLimiterStore` | OpenLineage ingestion | resolved address, whatever key presented | 2000/s, burst 4000 | — |
| `boundedRateLimiterStore` | each `/oauth/*` route | resolved address | 10/s, burst 20 | — |

`CreateUser` exempts a signed-in caller on the anonymous interceptor because an administrator may create members in bulk; those calls are counted by the principal budget instead. The numbers are constants in `backend/component/state/window_limiter.go`, `backend/component/state/login_limiter.go`, `backend/server/openlineage_ingestion.go:17-35` and `backend/server/oauth_endpoints.go:12-16`; the two allowlists are `principalLimitedProcedures` / `auditedReadExemptProcedures` in `backend/api/v1/principal_throttle_interceptor.go:22`, `:35`.

## Rows, not calls

Every budget above counts **calls**. What grows permanently is **rows**, and the two are different numbers: one accepted call can write many rows into a table no budget looks at. The bound that matters for each table, as measured:

| Table | Rows per call | What bounds it | Reclaimed? |
| --- | --- | --- | --- |
| `audit_log` | 1 | the call budgets above | never (decision) |
| `meta_registry_resource_history` | 1 per changed object (a schema sync's whole diff; up to 1000 instances per `BatchSyncInstances` call) | the call budgets only | never (decision) |
| `openlineage_run` / `_run_dataset` / `_dataset_member` | 1 per event, plus up to 1000 dataset refs per event | the ingestion buckets, then `openlineage_retention_days` | yes, with retention |
| `column_lineage` (OpenLineage edges) | the edges one event derives | the ingestion buckets | yes — the prune now deletes them with their run |
| `external_dataset` | 1 per distinct referenced dataset | the ingestion buckets | never (no delete path; recorded as permanent) |
| `manual_sql_tag` / `_attribute` | 1 per tag / attribute, now capped at 100 each | the cap plus the 4 MiB body | with the entry |
| `oauth_client` | 1 row + 1 audit row (each up to ~20 KiB) | one per-address bucket, 600/min | never (no caller of `DeleteOAuthClient`; recorded as such) |
| `revoked_token` | 1 per logout | 300/min plus the token's 7-day life | yes, by the maintenance prune |
| `oauth_refresh_token` / `web_refresh_token` | 1 per exchange / login | the route buckets | yes, after 30 days |
| `principal` | 1 per signup | the signup budget, 100/min | never |

Two consequences worth keeping in mind when adding a table or a bulk write: a budget on a method is not a budget on its rows, and "kept forever" tables need their per-call amplification reviewed rather than their call rate.

## Design decisions

- **A refused request leaves no ledger row — if its budget is on the chain.** Both Connect interceptors run before `AuditInterceptor`, so a request they stop never runs and never records. That is what makes the budget *backpressure on the caller* rather than a silent drop from a ledger that is kept forever: every action that happened is still recorded, and a caller that loops a ledger-writing method is told to slow down. A budget *inside* a handler is wrapped by the audit interceptor and does not get this property: its refusals are recorded too. The Login failure lockout and the device-login lookup budget are the two that remain, and both only ever refuse a request that already passed a chain budget, so their rows are bounded by it. `CreateDeviceLogin` was in that group and is now on the chain: as an anonymous, audited method it was an unauthenticated caller's way to write permanent rows at whatever rate the server could serve — tens of thousands a minute — because the handler's refusal was audited and nothing else bounded the requests.
- **The budget covers ledger writes, not reads, and the LLM calls.** The audit annotation decides the set, so a method that gains `audit = true` later is budgeted without touching the interceptor. `ListAuditLogs` is exempt: it is the one audited method that only reads.
- **That exemption is not "a reader of the ledger is unbounded" — it is worse.** The audit interceptor runs before the ACL interceptor, so a call the caller is not allowed to make still writes a row, and an ordinary member holds no `metaxisdata.auditLogs.search`. Every `ListAuditLogs` call from any signed-in account is therefore *refused and recorded*, with no rate bound on the recording: 3200 calls write 3200 permanent rows (measured against a real server). The budget also counts before the ACL, so a denied call spends quota as well. Closing this means treating "a read that writes a row" as a ledger write, which is a policy about how much of the ledger one caller may write; it is left as the one explicit hole rather than quietly bounded, and `docs/security-posture.md` states it.
- **The deployment-wide counter is shared, so it can be spent by one account.** A permission-less account can exhaust the 30000/min ceiling in about half a minute, after which every other principal is refused until the window rolls. The counter is what bounds the ledger's total growth across principals, which is why it exists; the availability cost is accepted and documented rather than papered over. `validate_only` calls are the cheapest way to spend it. The `Logout` ceiling has the same property from the anonymous side: three addresses spend its 300/min and nobody can log out for the window — still better than the unauthenticated replay writing rows without any bound.
- **`Logout` is budgeted even though it is idempotent.** Revoking the same token twice changes nothing, but every call still writes an audit row, and a token this server signed can be replayed for as long as the caller likes. It gets its own bucket rather than sharing Login's, so a token replay cannot spend the sign-in budget.
- **The MCP source budget is counted before the bearer check.** The per-principal tool budget cannot see a request that carries no or an invalid token — identity resolution refuses it first — so without the address budget an unauthenticated caller can drive token verification and a revocation lookup per invented token without limit. Its numbers are ten times the per-principal ones so it bounds probing without pacing a fleet of agents behind one NAT.
- **OpenLineage ingestion counts the resolved address as well as the key.** An unknown ingestion key is rejected without a password check, so forged keys are cheap and each one opens a fresh bucket; the key dimension therefore cannot bound that caller, and the address is the one identifier it cannot choose. The address budget is sized for a fan-in (a NAT, a shared Spark gateway): forty producers may each spend their own burst before it refuses. Forty is a choice, not a measurement — a deployment with more producers behind one address raises the number, and the cost of raising it is that a rotating caller gets more room before its own address refuses.
- **A deployment-wide bucket shared by every identifier was tried and removed.** It bounded the rotating caller's total rate, but a caller with forged keys (or, on the OAuth routes, a handful of addresses) exhausted it and every other producer on the route was refused `429` — an unbounded rate traded for a way to deny everyone else. The per-address dimension bounds the same caller without making anyone else pay. The OAuth routes carry no second dimension at all: their identifier is already the resolved address, and their request side is anonymous by design, so there was no rotating-identifier gap to close. That reasoning is bounded by the same premise as every other budget here: it holds for a caller that cannot present many addresses, so an address-range holder (an IPv6 `/64`) or a deployment whose trusted-proxy entry covers the client is not bounded by those buckets either, and each of these routes writes a ledger row per request.
- **Two limiter implementations, one per route family.** Connect methods use `state.WindowLimiter` (fixed window, key + global). The plain-HTTP routes use `server.boundedRateLimiterStore` (token bucket, per identifier) because they go through echo's rate-limit middleware. They differ in smoothing, not in what they protect, and both cap their tracked identifiers (4096; idle buckets swept after three minutes).
- **OpenLineage ingestion and each OAuth route stay separate budgets.** Sharing one ceiling across the four anonymous OAuth routes would tighten that surface four-fold and is the open I2 question in `.agents/docs/security-open-items.md`, not a decision this work makes.
- **Every counter is process-local.** Like the rest of `backend/component/state`, replicas each hold their own window; a multi-replica deployment that needs one global ceiling must put a shared limiter in front of the API.

## Tests and gate

- `backend/api/v1/principal_throttle_interceptor_test.go` walks the protobuf registry and requires every audited method to be bounded either per source or per principal; the two runtime allowlists (`principalLimitedProcedures`, `auditedReadExemptProcedures`) must match the test's copies exactly, so adding an exemption is a deliberate two-file change. The same file pins the per-key/global dimensions, that an anonymous caller and an unbudgeted method are untouched, and that a refused stream never reaches its handler.
- `backend/server/openlineage_ingestion_test.go` pins the key dimension, the address dimension and its resolver (a rotating `X-Forwarded-For` must not open a bucket per request — Echo's `RealIP` would), that an invented key cannot outrun the address one, and that one caller spending its own address budget does not refuse a producer on another address (the regression for the removed shared bucket).
- `backend/server/rate_limiter_test.go` pins the identifier ceiling and the property it deliberately does *not* provide: a rotating caller always gets a fresh bucket. `backend/api/v1/auth_service_device_login_test.go` pins that `CreateDeviceLogin` is bounded on the chain, not in its handler. `backend/mcp/source_limiter_test.go` pins the address budget, its key resolution and that a switched-off surface is not counted.
- `backend/test/integration/runner/rate_limit_service_test.go` runs the real server: a `Logout` replay is refused past its budget, an authenticated `FetchLLMModels` loop past the principal budget, and 40 anonymous `CreateDeviceLogin` calls from one address leave exactly the ten accepted calls' rows in the ledger.
- Gate: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`, build; `make test-integration-smoke`.

## Residuals

1. **`ListAuditLogs` is the one unbounded ledger write.** Every other anonymous or authenticated audited method is now bounded by a budget on the interceptor chain, which refuses before the audit row is written. It is exempt as a read, but the audit interceptor runs before the ACL interceptor, so every call from any signed-in account — permission or not — writes a permanent row while being refused, and nothing caps the rate (3200 calls, 3200 rows, measured). `docs/security-posture.md` states this; closing it means treating "a read that writes a row" as a ledger write.
2. **`BatchSyncInstances`, CSV export and similar bulk work share the per-method budget with interactive calls.** The shipped limit is generous (3000/min) so it does not pace work, which means it bounds a loop rather than a burst.
3. **The deployment-wide ceilings are shared and therefore spendable by one caller.** One account can exhaust the principal 30000/min and refuse every other principal for the window; three addresses can exhaust the Logout 300/min so nobody can log out. Both are the price of bounding aggregate ledger growth and are documented in `docs/security-posture.md`. Separately, budgets are decided before the ACL, so a call the caller may not make still counts (and still leaves a row).
4. **`/mcp` is not the largest ledger-writing channel.** A call refused by the per-principal budget is still audited and spends no principal budget, so what caps its rows per minute is the address budget (6000/min per address, 60000/min for the deployment) — but the OpenLineage ingestion routes allow 2000 requests a second from one address, each writing an audit row (120000/min), and the per-method principal ceiling is 3000/min for one method. Comparing configured ceilings across routes is fragile; the per-route numbers in the table above are the reference.
5. **`ExchangeDeviceLogin` and `GetWorkspaceProfileSetting` carry no budget.** The first needs a 256-bit device code that only a budgeted call can mint and is held to the protocol's minimum poll interval; the second reads one setting, writes nothing and is not audited.
6. **A limiter's table is pruned only at its ceiling**, so a long-running process accumulates every key it has ever seen and then evicts on every request — a full walk of the map under the limiter's mutex (~0.7 ms once the 16384-key principal table is full, against ~0.3 µs below it). The keys are server-chosen so a caller cannot force it, and the shipped global budgets sit below what the walk can sustain; it is a tail-latency boundary. The fix, if it is ever worth making, is to drop elapsed keys on an amortized schedule instead of only at the ceiling, which keeps the table near the set active in one window.
7. **Counters are per replica** (see above) and **not configurable**: the numbers are constants, so changing one is a code change with its test.

## Where things live

- Connect interceptors: `backend/api/v1/throttle_interceptor.go` (anonymous), `backend/api/v1/principal_throttle_interceptor.go` (per principal, with the two allowlists).
- Budget constants: `backend/component/state/window_limiter.go`; account/source login lockout `backend/component/state/login_limiter.go`; handler usage `backend/api/v1/auth_service.go:91`, `backend/api/v1/auth_service_device_login.go:93`.
- Plain-HTTP routes: `backend/server/openlineage_ingestion.go`, `backend/server/oauth_endpoints.go`, bounded store `backend/server/rate_limiter.go`.
- MCP: `backend/mcp/server.go`; wiring of both limiters at `backend/server/grpc_routes.go:335`.
- Enablement: nothing to enable — budgets are constants applied unconditionally; a changed number is a code change with its test.
