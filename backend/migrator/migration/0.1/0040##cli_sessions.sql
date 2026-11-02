-- A CLI device login is a connected app too: its session is an
-- oauth_refresh_token grant, exactly like an MCP authorization, so the same
-- management page lists it and the same revocation path cuts it. kind names the
-- ceremony that produced the row; both kinds rotate in place and slide their
-- expiry, and only the access token's audience differs.
ALTER TABLE oauth_refresh_token ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'mcp' CHECK (kind IN ('mcp', 'cli'));

-- The CLI build and the source address the device login came from. Display-only
-- and empty for an MCP grant: they are what lets a user tell their own CLI
-- logins apart on the Connected apps page.
ALTER TABLE oauth_refresh_token ADD COLUMN IF NOT EXISTS client_version TEXT NOT NULL DEFAULT '';

ALTER TABLE oauth_refresh_token ADD COLUMN IF NOT EXISTS request_ip TEXT NOT NULL DEFAULT '';
