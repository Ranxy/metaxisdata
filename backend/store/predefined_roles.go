package store

import "github.com/Ranxy/metaxisdata/backend/common/permission"

// WorkspaceAdminRole and WorkspaceMemberRole are the two predefined roles.
// They are defined in Go, read-only over the RoleService API, and resolvable
// in memory by the IAM manager.
const (
	WorkspaceAdminRole  = "workspaceAdmin"
	WorkspaceMemberRole = "workspaceMember"
)

func permissionSet(perms ...permission.Permission) map[permission.Permission]bool {
	m := make(map[permission.Permission]bool, len(perms))
	for _, p := range perms {
		m[p] = true
	}
	return m
}

// allPermissionSet is the union of every catalog permission. The workspaceAdmin
// role holds it so that admin access falls out of the normal role->permission
// resolution rather than a special-case branch in CheckPermission.
var allPermissionSet = func() map[permission.Permission]bool {
	m := make(map[permission.Permission]bool, len(permission.AllPermissions()))
	for _, p := range permission.AllPermissions() {
		m[p] = true
	}
	return m
}()

// memberBaselinePermissions is the permission set granted to any authenticated
// principal (roles/workspaceMember). It is the discovery/read baseline: browse
// instances and metadata, read and author manual SQL, read lineage, browse
// OpenLineage, and explain SQL. Administration (user/group/role/IAM, instance
// and data source writes, settings writes, audit logs, OpenLineage writes, LLM
// profiles) is deliberately absent and reachable only through workspaceAdmin or
// a custom role.
var memberBaselinePermissions = permissionSet(
	permission.InstancesGet,
	permission.InstancesList,
	permission.DatabasesList,
	permission.DatabasesRead,
	permission.ManualSQLsGet,
	permission.ManualSQLsList,
	permission.ManualSQLsCreate,
	permission.ManualSQLsUpdate,
	permission.ManualSQLsDelete,
	permission.LineageGet,
	permission.OpenLineageRead,
	permission.ExplainSQLExplain,
)

// PredefinedRoles are the read-only, Go-defined roles shown on the Roles page
// and resolvable in-memory by the IAM manager. WorkspaceAdmin carries the whole
// catalog; WorkspaceMember is the authenticated-principal baseline.
var PredefinedRoles = []*RoleMessage{
	{
		ResourceID:  WorkspaceAdminRole,
		Name:        "Workspace admin",
		Description: "Full control of the workspace, including users, roles, settings and every data source.",
		Permissions: allPermissionSet,
	},
	{
		ResourceID:  WorkspaceMemberRole,
		Name:        "Workspace member",
		Description: "Read access to metadata, lineage, OpenLineage and manual SQL, and the ability to explain SQL.",
		Permissions: memberBaselinePermissions,
	},
}

var predefinedRolesMap = func() map[string]*RoleMessage {
	m := make(map[string]*RoleMessage, len(PredefinedRoles))
	for _, r := range PredefinedRoles {
		m[r.ResourceID] = r
	}
	return m
}()

// GetPredefinedRole returns the predefined role with the given resource ID, or
// nil if no such predefined role exists.
func GetPredefinedRole(resourceID string) *RoleMessage {
	return predefinedRolesMap[resourceID]
}

// IsPredefinedRole reports whether the given resource ID names a predefined
// role. Predefined roles are read-only over the RoleService API.
func IsPredefinedRole(resourceID string) bool {
	return predefinedRolesMap[resourceID] != nil
}
