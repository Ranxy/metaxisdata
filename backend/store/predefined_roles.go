package store

import "github.com/Ranxy/metaxisdata/backend/common/permission"

// WorkspaceAdminRole and WorkspaceMemberRole are the two predefined roles.
// They are defined in Go, read-only over the RoleService API, and resolvable
// in memory by the IAM manager.
const (
	WorkspaceAdminRole  = "workspaceAdmin"
	WorkspaceMemberRole = "workspaceMember"
	// QualityAgentRole is the IAM actor of the built-in quality agent. It is the
	// minimum the internal agent needs: read the plans and checks it reasons about,
	// and write the suggestions and plan declarations the agent proposes. It
	// deliberately cannot publish, approve, edit rules or touch settings.
	QualityAgentRole = "qualityAgent"
	// QualityOwnerRole is the data-quality management bundle a scoped grant gives
	// the owner of a quality agent's databases: author, run and archive checks,
	// review issues, publish the target contract, and use the analyses. It is a
	// role, not a baseline: it takes effect only where a grant names it.
	QualityOwnerRole = "qualityOwner"
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
	permission.DataQualityExecutionsGet,
	permission.DataQualityExecutionsList,
	permission.DataQualityIssuesGet,
	permission.DataQualityIssuesList,
	permission.DataQualityExplain,
	// Rule templates and code tables are authoring helpers, so their reads are part
	// of the member baseline; creating, editing and deleting them is a write.
	permission.DataQualityTemplatesList,
	permission.DataQualityCodeTablesList,
	// Agents are readable by members so an owner can open the agents they own;
	// the handler filters the list to owned agents and rejects non-owners.
	permission.DataQualityAgentsGet,
	permission.DataQualityAgentsList,
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
	{
		ResourceID:  QualityAgentRole,
		Name:        "Quality agent",
		Description: "The built-in quality agent's internal IAM actor: read the evidence, plans and checks of the databases it is delegated, and write suggested checks and plan declarations. It cannot publish, approve or edit rules.",
		Permissions: qualityAgentPermissions,
	},
	{
		ResourceID:  QualityOwnerRole,
		Name:        "Quality owner",
		Description: "The data-quality manager of a scope: author, run and archive checks, review issues, publish the target contract, and use the analyses. Granted at an instance or database.",
		Permissions: qualityOwnerPermissions,
	},
}

// qualityAgentPermissions is the agent role's minimum. It stays a subset of the
// member read baseline plus the writes an agent owns. The quality agent drafts
// checks into the review queue through the suggestion pipeline, so it holds
// suggest; it still cannot publish, approve, edit rules or change settings.
var qualityAgentPermissions = permissionSet(
	permission.DataQualityRulesGet,
	permission.DataQualityRulesList,
	// Evidence is the agent's own read: the execution, its failure and the check's
	// definition. It is deliberately not in the member baseline, because a baseline
	// permission is granted to every principal on every target — the scoped grant
	// this role receives is the only thing that bounds the agent to its databases.
	permission.DataQualityRulesEvidence,
	// Repair is the narrow write: a field whose value the server can compute from the
	// registry (a structure snapshot). It is deliberately not rules.update, so the catalog
	// itself reads as "this actor may not rewrite an arbitrary definition".
	permission.DataQualityRulesRepair,
	// Running a check is a diagnostic action: it produces a verdict, never a definition change.
	permission.DataQualityRulesRun,
	permission.DataQualitySuggest,
	permission.DataQualityPlansUpdate,
)

// qualityOwnerPermissions is the data-quality management surface a scoped grant
// gives the owner of a database. It excludes agent administration and the
// workspace-level template and code-table writes: those are not properties of a
// database scope.
var qualityOwnerPermissions = permissionSet(
	permission.DataQualityRulesGet,
	permission.DataQualityRulesList,
	permission.DataQualityRulesCreate,
	permission.DataQualityRulesUpdate,
	permission.DataQualityRulesDelete,
	permission.DataQualityRulesRun,
	permission.DataQualityExecutionsGet,
	permission.DataQualityExecutionsList,
	permission.DataQualityIssuesGet,
	permission.DataQualityIssuesList,
	permission.DataQualityIssuesUpdate,
	permission.DataQualityExplain,
	permission.DataQualitySuggest,
	permission.DataQualityContractsPublish,
	permission.DataQualityPlansUpdate,
	permission.DataQualityProfileRun,
	permission.DataQualityLabelsUpdate,
)

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
