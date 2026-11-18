# MCP server and OAuth 2.1 authorization — reference

One process is both the MCP resource server (`/mcp`) and the OAuth 2.1 authorization server those clients register and authorize against. The resource server exposes the metadata registry,
column-level lineage and data-quality surface as read-only tools; the authorization server is plain HTTP, because OAuth clients do not speak ConnectRPC. Related: [agent-platform.md](agent-platform.md)
(the tool bodies under `backend/component/tools/`), [docs/mcp.md](../../docs/mcp.md) (the operator and integrator guide, which this file must not duplicate),
[security-posture.md](../../docs/security-posture.md) (accepted trade-offs), and [backend/mcp/AGENTS.md](../../backend/mcp/AGENTS.md). The SDK is `github.com/modelcontextprotocol/go-sdk` v1.8.0.

## Shape and decisions

`/mcp` is one stateless Streamable HTTP endpoint answering JSON: the 2026-07-28 protocol revision requires statelessness, which removes session hijacking and replica affinity, at the cost of the
server-to-client requests a session would allow. The data plane adds no new RPC: a tool calls the existing ConnectRPC handler in-process, against the same service instance the API mounts. The chain is
401 challenge → protected-resource metadata → authorization-server metadata → dynamic registration → browser authorization with PKCE and `resource` → consent → code → token exchange → per-request
bearer verification → tool (addressing, IAM, handler, rendering, audit).

| Decision | Choice | Why |
| --- | --- | --- |
| Transport | Streamable HTTP only, stateless | no stdio semantics; statelessness is a protocol requirement and needs no replica affinity |
| Authorization server | same process as the resource server | self-hosted with no external dependency; one process mints the audience-bound token, verifies it and revokes it |
| Registration | dynamic (RFC 7591), public clients (`none`) | DCR needs no pre-shared secret, and the token endpoint requires PKCE, so a client secret would only add a credential that can leak |
| Credentials | JWT access token bound to the MCP resource, plus an opaque rotating refresh token | one audience-bound access credential, one renewable credential |
| OAuth scope | one coarse `metaxisdata.mcp.read` | the real authorization is platform IAM per tool call; finer scopes would create a second ACL |
| Audience | `/mcp` accepts only `<canonical external_url>/mcp`; ConnectRPC keeps `mt.user.access.<mode>` | RFC 8707 requires audience binding; the two directions stay isolated |
| Tool implementation | in-process call of the existing handler methods | those read handlers do not depend on interceptor context, so MCP runs the same code; MCP supplies the context user, the IAM check and the audit row itself |
| Capability surface | read-only, gated by `mcp_enabled` (default false) | a new attack surface is opt-in; writes need confirmation semantics and a separate design |
| Quality surface | eight read-only tools: the per-object signal plus the directory, one target, the issues ledger, executions, one run's evidence, lineage impact and consumption coverage; no LLM explain | the MCP caller is the model, so it reasons over the evidence itself; an explain tool would add LLM quota and fight the endpoint's time budget |
| Consistency guards | a tool's permission must equal its RPC annotation and be read-only; output shares field and enum names with the CLI but not its rendering | `backend/mcp/tool_guard_test.go`; omit unset fields and return projection rows |

The refresh token is the durable half of an authorization: opaque, single-use, rotated on every use, stored only as a SHA-256 digest. Rotation updates the grant row in place, so its id survives and
serves as both the connected app's public identity and the access token's `gid` claim; concurrent refreshes serialize on that row and exactly one wins. The grant is pinned to the consented resource
and scope, and every rotation re-checks the principal (exists, not deactivated, not older than the last password change), revoking the row explicitly when that fails. `Logout` revokes rather than
deletes every connection the token's client holds for the user, and maintenance prunes rows once the refresh credential has been expired for a week.

## Tool surface

