# Rate limiting

Every request budget in the server, where it is enforced, and why the pieces are
shaped the way they are. The review that called for this work is
`docs/security-review-2026-10.md` §5.2 (M24 and the "ad-hoc per endpoint" note);
§10 there records what landed.

## The two dimensions

Two things are bounded, and they need different keys:

- **Anonymous entry points** have no principal. Their key is the client address
  resolved by the trusted-proxy rules — `audit.ClientAddress`, the same value the
  audit row records — so a caller cannot reset its bucket with a forwarding
  header. Each also carries a global counter so a deployment whose callers all
  appear to share one address (an unlisted reverse proxy) is still bounded.
- **A signed-in caller's work** is keyed by principal. Its buckets are
  `(principal, procedure)` plus one deployment-wide counter shared across every
  covered method, so one loud method cannot spend another method's quota while
  the deployment as a whole still has a ceiling.

Both are fixed windows: a key's window opens with its first allowed request and
lasts one minute, and a refused request does not extend or spend anything.

## What is budgeted, and where

| Where | Methods | Key | Per key | Global |
| --- | --- | --- | --- | --- |
| `ThrottleInterceptor` | `Login`, `Refresh` | resolved address | 120/min | 300/min |
| `ThrottleInterceptor` | `CreateUser` (signed-in callers exempt) | resolved address | 20/min | 100/min |
| `ThrottleInterceptor` | `Logout` | resolved address | 120/min | 300/min |
| `ThrottleInterceptor` | `CreateSSOState` | resolved address | 120/min | 300/min |
| `PrincipalThrottleInterceptor` | every authenticated audited method, except `ListAuditLogs`, plus `ExplainSQL` and `FetchLLMModels` | `user:{id}\|{procedure}` | 3000/min | 30000/min |
| `LoginLimiter` (handler) | `Login` failures | account, source | 10/5min, 20/5min | — |
| Device login (handler) | `CreateDeviceLogin` | resolved address | 10/min | 100/min |
| Device login (handler) | `GetDeviceLogin`, `ApproveDeviceLogin` | caller (`user:` or `ip:`) | 60/min | 600/min |
| MCP (`Server.invoke`) | tool calls | principal | 600/min | 6000/min |
| MCP (`Server.Handler`) | every request, before the bearer check | resolved address | 6000/min | 60000/min |
| `boundedRateLimiterStore` | OpenLineage ingestion | ingestion-key digest, else resolved address | 50/s, burst 100 | 10× |
| `boundedRateLimiterStore` | each `/oauth/*` route | resolved address | 10/s, burst 20 | 10× |

`CreateUser` exempts a signed-in caller on the anonymous interceptor because an
administrator may create members in bulk; those calls are counted by the
principal budget instead.

## Design decisions

- **A refused request leaves no ledger row.** Both Connect interceptors run
  before `AuditInterceptor`, so a request stopped by a budget never runs and never
  records. This is what makes the per-principal budget *backpressure on the
  caller* rather than a silent drop from a ledger that is kept forever: every
  action that happened is still recorded, and a caller that loops a ledger-writing
  method is told to slow down.
- **The budget covers ledger writes, not reads, and the LLM calls.** The audit
  annotation decides the set, so a method that gains `audit = true` later is
  budgeted without touching the interceptor. `ListAuditLogs` is exempt: it is the
  one audited method that only reads. It still writes a row per call, so a caller
  that loops *it* grows the ledger; that is the read path's amplification and is
  listed under residuals rather than silently bounded here.
- **`Logout` is budgeted even though it is idempotent.** Revoking the same token
  twice changes nothing, but every call still writes an audit row, and a token
  this server signed can be replayed for as long as the caller likes. It gets its
  own bucket rather than sharing Login's, so a token replay cannot spend the
  sign-in budget.
- **The MCP source budget is counted before the bearer check.** The per-principal
  tool budget cannot see a request that carries no or an invalid token — identity
  resolution refuses it first — so without the address budget an unauthenticated
  caller can drive token verification and a revocation lookup per invented token
  without limit. Its numbers are ten times the per-principal ones so it bounds
  probing without pacing a fleet of agents behind one NAT.
- **The plain-HTTP stores carry a deployment-wide bucket too.** Their
  per-identifier buckets are keyed by caller-chosen values (an ingestion key), the
  identifier ceiling bounds memory only, and a rotating caller reaches a fresh
  bucket per request; the shared counter is what bounds the total work.
- **Two limiter implementations, one per route family.** Connect methods use
  `state.WindowLimiter` (fixed window, key + global). The plain-HTTP routes use
  `server.boundedRateLimiterStore` (token bucket, key + global) because they go
  through echo's rate-limit middleware. They differ in smoothing, not in what they
  protect, and both cap their tracked identifiers.
- **OpenLineage ingestion and each OAuth route stay separate budgets.** Sharing
  one ceiling across the four anonymous OAuth routes would tighten that surface
  four-fold and is the open I2 question in the review, not a decision this work
  makes.
- **Every counter is process-local.** Like the rest of `backend/component/state`,
  replicas each hold their own window; a multi-replica deployment that needs one
  global ceiling must put a shared limiter in front of the API.

## Tests

- `backend/api/v1/principal_throttle_interceptor_test.go` walks the protobuf
  registry and requires every audited method to be bounded either per source or
  per principal; the two runtime allowlists (`principalLimitedProcedures`,
  `auditedReadExemptProcedures`) must match the test's copies exactly, so adding
  an exemption is a deliberate two-file change. The same file pins the per-key
  and global dimensions, that an anonymous caller and an unbudgeted method are
  untouched, and that a refused stream never reaches its handler.
- `backend/server/rate_limiter_test.go` pins the deployment-wide bucket, that a
  rotating caller is bounded by it, and that a refused request spends nothing.
- `backend/mcp/source_limiter_test.go` pins the address budget, its key
  resolution and that a switched-off surface is not counted.
- `backend/test/integration/runner/rate_limit_service_test.go` runs the real
  server: the `Logout` replay is refused past its budget, and an authenticated
  `FetchLLMModels` loop is refused past the principal budget (the method fails
  before any provider call, so it is thousands of cheap requests rather than
  thousands of rows).

## Residuals

1. **`ListAuditLogs` writes a ledger row per read and is not budgeted.** It is
   exempt as a read; bounding it would restrict ordinary reads and belongs with a
   policy about how much of the ledger a page may write, not with this budget.
2. **`BatchSyncInstances`, CSV export and similar bulk work share the per-method
   budget with interactive calls.** The shipped limit is generous (3000/min) so
   it does not pace work, which means it bounds a loop rather than a burst.
3. **The MCP row count is bounded by the call budget, not by the ledger's size.**
   A refused call is still audited (deliberate), so the per-principal budget caps
   rows per minute rather than rows in total.
4. **`ExchangeDeviceLogin` and `GetWorkspaceProfileSetting` carry no budget.**
   The first needs a 256-bit device code that only a budgeted call can mint and
   is held to the protocol's minimum poll interval; the second reads one setting,
   writes nothing and is not audited.
5. **Counters are per replica** (see above) and **not configurable**: the numbers
   are constants, so changing one is a code change with its test.
