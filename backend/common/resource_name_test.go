//nolint:revive
package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsValidRoleID(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"workspaceAdmin", "workspaceMember", "syncer", "read-only", "a", "a1"} {
		require.Truef(t, IsValidRoleID(id), "%q should be a valid role ID", id)
	}
	for _, id := range []string{"", "-lead", "has space", "has/slash", "trailing-", "a.b"} {
		require.Falsef(t, IsValidRoleID(id), "%q should not be a valid role ID", id)
	}
}

func TestGetRoleID(t *testing.T) {
	t.Parallel()

	id, err := GetRoleID("roles/workspaceAdmin")
	require.NoError(t, err)
	require.Equal(t, "workspaceAdmin", id)

	_, err = GetRoleID("roles/")
	require.Error(t, err)
	_, err = GetRoleID("users/101")
	require.Error(t, err)
	_, err = GetRoleID("roles/1bad")
	require.Error(t, err)
}

func TestFormatRoleRoundTrip(t *testing.T) {
	t.Parallel()

	require.Equal(t, "roles/syncer", FormatRole("syncer"))
	id, err := GetRoleID(FormatRole("syncer"))
	require.NoError(t, err)
	require.Equal(t, "syncer", id)
}

func TestGroupEmailRoundTrip(t *testing.T) {
	t.Parallel()

	require.Equal(t, "groups/eng@example.com", FormatGroupEmail("eng@example.com"))
	email, err := GetGroupEmail(FormatGroupEmail("eng@example.com"))
	require.NoError(t, err)
	require.Equal(t, "eng@example.com", email)
}
