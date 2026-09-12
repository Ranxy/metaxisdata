package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoginLimiterBlocksAfterTheLimit(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Now()

	require.False(t, limiter.Blocked("alice", now))
	for i := 0; i < loginAttemptLimit; i++ {
		require.False(t, limiter.Blocked("alice", now), "attempt %d must still be allowed", i+1)
		limiter.RecordFailure("alice", now)
	}
	require.True(t, limiter.Blocked("alice", now))

	// Another key is unaffected.
	require.False(t, limiter.Blocked("bob", now))

	// A success clears the window.
	limiter.Reset("alice")
	require.False(t, limiter.Blocked("alice", now))
}

func TestLoginLimiterWindowExpires(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Now()
	for i := 0; i < loginAttemptLimit; i++ {
		limiter.RecordFailure("alice", now)
	}
	require.True(t, limiter.Blocked("alice", now))

	// Just inside the window it is still blocked; past it, the slate is clean.
	require.True(t, limiter.Blocked("alice", now.Add(loginAttemptWindow-time.Second)))
	require.False(t, limiter.Blocked("alice", now.Add(loginAttemptWindow)))
}

func TestLoginLimiterPrunes(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Now()
	// Fill past the capacity with expired windows; the limiter must not grow
	// without bound.
	for i := 0; i < loginAttemptCapacity+10; i++ {
		limiter.RecordFailure(string(rune('a'+i%26))+time.Duration(i).String(), now.Add(-2*loginAttemptWindow))
	}
	require.LessOrEqual(t, len(limiter.attempts), loginAttemptCapacity+1)
}
