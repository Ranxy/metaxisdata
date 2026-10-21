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
// OpenLineage, read the applications that report their SQL, explain SQL, and
// read the workspace member directory (owner pickers and the owner filter need
// it). Administration — user writes, groups, roles, IAM, instance and data
// source writes, settings writes, audit logs, OpenLineage writes, LLM profiles,
// and — because issuing a key is a credential that writes data — the application
// key endpoints — is deliberately absent and reachable only through
// workspaceAdmin or a custom role.
var memberBaselinePermissions = permissionSet(
	permission.InstancesGet,
	permission.InstancesList,
	permission.EnvironmentsList,
	// Reading the member directory is part of the read baseline: owner pickers
	// and the owner filter resolve principals through it. User writes stay with
	// the administrator.
	permission.UsersList,
	permission.DatabasesList,
	permission.DatabasesRead,
	permission.ManualSQLsGet,
	permission.ManualSQLsList,
	permission.ManualSQLsCreate,
	permission.ManualSQLsUpdate,
	permission.ManualSQLsDelete,
	permission.LineageGet,
	permission.OpenLineageRead,
	permission.ApplicationsGet,
	permission.ApplicationsList,
	permission.ApplicationSQLsGet,
	permission.ApplicationSQLsList,
	permission.ExplainSQLExplain,
	// Quality evidence is discoverable for members like lineage and OpenLineage
	// are; writing rules, running checks and reviewing suggestions stay with an
	// administrator or a custom role.
	permission.DataQualityRulesGet,
	permission.DataQualityRulesList,
	permission.DataQualityRunsGet,
	permission.DataQualityRunsList,
	permission.DataQualityIssuesGet,
	permission.DataQualityIssuesList,
	permission.DataQualityExplain,
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
