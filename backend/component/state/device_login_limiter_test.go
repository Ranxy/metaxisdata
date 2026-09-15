package state

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeviceLoginLimiterBoundsOneSource(t *testing.T) {
	t.Parallel()

	limiter := newDeviceLoginLimiter()
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

	limiter := newDeviceLoginLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	for range deviceLoginSourceLimit {
		require.True(t, limiter.Allow("203.0.113.7", now))
	}
	require.False(t, limiter.Allow("203.0.113.7", now.Add(30*time.Second)))
	require.False(t, limiter.Allow("203.0.113.7", now.Add(59*time.Second)))

	require.True(t, limiter.Allow("203.0.113.7", now.Add(deviceLoginSourceWindow)),
		"the window starts at the first allowed request, not the last refused one")
}

func TestDeviceLoginLimiterCapsTheEndpointOverall(t *testing.T) {
	t.Parallel()

	limiter := newDeviceLoginLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	// One request each, so the per-source bucket is never the reason.
	for i := range deviceLoginGlobalLimit {
		source := "198.51.100." + strconv.Itoa(i)
		require.True(t, limiter.Allow(source, now), "source %d is under the global limit", i+1)
	}
	require.False(t, limiter.Allow("192.0.2.1", now), "the global limit is reached")

	require.True(t, limiter.Allow("192.0.2.1", now.Add(deviceLoginGlobalWindow)),
		"the global window rolls over")
}

// The source map is seeded directly: the global bucket caps one window at
// fewer requests than the map can hold, so the ceiling is only reached across
// windows.
func TestDeviceLoginLimiterPrunesAtCapacity(t *testing.T) {
	t.Parallel()

	limiter := newDeviceLoginLimiter()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := range deviceLoginLimiterCapacity {
		// Distinct times make the oldest entry deterministic.
		limiter.sources["198.51.100."+strconv.Itoa(i)] = loginAttempt{
			count: 1,
			first: now.Add(time.Duration(i) * time.Microsecond),
		}
	}
	require.Len(t, limiter.sources, deviceLoginLimiterCapacity)

	require.True(t, limiter.Allow("192.0.2.1", now.Add(time.Second)))
	require.Len(t, limiter.sources, deviceLoginLimiterCapacity, "the ceiling holds")
	require.NotContains(t, limiter.sources, "198.51.100.0", "the oldest entry is evicted")
	require.Contains(t, limiter.sources, "192.0.2.1")
}
