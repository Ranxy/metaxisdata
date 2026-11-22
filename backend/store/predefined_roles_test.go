package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
)

// TestPredefinedRolePermissionsAreCatalogued guards the role matrix against a
// typo or a removed catalog entry: every permission a predefined role grants
// must exist in the catalog, otherwise the role silently grants nothing.
func TestPredefinedRolePermissionsAreCatalogued(t *testing.T) {
	t.Parallel()

	for _, role := range PredefinedRoles {
		require.NotEmpty(t, role.Permissions, "role %s must grant something", role.ResourceID)
		for p := range role.Permissions {
			require.Truef(t, permission.Exist(p), "role %s grants %q, which is not in the catalog", role.ResourceID, p)
		}
	}
}

// TestWorkspaceAdminHoldsWholeCatalog pins the contract that admin access falls
// out of normal role resolution instead of a special case in the engine.
func TestWorkspaceAdminHoldsWholeCatalog(t *testing.T) {
	t.Parallel()

	admin := GetPredefinedRole(WorkspaceAdminRole)
	require.NotNil(t, admin)
	require.Len(t, admin.Permissions, len(permission.AllPermissions()))
	for _, p := range permission.AllPermissions() {
		require.Truef(t, admin.Permissions[p], "workspaceAdmin is missing %q", p)
	}
}

// TestWorkspaceMemberBaselineExcludesAdministration pins the read baseline: a
// member may read metadata and the workspace member directory but not
// administer the workspace.
func TestWorkspaceMemberBaselineExcludesAdministration(t *testing.T) {
	t.Parallel()

	member := GetPredefinedRole(WorkspaceMemberRole)
	require.NotNil(t, member)
	for _, p := range []permission.Permission{
		permission.InstancesList,
		permission.EnvironmentsList,
		permission.UsersList,
		permission.DatabasesRead,
		permission.ManualSQLsCreate,
		permission.LineageGet,
		permission.OpenLineageRead,
		permission.ExplainSQLExplain,
	} {
		require.Truef(t, member.Permissions[p], "workspaceMember should grant %q", p)
	}
	for _, p := range []permission.Permission{
		permission.UsersDelete,
		permission.UsersCreate,
		permission.GroupsCreate,
		permission.RolesCreate,
		permission.IAMSetPolicy,
		permission.IAMGetPolicy,
		permission.SettingsUpdate,
		permission.AuditLogsSearch,
		permission.InstancesCreate,
		permission.InstancesDelete,
		permission.InstancesSync,
		permission.DataSourcesCreate,
		permission.DatabasesSync,
		permission.OpenLineageApiKeysCreate,
		permission.LLMProfilesCreate,
		permission.LLMProfilesFetchModels,
	} {
		require.Falsef(t, member.Permissions[p], "workspaceMember must not grant %q", p)
	}
}

// TestQualityOwnerRoleBundle pins the scoped quality manager's matrix: it may
// author, run and archive checks and publish the target contract, but it cannot
// administer agents or the workspace.
func TestQualityOwnerRoleBundle(t *testing.T) {
	t.Parallel()

	owner := GetPredefinedRole(QualityOwnerRole)
	require.NotNil(t, owner)
	for _, p := range []permission.Permission{
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
		permission.DataQualityContractsPublish,
		permission.DataQualitySuggest,
		permission.DataQualityExplain,
		permission.DataQualityProfileRun,
		permission.DataQualityLabelsUpdate,
	} {
		require.Truef(t, owner.Permissions[p], "qualityOwner should grant %q", p)
	}
	for _, p := range []permission.Permission{
		permission.DataQualityAgentsManage,
		permission.DataQualityTemplatesCreate,
		permission.DataQualityCodeTablesCreate,
		permission.SettingsUpdate,
		permission.UsersCreate,
		permission.IAMSetPolicy,
	} {
		require.Falsef(t, owner.Permissions[p], "qualityOwner must not grant %q", p)
	}
}

func TestIsPredefinedRole(t *testing.T) {
	t.Parallel()

	require.True(t, IsPredefinedRole(WorkspaceAdminRole))
	require.True(t, IsPredefinedRole(WorkspaceMemberRole))
	require.True(t, IsPredefinedRole(QualityOwnerRole))
	require.False(t, IsPredefinedRole("syncer"))
	require.False(t, IsPredefinedRole(""))
}

// The scoped roles are only usable as scoped grants if every permission they
// carry is scopeable; a workspace-only permission would make the grant report
// access the resource path cannot satisfy.
func TestScopedRolesAreFullyScopeable(t *testing.T) {
	t.Parallel()

	for _, role := range []*RoleMessage{
		GetPredefinedRole(QualityOwnerRole),
		GetPredefinedRole(QualityAgentRole),
	} {
		require.NotNil(t, role)
		require.Emptyf(t, permission.NonScopeable(role.Permissions),
			"role %q carries permissions that cannot be granted at a scope", role.ResourceID)
	}
}
