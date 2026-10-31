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

func TestGetNameParentTokensRejectsEmptyTokens(t *testing.T) {
	t.Parallel()

	_, err := GetNameParentTokens("instances//databases/x", "instances/", "databases/")
	require.Error(t, err)

	_, err = GetNameParentTokens("instances/i/databases/", "instances/", "databases/")
	require.Error(t, err)

	tokens, err := GetNameParentTokens("instances/i/databases/x", "instances/", "databases/")
	require.NoError(t, err)
	require.Equal(t, []string{"i", "x"}, tokens)
}

// RandomResourceID must produce ids that the name parsers accept, and the
// notification-channel parser is the one that shipped broken once: it rejected
// 99.8% of the ids the old generator produced.
func TestRandomResourceIDIsAValidNotificationChannelName(t *testing.T) {
	t.Parallel()

	for i := 0; i < 500; i++ {
		id, err := RandomResourceID(12)
		require.NoError(t, err)
		require.Truef(t, IsValidResourceID(id), "%q should match the resource id pattern", id)
		require.Falsef(t, id[0] >= '0' && id[0] <= '9', "%q must start with a letter", id)

		name := "workspaces/ws-1/notificationChannels/" + id
		parsed, err := GetNotificationChannelID(name)
		require.NoErrorf(t, err, "%q should parse", name)
		require.Equal(t, id, parsed)
	}
}

func TestGetNotificationChannelIDRejectsMalformedNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"",
		"notificationChannels/abc",
		"workspaces/ws-1/notificationChannels/",
		"workspaces/ws-1/notificationChannels/ABC",
		"workspaces/ws-1/notificationChannels/1abc",
		"workspaces/ws-1/qualityIssues/abc",
	} {
		_, err := GetNotificationChannelID(name)
		require.Errorf(t, err, "%q should be rejected", name)
	}
}

func TestRandomResourceIDIsAValidNotificationRouteName(t *testing.T) {
	t.Parallel()

	for i := 0; i < 200; i++ {
		id, err := RandomResourceID(12)
		require.NoError(t, err)

		name := "workspaces/ws-1/notificationRoutes/" + id
		parsed, err := GetNotificationRouteID(name)
		require.NoErrorf(t, err, "%q should parse", name)
		require.Equal(t, id, parsed)
	}
}

func TestGetNotificationRouteIDRejectsMalformedNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"",
		"notificationRoutes/abc",
		"workspaces/ws-1/notificationRoutes/",
		"workspaces/ws-1/notificationRoutes/ABC",
		"workspaces/ws-1/notificationRoutes/1abc",
		"workspaces/ws-1/notificationChannels/abc",
	} {
		_, err := GetNotificationRouteID(name)
		require.Errorf(t, err, "%q should be rejected", name)
	}
}
