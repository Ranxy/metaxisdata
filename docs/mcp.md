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
   access token on every later `/mcp` request.

The token is bound to `<external URL>/mcp` as its audience: a web or CLI session
token is refused at `/mcp`, and an MCP token is refused on the ConnectRPC API.
Changing the external URL therefore invalidates outstanding MCP tokens, and the
client authorizes again. There is no refresh token — an expired token means
running the flow again — so the number of long-lived credentials stays at zero.

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
  `permission_denied`, `unauthenticated`, `unavailable`, `timeout`,
  `resource_exhausted`, `internal`), a `message`, usually a `hint`, and — for a
  name that was unknown or ambiguous — `details.candidates`.

Tool results use the same field naming as the rest of the API (lowerCamelCase,
enums by name) but omit what is unset, and a listing carries a projection rather
than whole metadata objects; fetch the details with `get_metadata` or `get_ddl`
when you need them.

## Operating it

- **Every tool call is audited**, reads included: one `audit_log` row naming the
  tool, the caller, the arguments (credential fields redacted) and the outcome.
  The ledger is permanent, so expect the row count to follow model usage.
- **Pending authorizations are process-local**, like device logins. An approval
  and its completion must reach the same replica: run one replica, or put sticky
  routing in front.
- **The endpoint is stateless.** No session survives a request, which is what
  makes it safe behind a load balancer, and it is wrapped in the standard
  library's cross-origin protection.
- Turning `mcp_enabled` off closes the surface immediately; it does not revoke
  tokens, which expire on their own.
