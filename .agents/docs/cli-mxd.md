# Agent CLI (`mxd`) and device login — Reference

> Maintenance reference for `cli/` and the server's device-login side. User-facing usage lives in [cli/README.md](../../cli/README.md); deliberate acceptance decisions (clear-text credential file, scopes only in the process environment, process-local device state) are listed in [docs/security-posture.md](../../docs/security-posture.md); session rotation and connected apps in [refresh-tokens.md](refresh-tokens.md) and [connected-apps.md](connected-apps.md); directory rules in `cli/AGENTS.md`.

## What it is

A CLI aimed at agents (and usable by people) that talks ConnectRPC with a bearer JWT. It offers three capabilities:

1. **Metadata structure queries** over the existing `InstanceService`/`DatabaseService` RPCs, with no backend change.
2. **Lineage for arbitrary SQL** through `LineageService.AnalyzeSQL` (stateless, stores nothing); `lineage sql --depth N` then feeds the resolved real target into `GetLineageGraph`.
3. **Multi-level upstream/downstream lineage** through `LineageService.GetLineageGraph`, a server-bounded BFS that returns the whole graph in one call.

Authentication is **Device Login** (RFC 8628-shaped, but the platform issues its own JWT): the CLI calls `CreateDeviceLogin`, a person approves on `/device`, and `ExchangeDeviceLogin` returns a session credential pair bound to the approver's identity (7-day access token plus a sliding 30-day refresh token). That session is also a **connected app** (`kind='cli'`), so the user can see on Connected apps which machines run the CLI and revoke one; when the access token expires the CLI rotates the pair itself instead of asking for another approval.

`cli/` lives in the same Go module as the backend but is physically isolated, and `depguard` in `.golangci.yaml` allows only the standard library, cobra, connect, protobuf and `backend/generated-go` (`cli/AGENTS.md`). The stdout contract is **exactly one JSON document**; progress and errors go to stderr.

## Decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Location and artifact | `cli/` at the repository root; `make build-cli` produces `build/mxd` | Top level is organized by deliverable, and one binary is easy for an agent to install |
| Module | Same module as the backend | A separate module would need `replace` to import `backend/generated-go`, and `replace` does not apply to `go install <pkg>@version` |
| Dependency boundary | `depguard` makes "the CLI is only an API client" executable | A separate directory alone is not dependency isolation |
| Authentication | Reuse the existing JWT, signed for the approver | The permission model is unchanged (the CLI acts as the user); no OAuth client registration is introduced. `--service-account` reuses `AuthService.Login` for a 1-hour API token |
| Device session storage | Process memory (a store on `state.State`, lazy TTL) | Matches `SSOStateCache`; a 10-minute lifetime does not justify a migration. The cost is a single replica or sticky routing |
| Confirmation link | Print the link **with the code** by default; `--no-prefill-url` prints the bare address; when the server has no `external_url` the CLI falls back to `<server>/device` | A bare address forces the user to copy the code by hand; the real defense is the confirmation page showing client, source IP, time and requiring an explicit click |
| Code format | `user_code` 40 bits, `device_code` 256 bits; resource name `deviceLogins/{user_code}` | Only `user_code` appears in pages and URLs; `device_code` never enters a URL or the audit ledger |
| Multi-level lineage | A server-side BFS RPC, `GetLineageGraph` | Avoids N+1 calls from the CLI |
| Graph bounds | Full pagination per node plus three ceilings: 500 nodes, 10000 edges, a wall-clock budget | `column_lineage` has no per-object edge-count invariant |
| SQL lineage | A stateless RPC, `AnalyzeSQL` | Agents need an immediate answer; creating a ManualSQL is a write and asynchronous, a different semantic |
| Scope source | Only the `METAXISDATA_SCOPES` process environment; `--scope` overrides per call; the CLI never stores, manages or infers scopes | The environment gives same-machine agents their own project context; a file would be overwritten by whichever agent ran last |
| Multi-scope semantics | Parse the same SQL independently per scope, return results grouped by scope, never merge contexts | Scopes may target different engines; merging would invent cross-environment edges |
| GUID reachability | `Database.guid` and `StoredMetadata.guid` are OUTPUT_ONLY fields | A scope needs an exact GUID, so only the server may supply it; the user copies it and the CLI treats it as opaque |
| Pagination | Every list command pages automatically, `--max-items` defaults to 1000, and hitting it emits `truncated` plus `nextPageToken` | Server defaults are 10/50; silent truncation is worse than an error |
| JSON style | `protojson` with `EmitUnpopulated: true`, including nested messages in envelopes | Matches audit, JSONB and REST; the document shape does not change with the data |
| Temp relations | `lineage sql` hides `is_temp` targets by default; `--include-temp` reveals them; an emptied scope reports `tempRelationsHidden` plus a hint | A bare SELECT's temp target is the answer, so an empty result must not be silent |
| Server address | `auth login --server <url>` persists it; resolution order is `--server` > `METAXISDATA_SERVER` > credential file > **error** | Self-hosted servers rarely move; no guessed default and no localhost probing |
| Token storage | The CLI never prints a token; it writes a 0600 credential file | Agent logs may be forwarded or pasted |
| Session renewal | Device login signs access and refresh together; on expiry the CLI calls `ExchangeRefreshToken`, gets a new pair and writes it back | A human approval every 7 days is a real cost for an agent; the rotated old refresh token dies immediately, bounding exposure |
| Renewal trigger | Lazy: one RPC returns `Unauthenticated`, the CLI renews once and replays once | Same shape as the SPA session interceptor, and no command writes the file unnecessarily |
| Cross-process concurrency | An in-process mutex single-flights the rotation; a cross-process loser rereads the file, adopts the winner's pair and retries 5×100 ms | Same-machine agents share one file; rotation is single-use, so not rereading would misreport a live session as dead |
| Logout scope | `auth logout` ends only **this machine's** connection (the server revokes the grant named by the token's `gid`) and keeps the server | One CLI record is one machine; signing out one machine must not sign out the others |
| `--service-account` | Still a 1-hour API token with no refresh; writing it clears the old session fields | It is not an interactive session, and a leftover refresh token could later rotate into a different identity |

