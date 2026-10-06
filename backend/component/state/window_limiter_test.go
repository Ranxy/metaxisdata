package state

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceLoginLimiterBoundsOneSource(t *testing.T) {
	t.Parallel()

	limiter := newDeviceLoginCreateLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	for i := range deviceLoginSourceLimit {
		require.True(t, limiter.Allow("203.0.113.7", now), "request %d is under the limit", i+1)
	}
	require.False(t, limiter.Allow("203.0.113.7", now), "the limit is reached")
	require.True(t, limiter.Allow("203.0.113.8", now), "another source has its own bucket")
}

// A refused request must not count, otherwise hammering the endpoint would keep
// pushing the window forward and lock the source out forever.
func TestDeviceLoginLimiterRefusedRequestsDoNotExtendTheWindow(t *testing.T) {
	t.Parallel()

	limiter := newDeviceLoginCreateLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	for range deviceLoginSourceLimit {
		require.True(t, limiter.Allow("203.0.113.7", now))
	}
	require.False(t, limiter.Allow("203.0.113.7", now.Add(30*time.Second)))
	require.False(t, limiter.Allow("203.0.113.7", now.Add(59*time.Second)))

	require.True(t, limiter.Allow("203.0.113.7", now.Add(throttleWindow)),
		"the window starts at the first allowed request, not the last refused one")
}

func TestDeviceLoginLimiterCapsTheEndpointOverall(t *testing.T) {
	t.Parallel()

	limiter := newDeviceLoginCreateLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	// One request each, so the per-source bucket is never the reason.
	for i := range deviceLoginGlobalLimit {
		source := "198.51.100." + strconv.Itoa(i)
		require.True(t, limiter.Allow(source, now), "source %d is under the global limit", i+1)
	}
	require.False(t, limiter.Allow("192.0.2.1", now), "the global limit is reached")

	require.True(t, limiter.Allow("192.0.2.1", now.Add(throttleWindow)),
		"the global window rolls over")
}

// The source map is seeded directly: the global bucket caps one window at
// fewer requests than the map can hold, so the ceiling is only reached across
// windows.
func TestDeviceLoginLimiterPrunesAtCapacity(t *testing.T) {
	t.Parallel()

	limiter := newDeviceLoginCreateLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := range limiterCapacity {
		// Distinct times make the oldest entry deterministic.
		limiter.sources["198.51.100."+strconv.Itoa(i)] = loginAttempt{
			count: 1,
			first: now.Add(time.Duration(i) * time.Microsecond),
		}
	}
	require.Len(t, limiter.sources, limiterCapacity)

	require.True(t, limiter.Allow("192.0.2.1", now.Add(time.Second)))
	require.Len(t, limiter.sources, limiterCapacity, "the ceiling holds")
	require.NotContains(t, limiter.sources, "198.51.100.0", "the oldest entry is evicted")
	require.Contains(t, limiter.sources, "192.0.2.1")
}

// The lookup budget is per caller, so one account cannot spend another's.
func TestDeviceLoginLookupLimiterIsPerCaller(t *testing.T) {
	t.Parallel()

	limiter := newDeviceLoginLookupLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	for range deviceLoginLookupLimit {
		require.True(t, limiter.Allow("user:42", now))
	}
	require.False(t, limiter.Allow("user:42", now), "the caller's budget is spent")
	require.True(t, limiter.Allow("user:7", now), "another caller has its own budget")
}

// The MCP budget is per principal, not per token or per address: one agent
// cannot spend another's, and a model that runs a call loop eventually hits it.
func TestMCPCallLimiterIsPerPrincipal(t *testing.T) {
	t.Parallel()

	limiter := newMCPCallLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	for i := range mcpCallLimit {
		require.True(t, limiter.Allow("user:42", now), "call %d is under the limit", i+1)
	}
	require.False(t, limiter.Allow("user:42", now), "the principal's budget is spent")
	require.True(t, limiter.Allow("user:7", now), "another principal has its own budget")
	require.True(t, limiter.Allow("user:42", now.Add(throttleWindow)), "the window rolls over")
}

// CreateSSOState is anonymous and writes one nonce into the bounded state cache
// per call, so the budget is what keeps one source from spending the cache.
func TestSSOStateLimiterBoundsOneSource(t *testing.T) {
	t.Parallel()

	limiter := newSSOStateRequestLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	for i := range ssoStateSourceLimit {
		require.True(t, limiter.Allow("203.0.113.7", now), "request %d is under the limit", i+1)
	}
	require.False(t, limiter.Allow("203.0.113.7", now), "the limit is reached")
	require.True(t, limiter.Allow("203.0.113.8", now), "another source has its own bucket")
}

// The cache has to hold every nonce the global budget can admit while a nonce is
// still usable. The window is fixed rather than sliding, so one TTL can cover a full
// quota at the end of a window and another at the start of the next: the bound is one
// quota per window *plus one*, not the TTL divided by the window. Below it a caller
// that only mints nonces fills the cache and evicts the state of users who are
// mid-sign-in — the DoS the budget exists to close, because the capacity, not the
// request rate, is then the binding constraint. Changing either number alone breaks
// this relation rather than the test.
func TestSSOStateCacheOutgrowsTheStateBudget(t *testing.T) {
	t.Parallel()

	quotaPerWindow := int(SSOStateTTL/throttleWindow) + 1
	admittedPerTTL := ssoStateGlobalLimit * quotaPerWindow
	require.Greater(t, ssoStateCapacity, admittedPerTTL,
		"the state cache must hold every nonce the global budget admits within one TTL")
}
