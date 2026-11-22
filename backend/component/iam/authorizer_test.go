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

func TestWorkspaceAuthorizerCheck(t *testing.T) {
	t.Parallel()

	principal := &store.UserMessage{ID: 7}
	table := resource.NewRef(resource.TypeTable, "i;db;orders")

	t.Run("a missing principal is denied without a check", func(t *testing.T) {
		t.Parallel()
		checker := &fakeChecker{allowed: true}
		allowed, err := NewWorkspaceAuthorizer(checker).Check(context.Background(), nil, permission.DataQualityRulesGet, table)
		require.NoError(t, err)
		require.False(t, allowed)
		require.Zero(t, checker.calls)
	})

	t.Run("the workspace decision answers for the resource", func(t *testing.T) {
		t.Parallel()
		checker := &fakeChecker{allowed: true}
		allowed, err := NewWorkspaceAuthorizer(checker).Check(context.Background(), principal, permission.DataQualityRulesGet, table)
		require.NoError(t, err)
		require.True(t, allowed)
		require.Equal(t, 1, checker.calls)

		checker.allowed = false
		allowed, err = NewWorkspaceAuthorizer(checker).Check(context.Background(), principal, permission.DataQualityRulesGet, table)
		require.NoError(t, err)
		require.False(t, allowed)
	})

	t.Run("a checker error is returned", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("policy read failed")
		allowed, err := NewWorkspaceAuthorizer(&fakeChecker{err: cause}).Check(
			context.Background(), principal, permission.DataQualityRulesGet, table)
		require.ErrorIs(t, err, cause)
		require.False(t, allowed)
	})
}

func TestWorkspaceAuthorizerScopes(t *testing.T) {
	t.Parallel()

	principal := &store.UserMessage{ID: 7}
	scopes, err := NewWorkspaceAuthorizer(&fakeChecker{allowed: true}).Scopes(
		context.Background(), principal, permission.DataQualityRulesList)
	require.NoError(t, err)
	require.True(t, scopes.All)
	require.False(t, scopes.IsEmpty())

	scopes, err = NewWorkspaceAuthorizer(&fakeChecker{allowed: false}).Scopes(
		context.Background(), principal, permission.DataQualityRulesList)
	require.NoError(t, err)
	require.True(t, scopes.IsEmpty())

	cause := errors.New("policy read failed")
	scopes, err = NewWorkspaceAuthorizer(&fakeChecker{err: cause}).Scopes(
		context.Background(), principal, permission.DataQualityRulesList)
	require.ErrorIs(t, err, cause)
	require.True(t, scopes.IsEmpty())
}