**Deliberately not done (reviewed, not backlog)**: `AnalyzeSQL` is called **serially** per scope, because ten scopes are a few small queries and one CPU parse, and concurrency would only add a shared failure mode. `DeviceLoginStore.pruneLocked` scans the map instead of maintaining insertion order, because the ceiling is ten thousand entries and it runs only when a create finds the store full. `cli/cmd` keeps parse results in package-level state, so its tests cover the pure functions rather than running commands. Besides the node and edge ceilings, `GetLineageGraph` has a wall-clock budget, and exhausting it also reports `truncated`; the edge ceiling is enforced inside each object's read (the fetcher receives the remaining budget and probes one edge beyond it), so reading one hub's entire edge set cannot pull it into memory.

## Output contract and exit codes

stdout carries exactly one JSON object (protojson lowerCamelCase, two-space indent); `--format table` exists for people (column widths inferred by tabwriter); errors go to stderr as `{"error":{"code","message","hint"}}`. `--format mermaid` does not exist.

Exit-code **semantics** (`client.ExitCode`), which an agent branches on:

- **0** success.
- **1** bad argument or bad local input: a `UsageError` (`server_required`, `scope_required`, `config_invalid`) or a `CodeInvalidArgument`/`CodeFailedPrecondition` from the server.
- **2** unauthenticated: the access token was refused and renewal was also refused, or the device session is gone (`device_session_expired`). The next step is always `mxd auth login`.
- **3** resource not found; **4** permission denied; **5** any other server error; **6** timeout or cancellation, local or remote.

The error vocabulary (`output.CodeOf`): `unauthenticated`, `permission_denied`, `not_found`, `invalid_argument` (both InvalidArgument and FailedPrecondition), `timeout`, `resource_exhausted`, `already_exists`, `unavailable`, `unimplemented`, `internal`, plus the CLI-local `server_required`, `scope_required`, `config_invalid` and `device_session_expired`. `CodeUnauthenticated` first attempts one session renewal; if renewal is refused it returns the original error (exit 2) and **does not retry or loop**.

## Address and credential resolution

| Item | Source (high → low) | Persisted? |
| --- | --- | --- |
| server | `--server` > `METAXISDATA_SERVER` > credential file > **`server_required`** (exit 1) | `auth login` writes it together with the token |
| token | `--token` > `METAXISDATA_TOKEN` > credential file | `auth login` writes it |
| refresh token | **Credential file only, and only while the effective server matches the server recorded in that file** (a `--token`/environment override or a different `--server` disables renewal) | `auth login` and every rotation write it |
| scope | `--scope` > `METAXISDATA_SCOPES` | **Never written to any file** |
| `--ca-cert` / `--insecure` | Flags only | Not persisted; they describe the current network path |

