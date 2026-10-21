package state

import (
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/pkg/errors"
)

// SSOStateTTL is how long a one-time OAuth2 state nonce stays valid.
const SSOStateTTL = 5 * time.Minute

// ssoStateCapacity bounds the in-flight OAuth2 state nonces. It has to stay above
// everything the anonymous budget can admit while a nonce is still usable. The
// budget's window is fixed rather than sliding, so the adversary's first quota may
// start up to one window before the nonce is minted and still land inside its
// lifetime, and every window boundary after that hands out another: the ceiling is
// (ceil(SSOStateTTL/throttleWindow) + 1) × ssoStateRequestGlobalLimit — 6 × 300 = 1800
// for the shipped values. Below it a caller that only mints nonces could fill the
// cache and evict the state of everyone who is mid-sign-in — the DoS the budget exists
// to close. window_limiter_test.go pins the relation and measures the property itself
// against the real limiter and cache.
const ssoStateCapacity = 4096

type State struct {
	// TokenRevocationCache caches lookups in the persistent revoked_token table,
	// which is the authority; see RevocationCache.
	TokenRevocationCache *RevocationCache
	// GrantRevocationCache caches whether an MCP access token's connected app is
	// still live, in front of the grant row, which is the authority. It is a
	// separate instance from TokenRevocationCache: token entries are written on
	// every authenticated request, so sharing one bounded cache would let token
	// traffic evict connection entries.
	GrantRevocationCache *RevocationCache
	// instanceConnections hands out the per-instance connection budget to every
	// path that opens a driver against a target database. See
	// .agents/docs/instance-connections.md.
	instanceConnections *connectionGovernor
	// LoginLimiter throttles failed password logins per account and per source.
	LoginLimiter *LoginLimiter
	// LoginRequestLimiter bounds anonymous Login requests per source and
	// globally, before either the account limiter or bcrypt runs.
	LoginRequestLimiter *WindowLimiter
	// CreateUserRequestLimiter bounds anonymous CreateUser requests per source
	// and globally; each one checks email existence and hashes a password.
	CreateUserRequestLimiter *WindowLimiter
	// LogoutRequestLimiter bounds anonymous Logout requests per source and
	// globally; each one writes a ledger row even when the revocation it asks
	// for is a repeated no-op.
	LogoutRequestLimiter *WindowLimiter
	// PrincipalRequestLimiter bounds the ledger-writing calls of a signed-in
	// caller, keyed by principal and procedure; the ledger never shrinks, so its
	// growth is bounded for an authenticated caller too. See
	// PrincipalThrottleInterceptor.
	PrincipalRequestLimiter *WindowLimiter
	// SSOStateCache holds one-time OAuth2 state nonces issued by
	// CreateSSOState, mapped to their issue time.
	SSOStateCache *lru.Cache[string, time.Time]
	// SSOStateRequestLimiter bounds anonymous CreateSSOState requests per source
	// and globally, before the handler adds a nonce to SSOStateCache.
	SSOStateRequestLimiter *WindowLimiter
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
	// MCPSourceCallLimiter bounds requests to the MCP endpoint per source
	// address, counted before the bearer check, so an unauthenticated probe is
	// bounded even though it never becomes a tool call.
	MCPSourceCallLimiter *WindowLimiter
}

func New() (*State, error) {
	revocationCache, err := newRevocationCache()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create token revocation cache")
	}
	grantRevocationCache, err := newRevocationCache()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create grant revocation cache")
	}
	ssoStateCache, err := lru.New[string, time.Time](ssoStateCapacity)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create sso state cache")
	}
	return &State{
		instanceConnections:            newConnectionGovernor(),
		TokenRevocationCache:           revocationCache,
		GrantRevocationCache:           grantRevocationCache,
		LoginLimiter:                   newLoginLimiter(),
		LoginRequestLimiter:            newLoginRequestLimiter(),
		CreateUserRequestLimiter:       newCreateUserRequestLimiter(),
		LogoutRequestLimiter:           newLogoutRequestLimiter(),
		PrincipalRequestLimiter:        newPrincipalRequestLimiter(),
		SSOStateCache:                  ssoStateCache,
		SSOStateRequestLimiter:         newSSOStateRequestLimiter(),
		DeviceLoginStore:               newDeviceLoginStore(),
		DeviceLoginLimiter:             newDeviceLoginCreateLimiter(),
		DeviceLoginLookupLimiter:       newDeviceLoginLookupLimiter(),
		OAuthAuthorizationRequestStore: NewOAuthAuthorizationRequestStore(),
		MCPCallLimiter:                 newMCPCallLimiter(),
		MCPSourceCallLimiter:           newMCPSourceCallLimiter(),
	}, nil
}
