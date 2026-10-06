package state

import (
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Revocation decisions are cached in front of the persistent revoked_token
// table, which is the authority. The cache only saves a database read: an
// eviction or an expiry costs one lookup, never an accepted revoked token, so a
// principal cannot evict another principal's revocation the way a bounded set of
// revoked tokens could. The TTL bounds how long this process may keep believing a
// token is live after another replica revoked it; a revocation performed here
// enters the cache immediately.
const (
	revocationCacheTTL      = 30 * time.Second
	revocationCacheCapacity = 8192
)

// RevocationCache remembers whether a token id has been revoked.
type RevocationCache struct {
	entries *lru.Cache[string, revocationDecision]
}

type revocationDecision struct {
	revoked   bool
	checkedAt time.Time
}

func newRevocationCache() (*RevocationCache, error) {
	entries, err := lru.New[string, revocationDecision](revocationCacheCapacity)
	if err != nil {
		return nil, err
	}
	return &RevocationCache{entries: entries}, nil
}

// Lookup returns the cached decision for a token id and whether it is still
// fresh. A stale entry is treated as unknown so the caller reads the table.
func (c *RevocationCache) Lookup(tokenID string, now time.Time) (revoked, fresh bool) {
	decision, ok := c.entries.Get(tokenID)
	if !ok || now.Sub(decision.checkedAt) >= revocationCacheTTL {
		return false, false
	}
	return decision.revoked, true
}

// Remember records a decision read from the persistent table. It never
// downgrades a revoked decision already in the cache: a table read that started
// before a concurrent logout can finish after it, and overwriting the entry
// would serve the revoked token until the next read.
func (c *RevocationCache) Remember(tokenID string, revoked bool, now time.Time) {
	if !revoked {
		if existing, ok := c.entries.Peek(tokenID); ok && existing.revoked {
			return
		}
	}
	c.entries.Add(tokenID, revocationDecision{revoked: revoked, checkedAt: now})
}

// Revoke records a revocation this process just performed, so the token is
// refused immediately instead of after the next table read.
func (c *RevocationCache) Revoke(tokenID string, now time.Time) {
	c.entries.Add(tokenID, revocationDecision{revoked: true, checkedAt: now})
}