Resolution lives in `cli/cmd/root.go`'s `resolve`. Renewal is enabled only when the token came from the credential file and the effective server equals the recorded one: a `--token`/`METAXISDATA_TOKEN` value is not on disk, and renewing it would silently swap the caller's identity for the file's; a different `--server`/`METAXISDATA_SERVER` host should not receive a 30-day refresh token. `client.New` normalizes the address through `normalizeServer`: an `http`/`https` scheme is required, the trailing `/` is dropped, and userinfo, query and fragment are refused; invalid input becomes `invalid_argument` (exit 1). There is no default localhost and no port probing.

**Credential file**: `$XDG_CONFIG_HOME/metaxisdata/config.json` by default (`config.Path`), or another file through `--config`/`METAXISDATA_CONFIG`. It is created `chmod 0600`, written to a temporary file with the final mode and renamed (`config.Save`). It holds `server`, `token`, `refreshToken`, `user`, `tokenExpiresAt` and `refreshExpiresAt` — **no scope** — and a credential file without refresh fields degrades to signing in again after 7 days.

**`auth login` write semantics** (`storeLogin`):

- Resolve the effective address, then persist the server and the whole session pair as **one atomic write**; updating only the token while the recorded address belongs to another server makes every later request fail with a silent 401.
- When the effective address differs from the file's, print `server changed: <old> -> <new>` on stderr; no interactive confirmation.
- Print the credential file path actually written on stderr (for multi-agent machines) and **never read or write scope**.
- On success, stdout receives `{"status":"approved","server","user","tokenExpiresAt","refreshExpiresAt"}` (the refresh field is omitted for an API token).
- The `--service-account` path calls `AuthService.Login` with the key from `METAXISDATA_SERVICE_KEY`, then explicitly clears the refresh fields.
- **`auth logout` first calls `Logout` with the current token; only when that returns 401** (an expired or unverifiable access token) **and a renewable session is held does it renew once and retry**, then clear token/refresh/user/expiry while keeping the server. A refused renewal (`ErrSessionExpired`, the session is gone) makes local cleanup the whole job; a network or disk failure during renewal is reported as such instead of claiming "signed out".

**`config show`** (read-only) echoes `server{value,source}`, `token{value:"[REDACTED]",source}`, `refreshToken{value:"[REDACTED]",source}` (`none` when renewal is disabled), `configFile`, `scopes[{name?,guid}]` and `scopeSource` (`env:METAXISDATA_SCOPES` or `none`).

## Execution scopes

`catalog.AnalysisContext{InstanceID, Database, Schema}` is the only source for resolving unqualified names: SQL text names an object at most down to `database.schema.table`, and **the instance always comes from the context**. A scope is therefore mandatory; there is no "guess one" fallback.

- The only source is the `METAXISDATA_SCOPES` process environment: comma-separated entries, each a `guid` or `name=guid` (`name` only attributes output). Example: `dev=1;shop,prod=9;shop;public`.
- It is evaluated **per command invocation**, never written to a file or cache, and disappears when the process exits.
- `--scope <name|guid|all>` overrides per call: a value containing `;` is treated as a GUID, any other value must match a configured name, and `all` selects every scope (`env.Select`).
- Missing or empty means no scope. A missing scope fails with exit 1, `code=scope_required`, and a hint that gives the format and the commands that print GUIDs, so an agent can relay it verbatim.

**Multi-scope semantics:** several scopes mean the same SQL is parsed independently several times, not merged; output is **always grouped by scope** (the shape keeps `results[]` even for one scope). Real cross-environment flows are not discovered by selecting more scopes — a scope decides how unqualified names land, it does not create edges. `GetLineageGraph` needs no scope because its root is an absolute GUID.

**Where GUIDs come from:** `mxd database list` prints `guid` per row (MySQL family); `mxd meta list <db-guid> --type SCHEMA` prints `guid` per row (PostgreSQL). The CLI treats a GUID as an opaque string and never assembles, splits or infers one.

## Device login

