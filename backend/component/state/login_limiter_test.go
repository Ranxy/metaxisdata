package state

import (
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

func TestLoginLimiterPrunes(t *testing.T) {
	t.Parallel()

	limiter := newLoginLimiter()
	now := time.Now()
	// Fill past the capacity with expired windows; neither map may grow without
	// bound.
	for i := range loginAttemptCapacity + 10 {
		limiter.RecordFailure(string(rune('a'+i%26))+time.Duration(i).String()+"@example.com",
			"203.0.113."+time.Duration(i%200).String(), now.Add(-2*loginAttemptWindow))
	}
	require.LessOrEqual(t, len(limiter.accounts), loginAttemptCapacity+1)
	require.LessOrEqual(t, len(limiter.sources), loginAttemptCapacity+1)
}
