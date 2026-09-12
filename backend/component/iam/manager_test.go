package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	exprpb "google.golang.org/genproto/googleapis/type/expr"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// errBoom is the sentinel the policy-error test returns.
var errBoom = errors.New("boom")

// fakeStore implements Store from memory so permission resolution can be tested
// without a database.
type fakeStore struct {
	policy    *storepb.IamPolicy
	roles     map[string]*store.RoleMessage
	groups    map[string]*store.GroupMessage
	policyErr error
}

func (f *fakeStore) GetWorkspaceIamPolicy(context.Context) (*store.IamPolicyMessage, error) {
	if f.policyErr != nil {
		return nil, f.policyErr
	}
	return &store.IamPolicyMessage{Policy: f.policy, Etag: "1"}, nil
}

func (f *fakeStore) GetRoleSnapshot(_ context.Context, resourceID string) (*store.RoleMessage, error) {
	if role := store.GetPredefinedRole(resourceID); role != nil {
		return role, nil
	}
	return f.roles[resourceID], nil
}

func (f *fakeStore) GetGroup(_ context.Context, email string) (*store.GroupMessage, error) {
	return f.groups[email], nil
}

func (*fakeStore) GetUserByID(_ context.Context, id int) (*store.UserMessage, error) {
	return &store.UserMessage{ID: id}, nil
}

func binding(role string, members ...string) *storepb.Binding {
	return &storepb.Binding{Role: role, Members: members}
}

func testUser(id int) *store.UserMessage {
	return &store.UserMessage{ID: id, Email: "user@example.com", Type: storepb.PrincipalType_END_USER}
}

func TestCheckPermissionBaseline(t *testing.T) {
	t.Parallel()
	m := &Manager{store: &fakeStore{policy: &storepb.IamPolicy{}}}
	user := testUser(101)

	// Every authenticated principal gets the member baseline.
	require.True(t, mustCheck(t, m, permission.InstancesList, user))
	require.True(t, mustCheck(t, m, permission.DatabasesRead, user))
	require.True(t, mustCheck(t, m, permission.ManualSQLsCreate, user))
	require.True(t, mustCheck(t, m, permission.ExplainSQLExplain, user))

	// Administration is not part of the baseline.
	require.False(t, mustCheck(t, m, permission.UsersDelete, user))
	require.False(t, mustCheck(t, m, permission.UsersCreate, user))
	require.False(t, mustCheck(t, m, permission.IAMSetPolicy, user))
	require.False(t, mustCheck(t, m, permission.InstancesCreate, user))
	require.False(t, mustCheck(t, m, permission.SettingsUpdate, user))
	require.False(t, mustCheck(t, m, permission.AuditLogsSearch, user))
	require.False(t, mustCheck(t, m, permission.OpenLineageApiKeysCreate, user))
}

func TestCheckPermissionNoUser(t *testing.T) {
	t.Parallel()
	m := &Manager{store: &fakeStore{policy: &storepb.IamPolicy{}}}
	ok, err := m.CheckPermission(context.Background(), permission.InstancesList, nil)
	require.NoError(t, err)
	require.False(t, ok, "an unauthenticated caller gets nothing, not even the baseline")
}

func TestCheckPermissionPredefinedAdminRole(t *testing.T) {
	t.Parallel()
	m := &Manager{store: &fakeStore{policy: &storepb.IamPolicy{
		Bindings: []*storepb.Binding{binding(common.FormatRole(store.WorkspaceAdminRole), "users/101")},
	}}}
	admin := testUser(101)
	other := testUser(102)

	require.True(t, mustCheck(t, m, permission.UsersDelete, admin))
	require.True(t, mustCheck(t, m, permission.IAMSetPolicy, admin))
	require.False(t, mustCheck(t, m, permission.UsersDelete, other))
}

