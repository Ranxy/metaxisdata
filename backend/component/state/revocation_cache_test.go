package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRevocationCacheKeepsADecisionUntilItExpires(t *testing.T) {
	t.Parallel()

	cache, err := newRevocationCache()
	require.NoError(t, err)
	now := time.Now()

	// An unknown token is not fresh, so the caller reads the table.
	_, fresh := cache.Lookup("unknown", now)
	require.False(t, fresh)

	cache.Remember("live-token", false, now)
	revoked, fresh := cache.Lookup("live-token", now)
	require.True(t, fresh)
	require.False(t, revoked)

	// Past the TTL the decision is stale and the table decides again.
	_, fresh = cache.Lookup("live-token", now.Add(revocationCacheTTL))
	require.False(t, fresh)

	cache.Revoke("logged-out", now)
	revoked, fresh = cache.Lookup("logged-out", now)
	require.True(t, fresh)
	require.True(t, revoked)
}

// A revocation performed here is refused immediately even though the cache held
// the opposite decision a moment ago.
func TestRevocationCacheOverwritesACachedLiveDecision(t *testing.T) {
	t.Parallel()

	cache, err := newRevocationCache()
	require.NoError(t, err)
	now := time.Now()

	cache.Remember("token", false, now)
	cache.Revoke("token", now)

	revoked, fresh := cache.Lookup("token", now)
	require.True(t, fresh)
	require.True(t, revoked)
}

// A table read that started before a concurrent logout can finish after it; its
// "not revoked" answer must not overwrite the revocation the logout just cached,
// or the token would be served until the next read.
func TestRevocationCacheDoesNotDowngradeARevocation(t *testing.T) {
	t.Parallel()

	cache, err := newRevocationCache()
	require.NoError(t, err)
	now := time.Now()

	cache.Revoke("token", now)
	cache.Remember("token", false, now)

	revoked, fresh := cache.Lookup("token", now)
	require.True(t, fresh)
	require.True(t, revoked, "a stale read must not clear a revocation")

	// A later read of the table is free to record a revocation, of course.
	cache.Remember("other", true, now)
	revoked, fresh = cache.Lookup("other", now)
	require.True(t, fresh)
	require.True(t, revoked)
}
