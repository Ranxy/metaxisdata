package state

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The two dimensions are independent: an account is protected from distributed
// guessing, and a source is protected from spraying across accounts. A single
// (email, source) key could do neither.
func TestLoginLimiterCountsAccountsAndSourcesSeparately(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Now()

	for range loginAccountAttemptLimit {
		limiter.RecordFailure("alice@example.com", "203.0.113.5", now)
	}
	require.True(t, limiter.Blocked("alice@example.com", "203.0.113.5", now))
	// The account is blocked from every other source too: that is the point of
	// the account dimension.
	require.True(t, limiter.Blocked("alice@example.com", "198.51.100.9", now))
	// Other accounts are unaffected while this source is still below its own cap.
	require.False(t, limiter.Blocked("bob@example.com", "203.0.113.5", now))
}

func TestLoginLimiterBlocksASprayingSource(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Now()

	// One failure per email: no account reaches its limit, but the source does.
	for i := range loginSourceAttemptLimit {
		limiter.RecordFailure(string(rune('a'+i))+time.Duration(i).String()+"@example.com", "203.0.113.5", now)
	}
	require.True(t, limiter.Blocked("victim@example.com", "203.0.113.5", now))
	require.False(t, limiter.Blocked("victim@example.com", "198.51.100.9", now))
}

func TestLoginLimiterResetAccountKeepsTheSourceCounter(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Now()
	for range loginAccountAttemptLimit {
		limiter.RecordFailure("alice@example.com", "203.0.113.5", now)
	}
	require.True(t, limiter.Blocked("alice@example.com", "203.0.113.5", now))

	limiter.ResetAccount("alice@example.com")
	require.False(t, limiter.Blocked("alice@example.com", "203.0.113.5", now))

	// The source keeps the ten failures it already recorded, so a caller who
	// knows one password cannot reset the spraying budget from a shared address.
	for range loginSourceAttemptLimit - loginAccountAttemptLimit {
		limiter.RecordFailure("carol@example.com", "203.0.113.5", now)
	}
	require.True(t, limiter.Blocked("dave@example.com", "203.0.113.5", now))
}

func TestLoginLimiterNormalizesTheAccount(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Now()
	for range loginAccountAttemptLimit {
		limiter.RecordFailure("  Alice@Example.com ", "203.0.113.5", now)
	}
	require.True(t, limiter.Blocked("alice@example.com", "203.0.113.5", now))
}

func TestLoginLimiterWindowExpires(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Now()
	for range loginAccountAttemptLimit {
		limiter.RecordFailure("alice@example.com", "203.0.113.5", now)
	}
	require.True(t, limiter.Blocked("alice@example.com", "203.0.113.5", now))

	// Just inside the window it is still blocked; past it, the slate is clean.
	require.True(t, limiter.Blocked("alice@example.com", "203.0.113.5", now.Add(loginAttemptWindow-time.Second)))
	require.False(t, limiter.Blocked("alice@example.com", "203.0.113.5", now.Add(loginAttemptWindow)))
}

// Both maps are bounded: at capacity, recording a new key drops the oldest
// window instead of letting memory grow with the number of accounts tried.
func TestLoginLimiterAtCapacityEvictsTheOldestWindow(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	// The oldest account and a younger one exhaust their budgets first.
	for range loginAccountAttemptLimit {
		limiter.RecordFailure("oldest@example.com", "203.0.113.1", now)
		limiter.RecordFailure("second@example.com", "203.0.113.2", now.Add(time.Microsecond))
	}
	require.True(t, limiter.Blocked("oldest@example.com", "198.51.100.1", now))

	// Fill both maps to the ceiling with live windows, so expiry cannot be what
	// makes room.
	for i := range loginAttemptCapacity - 2 {
		limiter.RecordFailure("account-"+strconv.Itoa(i), "source-"+strconv.Itoa(i),
			now.Add(time.Duration(i+2)*time.Microsecond))
	}

	// One more distinct account evicts the oldest window rather than growing the map.
	limiter.RecordFailure("newest@example.com", "192.0.2.1", now.Add(time.Second))
	require.False(t, limiter.Blocked("oldest@example.com", "198.51.100.1", now.Add(time.Second)),
		"the oldest window was evicted at capacity")
	require.True(t, limiter.Blocked("second@example.com", "198.51.100.2", now.Add(time.Second)),
		"the younger spent window is untouched")
}
