# Connecting an MCP client

Metaxisdata can serve its metadata registry and lineage to a Model Context
Protocol client as a set of read-only tools. The endpoint is `/mcp`, and the same
server acts as the OAuth 2.1 authorization server those clients register and
authorize against. Nothing here writes.

## Before it will answer

Two workspace settings, in this order:

1. **External URL** (Settings → General). It is the issuer and the resource
   identifier the whole flow is built from, so it must be the address clients
   reach this deployment at. It must be `https`, except on a loopback address
   during development, and it may carry a path prefix if the server is mounted
   under one.
2. **`mcp_enabled`** (same page). Off by default. The server refuses to turn it on
   until the external URL is set, and while it is off the endpoint, both metadata
   documents and every `/oauth/*` route answer `404` — a disabled deployment
   advertises nothing.

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

The first request has no token, so the client is told where to authorize and then
does the rest itself:

1. `POST /mcp` answers `401` with
   `WWW-Authenticate: Bearer resource_metadata="https://mx.example.com/.well-known/oauth-protected-resource", scope="metaxisdata.mcp.read", error="invalid_token"`.
2. The client reads the protected-resource metadata, follows
   `authorization_servers` to the authorization-server metadata, and registers
   itself with `POST /oauth/register` (public clients only; PKCE is mandatory).
3. The client opens `GET /oauth/authorize` in a browser with a PKCE challenge and
   `resource=https://mx.example.com/mcp`. A signed-in user decides on the
   `/oauth/consent` page, showing the client's name, where the code would be sent,
   the resource and the scope. Nothing is approved by opening the page.
4. The browser completes the flow at `GET /oauth/authorize/complete`, which mints
   the authorization code server-side and redirects it to the client — the code
   never passes through the page.
5. The client exchanges the code at `POST /oauth/token` and presents the resulting
   access token on every later `/mcp` request. The access token lives one hour —
   short enough that a leaked copy is useless soon, and renewed from the refresh
   token in the same response without another browser round trip.

The access token is bound to `<external URL>/mcp` as its audience: a web or CLI
session token is refused at `/mcp`, and an MCP token is refused on the ConnectRPC
API. Changing the external URL therefore invalidates outstanding MCP tokens and
grants, and the client authorizes again.

Refreshing is a rotation, not a renewal of the same credential: presenting the
refresh token with `grant_type=refresh_token`, `client_id`, `resource` and (when
the client tracks one) the consented `scope` replaces it and returns a new access
token plus a new refresh token. The token is single-use — the grant row is
updated in place, not recreated — so two clients racing with the same token leave
exactly one working, and a copy stolen from a client's storage is worthless once
the real client refreshes. A grant lives 30 days and is re-checked against the
user's account at every rotation, so a deactivated user or one who changed their
password since consent has to authorize again. The grant row is also the record
you manage under **Connected apps**, which is why it keeps its id across
rotations.

Upgrading from a build that did not put a connection id in MCP access tokens
refuses those older tokens once. A client with a valid refresh token recovers on
its next refresh — the refresh credential lives in the database and survives the
upgrade — and only a client whose refresh token has also expired needs the
browser flow again.

## Disconnecting

A client can end its own connection without the browser or an administrator:
`POST /oauth/revoke` (RFC 7009), form-encoded, with `token` set to either its
access token or its refresh token. Either one revokes the whole connection — the
two are halves of one authorization — so the other stops working too.
`token_type_hint` is accepted and ignored, `client_id` is optional, and a
revocation is answered with an empty `200` whether or not the token was valid, so
the endpoint cannot be used to probe which tokens exist. The authorization-server
metadata advertises it as `revocation_endpoint`. Revoking from **Connected apps**
below does the same thing for a connection whose client is gone.

## The tools

| Tool | Answers |
| --- | --- |
| `list_instances` | which instances are registered |
| `list_databases` | which databases an instance has, with the GUIDs that identify them |
| `search_metadata` | where a name lives, when you only have the name |
| `list_metadata` | what is directly under an object (schemas, tables, views, columns) |
| `get_metadata` | one object's full shape: columns, indexes, keys, partitions |
| `get_ddl` | the object's definition as its source engine reports it |
| `analyze_sql` | what one SQL statement reads and writes, column by column |
| `get_lineage_graph` | the multi-level lineage around an object |
| `whoami` | who the caller is and what they may read |

Three rules decide whether a call works:

