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
