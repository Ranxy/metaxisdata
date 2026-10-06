package state

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The connection test opens a driver like every other path, so it has to be
// counted by the same per-instance limiter instead of stacking attempts.
func TestAcquireInstanceConnectionSharesThePerInstanceLimit(t *testing.T) {
	t.Parallel()

	stateCfg, err := New()
	require.NoError(t, err)

	releaseSync, err := stateCfg.AcquireInstanceConnection("i1", 2)
	require.NoError(t, err)
	releaseTest, err := stateCfg.AcquireInstanceConnection("i1", 2)
	require.NoError(t, err)

	_, err = stateCfg.AcquireInstanceConnection("i1", 2)
	require.ErrorIs(t, err, ErrInstanceConnectionLimit)

	releaseSync()
	releaseThird, err := stateCfg.AcquireInstanceConnection("i1", 2)
	require.NoError(t, err)

	releaseTest()
	releaseThird()
	_, err = stateCfg.AcquireInstanceConnection("i1", 0)
	require.NoError(t, err, "a limit of zero means unlimited")
}

func TestResourceLimiterEnforcesTheLimit(t *testing.T) {
	t.Parallel()

	limiter := &resourceLimiter{connections: map[string]int{}}
	require.False(t, limiter.Increment("i1", 2))
	require.False(t, limiter.Increment("i1", 2))
	require.True(t, limiter.Increment("i1", 2), "the third connection exceeds the limit")
}

// A stray decrement used to drive the counter negative, which made the limiter
// refuse every later connection, and zero entries were never removed.
func TestResourceLimiterNeverGoesNegative(t *testing.T) {
	t.Parallel()

	limiter := &resourceLimiter{connections: map[string]int{}}
	require.False(t, limiter.Increment("i1", 2))
	require.False(t, limiter.Increment("i1", 2))
	limiter.Decrement("i1")
	limiter.Decrement("i1")
	limiter.Decrement("i1")

	_, ok := limiter.connections["i1"]
	require.False(t, ok, "the counter must not go negative or keep a zero entry")
	require.False(t, limiter.Increment("i1", 2), "the instance is usable again")
}

func TestResourceLimiterUnlimited(t *testing.T) {
	t.Parallel()

	limiter := &resourceLimiter{connections: map[string]int{}}
	for range 5 {
		require.False(t, limiter.Increment("i1", 0), "a limit of zero means unlimited")
	}
	limiter.Decrement("i1")
	require.Equal(t, 4, limiter.connections["i1"])
}
