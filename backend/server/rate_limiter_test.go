package server

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// newFrozenRateLimiterStore returns a store whose clock never advances, so a test
// can assert the exact budget boundary instead of racing the token refill.
func newFrozenRateLimiterStore(limit rate.Limit, burst int) *boundedRateLimiterStore {
	store := newBoundedRateLimiterStore(limit, burst)
	now := time.Unix(0, 0)
	store.timeNow = func() time.Time { return now }
	store.lastCleanup = now
	return store
}

// The store must not grow with the number of identifiers a caller invents: the
// budget key of a key-less ingestion request is a caller-chosen header, and a
// rotating one would otherwise add a bucket per request (M14).
func TestBoundedRateLimiterStoreEvictsAtCapacity(t *testing.T) {
	t.Parallel()

	store := newBoundedRateLimiterStore(rate.Limit(1), 1)
	store.capacity = 2

	for _, identifier := range []string{"a", "b"} {
		allowed, err := store.Allow(identifier)
		require.NoError(t, err)
		require.True(t, allowed)
	}
	require.Len(t, store.visitors, 2)

	allowed, err := store.Allow("c")
	require.NoError(t, err)
	require.True(t, allowed)
	require.Len(t, store.visitors, 2, "the ceiling must hold")
	require.Contains(t, store.visitors, "c")

	survivors := 0
	for _, identifier := range []string{"a", "b"} {
		if _, ok := store.visitors[identifier]; ok {
			survivors++
		}
	}
	require.Equal(t, 1, survivors, "exactly one older bucket makes room")

	// An evicted identifier starts over with a full burst: making room grants, it
	// never denies, which is why the victim can be arbitrary and found in O(1).
	evicted := "a"
	if _, ok := store.visitors["a"]; ok {
		evicted = "b"
	}
	allowed, err = store.Allow(evicted)
	require.NoError(t, err)
	require.True(t, allowed)
}

// Rotating identifiers is the M14 attack on memory: the map must stay at the
// ceiling no matter how many the caller invents.
func TestBoundedRateLimiterStoreHoldsTheCeilingUnderRotation(t *testing.T) {
	t.Parallel()

	store := newBoundedRateLimiterStore(rate.Limit(1), 1)
	store.capacity = 8

	for i := 0; i < 100; i++ {
		allowed, err := store.Allow("rotated-" + strconv.Itoa(i))
		require.NoError(t, err)
		require.True(t, allowed, "a new identifier always gets its own full burst")
	}
	require.Len(t, store.visitors, 8)
}

// An idle bucket is swept instead of being kept until eviction, so a caller that
// presents one identifier and leaves cannot pin memory indefinitely.
func TestBoundedRateLimiterStorePrunesIdleBuckets(t *testing.T) {
	t.Parallel()

	store := newFrozenRateLimiterStore(1, 1)

	_, err := store.Allow("idle")
	require.NoError(t, err)

	store.timeNow = func() time.Time { return time.Unix(0, 0).Add(rateLimiterExpiresIn) }
	_, err = store.Allow("fresh")
	require.NoError(t, err)
	require.NotContains(t, store.visitors, "idle")
	require.Contains(t, store.visitors, "fresh")
}

// Bounding the map must not change what the limiter does: one identifier still
// gets its rate and burst.
func TestBoundedRateLimiterStoreStillLimitsOneIdentifier(t *testing.T) {
	t.Parallel()

	store := newFrozenRateLimiterStore(1, 1)

	allowed, err := store.Allow("key")
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = store.Allow("key")
	require.NoError(t, err)
	require.False(t, allowed, "the burst is spent")

	store.timeNow = func() time.Time { return time.Unix(0, 0).Add(time.Second) }
	allowed, err = store.Allow("key")
	require.NoError(t, err)
	require.True(t, allowed, "the bucket refills")
}

// Making room is forced on every request by a caller that rotates identifiers, so
// it must stay constant-time under the store's mutex: a search for a victim would
// be a cost an attacker can demand while blocking every other producer. Timing is
// coarse, so the bar is a large multiple of the same insertion work done without
// eviction; the pre-hardening search for the least recently seen bucket was
// measured at roughly 300x that work.
func TestBoundedRateLimiterStoreEvictionStaysConstantTime(t *testing.T) {
	t.Parallel()

	const rounds = 20000
	// measure times `rounds` insertions of identifiers the store does not yet
	// track, after pre-filling `fill` of them into a store that holds `capacity`.
	// The baseline fills well below capacity, so its timed loop never evicts; the
	// measured run starts full, so every insertion evicts.
	measure := func(capacity, fill int) time.Duration {
		store := newBoundedRateLimiterStore(rate.Limit(1), 1)
		store.capacity = capacity
		for i := 0; i < fill; i++ {
			_, _ = store.Allow("filler-" + strconv.Itoa(i))
		}
		start := time.Now()
		for i := 0; i < rounds; i++ {
			_, _ = store.Allow("rotated-" + strconv.Itoa(i))
		}
		return time.Since(start)
	}

	withoutEviction := measure(3*rounds, 2*rounds)
	withEviction := measure(rateLimiterCapacity, rateLimiterCapacity)
	require.Less(t, withEviction, 50*withoutEviction,
		"eviction at capacity must not scale with the number of tracked identifiers")
}
