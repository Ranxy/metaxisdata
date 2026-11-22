-- Resource-scoped IAM grants. The workspace IAM policy stays the root decision;
-- a grant at a resource adds its role over that resource's subtree. Only
-- time-only conditions are stored, because a resource-attribute condition could
-- not be pushed into a list query.
CREATE TABLE IF NOT EXISTS iam_grant (
    id BIGSERIAL PRIMARY KEY,
    resource_type TEXT COLLATE "C" NOT NULL,
    resource_id TEXT COLLATE "C" NOT NULL DEFAULT '',
    role TEXT COLLATE "C" NOT NULL,
    member TEXT COLLATE "C" NOT NULL,
    condition TEXT NOT NULL DEFAULT '',
    created_by INT4 REFERENCES principal(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_iam_grant_unique
    ON iam_grant(resource_type, resource_id, role, member, condition);
CREATE INDEX IF NOT EXISTS idx_iam_grant_member_resource
    ON iam_grant(member, resource_type);
CREATE INDEX IF NOT EXISTS idx_iam_grant_resource
    ON iam_grant(resource_type, resource_id);
COMMENT ON TABLE iam_grant IS 'Resource-scoped IAM grant: a role bound to a member at one resource subtree; additive to the workspace policy';
