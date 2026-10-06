-- Access tokens revoked before their expiry, so a logout is not undone by a
-- process-local cache that a caller can churn.
--
-- Logout used to add the token string to a bounded in-memory LRU. Any account
-- holder could log in and log out more than the cache's capacity times, evicting
-- another user's revoked token, and a leaked seven-day token that had been
-- revoked came back to life. The set is now a table keyed by the token's jti: an
-- entry can only be displaced by its own expiry, which bounds it by design, and
-- a second replica sees the revocation too. A partial index is not needed: the
-- primary key answers the lookup and the TTL prune scans expires_at.
CREATE TABLE IF NOT EXISTS revoked_token (
    jti text PRIMARY KEY,
    -- The token's own expiry: past it the row can never refuse anything.
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz NOT NULL DEFAULT now()
);

-- The maintenance runner prunes rows whose token has expired. Without this
-- index that is a sequential scan of every token ever revoked.
CREATE INDEX IF NOT EXISTS idx_revoked_token_expires_at ON revoked_token (expires_at);
