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

func TestParseAuditLogFilterRejectsUnknownField(t *testing.T) {
	t.Parallel()

	filter, err := parseAuditLogFilter(`actor == "users/101"`)
	require.Error(t, err)
	require.Nil(t, filter)
}
