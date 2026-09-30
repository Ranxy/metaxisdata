package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseAuditLogFilter(t *testing.T) {
	t.Parallel()

	filter, err := parseAuditLogFilter(`resource == "users/101" && create_time >= "2026-05-28T00:00:00Z"`)
	require.NoError(t, err)
	require.NotNil(t, filter)
	require.Contains(t, filter.Where, "payload->>'resource' = $1")
	require.Contains(t, filter.Where, "created_at >= $2")
	require.Len(t, filter.Args, 2)
	require.Equal(t, "users/101", filter.Args[0])
	require.Equal(t, time.Date(2026, time.May, 28, 0, 0, 0, 0, time.UTC), filter.Args[1])
}

func TestParseAuditLogFilterMatches(t *testing.T) {
	t.Parallel()

	filter, err := parseAuditLogFilter(`method.matches("Login") && resource.matches("users")`)
	require.NoError(t, err)
	require.NotNil(t, filter)
	require.Contains(t, filter.Where, "LOWER(payload->>'method') LIKE $1")
	require.Contains(t, filter.Where, "LOWER(payload->>'resource') LIKE $2")
	require.Len(t, filter.Args, 2)
	require.Equal(t, "%login%", filter.Args[0])
	require.Equal(t, "%users%", filter.Args[1])
}

// The actor filter picks a user resource name, so it stays exact: a substring
// would let `users/10` select `users/101`.
func TestParseAuditLogFilterKeepsActorExact(t *testing.T) {
	t.Parallel()

	filter, err := parseAuditLogFilter(`user == "users/101"`)
	require.NoError(t, err)
	require.NotNil(t, filter)
	require.Contains(t, filter.Where, "payload->>'user' = $1")
	require.Equal(t, "users/101", filter.Args[0])

	_, err = parseAuditLogFilter(`user.matches("users")`)
	require.Error(t, err)
}

func TestParseAuditLogFilterRejectsUnknownField(t *testing.T) {
	t.Parallel()

	filter, err := parseAuditLogFilter(`actor == "users/101"`)
	require.Error(t, err)
	require.Nil(t, filter)
}
