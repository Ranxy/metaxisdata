package state

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
