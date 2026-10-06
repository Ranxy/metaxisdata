-- Refresh tokens for the two flows that mint long-lived credentials: the MCP
-- OAuth 2.1 authorization server (`oauth_refresh_token`) and the SPA's web
-- session (`web_refresh_token`).
--
-- Both tables store only the SHA-256 hex digest of the opaque token, never the
-- plaintext, so database read access does not hand out a usable credential.
-- Both are single-use: a refresh atomically deletes the row it consumes
-- (`DELETE ... RETURNING`) and inserts a replacement, so two concurrent
-- refreshes race and exactly one wins, and a replayed token resolves to no row.
-- That is what makes rotation detectable rather than merely nominal.
--
-- user_id references principal(id) because an account is identified by its
-- principal id; the email address is administrative and mutable. Deactivating an
-- account is a soft delete, so the row is not removed by the cascade -- the
-- refresh handlers re-read the principal and refuse a deactivated one, and
-- refuse a grant issued before the last password change, exactly as
-- TokenAuthenticator.Resolve does for an access token.
--
-- expires_at is a real column rather than a TTL computation because the
-- maintenance runner prunes it (through the indexes below) and the refresh
-- handler compares it to the request's clock.

-- oauth_refresh_token is the MCP OAuth 2.1 grant state carried across a
-- refresh. resource pins the grant to the `<external_url>/mcp` the user
-- consented to, so moving the deployment's external URL invalidates outstanding
-- grants instead of re-minting a token for a resource identifier that no longer
-- exists; scope is carried forward verbatim, so a refresh re-issues the grant as
-- consented and never widens it.
CREATE TABLE IF NOT EXISTS oauth_refresh_token (
    id BIGSERIAL PRIMARY KEY,
    token_hash TEXT NOT NULL,
    client_id TEXT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    resource TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- The token hash is the lookup key and is unique by construction; the unique
-- index is also what turns a hash collision into an error rather than a
-- silently ambiguous row.
CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_refresh_token_hash ON oauth_refresh_token(token_hash);

-- Logout revokes every grant a user holds for one client, which is this index's
-- query. The consume path is served by the unique index above.
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_token_user_client ON oauth_refresh_token(user_id, client_id);

-- The maintenance runner's TTL prune.
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_token_expires_at ON oauth_refresh_token(expires_at);

-- web_refresh_token is the SPA session's rotation state. It deliberately carries
-- nothing but the principal and the absolute expiry: the workspace is single
-- tenant, and a refreshed access token is minted from the reloaded principal
-- rather than from anything the cookie asserts.
CREATE TABLE IF NOT EXISTS web_refresh_token (
    id BIGSERIAL PRIMARY KEY,
    token_hash TEXT NOT NULL,
    user_id INTEGER NOT NULL REFERENCES principal(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_web_refresh_token_hash ON web_refresh_token(token_hash);

-- The maintenance runner's TTL prune.
CREATE INDEX IF NOT EXISTS idx_web_refresh_token_expires_at ON web_refresh_token(expires_at);