The 17 read-only tools are defined by `toolDefinitions` in `backend/mcp/tool.go`: name, description, input schema, required permission, the RPC it wraps and its projection. Tool bodies live in
`backend/component/tools/{metadata,lineage,quality}` and are shared with other consumers. The catalog and model-facing semantics belong to the guide prompt and [docs/mcp.md](../../docs/mcp.md); do not
restate them here. Addressing is shared: an object is `{instance, database, schema?, name}` or a returned `guid`, resolved by `metadata.Resolver`, which returns candidates rather than guessing and
also drives scope resolution.

## Authorization endpoints

| Endpoint | Method | Authentication | Notes |
| --- | --- | --- | --- |
| `/.well-known/oauth-protected-resource` (and path-insertion form) | GET | anonymous | RFC 9728; `resource` is the canonical external URL plus `/mcp`, one authorization server, one scope, bearer transport `header` |
| `/.well-known/oauth-authorization-server` | GET | anonymous | RFC 8414; both grants, S256 only, `none` client authentication, RFC 9207 `iss` declared; deliberately no `jwks_uri` |
| `/oauth/authorize` | GET | browser session | validates client and redirect URI first, then response type, PKCE, `resource` and scope; records a pending request and redirects to the SPA consent page |
| `/oauth/authorize/complete` | GET | browser session | mints the code and redirects it to the client; denial returns `access_denied`; only the approving user may complete |
| `/oauth/token` | POST (form) | anonymous, PKCE | `authorization_code` and `refresh_token`; `resource` is required and must match this deployment; errors follow RFC 6749 |
| `/oauth/register` | POST (JSON) | anonymous, rate-limited | RFC 7591, public clients only, redirect URIs https or loopback; no registration access token |
| `OAuthService.GetOAuthAuthorizationRequest` / `.ApproveOAuthAuthorizationRequest` | ConnectRPC | signed-in, no permission annotation; approve is audited | what the consent page reads and writes; no code, PKCE challenge or client state in the read; approver eligibility uses the device-login rule |

Both consent RPCs must stay in `unannotatedMethods` in `backend/api/v1/acl_interceptor_test.go`, or `TestEveryMethodIsPermissionGated` fails.

## Tokens and claims

| Claim | Value | Enforced by |
| --- | --- | --- |
| `aud` | canonical MCP resource URI | `/mcp` verifies it; the user-API verifier accepts only its own audience, so neither token works on the other surface |
| `scope` | the granted scopes | the transport middleware checks the token's own scope; a missing scope is 403 |
| `cid` | client id | `Logout` revokes every connection that client holds for the user |
| `gid` | grant row id | per-request liveness check; an MCP token without `gid` is refused |
| `iss` | the constant `metaxisdata` | distinct from the RFC 8414 issuer and the RFC 9207 `iss`, which are the external URL |

`sub` is the user id and becomes `TokenInfo.UserID`; `exp` is the access-token lifetime, checked with a 30-second clock skew; `rst` is unused for MCP and reserved for restricted tokens such as a
password-reset credential. Verification reuses the identity chain in `backend/api/auth/authenticator.go`: signature, algorithm, issuer, audience and expiry → `jti` revocation → connection liveness →
user exists → not deactivated → password-change cutoff. The connection check runs only for tokens carrying `gid`: first the process-local `GrantRevocationCache` (30-second TTL), then
`IsOAuthGrantLive` bounded by grant id, user and client, requiring neither revoked nor expired; a missing row is not live, and a read failure fails closed. Without this chain a deactivated user's MCP
token would keep working, and a revoked connection's access token would remain valid until expiry.

## Pending requests and registrations

- **Process-local**: `backend/component/state/oauth_authorization_request.go` holds pending requests and issued codes with the lifecycle `PENDING → APPROVED → COMPLETED` or `DENIED`. A request lives
  ten minutes and the store is bounded. The code is minted at completion, single-use, valid for 60 seconds and delivered only through a 302 redirect; completion requires the approving user, the
  exchange re-checks client, redirect URI, PKCE and resource, and reuse is refused as an attack. Approval and completion must reach the same replica.