func TestCheckPermissionGroupBinding(t *testing.T) {
	t.Parallel()
	m := &Manager{store: &fakeStore{
		policy: &storepb.IamPolicy{
			Bindings: []*storepb.Binding{binding(common.FormatRole(store.WorkspaceAdminRole), "groups/eng@example.com")},
		},
		groups: map[string]*store.GroupMessage{
			"eng@example.com": {
				Email: "eng@example.com",
				Payload: &storepb.GroupPayload{Members: []*storepb.GroupMember{
					{Member: "users/101"},
				}},
			},
		},
	}}

	require.True(t, mustCheck(t, m, permission.UsersDelete, testUser(101)), "a group member inherits the role")
	require.False(t, mustCheck(t, m, permission.UsersDelete, testUser(102)), "a non-member does not")
}

func TestCheckPermissionAllUsersBinding(t *testing.T) {
	t.Parallel()
	m := &Manager{store: &fakeStore{policy: &storepb.IamPolicy{
		Bindings: []*storepb.Binding{binding(common.FormatRole(store.WorkspaceAdminRole), "allUsers")},
	}}}
	require.True(t, mustCheck(t, m, permission.UsersDelete, testUser(101)))
	require.True(t, mustCheck(t, m, permission.SettingsUpdate, testUser(999)))
}

func TestCheckPermissionCustomRole(t *testing.T) {
	t.Parallel()
	m := &Manager{store: &fakeStore{
		policy: &storepb.IamPolicy{
			Bindings: []*storepb.Binding{binding("roles/syncer", "users/101")},
		},
		roles: map[string]*store.RoleMessage{
			"syncer": {
				ResourceID:  "syncer",
				Permissions: map[permission.Permission]bool{permission.InstancesSync: true},
			},
		},
	}}

	require.True(t, mustCheck(t, m, permission.InstancesSync, testUser(101)))
	require.False(t, mustCheck(t, m, permission.InstancesDelete, testUser(101)), "a custom role grants exactly what it lists")
}

func TestCheckPermissionUnknownRoleGrantsNothing(t *testing.T) {
	t.Parallel()
	m := &Manager{store: &fakeStore{policy: &storepb.IamPolicy{
		Bindings: []*storepb.Binding{binding("roles/ghost", "users/101")},
	}}}
	require.False(t, mustCheck(t, m, permission.UsersDelete, testUser(101)))
}

// A binding condition that references an attribute this deployment does not
// bind cannot be evaluated; the binding must be dropped rather than treated as
// satisfied.
func TestCheckPermissionUnevaluableConditionFailsClosed(t *testing.T) {
	t.Parallel()
	b := binding(common.FormatRole(store.WorkspaceAdminRole), "users/101")
	b.Condition = &exprpb.Expr{Expression: "resource.database == 'db'"}
	m := &Manager{store: &fakeStore{policy: &storepb.IamPolicy{Bindings: []*storepb.Binding{b}}}}
	require.False(t, mustCheck(t, m, permission.UsersDelete, testUser(101)))
}

func TestCheckPermissionPolicyErrorPropagates(t *testing.T) {
	t.Parallel()
	m := &Manager{store: &fakeStore{policyErr: errBoom}}
	_, err := m.CheckPermission(context.Background(), permission.UsersDelete, testUser(101))
	require.ErrorIs(t, err, errBoom)
}

func TestEffectivePermissions(t *testing.T) {
	t.Parallel()
	m := &Manager{store: &fakeStore{policy: &storepb.IamPolicy{
		Bindings: []*storepb.Binding{binding(common.FormatRole(store.WorkspaceAdminRole), "users/101")},
	}}}
	perms, err := m.EffectivePermissions(context.Background(), testUser(101))
	require.NoError(t, err)
	require.Len(t, perms, len(permission.AllPermissions()), "workspaceAdmin holds the whole catalog")
	require.True(t, sortedAscending(perms))

	perms, err = m.EffectivePermissions(context.Background(), testUser(102))
	require.NoError(t, err)
	require.Contains(t, perms, permission.InstancesList)
	require.NotContains(t, perms, permission.UsersDelete)
}

func mustCheck(t *testing.T, m *Manager, perm permission.Permission, user *store.UserMessage) bool {
	t.Helper()
	ok, err := m.CheckPermission(context.Background(), perm, user)
	require.NoError(t, err)
	return ok
}

func sortedAscending(perms []permission.Permission) bool {
	for i := 1; i < len(perms); i++ {
		if perms[i-1] > perms[i] {
			return false
		}
	}
	return true
}
