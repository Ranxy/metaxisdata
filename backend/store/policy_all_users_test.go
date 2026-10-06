package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// M9: allUsers matches every authenticated principal, so it must not be a
// vehicle for widening what everyone who ever signs up is granted, and a full
// replace must not drop the implicit baseline binding.
func TestCheckAllUsersBinding(t *testing.T) {
	t.Parallel()

	baselineBinding := &storepb.Binding{Role: allUsersBaselineRole, Members: []string{common.AllUsers}}
	customReader := &storepb.Binding{Role: "roles/reader", Members: []string{common.AllUsers}}
	adminBinding := &storepb.Binding{Role: common.FormatRole(common.WorkspaceAdmin), Members: []string{common.AllUsers}}
	namedUser := &storepb.Binding{Role: "roles/reader", Members: []string{common.FormatUserUID(7)}}

	tests := []struct {
		name     string
		bindings []*storepb.Binding
		wantErr  string
	}{
		{
			name:     "the baseline binding alone is accepted",
			bindings: []*storepb.Binding{baselineBinding},
		},
		{
			name:     "other bindings without allUsers are accepted",
			bindings: []*storepb.Binding{baselineBinding, namedUser},
		},
		{
			name: "extra members on the baseline binding are accepted",
			bindings: []*storepb.Binding{{
				Role:    allUsersBaselineRole,
				Members: []string{common.AllUsers, common.FormatUserUID(7)},
			}},
		},
		{
			name:     "a duplicated baseline binding is accepted",
			bindings: []*storepb.Binding{baselineBinding, baselineBinding},
		},
		{
			name:     "allUsers bound to the admin role is refused",
			bindings: []*storepb.Binding{baselineBinding, adminBinding},
			wantErr:  "may only be bound to " + allUsersBaselineRole,
		},
		{
			name:     "allUsers bound to a custom role is refused",
			bindings: []*storepb.Binding{baselineBinding, customReader},
			wantErr:  "may only be bound to " + allUsersBaselineRole,
		},
		{
			name:     "a policy without the baseline binding is refused",
			bindings: []*storepb.Binding{namedUser},
			wantErr:  "must keep its " + allUsersBaselineRole + " binding to " + common.AllUsers,
		},
		{
			name:     "even allUsers in a custom role alone is refused for both reasons",
			bindings: []*storepb.Binding{customReader},
			wantErr:  "may only be bound to " + allUsersBaselineRole,
		},
		{
			name:     "an empty policy is refused",
			bindings: nil,
			wantErr:  "must keep its " + allUsersBaselineRole + " binding to " + common.AllUsers,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := CheckAllUsersBinding(&storepb.IamPolicy{Bindings: test.bindings})
			if test.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantErr)
		})
	}
}

// The incremental patch path runs against whatever policy is already stored, so
// it enforces the role half of the invariant only: a policy that predates the
// baseline binding is not a reason to fail a first-admin grant, but moving
// allUsers off the baseline is refused wherever it happens.
func TestCheckAllUsersRoleAllowsAMissingBaseline(t *testing.T) {
	t.Parallel()

	require.NoError(t, checkAllUsersRole(&storepb.IamPolicy{
		Bindings: []*storepb.Binding{{Role: common.FormatRole(common.WorkspaceAdmin), Members: []string{common.FormatUserUID(7)}}},
	}))

	err := checkAllUsersRole(&storepb.IamPolicy{
		Bindings: []*storepb.Binding{{Role: common.FormatRole(common.WorkspaceAdmin), Members: []string{common.AllUsers}}},
	})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}
