package iam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/common/resource"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type fakeChecker struct {
	allowed bool
	err     error
	calls   int
}

func (f *fakeChecker) CheckPermission(context.Context, permission.Permission, *store.UserMessage) (bool, error) {
	f.calls++
	return f.allowed, f.err
}

type fakeAuthorizer struct {
	scopes     resource.ScopeSet
	err        error
	ref        resource.Ref
	checkCalls int
}

func (f *fakeAuthorizer) Check(_ context.Context, _ *store.UserMessage, _ permission.Permission, ref resource.Ref) (bool, error) {
	f.ref = ref
	f.checkCalls++
	return !f.scopes.IsEmpty(), f.err
}

func (f *fakeAuthorizer) Scopes(context.Context, *store.UserMessage, permission.Permission) (resource.ScopeSet, error) {
	return f.scopes, f.err
}

// The flat checker the in-process tool pipeline uses is the capability gate: the
// agent holds the permission when it holds it in at least one scope.
func TestScopedPermissionChecker(t *testing.T) {
	t.Parallel()

	principal := &store.UserMessage{ID: 7}

	allowed, err := NewScopedPermissionChecker(&fakeAuthorizer{scopes: resource.ScopeSet{
		Scopes: []resource.Scope{resource.NewScope(resource.TypeDatabase, "i;db")},
	}}).CheckPermission(context.Background(), permission.DataQualityRulesList, principal)
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = NewScopedPermissionChecker(&fakeAuthorizer{}).
		CheckPermission(context.Background(), permission.DataQualityRulesList, principal)
	require.NoError(t, err)
	require.False(t, allowed)

	allowed, err = NewScopedPermissionChecker(&fakeAuthorizer{}).
		CheckPermission(context.Background(), permission.DataQualityRulesList, nil)
	require.NoError(t, err)
	require.False(t, allowed)

	cause := errors.New("grant read failed")
	allowed, err = NewScopedPermissionChecker(&fakeAuthorizer{err: cause}).
		CheckPermission(context.Background(), permission.DataQualityRulesList, principal)
	require.ErrorIs(t, err, cause)
	require.False(t, allowed)
}

// CheckResource answers on one resource, which the agent's target session binds
// its tool checks to.
func TestScopedPermissionCheckerCheckResource(t *testing.T) {
	t.Parallel()

	principal := &store.UserMessage{ID: 7}
	ref := resource.NewRef(resource.TypeTable, "i;db;public;orders")
	authorizer := &fakeAuthorizer{scopes: resource.ScopeSet{
		Scopes: []resource.Scope{resource.NewScope(resource.TypeDatabase, "i;db")},
	}}
	allowed, err := NewScopedPermissionChecker(authorizer).
		CheckResource(context.Background(), permission.DataQualityRulesList, principal, ref)
	require.NoError(t, err)
	require.True(t, allowed)
	require.Equal(t, ref, authorizer.ref)

	allowed, err = NewScopedPermissionChecker(&fakeAuthorizer{}).
		CheckResource(context.Background(), permission.DataQualityRulesList, nil, ref)
	require.NoError(t, err)
	require.False(t, allowed)
}
