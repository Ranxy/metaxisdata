# Refresh token support

Implementation plan for two independent refresh-token flows:

- **MCP OAuth 2.1** — the authorization server at `/oauth/*` issues a rotating
  refresh token alongside the access token, so an MCP client whose access token
  expired no longer has to send the user through the browser flow again.
- **Web session** — the SPA's login issues a rotating refresh token beside the
  access-token cookie, so `AuthService.Refresh` can mint a new access token
  without re-entering credentials.

Both use **single-use rotation**: the old refresh token row is atomically
deleted (`DELETE … RETURNING`) and a new one is issued, so two concurrent
refreshes race and exactly one wins. Replaying a consumed token is an
`invalid_grant` / `Unauthenticated`, never a new token pair. Refresh tokens
live 30 days; access tokens keep their current seven-day lifetime.

The project has not shipped, so no historical compatibility is kept: the schema
is additive, no legacy-token fallback exists.

## Design decisions

- **A refresh token is a digest, never a secret at rest.** Only the SHA-256 hex
  digest of the opaque token is stored, so a database read does not hand out a
  usable credential. There is nothing to brute force in a 256-bit random string,
  so no slow KDF is needed and a lookup stays one indexed read.
- **Web sessions are keyed by `user_id`.** An account is identified by its
  principal id; the email address is administrative and mutable, so keying a
  session by it would let an address change orphan or adopt one.
- **The MCP grant set is validated but not persisted.** The server has exactly
  one client shape — public, authorization code plus PKCE — so `grant_types` is
  checked against the supported set at registration and every issued grant
  carries a refresh token; there is no per-client capability to remember.
- **Every refresh re-validates the principal from persisted state.** A
  deactivated account is refused, and so is a grant issued before the last
  password change — the same rule `TokenAuthenticator.Resolve` applies to an
  access token. Without it, refreshing would resurrect exactly the session a
  password change retired, and would let a grant outlive a deactivation. Because
  a rotation therefore must not reset the grant's issue time, the rotated row
  carries the original `created_at` forward.
- **Web rotation keeps an absolute session lifetime.** The rotated token
  inherits the original issue time and deadline, so refreshing cannot extend a
  session indefinitely. The MCP grant's expiry slides on activity instead,
  because a client that is being used has no natural point to re-authorize.
- **Revocation covers what could replace the credential.** `Logout` deletes the
  web row the cookie stands for, and for an MCP token — whose new `cid` claim
  names its registration — every grant that client holds, so revoking one token
  ends the connection rather than leaving it one refresh away from a new pair.

## Persisted state

Two tables, one incremental migration plus `LATEST.sql` (see
[backend/migrator/AGENTS.md](../backend/migrator/AGENTS.md)):

```sql
oauth_refresh_token(token_hash PK, client_id, user_id → principal(id), resource, scope, expires_at, created_at)
web_refresh_token  (token_hash PK, user_id → principal(id), expires_at, created_at)
```

`token_hash` is the SHA-256 hex digest of the opaque token; the plaintext is
never stored. `resource` pins an MCP grant to the `<external_url>/mcp` it was
consented for, so moving the deployment's external URL invalidates outstanding
grants instead of re-minting a token for a resource that no longer exists.

## MCP OAuth 2.1 (`backend/api/oauth`, `backend/store`)

- `TokenHandler` dispatches `authorization_code` and `refresh_token`; every
  other grant is `unsupported_grant_type`.
- The authorization-code exchange now also mints a refresh token and returns
  `refresh_token` in the token body.
- The refresh grant validates, in order: `client_id` binding, row presence,
  expiry, `resource` equals the current endpoint, the requested `scope` does not
  differ from the consented one. Only then does it consume the row atomically.
  After the consume it reloads the principal and refuses a deactivated account
  or a grant issued before the last password change. It then issues a new
  access token (carrying the `cid` claim) and a rotated refresh token.
- `AuthServerMetadata.GrantTypesSupported` advertises both grants; the
  registration endpoint accepts `grant_types` drawn from the supported set
  (defaulting to both) and reports both back.
- `Logout` deletes the refresh tokens of the client named by the presented
  access token's `cid` claim.

## Web session (`backend/api/v1`, `backend/api/auth`, `proto/v1`)

- `Login` (web, end user, not forced-password-reset) stores a refresh token and
  sets a second `HttpOnly` cookie beside the access-token cookie.
- `AuthService.Refresh` consumes the cookie's token atomically, re-validates the
  principal (deactivation, password-change cutoff), and rotates the pair. The
  rotated token inherits the original issue time and expiry, so the session has
  an absolute 30-day lifetime rather than sliding forever.
- `Logout` deletes the cookie's refresh token and clears both cookies.
- The SPA's transport interceptor answers a mid-session `Unauthenticated` with
  one refresh attempt (single-flight, shared by concurrent requests) and retries
  the original call; only a failed refresh ends the session.

## Out of scope

The `mxd` CLI's device-login token stays a plain access token: it has no cookie
jar and re-running `mxd login` is the ordinary recovery.

## Verification

- Hermetic: store query-shape guard tests, auth cookie/hash tests, OAuth
  metadata/registration/token-endpoint error tests, frontend interceptor tests.
- Integration (`backend/test/integration/runner/mcp_service_test.go` and
  `refresh_token_service_test.go`): a full grant, refresh rotation, replay
  refusal, and web-session refresh over the real server.
- Gates: `gofmt`, `golangci-lint run --allow-parallel-runners`, `go test ./...`,
  build; `biome:check`, `lint`, `i18n`, `type-check`, `test run` for the SPA.