- **Persisted**: `oauth_client` through `backend/store/oauth_client.go`, and the grant row through `backend/store/oauth_grant.go` (its table predates the Go name and is still `oauth_refresh_token`).
  One row is one authorization and one connected app; `ConnectedAppService` reads the table and joins the client for its display name. Anonymous input is bounded: redirect URI 2 KiB, client `state`
  512 bytes, PKCE challenge exactly the 43-character S256 digest, scope 256 bytes.

## Mounting and cross-cutting concerns

Plain HTTP routes sit outside the Connect interceptor chain and do their own authentication, rate limiting, audit and logging. Routes are mounted in `backend/server/grpc_routes.go`; identifiers come
from `oauth.WorkspaceEndpoints`, read per request, so toggling the surface or moving the external URL needs no restart. Off means 404; on with a missing or invalid `external_url` means 503 and no
metadata.

- The anonymous OAuth routes each carry their own per-address ceiling (10 requests per second, burst 20, 30-second timeout); metadata documents are not rate-limited.
- `/mcp` counts requests against the source address before the bearer check, then runs the SDK bearer middleware; `challengeWriter` supplies the per-request `resource_metadata` URL and the RFC 6750
  error code the SDK cannot know. The handler is wrapped in `http.NewCrossOriginProtection` and a 60-second `http.TimeoutHandler`.
- Audit: one row per tool call with method `mcp/tools/call:<tool>`, the actor, status and severity, latency and request metadata. Arguments are redacted and bounded (8 KiB per string, 256 KiB per
  payload, and an argument tree over 64 KiB replaced by a marker). Each of the four `/oauth/*` routes writes one row per request, except that a 404 for a switched-off surface writes none; codes and
  tokens are never recorded. A tool call also updates the connected app's `last_tool_call_at`, throttled to one write per five minutes.

## Security invariants

1. **Audience binding.** `/mcp` accepts only the MCP audience; a CLI or web token there is 401, and an MCP token on the user API is 401.
2. **No token passthrough.** An inbound token is never forwarded upstream; tools read the local store.
3. **PKCE is mandatory and S256-only.**
4. **Redirect URIs match exactly.** The only relaxation is a loopback port; wildcards, userinfo and fragments are refused.
5. **`external_url` is the only source of issuer and resource.** It is never derived from the request `Host`; plain `http` is allowed only on loopback, and a missing setting fails loudly instead of
   guessing.
6. **`iss` travels with every authorization response**, success and error alike, and the metadata document declares support.
7. **The two authorization layers report separately.** Transport failures are HTTP 401/403 with `WWW-Authenticate`; an IAM refusal is an in-band result with `isError` and `permission_denied`, so the
   client can re-authorize and the model can see the cause.
8. **Anonymous endpoints count per source address, not per client.** A `client_id` is self-reported at registration, so bucketing on it would hand an attacker a new key.
9. **Consent is explicit.** The page shows the client name, redirect target, resource, scope, creation time and source address; approver eligibility matches a device login; no token or code ever
   appears on the page.
10. **Everything is audited.** OAuth protocol events and every tool call, refusals included, reach the permanent ledger; redaction covers `code_verifier`, `client_secret`, authorization codes and
    device codes.
11. **`/mcp` is not exposed cross-origin.** CORS is opt-in and off by default in production (metadata documents are the only responses with `Access-Control-Allow-Origin: *`), cross-origin protection
    is applied explicitly because the SDK applies none, and DNS-rebinding protection stays at its default.
12. **Revocation reaches both credentials.** Any token this server signed can be revoked by provenance without checking its audience; a token carrying `gid` is refused once its connection is revoked,
    immediately on the revoking replica and within 30 seconds elsewhere; `Logout` revokes the client's connections rather than deleting refresh credentials. Tokens minted before `gid` existed are
    refused once and recover with a refresh. Revocation and rotation serialize on the same row.
13. **A switched-off surface publishes nothing.** `/mcp`, both metadata documents and every `/oauth/*` route answer 404.

## Failure modes