```text
CLI ── CreateDeviceLogin {clientName,clientVersion} ──► server generates device_code(256 bit) + user_code(40 bit)
    ◄── device_code, user_code, verification_uri(complete for prefill), expires_in=600, interval=3
    stderr prints the URL and code (no browser launch with --no-browser)
Browser /device: GetDeviceLogin details → ApproveDeviceLogin → PENDING→APPROVED (binds approver user_id, audited)
CLI ── ExchangeDeviceLogin {device_code} ──► first APPROVED exchange: create CLI grant (kind=cli), sign JWT, consume entry
    ◄── token + refresh_token + user + expires_in/refresh_expires_in; written to the credential file, one JSON to stdout
CLI ── ExchangeRefreshToken {refresh_token} ──► atomic grant rotation (sliding 30 days)
    ◄── access + new refresh; written back (happens automatically after an RPC 401, no stdout)
```

**State machine**: `PENDING --approve--> APPROVED --first exchange--> CONSUMED (entry deleted)`; `PENDING --deny--> DENIED`; TTL expiry → `EXPIRED` (lazily cleaned). Approval binds the approver's `user_id`; exchange creates the connected-app grant, signs a **CLI access token** (user API audience, `cid=mxd`, `gid`, last-login update) and returns the refresh token. Exchange is one-shot: polling it again yields `CodeNotFound`, which the CLI translates to `device_session_expired` (exit 2) rather than a generic "not found".

**Server bounds** (`state.DeviceLoginStore`): TTL 10 minutes, minimum poll interval 1 s, recommended interval 3 s, capacity 10000. `user_code` is eight Crockford base32 characters in `XXXX-XXXX` form (the alphabet omits I, L, O and U; typed input normalizes O→0 and I/L→1) and `device_code` is 43 base62 characters; generation checks uniqueness and retries a collision five times (`ErrDeviceLoginCodeTaken`).

## Invariants

1. **Server and the whole session pair are one atomic write**: `auth login` must write both; splitting them yields "token belongs to B, address says A" failures.
2. **Scope never touches disk**: it is read only from the process environment. Persisting it would let two same-machine agents overwrite each other, silently.
3. **stdout carries exactly one JSON document**: progress, hints and human text go to stderr; `--format table` also puts only the table on stdout.
4. **Neither token is ever printed**: both go only into the 0600 file, and `config show` echoes `[REDACTED]`.
5. **A token never leaves the server host**: the HTTP client refuses a redirect that leaves `--server` (including a subdomain or an https→http downgrade) while keeping the standard library's 10-hop cap; the bearer transport attaches the header only to the same host (`newTransport`).
6. **Device state is process-local**: create, approve and exchange must reach the same replica, or an approval lands elsewhere as NotFound; the deployment needs one replica or sticky routing (`docs/security-posture.md`).
7. **Exchange is one-shot**: the entry is deleted as soon as the credential is issued; `CodeNotFound` becomes `device_session_expired` (exit 2).
8. **Approver eligibility matches the web session without re-evaluating the password policy**: only END_USER, not deactivated, and passing domain restrictions (`validateApprover`); device login and OAuth consent share one implementation. Re-checking the password policy would lock SSO-only users out.
9. **`device_code` never reaches audit**: the audit interceptor records request and response and its redaction list contains `devicecode`/`device_code`; `userCode` is not a secret and is not redacted.
10. **Account state is re-checked at exchange**: the account may be deactivated between approval and exchange, so the issuer reloads the user and reruns `validateApprover` before signing.
11. **Rate limiting has two layers**: `CreateDeviceLogin` uses a source-address plus global budget on the interceptor chain (`limiterFor`), `GetDeviceLogin`/`ApproveDeviceLogin` count per caller, and an `Exchange` poll under 1 s returns `CodeResourceExhausted` (`slow_down`).
12. **`user_code` is one-to-one**: the `DeviceLogin` resource name is `deviceLogins/{user_code}`; generation checks uniqueness and retries.
13. **A GUID is an opaque string**: the CLI passes through server-issued `guid` values and never parses or assembles them.
14. **`lineage sql --depth N` passes N unchanged** into `GetLineageGraph` (`expandScope` sets `graphDepth := depth`), not N−1.
15. **Renewal starts only from a file-backed refresh token**: `--token`/`METAXISDATA_TOKEN` never renew and never write the file; `--service-account` clears the refresh field on write, so a later 401 cannot silently rotate into a previous identity.
16. **A refresh token is single-use**: an in-process mutex makes concurrent callers share one rotation; a cross-process loser rereads the file and adopts the winner's pair (`adoptFromDisk`), retrying 5×100 ms before declaring the session dead. On failure the file is deliberately **not** cleared, because another process may be writing its new credential.
17. **Failed renewal does not loop**: each request renews and replays at most once; a refused renewal (`ErrSessionExpired`) returns the original 401 (exit 2), while a renewal network/disk error is reported as itself (exit 5).
18. **`auth logout` revokes only this machine's grant**: the server revokes the grant named by the access token's `gid` (the `identity.ClientID == "mxd"` branch); other machines stay signed in, and web/MCP logout semantics are unchanged.

