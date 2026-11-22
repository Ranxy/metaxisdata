package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type fakeGrantReader struct {
	grants        []*store.IamGrantMessage
	relations     []*store.AgentGrantRelation
	groups        map[string]*store.GroupMessage
	generation    uint64
	grantCalls    int
	relationCalls int
	lastFind      *store.FindIamGrantMessage
}

func (f *fakeGrantReader) ListIamGrants(_ context.Context, find *store.FindIamGrantMessage) ([]*store.IamGrantMessage, error) {
	f.grantCalls++
	f.lastFind = find
	return f.grants, nil
}

func (f *fakeGrantReader) ListAgentGrantRelations(_ context.Context, _ int) ([]*store.AgentGrantRelation, error) {
	f.relationCalls++
	return f.relations, nil
}

func (f *fakeGrantReader) AuthorizationGeneration() uint64 {
	return f.generation
}

func (*fakeGrantReader) GetRoleSnapshot(_ context.Context, resourceID string) (*store.RoleMessage, error) {
	return store.GetPredefinedRole(resourceID), nil
}

func (f *fakeGrantReader) GetGroup(_ context.Context, email string) (*store.GroupMessage, error) {
	return f.groups[email], nil
}

func qualityOwnerGrant(member string) *store.IamGrantMessage {
	return &store.IamGrantMessage{
		ResourceType: "database",
		ResourceID:   "i;db",
		Role:         common.FormatRole(store.QualityOwnerRole),
		Member:       member,
	}
}

func agentRelation(role, databaseRef string) *store.AgentGrantRelation {
	return &store.AgentGrantRelation{Role: common.FormatRole(role), DatabaseRef: databaseRef}
}

