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
	grants []*store.IamGrantMessage
	groups map[string]*store.GroupMessage
	find   *store.FindIamGrantMessage
	calls  int
}

func (f *fakeGrantReader) ListIamGrants(_ context.Context, find *store.FindIamGrantMessage) ([]*store.IamGrantMessage, error) {
	f.calls++
	f.find = find
	return f.grants, nil
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

func TestScopedAuthorizerCheck(t *testing.T) {
	t.Parallel()

	principal := &store.UserMessage{ID: 7}
	table := resource.NewRef(resource.TypeTable, "i;db;public;orders")

	t.Run("a workspace permission needs no grant read", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: true}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.True(t, allowed)
		require.Zero(t, reader.calls)
	})

	t.Run("a grant at an ancestor allows the target", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{grants: []*store.IamGrantMessage{qualityOwnerGrant(common.FormatUserUID(7))}}
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.True(t, allowed)
		require.NotNil(t, reader.find)
		require.Equal(t, []string{common.FormatUserUID(7), common.AllUsers}, reader.find.Members)
		require.True(t, reader.find.IncludeGroupGrants)
		require.NotEmpty(t, reader.find.Scopes)
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

	t.Run("a checker error is returned", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("policy read failed")
		allowed, err := NewScopedAuthorizer(&fakeChecker{err: cause}, &fakeGrantReader{}).
			Check(context.Background(), principal, permission.DataQualityRulesCreate, table)
		require.ErrorIs(t, err, cause)
		require.False(t, allowed)
	})

	t.Run("a missing principal is denied", func(t *testing.T) {
		t.Parallel()
		allowed, err := NewScopedAuthorizer(&fakeChecker{allowed: true}, &fakeGrantReader{}).
			Check(context.Background(), nil, permission.DataQualityRulesCreate, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})
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

	t.Run("scoped grants become the scope set", func(t *testing.T) {
		t.Parallel()
		reader := &fakeGrantReader{grants: []*store.IamGrantMessage{
			qualityOwnerGrant(common.FormatUserUID(7)),
			{ResourceType: "instance", ResourceID: "i2", Role: common.FormatRole(store.QualityAgentRole), Member: common.FormatUserUID(7)},
		}}
		scopes, err := NewScopedAuthorizer(&fakeChecker{allowed: false}, reader).
			Scopes(context.Background(), principal, permission.DataQualityRulesCreate)
		require.NoError(t, err)
		require.False(t, scopes.All)
		require.Equal(t, []resource.Scope{resource.NewScope(resource.TypeDatabase, "i;db")}, scopes.Scopes,
			"the agent role has no rules.create, so only the owner grant contributes")
	})

	t.Run("an empty grant set is empty", func(t *testing.T) {
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
