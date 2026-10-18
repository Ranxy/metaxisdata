-- A grant row (oauth_refresh_token) becomes the durable record of one connected
-- app: one user's authorization of one MCP client. The OAuth token endpoint now
-- rotates the refresh credential in place instead of deleting and reinserting the
-- row, so the row's id is a stable handle an access token can carry and an
-- administrator can revoke by.
--
-- last_tool_call_at records the most recent tool call made with this connection.
-- It is deliberately separate from oauth_client.last_used_at, which is per client
-- registration: this column is per authorization and is updated by the MCP tool
-- path (throttled), not by token issuance.
--
-- revoked_at is a soft revocation: the row stays so the management page can show
-- what was revoked and by whom. Every path that accepts a credential or rotates
-- one treats a non-NULL value as terminal, and nothing may clear it -- a client
-- that authorizes again creates a new row with a new id. revoked_by is who
-- revoked it (NULL means the system ended it when a refresh was refused for a
-- gone/deactivated principal or a password change); ON DELETE SET NULL so
-- deleting an administrator's account does not delete the connection's history.
--
-- The maintenance runner still reclaims rows by expires_at; the management page
-- reads revoked and expired rows until then.

ALTER TABLE oauth_refresh_token ADD COLUMN IF NOT EXISTS last_tool_call_at TIMESTAMPTZ;
ALTER TABLE oauth_refresh_token ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ;
ALTER TABLE oauth_refresh_token ADD COLUMN IF NOT EXISTS revoked_by INTEGER
    REFERENCES principal(id) ON DELETE SET NULL;

-- The management page lists newest first and the self-service scope filters by
-- user; the unique token hash index and the (user_id, client_id) index already
-- serve the rotation and Logout paths.
CREATE INDEX IF NOT EXISTS idx_oauth_refresh_token_user_created
    ON oauth_refresh_token(user_id, created_at DESC);
