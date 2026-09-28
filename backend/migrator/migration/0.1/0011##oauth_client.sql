-- Persistent OAuth client registry (RFC 7591 dynamic client registration).
--
-- The server is becoming the OAuth 2.1 authorization server for its MCP
-- endpoint: MCP clients POST /oauth/register, get a client_id back, and keep
-- using it. A registration is a governance object -- "which application has
-- connected to this workspace" -- so it must survive a restart, unlike the
-- process-local pending authorization requests.
--
-- client_id is the unique public identifier; the surrogate id is internal and
-- never leaves the store. The column names follow the table conventions in
-- LATEST.sql: created_at, and last_used_at as in openlineage_api_key.
--
-- redirect_uris is a JSON array of strings. It is deliberately not bound to a
-- proto/store message: there is no message for a bare string list, and the
-- value is server-built, like explain_sql_cache.explanation_json. The column
-- comment below states that contract.
--
-- token_endpoint_auth_method stores whatever the registration requested; only
-- public clients ('none') are accepted today. The endpoint enforces that, not a
-- CHECK constraint, so supporting a confidential method later needs no schema
-- change.

CREATE TABLE IF NOT EXISTS oauth_client (
    id BIGSERIAL PRIMARY KEY,
    client_id TEXT NOT NULL,
    client_name TEXT NOT NULL DEFAULT '',
    redirect_uris JSONB NOT NULL,
    token_endpoint_auth_method TEXT NOT NULL DEFAULT 'none',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_oauth_client_client_id ON oauth_client(client_id);

COMMENT ON COLUMN oauth_client.redirect_uris IS 'Server-built JSON (array of strings); not a proto message.';
