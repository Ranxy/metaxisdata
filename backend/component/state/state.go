package state

import (
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pkg/errors"
)

// tokenRevocationCapacity bounds the process-local set of revoked access
// tokens. Logout only revokes a token it has already verified, so the set can
// only grow through legitimate logins; the LRU eviction is therefore safe.
const tokenRevocationCapacity = 4096

// SSOStateTTL is how long a one-time OAuth2 state nonce stays valid.
const SSOStateTTL = 5 * time.Minute

// ssoStateCapacity bounds the in-flight OAuth2 state nonces.
const ssoStateCapacity = 1024

type State struct {
	TokenExpireCache *lru.Cache[string, bool]
	// InstanceOutstandingConnections is the maximum number of connections per instance.
	InstanceOutstandingConnections *resourceLimiter
	// LoginLimiter throttles failed password logins per (email, source).
	LoginLimiter *LoginLimiter
	// SSOStateCache holds one-time OAuth2 state nonces issued by
	// CreateSSOState, mapped to their issue time.
	SSOStateCache *lru.Cache[string, time.Time]
}

func New() (*State, error) {
	expireCache, err := lru.New[string, bool](tokenRevocationCapacity)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create auth expire cache")
	}
	ssoStateCache, err := lru.New[string, time.Time](ssoStateCapacity)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create sso state cache")
	}
	return &State{
		InstanceOutstandingConnections: &resourceLimiter{connections: map[string]int{}},
		TokenExpireCache:               expireCache,
		LoginLimiter:                   newLoginLimiter(),
		SSOStateCache:                  ssoStateCache,
	}, nil
}

type resourceLimiter struct {
	sync.Mutex
	connections map[string]int
}

// limit <= 0 means no limit.
func (c *resourceLimiter) Increment(key string, limit int) bool {
	c.Lock()
	defer c.Unlock()
	if limit <= 0 {
		// No limit.
		// Increment anyway to balance the decrement.
		c.connections[key]++
		return false
	}
	if c.connections[key] >= limit {
		return true
	}
	c.connections[key]++
	return false
}

func (c *resourceLimiter) Decrement(key string) {
	c.Lock()
	defer c.Unlock()
	c.connections[key]--
}
