# Connecting an MCP client

Metaxisdata can serve its metadata registry, its lineage and its data-quality targets,
problems and evidence to a Model Context Protocol client as a set of read-only tools. The
endpoint is `/mcp`, and the same server acts as the OAuth 2.1 authorization server those
clients register and authorize against. Nothing here writes. The decisions behind it,
including behavior that looks like a bug, are in [security posture](security-posture.md).

## Before it will answer

Two workspace settings, in this order:

1. **External URL** (Settings → General). It is the issuer and the resource identifier the
   whole flow is built from, so it must be the address clients reach this deployment at. It
   must be `https`, except on a loopback address, and it may carry a path prefix if the
   server is mounted under one.
2. **`mcp_enabled`** (same page). Off by default, and the server refuses to turn it on until
   the external URL is set. While it is off the endpoint, both metadata documents and every
   `/oauth/*` route answer `404` — a disabled deployment advertises nothing.

## Configuring a client

An MCP client that speaks streamable HTTP takes the endpoint URL. For example:

```json
{
  "mcpServers": {
    "metaxisdata": {
      "type": "http",
      "url": "https://mx.example.com/mcp"
    }
  }
}
```

The first request has no token, so the client is told where to authorize and then does the
rest itself:

1. `POST /mcp` answers `401` with `WWW-Authenticate: Bearer
   resource_metadata="https://mx.example.com/.well-known/oauth-protected-resource",
   scope="metaxisdata.mcp.read", error="invalid_token"`.
2. The client reads the protected-resource metadata, follows `authorization_servers` to the
   authorization-server metadata, and registers itself with `POST /oauth/register` (public
   clients only; PKCE is mandatory).
3. The client opens `GET /oauth/authorize` in a browser with a PKCE challenge and
   `resource=https://mx.example.com/mcp`. A signed-in user decides on the `/oauth/consent`
   page, which shows the client's name, where the code would be sent, the resource and the
   scope. Nothing is approved by opening the page.
4. The browser completes the flow at `GET /oauth/authorize/complete`, which mints the code
   server-side and redirects it to the client — the code never passes through the page.
5. The client exchanges the code at `POST /oauth/token` and presents the resulting access
   token on every later `/mcp` request. The same response carries a refresh token, exchanged
   at the same endpoint when the access token expires.

The access token is bound to `<external URL>/mcp` as its audience: a web or CLI session
token is refused at `/mcp`, and an MCP token is refused on the ConnectRPC API. Changing the
external URL therefore invalidates outstanding MCP tokens and grants, and the client
authorizes again.

Refreshing is a rotation, not a renewal of the same credential: presenting the refresh token
with `grant_type=refresh_token`, `client_id`, `resource` and the consented `scope` returns a
new access token plus a new refresh token, and consumes the presented one. The grant row is
updated in place, so two clients racing with the same token leave exactly one working, and a
copy stolen from a client's storage is worthless once the real client refreshes. The refresh
credential lives 30 days, sliding on each rotation, and every rotation re-checks the user's
account, so a deactivated user or one who changed their password since consent has to
authorize again. The grant row is the record managed under **Connected apps**, which is why
its id is stable across rotations.

A token minted before the connection id existed is refused once; a client with a valid
refresh token recovers on its next refresh, and only an expired refresh token needs the
browser flow again.

## The tools

| Tool | Answers |
| --- | --- |
| `list_instances` | which instances are registered |
| `list_databases` | which databases an instance has, with the GUIDs that identify them |
| `search_metadata` | where a name lives, when you only have the name |
| `list_metadata` | what is directly under an object (schemas, tables, views, columns) |
| `get_metadata` | one object's full shape: columns, indexes, keys, partitions |
| `get_ddl` | the object's definition as its source engine reports it |
| `get_quality_signal` | whether an object is watched by data quality checks, and how healthy it is |
| `list_quality_targets` | which tables data quality checks watch, and which need attention |
| `get_quality_target` | one table's checks, health, schedule and unresolved problems |
| `list_quality_issues` | the problem ledger: what is open, acknowledged, resolved or muted |
| `list_quality_executions` | recent check runs with their verdicts, thresholds and evidence flags |
| `get_quality_execution` | one run's evidence: the executed SQL, failed rows, passing-row samples, a segment check's observed and changed segments, thresholds and baseline |
| `get_quality_impact` | what a table's data reaches downstream, and which upstream targets may explain a failure |
| `get_quality_consumption_coverage` | how much of the data other assets read is watched, and which consumed tables are not |
| `analyze_sql` | what one SQL statement reads and writes, column by column |
| `get_lineage_graph` | the multi-level lineage around an object |
| `whoami` | who the caller is and what they may read |

`get_quality_signal` answers "is this table healthy, should I trust it". A table with no
quality check at all is **not monitored**, reported as `monitored: false` rather than as an
error: the object exists, nothing watches it. A table whose checks are all paused answers
with `allPaused: true` and `enabledRuleCount: 0`, and its verdict is omitted: the checks are
history, and nothing is verified until one is resumed. The signal always emits that counter;
a nested summary rendered as raw protoJSON omits zero-valued fields instead. The judged
fields are absent until a check has produced a verdict.

