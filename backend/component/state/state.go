package state

import (
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pkg/errors"
)

type State struct {
	TokenExpireCache *lru.Cache[string, bool]
	// InstanceOutstandingConnections is the maximum number of connections per instance.
	InstanceOutstandingConnections *resourceLimiter
}

func New() (*State, error) {
	expireCache, err := lru.New[string, bool](128)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create auth expire cache")
	}
	return &State{
		InstanceOutstandingConnections: &resourceLimiter{connections: map[string]int{}},
		TokenExpireCache:               expireCache,
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
