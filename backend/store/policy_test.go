package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestPatchIamPolicyBindings(t *testing.T) {
	t.Parallel()

	t.Run("grants a role through a new binding", func(t *testing.T) {
		t.Parallel()

		policy := &storepb.IamPolicy{}
		patchIamPolicyBindings(policy, "users/alice", []string{"roles/workspaceAdmin"})

		require.Equal(t, []*storepb.Binding{
			{Role: "roles/workspaceAdmin", Members: []string{"users/alice"}},
		}, policy.Bindings)
	})

	t.Run("adds to an existing binding without duplicating the member", func(t *testing.T) {
		t.Parallel()

		policy := &storepb.IamPolicy{Bindings: []*storepb.Binding{
			{Role: "roles/workspaceAdmin", Members: []string{"users/bob"}},
		}}
		patchIamPolicyBindings(policy, "users/alice", []string{"roles/workspaceAdmin"})
		patchIamPolicyBindings(policy, "users/alice", []string{"roles/workspaceAdmin"})

		require.Equal(t, []*storepb.Binding{
			{Role: "roles/workspaceAdmin", Members: []string{"users/bob", "users/alice"}},
		}, policy.Bindings)
	})

	t.Run("revokes every role that is not requested", func(t *testing.T) {
		t.Parallel()

		policy := &storepb.IamPolicy{Bindings: []*storepb.Binding{
			{Role: "roles/workspaceAdmin", Members: []string{"users/alice", "users/bob"}},
			{Role: "roles/workspaceMember", Members: []string{"users/alice"}},
		}}
		patchIamPolicyBindings(policy, "users/alice", nil)

		require.Equal(t, []*storepb.Binding{
			{Role: "roles/workspaceAdmin", Members: []string{"users/bob"}},
			{Role: "roles/workspaceMember", Members: []string{}},
		}, policy.Bindings)
	})

	t.Run("moves a member between roles", func(t *testing.T) {
		t.Parallel()

		policy := &storepb.IamPolicy{Bindings: []*storepb.Binding{
			{Role: "roles/workspaceMember", Members: []string{"users/alice"}},
		}}
		patchIamPolicyBindings(policy, "users/alice", []string{"roles/workspaceAdmin"})

		require.Equal(t, []*storepb.Binding{
			{Role: "roles/workspaceMember", Members: []string{}},
			{Role: "roles/workspaceAdmin", Members: []string{"users/alice"}},
		}, policy.Bindings)
	})

	t.Run("creates missing roles in request order", func(t *testing.T) {
		t.Parallel()

		policy := &storepb.IamPolicy{}
		patchIamPolicyBindings(policy, "users/alice", []string{"roles/z", "roles/a"})

		require.Equal(t, []*storepb.Binding{
			{Role: "roles/z", Members: []string{"users/alice"}},
			{Role: "roles/a", Members: []string{"users/alice"}},
		}, policy.Bindings)
	})

	t.Run("keeps other members", func(t *testing.T) {
		t.Parallel()

		policy := &storepb.IamPolicy{Bindings: []*storepb.Binding{
			{Role: "roles/workspaceAdmin", Members: []string{"users/bob"}},
		}}
		patchIamPolicyBindings(policy, "users/alice", []string{"roles/workspaceMember"})

		require.Equal(t, []*storepb.Binding{
			{Role: "roles/workspaceAdmin", Members: []string{"users/bob"}},
			{Role: "roles/workspaceMember", Members: []string{"users/alice"}},
		}, policy.Bindings)
	})
}

func TestGenerateEtagIsMillisecondPrecision(t *testing.T) {
	t.Parallel()

	base := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	require.Equal(t, "1714564800000", generateEtag(base))
	require.Equal(t, generateEtag(base), generateEtag(base.Add(999*time.Microsecond)))
	require.NotEqual(t, generateEtag(base), generateEtag(base.Add(time.Millisecond)))
}