Two honest limits: `get_quality_impact` is a bounded walk, so an empty answer means the
recorded lineage knows no such object, not that nothing consumes it; and
`get_quality_consumption_coverage` counts only the objects something reads, so its ratio is
not comparable to the signal's per-object column coverage.

There is deliberately no tool that asks a model to explain a failure: the agent calling
these tools is the model, so it reasons over the evidence itself. Failed and passing sample
values are real row data, shown to a caller the `executions.get` permission already entitles.

The deployment also publishes the `metaxisdata_guide` prompt, the embedded guide for a
client's model; it carries the field-level semantics this page leaves out, including
protoJSON's zero-value omissions, which filters accept which object types, and what each
failure code means.

Three rules decide whether a call works:

- **Objects are addressed by name**: `{instance, database, schema?, name}`, each value being
  a name or the id a listing returned; a `guid` from an earlier result works too. The server
  resolves the rest, engine-specific identifier layout is never the caller's problem, and a
  name that matches nothing or several things comes back with candidates instead of a guess.
- **`analyze_sql` needs an explicit scope**: `scopes: [{instance, database, schema?}]`. A
  statement's unqualified names cannot say which instance they belong to, so the tool
  refuses to pick one and lists the databases that exist.
- **A failure is a question to rephrase.** Every failure is one JSON object with a stable
  `code` (`invalid_argument`, `not_found`, `ambiguous`, `scope_required`, `unsupported`,
  `permission_denied`, `unauthenticated`, `unavailable`, `timeout`, `resource_exhausted`,
  `internal`), a `message`, usually a `hint`, and — for an unknown or ambiguous name —
  `details.candidates`.

Tool results use the API's field naming (lowerCamelCase, enums by name) but omit what is
unset, and a listing carries a projection rather than whole metadata objects; fetch details
with `get_metadata` or `get_ddl` when you need them.

## Operating it

- **Every tool call is audited**, reads included: one `audit_log` row naming the tool, the
  caller, the arguments (credential fields redacted, long values truncated) and the outcome.
  A call refused for lack of permission leaves a row too, and the ledger is permanent, so
  expect the row count to follow model usage.
- **A connection can be revoked, and a token can be logged out.** `Logout` (what
  `mxd logout` sends) revokes the presented token immediately, for `/mcp` as well as the
  API, because the server checks that it signed the token rather than which audience it
  carries. Because an MCP token names its client, the logout also revokes every connection
  that client holds for that user: access tokens stop being accepted (immediately on the
  replica that handled the request, within 30 seconds on another) and refresh tokens can
  mint nothing. An administrator or the user can do the same from Connected apps without
  holding the agent's token.
- **The anonymous endpoints are rate-limited per address** — registration, the token
  exchange, and the two browser steps — and a disabled deployment answers `404` without
  writing a ledger row, so probes cost nothing to keep.
- **Pending authorizations are process-local**, like device logins: an approval and its
  completion must reach the same replica, so run one replica or add sticky routing. Refresh
  tokens are not process-local; a refresh may reach any replica.
- **The endpoint is stateless and cross-origin protected**, so no session survives a
  request. Turning `mcp_enabled` off closes the surface immediately without revoking tokens
  or grants, which expire on their own.

## Connected apps

An authorized MCP client is recorded as one **connected app**: one user, one client, one
authorization. A CLI device login is the same kind of record — one user, the built-in `mxd`
client, one machine — so both are listed and revoked the same way, and the record exists
even when no `/mcp` request is in flight: what lasts is the OAuth grant, not a session.

- **Where to see it.** An administrator, or a custom role holding
  `metaxisdata.connectedApps.list`, sees every connection in the workspace under Settings →
  Connected apps, with filters for user, state and client. Any signed-in user sees their own
  from the avatar menu → Connected apps. The pages keep working while `mcp_enabled` is off,
  so they are also the way to clean up after the surface has been switched off.
- **What it shows.** The client (its registration name or id; a CLI row shows the built-in
  client and `CLI · v<build> · <address>`), the user, when the authorization completed, the
  most recent tool call for an MCP connection (not the most recent token refresh; written at
  most once every five minutes and blank for a CLI session), the refresh credential's expiry
  (it slides while the connection is used), and a derived state: `ACTIVE`, `REAUTH_REQUIRED`
  (an MCP grant whose external URL changed, or one older than the user's last password
  change), `INACTIVE` (the account is deactivated; the connection resumes if restored),
  `EXPIRED`, or `REVOKED`.
- **What revoke does.** Outstanding access tokens are refused — immediately on the replica
  that performed the revoke, within 30 seconds on the others — and the refresh token can
  mint nothing more. Revoking a CLI session cuts that machine only: the next `mxd` command
  exits 2 and asks the user to sign in again, while other machines keep working. Authorizing
  the same client again creates a new connection and a new entry, not a resurrection of the
  old one. Revoking your own connection needs no permission; revoking another user's needs
  `metaxisdata.connectedApps.revoke`.
- **How long records stay.** A revoked or expired connection stays listed for seven days
  after its refresh credential expires, so its final state can be seen; then maintenance
  reclaims the row. The audit ledger is permanent, and every revoke writes a row naming what
  was revoked.
