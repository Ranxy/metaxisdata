package state

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Every limiter this package wires spends its own fixed window on the key it is
// given: the key is refused at the budget and another key is unaffected, so one
// caller cannot spend another's. The table is the registry of budgets — a limiter
// added without a row would be a budget nobody pinned.
func TestRequestLimitersEnforceTheirOwnBudgetsPerKey(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		limiter *WindowLimiter
		limit   int
	}{
		{"device login create per source", newDeviceLoginCreateLimiter(), deviceLoginSourceLimit},
		{"device login lookup per caller", newDeviceLoginLookupLimiter(), deviceLoginLookupLimit},
		{"anonymous login per source", newLoginRequestLimiter(), loginRequestSourceLimit},
		{"anonymous create user per source", newCreateUserRequestLimiter(), createUserRequestSourceLimit},
		{"anonymous logout per source", newLogoutRequestLimiter(), logoutRequestSourceLimit},
		{"mcp call per principal", newMCPCallLimiter(), mcpCallLimit},
		{"mcp endpoint per address", newMCPSourceCallLimiter(), mcpSourceCallLimit},
		{"sso state per source", newSSOStateRequestLimiter(), ssoStateSourceLimit},
		{"principal request per (principal, procedure)", newPrincipalRequestLimiter(), principalMethodLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i := range tc.limit {
				require.True(t, tc.limiter.Allow("first", now), "request %d is under the limit", i+1)
			}
			require.False(t, tc.limiter.Allow("first", now), "the key's budget is spent")
			require.True(t, tc.limiter.Allow("second", now), "another key has its own bucket")
		})
	}
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

// The source map is seeded directly: the global bucket caps one window at one
// request per source, far below what the map can hold, so the ceiling is only
// reached across windows. A seeded entry is spent, so its presence is observable
// through Allow: it refuses until the create that hits the ceiling evicts it.
func TestDeviceLoginLimiterPrunesAtCapacity(t *testing.T) {
	t.Parallel()

	limiter := newDeviceLoginCreateLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := range limiterCapacity {
		// Distinct times make the oldest entry deterministic.
		limiter.sources["198.51.100."+strconv.Itoa(i)] = loginAttempt{
			count: deviceLoginSourceLimit,
			first: now.Add(time.Duration(i) * time.Microsecond),
		}
	}

	require.True(t, limiter.Allow("192.0.2.1", now.Add(time.Second)), "a full map still admits one more")
	require.False(t, limiter.Allow("198.51.100.1", now.Add(time.Second)),
		"the other spent windows are untouched")
	require.True(t, limiter.Allow("198.51.100.0", now.Add(time.Second)),
		"the oldest entry was evicted, so that source has a fresh bucket")
}

// The flood spends the worst case the counter allows: a full quota one window
// before the victim mints its nonce (so the next quota lands immediately inside the
// victim's lifetime), then a fresh quota at every window boundary until the nonce
// expires. The cache must hold every nonce the budget can admit, or a caller that
// only mints nonces evicts the state of users who are mid-sign-in — the DoS the
// budget exists to close, because the capacity, not the request rate, is then the
// binding constraint.
func TestSSOStateFloodCannotEvictAValidNonce(t *testing.T) {
	t.Parallel()

	stateCfg, err := New(nil)
	require.NoError(t, err)

	minted := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	const victim = "victim-nonce"
	stateCfg.SSOStateCache.Add(victim, minted)

	limiter := stateCfg.SSOStateRequestLimiter
	admitted := 0
	for quotaTime := minted.Add(-throttleWindow); quotaTime.Before(minted.Add(SSOStateTTL)); quotaTime = quotaTime.Add(throttleWindow) {
		// Distinct sources: the flood is not one client's, and the global budget is
		// what has to hold.
		for i := range ssoStateGlobalLimit {
			if !limiter.Allow("198.51.100."+strconv.Itoa(i), quotaTime) {
				break
			}
			admitted++
			stateCfg.SSOStateCache.Add("flood-"+strconv.Itoa(admitted), quotaTime)
		}
	}
	require.GreaterOrEqual(t, admitted, ssoStateGlobalLimit, "the flood has to be able to spend at least one quota")
	require.True(t, stateCfg.SSOStateCache.Contains(victim),
		"a nonce a user just minted must survive the anonymous flood for its whole TTL")
}
