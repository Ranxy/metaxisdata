-- An agent principal is an independent identity for autonomous quality work, not a
-- user acting on someone's behalf. It has a principal row (type SERVICE_ACCOUNT),
-- a workspace role with the minimum permissions, and this row for its display name
-- and enabled state. Tokens are normal access tokens for the principal, so
-- disabling the principal ends them all.
CREATE TABLE IF NOT EXISTS quality_agent (
    id BIGSERIAL PRIMARY KEY,
    resource_id TEXT COLLATE "C" NOT NULL,
    principal_id INT4 NOT NULL UNIQUE REFERENCES principal(id) ON DELETE CASCADE,
    display_name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_quality_agent_resource
    ON quality_agent(resource_id);
COMMENT ON TABLE quality_agent IS 'Workspace-level agent principals for autonomous quality work';
COMMENT ON COLUMN quality_agent.principal_id IS 'The independent principal the agent authenticates as (type SERVICE_ACCOUNT)';
ALTER SEQUENCE quality_agent_id_seq RESTART WITH 101;
