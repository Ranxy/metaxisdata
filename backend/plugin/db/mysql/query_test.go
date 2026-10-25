package mysql

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestReadOnlyServerCapOutlivesClientDeadline pins the ordering that keeps the
// timeout single-sourced: if the server cap can fire first, MySQL can report the
// interrupt of a SELECT that reads no table as a successful row (an interrupted
// SLEEP returns 1), so the caller sees a result instead of the deadline error the
// timeout promises.
func TestReadOnlyServerCapOutlivesClientDeadline(t *testing.T) {
	t.Parallel()

	for _, timeout := range []time.Duration{
		time.Millisecond,
		250 * time.Millisecond,
		time.Second,
		time.Minute,
	} {
		require.Greater(t, readOnlyServerCapMillis(timeout), timeout.Milliseconds(), "timeout %v", timeout)
	}
}