| Situation | Behavior |
| --- | --- |
| no token, or a token that is invalid, expired or revoked, or whose connection is revoked or pruned | 401 with `WWW-Authenticate: Bearer resource_metadata=…, scope=…, error="invalid_token"` |
| an MCP access token minted before `gid` existed | 401 `invalid_token`; one refresh recovers it, and only an expired refresh token needs the browser again |
| token missing the scope | 403 with the same challenge shape and `error="insufficient_scope"` |
| `mcp_enabled` off | endpoint, both metadata documents and `/oauth/*` are 404; no audit row |
| enabled but `external_url` missing or unusable | 503, with no metadata published |
| a tool the caller's IAM does not allow | HTTP 200, `isError` true, `code=permission_denied`; audit row WARNING |
| a name is ambiguous or unknown | `isError`, `ambiguous` or `not_found`, with `details.candidates` |
| `analyze_sql` without a scope | `scope_required` with the existing databases under `details` |
| no lineage analyzer for the engine, or an object never synced | `unsupported` (neither a bad argument nor a transient fault) |
| a tool call exceeds 60 seconds | `{"error":"temporarily_unavailable",...}` |
| request body over 2 MiB | rejected by the transport |
| per-principal call budget exhausted | tool-level `resource_exhausted`; the refusal is still audited |
| per-address request budget exhausted | 429 with `Retry-After: 60`, before bearer verification |
| cross-site POST to `/mcp` | rejected by cross-origin protection |
| refresh reuse, expiry, another client, a moved resource, a revoked connection | `invalid_grant`; an expired row is kept for the management page, a revoked one mints nothing |

## Where things live

- Resource server and tools: `backend/mcp/server.go` (config, handler, dispatch, audit, metrics), `tool.go` (definitions and projection helpers), `tool_{metadata,lineage,quality}.go` (delegation),
  `ref.go`, `errors.go` (the error envelope), `auth.go` (`NewTokenVerifier`), `guide.go` with `guide.md` (the `metaxisdata_guide` prompt).
- Authorization server: `backend/api/oauth/resource.go` (identifiers), `metadata.go` (both documents and the switch), `authorize.go` (PKCE, redirect and scope validation, completion), `token.go` (both
  grants), `register.go` (dynamic registration), `grant.go` (rotation and principal re-check), `server.go` (session and error rendering), `audit.go`, `cli.go`.
- Consent and connected apps: `backend/api/v1/oauth_service.go` with `proto/v1/v1/oauth_service.proto`; `backend/api/v1/connected_app_service.go`, `filter.go` (`parseConnectedAppFilter`) with
  `proto/v1/v1/connected_app_service.proto`, and the frontend pages under `frontend/src/pages/`.
- Tokens, state and storage: `backend/api/auth/mcp_token.go`, `grant_token.go`, `refresh_token.go`, `authenticator.go`; `backend/component/state/oauth_authorization_request.go` and the
  `GrantRevocationCache` in `state.go`; `backend/store/oauth_client.go`, `backend/store/oauth_grant.go`.
- Settings: `mcp_enabled` and `external_url` in `proto/store/store/setting.proto`, validated in `backend/api/v1/setting_service.go`, set on the frontend general settings page; the consent page is
  `frontend/src/pages/OAuthConsentPage.vue` with `frontend/src/api/oauth.ts`.
- Tests: `go test ./backend/mcp/... ./backend/api/oauth/... ./backend/api/auth/... ./backend/store/...` is hermetic. `backend/mcp/tool_guard_test.go` pins tool permissions and `sdk_contract_test.go`
  pins the SDK behaviors relied on; also `backend/store/oauth_grant_test.go`, `backend/api/auth/authenticator_test.go` and `backend/api/v1/connected_app_test.go`. `make test-integration-smoke` runs
  `backend/test/integration/runner/mcp_service_test.go` against a real server: authorize → list → revoke → immediate 401 → refresh `invalid_grant` → idempotent repeat, plus concurrent refreshes
  leaving exactly one winner.
