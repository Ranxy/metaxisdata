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

// ErrInstanceConnectionLimit reports that an instance is already using all of
// its outstanding connection slots; the caller should retry later.
var ErrInstanceConnectionLimit = errors.New("instance connection limit reached")

type State struct {
	TokenExpireCache *lru.Cache[string, bool]
	// InstanceOutstandingConnections counts the open connections per instance.
	InstanceOutstandingConnections *resourceLimiter
	// LoginLimiter throttles failed password logins per (email, source).
	LoginLimiter *LoginLimiter
	// SSOStateCache holds one-time OAuth2 state nonces issued by
	// CreateSSOState, mapped to their issue time.
	SSOStateCache *lru.Cache[string, time.Time]
	// DeviceLoginStore holds the in-flight device authorization requests.
	DeviceLoginStore *DeviceLoginStore
	// DeviceLoginLimiter throttles CreateDeviceLogin per source address.
	DeviceLoginLimiter *WindowLimiter
	// DeviceLoginLookupLimiter throttles GetDeviceLogin and ApproveDeviceLogin
	// per caller, so holding an account does not buy unlimited guesses at
	// someone else's user code.
	DeviceLoginLookupLimiter *WindowLimiter
	// OAuthAuthorizationRequestStore holds the pending OAuth 2.1 authorization
	// requests and the single-use authorization codes minted from them.
	OAuthAuthorizationRequestStore *OAuthAuthorizationRequestStore
	// MCPCallLimiter throttles MCP tool calls per principal. The endpoint is a
	// remote entry point a model drives, and every call reads the registry and
	// writes a ledger row.
	MCPCallLimiter *WindowLimiter
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
		DeviceLoginStore:               newDeviceLoginStore(),
		DeviceLoginLimiter:             newDeviceLoginCreateLimiter(),
		DeviceLoginLookupLimiter:       newDeviceLoginLookupLimiter(),
		OAuthAuthorizationRequestStore: NewOAuthAuthorizationRequestStore(),
		MCPCallLimiter:                 newMCPCallLimiter(),
	}, nil
}

type resourceLimiter struct {
	sync.Mutex
	connections map[string]int
}

// AcquireInstanceConnection reserves one of the instance's outstanding
// connection slots and returns the release function. A maximumConnections <= 0
// means no limit. Every path that opens a driver for an instance — the periodic
// sync, an API-triggered sync and the connection test — goes through it, so a
// slow or hostile target cannot stack connections past the instance's ceiling.
func (s *State) AcquireInstanceConnection(instanceID string, maximumConnections int) (func(), error) {
	if s.InstanceOutstandingConnections.Increment(instanceID, maximumConnections) {
		return nil, errors.Wrapf(ErrInstanceConnectionLimit, "instance %q already has %d outstanding connections", instanceID, maximumConnections)
	}
	return func() { s.InstanceOutstandingConnections.Decrement(instanceID) }, nil
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
	if c.connections[key] <= 1 {
		// Never go negative, and do not keep zero entries forever.
		delete(c.connections, key)
		return
	}
	c.connections[key]--
}