- **Objects are addressed by name**: `{instance, database, schema?, name}`, each
  value being a name or the id a listing returned. A `guid` from an earlier result
  works too. The server resolves the rest — engine-specific identifier layout is
  never the caller's problem — and a name that matches nothing, or several things,
  comes back with the candidates instead of a guess.
- **`analyze_sql` needs an explicit scope**: `scopes: [{instance, database, schema?}]`.
  A statement's unqualified names cannot say which instance they belong to, so the
  tool refuses to pick one and lists the databases that exist.
- **A failure is a question to rephrase.** Every failure is one JSON object with a
  stable `code` (`invalid_argument`, `not_found`, `ambiguous`, `scope_required`,
  `unsupported`, `permission_denied`, `unauthenticated`, `unavailable`, `timeout`,
  `resource_exhausted`, `internal`), a `message`, usually a `hint`, and — for a
  name that was unknown or ambiguous — `details.candidates`.

Tool results use the same field naming as the rest of the API (lowerCamelCase,
enums by name) but omit what is unset, and a listing carries a projection rather
than whole metadata objects; fetch the details with `get_metadata` or `get_ddl`
when you need them.

## Operating it

- **Every tool call is audited**, reads included: one `audit_log` row naming the
  tool, the caller, the arguments (credential fields redacted, long values
  truncated) and the outcome. A call refused for lack of permission leaves a row
  too. The ledger is permanent, so expect the row count to follow model usage.
- **A connection can be revoked, and a token can be logged out.** `Logout` (what
  `mxd logout` sends) revokes the presented token immediately, for `/mcp` as well as
  the API, because the server checks that it signed the token rather than which
  audience it carries. Because an MCP token names its client, the logout also
  revokes every connection that client holds for that user: their access tokens
  stop being accepted (immediately on the replica that handled the request,
  within 30 seconds on another) and their refresh tokens can mint nothing. The
  client itself can end its connection with `POST /oauth/revoke` above, and an
  administrator or the user can do the same from Connected apps without holding
  the agent's token; see below.
- **The plain-HTTP OAuth endpoints are rate-limited per address** — registration,
  the token exchange, the two browser steps, and revocation (which carries a
  token but the same ceiling) — and a disabled deployment answers `404` without
  writing a ledger row, so probes cost nothing to keep.
- **Pending authorizations are process-local**, like device logins. An approval
  and its completion must reach the same replica: run one replica, or put sticky
  routing in front. Refresh tokens are not: they live in the database, so a
  refresh may reach any replica.
- **The endpoint is stateless.** No session survives a request, which is what
  makes it safe behind a load balancer, and it is wrapped in the standard
  library's cross-origin protection.
- Turning `mcp_enabled` off closes the surface immediately; it does not revoke
  tokens or grants, which expire on their own. `POST /oauth/revoke` above, the
  Logout route, or Connected apps below is the way to retire a live connection.

## Connected apps

An authorized MCP client is recorded as one **connected app**: one user, one
client, one authorization. The record exists even when no `/mcp` request is in
flight, because `/mcp` is stateless — what lasts is the OAuth grant, not a
transport session.

- **Where to see it.** An administrator, or a custom role holding
  `metaxisdata.connectedApps.list`, sees every connection in the workspace under
  Settings → Connected apps, with filters for user, state and client. Any
  signed-in user sees their own connections from the avatar menu → Connected
  apps. The pages keep working while `mcp_enabled` is off: they are also the way
  to clean up connections after the surface has been switched off.
- **What it shows.** The client (the name it reported at registration, or its
  id), the user, when the authorization completed, the most recent tool call
  (not the most recent token refresh — a connection authorized but never used
  shows "no calls yet"; the column is written at most once every five minutes),
  the refresh credential's expiry, and a derived state: `ACTIVE`,
  `REAUTH_REQUIRED` (the external URL changed, or the grant predates the user's
  last password change), `INACTIVE` (the account is deactivated and the
  connection resumes if it is restored), `EXPIRED`, or `REVOKED`.
- **What revoke does.** The connection's outstanding access tokens are refused —
  immediately on the replica that performed the revoke, within 30 seconds on the
  others — and its refresh token can mint nothing more. Authorizing the same
  client again creates a new connection and a new entry, not a resurrection of
  the old one. Revoking your own connection needs no permission; revoking
  another user's needs `metaxisdata.connectedApps.revoke`, which a workspace
  administrator holds and a custom role can be granted.
- **How long records stay.** A revoked or expired connection stays listed for
  seven days after its refresh credential expires, so its final state can be
  seen; then maintenance reclaims the row. The audit ledger is permanent, and
  every revoke writes a row there naming what was revoked.