func TestScopedAuthorizerCheck(t *testing.T) {
	t.Parallel()

	principal := &store.UserMessage{ID: 7}
	table := resource.NewRef(resource.TypeTable, "i;db;public;orders")

	t.Run("a workspace permission needs no relationship read", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: true}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.True(t, allowed)
		require.Zero(t, reader.grantCalls)
		require.Zero(t, reader.relationCalls)
	})

	t.Run("a grant at an ancestor allows the target", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{grants: []*store.IamGrantMessage{qualityOwnerGrant(common.FormatUserUID(7))}}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.True(t, allowed)
		require.NotNil(t, reader.lastFind)
		require.Equal(t, []string{common.FormatUserUID(7), common.AllUsers}, reader.lastFind.Members)
		require.True(t, reader.lastFind.IncludeGroupGrants)
		require.Nil(t, reader.lastFind.Scopes, "the snapshot reads the principal's grants once, not a scope query")
	})

	t.Run("a grant on another database does not allow the target", func(t *testing.T) {
		t.Parallel()
		grant := qualityOwnerGrant(common.FormatUserUID(7))
		grant.ResourceID = "i;other"
		reader := &fakeGrantReader{grants: []*store.IamGrantMessage{grant}}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("a grant for another member does not allow", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{grants: []*store.IamGrantMessage{qualityOwnerGrant(common.FormatUserUID(99))}}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("a role without the permission does not allow", func(t *testing.T) {
		t.Parallel()
		grant := qualityOwnerGrant(common.FormatUserUID(7))
		grant.Role = common.FormatRole(store.QualityAgentRole)
		reader := &fakeGrantReader{grants: []*store.IamGrantMessage{grant}}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("a group grant allows a member", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{
			grants: []*store.IamGrantMessage{qualityOwnerGrant("groups/data-team@example.com")},
			groups: map[string]*store.GroupMessage{
				"data-team@example.com": {Payload: &storepb.GroupPayload{
					Members: []*storepb.GroupMember{{Member: common.FormatUserUID(7)}},
				}},
			},
		}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.True(t, allowed)
	})

	t.Run("a group grant does not allow a non-member", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{
			grants: []*store.IamGrantMessage{qualityOwnerGrant("groups/data-team@example.com")},
			groups: map[string]*store.GroupMessage{
				"data-team@example.com": {Payload: &storepb.GroupPayload{
					Members: []*storepb.GroupMember{{Member: common.FormatUserUID(99)}},
				}},
			},
		}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("an unevaluable condition fails closed", func(t *testing.T) {
		t.Parallel()
		grant := qualityOwnerGrant(common.FormatUserUID(7))
		grant.Condition = "resource.database == 'db'"
		reader := &fakeGrantReader{grants: []*store.IamGrantMessage{grant}}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("a checker error is returned before the snapshot", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("policy read failed")
		reader := &fakeGrantReader{}
		allowed, err := NewScopedAuthorizer(&fakeChecker{err: cause}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.ErrorIs(t, err, cause)
		require.False(t, allowed)
		require.Zero(t, reader.grantCalls)
	})

	t.Run("a missing principal is denied", func(t *testing.T) {
		t.Parallel()
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: true}, &fakeGrantReader{}).
			Check(context.Background(), nil, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})
}

// The agent ownership model derives two relations: the owner gets qualityOwner
// on the agent's databases, and the agent's own identity gets the agent role
// there.
func TestScopedAuthorizerDerivedAgentSources(t *testing.T) {
	t.Parallel()

	principal := &store.UserMessage{ID: 7}
	table := resource.NewRef(resource.TypeTable, "inst-1;sales;public;orders")
	otherTable := resource.NewRef(resource.TypeTable, "inst-1;other;public;orders")

	t.Run("an owner relation grants the owner role inside the database", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{relations: []*store.AgentGrantRelation{
			agentRelation(store.QualityOwnerRole, "instances/inst-1/databases/sales"),
		}}
		authorizer := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader)
		allowed, err := authorizer.Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.True(t, allowed)
		allowed, err = authorizer.Check(context.Background(), principal, permission.DataQualityRulesCreate, otherTable)
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("an agent identity relation grants only the agent permissions", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{relations: []*store.AgentGrantRelation{
			agentRelation(store.QualityAgentRole, "instances/inst-1/databases/sales"),
		}}
		authorizer := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader)
		allowed, err := authorizer.Check(context.Background(), principal, permission.DataQualityRulesGet, table)
		require.NoError(t, err)
		require.True(t, allowed)
		allowed, err = authorizer.Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("a malformed relation is skipped", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{relations: []*store.AgentGrantRelation{
			agentRelation(store.QualityOwnerRole, "not-a-database"),
		}}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})
}

// The snapshot is rebuilt only when the store's authorization generation moves;
// a role or grant write bumps it, a read does not.
func TestScopedAuthorizerCachesThePrincipalSnapshot(t *testing.T) {
	t.Parallel()

	principal := &store.UserMessage{ID: 7}
	table := resource.NewRef(resource.TypeTable, "i;db;public;orders")
	reader := &fakeGrantReader{grants: []*store.IamGrantMessage{qualityOwnerGrant(common.FormatUserUID(7))}}
	authorizer := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader)

	allowed, err := authorizer.Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = authorizer.Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, 1, reader.grantCalls)
	require.Equal(t, 1, reader.relationCalls)

	// A second permission reuses the same snapshot.
	scopes, err := authorizer.Scopes(context.Background(), principal, permission.DataQualityRulesCreate)
	require.NoError(t, err)
	require.False(t, scopes.IsEmpty())
	require.Equal(t, 1, reader.grantCalls)

	// A grant/role/agent write bumps the generation and the next read rebuilds.
	reader.generation = 1
	reader.grants = nil
	allowed, err = authorizer.Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
	require.NoError(t, err)
	require.False(t, allowed)
	require.Equal(t, 2, reader.grantCalls)
}

func TestScopedAuthorizerScopes(t *testing.T) {
	t.Parallel()

	principal := &store.UserMessage{ID: 7}

	t.Run("a workspace permission is All", func(t *testing.T) {
		t.Parallel()
		scopes, err := NewScopedAuthorizer(&fakeChecker{allowed: true}, &fakeGrantReader{}).
			Scopes(context.Background(), principal, permission.DataQualityRulesCreate)
		require.NoError(t, err)
		require.True(t, scopes.All)
	})

	t.Run("grants and relations become the scope set", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{
			grants: []*store.IamGrantMessage{
				qualityOwnerGrant(common.FormatUserUID(7)),
				{ResourceType: "instance", ResourceID: "i2", Role: common.FormatRole(store.QualityAgentRole), Member: common.FormatUserUID(7)},
			},
			relations: []*store.AgentGrantRelation{
				agentRelation(store.QualityOwnerRole, "instances/inst-1/databases/sales"),
			},
		}
		scopes, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Scopes(context.Background(), principal, permission.DataQualityRulesCreate)
		require.NoError(t, err)
		require.False(t, scopes.All)
		require.Equal(t, []resource.Scope{
			resource.NewScope(resource.TypeDatabase, "i;db"),
			resource.NewScope(resource.TypeDatabase, common.BuildMetaGUID("inst-1", "sales")),
		}, scopes.Scopes, "the agent role lacks rules.create, so only the owner grant and relation contribute")
	})

	t.Run("a condition-failing grant adds no scope", func(t *testing.T) {
		t.Parallel()
		grant := qualityOwnerGrant(common.FormatUserUID(7))
		grant.Condition = "resource.database == 'db'"
		reader := &fakeGrantReader{grants: []*store.IamGrantMessage{grant}}
		scopes, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Scopes(context.Background(), principal, permission.DataQualityRulesCreate)
		require.NoError(t, err)
		require.True(t, scopes.IsEmpty())
	})

	t.Run("an empty relationship set is empty", func(t *testing.T) {
		t.Parallel()
		scopes, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, &fakeGrantReader{}).
			Scopes(context.Background(), principal, permission.DataQualityRulesCreate)
		require.NoError(t, err)
		require.True(t, scopes.IsEmpty())
	})

	t.Run("a checker error is returned", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("policy read failed")
		scopes, err := NewScopedAuthorizer(&fakeChecker{err: cause}, &fakeGrantReader{}).
			Scopes(context.Background(), principal, permission.DataQualityRulesCreate)
		require.ErrorIs(t, err, cause)
		require.True(t, scopes.IsEmpty())
	})
}