## Failure modes

| Scenario | Behavior |
| --- | --- |
| Fresh machine with no address | `server_required`, exit 1, hint points at `mxd auth login --server <url>` |
| No scope | `scope_required`, exit 1, hint gives the format and the GUID commands |
| Access token expired or revoked | Renew with the refresh token and replay once; on success the command returns normally (stdout unchanged) |
| Session revoked, refresh expired, or password changed | Renewal refused → `unauthenticated`, exit 2 → `mxd auth login`; the file is kept and the next command tries again |
| Another process rotated the credential file | Reread the file, adopt the new pair and replay; the user notices nothing |
| Device session consumed, denied or expired | `device_session_expired`, exit 2 |
| Invalid address scheme or corrupted credential file | `invalid_argument` / `config_invalid`, exit 1 |
| Server redirects to another host or downgrades TLS | Refused, and the error names both hosts |
| Listing hits `--max-items` | `"truncated": true` plus `"nextPageToken"`; the agent decides whether to continue |
| A scope's engine cannot support `lineage sql` | That scope returns empty relations plus a scope-level warning and the request still succeeds (unless every scope failed the same way) |
| Bare SELECT | No real target; `is_temp=true` relations are hidden by default and the output reports `tempRelationsHidden` with a hint |
| Analyzer cannot fully model the statement | Relations are still returned with `diagnostics` (`not_modelled`/`unresolved_reference`/`ambiguous_reference`/`catalog_unavailable`); a parse error fails that whole scope |
| `GetLineageGraph` exceeds a ceiling | `truncated=true` states the fact, but there is no `next_page_token`; a real continuation would need a frontier snapshot as the cursor |

## Where things live

- CLI: `cli/main.go`; commands in `cli/cmd/` (root, auth, config, instance, database, meta, lineage, pagination, skill); client and redirect policy in `cli/client/client.go`, session renewal and the 401 interceptor in `cli/client/session.go`; credential file in `cli/config/config.go`; environment and scope parsing in `cli/env/env.go`; rendering in `cli/output/output.go`; device flow in `cli/authflow/device.go`; embedded agent skill in `cli/skill/` (`SKILL.md` is a runtime `//go:embed` asset).
- Server device login and sessions: `backend/api/v1/auth_service_device_login.go` (four RPCs, code generator, `validateApprover`, token issuance), `auth_service_exchange_refresh_token.go`, `backend/api/oauth/cli.go` (CLI grant create/rotate), `backend/api/oauth/grant.go` (rotation and principal re-check shared with MCP), `backend/api/auth/grant_token.go` (`GenerateCLIAccessToken`), `backend/component/state/device_login.go` (store, state machine, TTL, limiter keys), `backend/api/v1/throttle_interceptor.go`; proto `proto/v1/v1/auth_service.proto`.
- Lineage RPCs: `backend/api/v1/lineage_service_analyze.go`, `lineage_service_graph.go`; proto `proto/v1/v1/lineage_service.proto`; GUID outputs in `proto/v1/v1/database_service.proto`.
- Frontend confirmation page: `pages/DeviceLoginPage.vue` (route name `DeviceLogin`), `api/device-login.ts`, locales.
- Gates: `gofmt` plus `golangci-lint run --allow-parallel-runners` (including `depguard`); `go test ./cli/...` (`cli/client/client_test.go` has `TestExitCode` and `TestNormalizeServer`, plus `cli/env`, `cli/config`, `cli/authflow`, `cli/cmd`); the real-server end-to-end suite is `make test-integration-smoke` over `backend/test/integration/runner/agent_cli_service_test.go` (`TestDeviceLoginRealServerIntegration` covers authorize→approve→exchange, with denied, CLI-session listing/revocation/rotation/per-device logout, `AnalyzeSQL`, GUID and lineage-graph cases).
