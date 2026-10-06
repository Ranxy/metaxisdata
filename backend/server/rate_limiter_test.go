package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// The store must not grow with the number of identifiers a caller invents: the
// budget key of a key-less ingestion request is a caller-chosen header, and a
// rotating one would otherwise add a bucket per request (M14).
func TestBoundedRateLimiterStoreEvictsAtCapacity(t *testing.T) {
	t.Parallel()

	store := newBoundedRateLimiterStore(rate.Limit(1), 1)
	store.capacity = 2

	allowed, err := store.Allow("a")
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = store.Allow("b")
	require.NoError(t, err)
	require.True(t, allowed)
	require.Len(t, store.visitors, 2)

	allowed, err = store.Allow("c")
	require.NoError(t, err)
	require.True(t, allowed)
	require.Len(t, store.visitors, 2, "the ceiling must hold")
	require.NotContains(t, store.visitors, "a", "the least recently seen bucket makes room")
	require.Contains(t, store.visitors, "c")
}

// An idle bucket is swept instead of being kept until eviction, so a caller that
// presents one identifier and leaves cannot pin memory indefinitely.
func TestBoundedRateLimiterStorePrunesIdleBuckets(t *testing.T) {
	t.Parallel()

	store := newBoundedRateLimiterStore(rate.Limit(1), 1)
	now := time.Unix(0, 0)
	store.timeNow = func() time.Time { return now }
	store.lastCleanup = now

	_, err := store.Allow("idle")
	require.NoError(t, err)

	now = now.Add(rateLimiterExpiresIn)
	_, err = store.Allow("fresh")
	require.NoError(t, err)
	require.NotContains(t, store.visitors, "idle")
	require.Contains(t, store.visitors, "fresh")
}

// Bounding the map must not change what the limiter does: one identifier still
// gets its rate and burst.
func TestBoundedRateLimiterStoreStillLimitsOneIdentifier(t *testing.T) {
	t.Parallel()

	store := newBoundedRateLimiterStore(rate.Limit(1), 1)
	now := time.Unix(0, 0)
	store.timeNow = func() time.Time { return now }
	store.lastCleanup = now

	allowed, err := store.Allow("key")
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = store.Allow("key")
	require.NoError(t, err)
	require.False(t, allowed, "the burst is spent")

	now = now.Add(time.Second)
	allowed, err = store.Allow("key")
	require.NoError(t, err)
	require.True(t, allowed, "the bucket refills")
}
