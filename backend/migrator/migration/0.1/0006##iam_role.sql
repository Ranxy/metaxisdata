-- Custom IAM roles.
--
-- Roles were dropped in 0003 as dead Bytebase-era schema: back then a role was a
-- bare string in the IAM policy payload with no permission bundle attached. The
-- IAM subsystem re-introduces the table so operators can define their own
-- permission bundles alongside the predefined workspaceAdmin/workspaceMember
-- roles (which stay in Go and never get a row here).
--
-- resource_id is the role's short ID (the `{role}` in `roles/{role}`); name and
-- description are display metadata; permissions is the protojson encoding of
-- RolePermissions (proto/store/store/role.proto).
CREATE TABLE IF NOT EXISTS role (
    resource_id text PRIMARY KEY,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    permissions jsonb NOT NULL DEFAULT '{}'
);
