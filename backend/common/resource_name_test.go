//nolint:revive
package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The role ID is the segment of roles/{role}. The predefined IDs are camelCased,
// so uppercase is accepted here, unlike the lowercased resource IDs used in URL
// paths.
func TestRoleResourceName(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"workspaceAdmin", "workspaceMember", "syncer", "read-only", "a", "a1"} {
		name := FormatRole(id)
		require.Equal(t, "roles/"+id, name)
		parsed, err := GetRoleID(name)
		require.NoError(t, err)
		require.Equal(t, id, parsed)
	}
	for _, name := range []string{
		"roles/",
		"users/101",
		"roles/-lead",
		"roles/has space",
		"roles/has/slash",
		"roles/trailing-",
		"roles/a.b",
		"roles/1bad",
	} {
		_, err := GetRoleID(name)
		require.Errorf(t, err, "%q should be rejected", name)
	}
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
// notification parsers are the ones that shipped broken once: they rejected
// 99.8% of the ids the old generator produced.
func TestRandomResourceIDIsValidForNotificationNames(t *testing.T) {
	t.Parallel()

	for i := 0; i < 500; i++ {
		id, err := RandomResourceID(12)
		require.NoError(t, err)
		require.Truef(t, IsValidResourceID(id), "%q should match the resource id pattern", id)
		require.Falsef(t, id[0] >= '0' && id[0] <= '9', "%q must start with a letter", id)

		channelName := "workspaces/ws-1/notificationChannels/" + id
		parsed, err := GetNotificationChannelID(channelName)
		require.NoErrorf(t, err, "%q should parse", channelName)
		require.Equal(t, id, parsed)

		routeName := "workspaces/ws-1/notificationRoutes/" + id
		parsed, err = GetNotificationRouteID(routeName)
		require.NoErrorf(t, err, "%q should parse", routeName)
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
