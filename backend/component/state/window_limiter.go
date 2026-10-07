package state

import (
	"sync"
	"time"
)

// Throttling budgets, all counted over a fixed window: a key's window opens with its
// first allowed request and lasts throttleWindow. The device login endpoints need two
// different ones:
//
//   - creating a request is anonymous, allocates memory and drives a human
//     approval, so it is counted per source address with a loose global backstop
//     for the case where every caller shares one address (a reverse proxy that
//     is not listed as trusted);
//   - reading or deciding a request needs a signed-in caller, and is counted per
//     caller so holding an account does not buy unlimited guesses at someone
//     else's user code.
//
// The anonymous ConnectRPC methods that spend a bcrypt comparison or hash on an
// unauthenticated caller — Login and CreateUser — carry a source budget and a
// global backstop. The source budget is what bounds one client's CPU; the global
// one is what bounds the deployment's when every caller appears to share one
// address. Login is the more generous of the two because a person mistyping a
// password is normal use.
//
// CreateSSOState spends no CPU: it writes one nonce into the bounded SSO state
// cache. Its budget is what keeps one caller from evicting the nonces of users who
// are mid-sign-in; it mirrors the Login budget because the two are two halves of
// one sign-in flow. The cache is sized above what that global budget can admit
// while a nonce is still usable, which is what makes the budget a bound on the cache
// and not only on the request rate.
//
// The MCP budget is per principal rather than per address: agents behind one
// address share it, and every call reads the registry and writes a ledger row.
// It bounds abuse rather than normal use — a client would have to sustain ten
// calls a second to reach it.
//
// The principal budget generalizes that to every authenticated method that writes
// a ledger row (plus the calls that reach an upstream LLM): the ledger is kept
// forever, so its growth has to be bounded for a signed-in caller too, not only
// for the anonymous endpoints. A request refused by it never runs and therefore
// leaves no row, which is what makes it backpressure on the caller rather than a
// silent drop from the ledger.
const (
	// throttleWindow is the window every budget counts over.
	throttleWindow = time.Minute

	// limiterCapacity bounds the tracked sources in each limiter.
	limiterCapacity = 4096

	deviceLoginSourceLimit  = 10
	deviceLoginGlobalLimit  = 100
	deviceLoginLookupLimit  = 60
	deviceLoginLookupGlobal = 600

	// The per-source Login budget is deliberately loose: an office behind one NAT
	// address is normal use, and a tight per-source cap would lock real users out.
	// The global budget is what bounds the deployment's bcrypt work.
	loginRequestSourceLimit = 120
	loginRequestGlobalLimit = 300

	createUserRequestSourceLimit = 20
	createUserRequestGlobalLimit = 100

	// Logout is anonymous, audited and idempotent: a caller holding one valid
	// token can replay it, and every replay still writes a permanent ledger row
	// even though the revocation itself is a no-op. It neither spends bcrypt nor
	// writes a new revocation record, so its own budget is enough and it does not
	// share Login's.
	logoutRequestSourceLimit = 120
	logoutRequestGlobalLimit = 300

	// CreateSSOState also gates the outbound call to the identity provider, which
	// only a state this server issued can reach. The budget mirrors Login's: a
	// tighter one would cap the flow below the login budget it feeds.
	ssoStateSourceLimit = 120
	ssoStateGlobalLimit = 300

	mcpCallLimit  = 600
	mcpCallGlobal = 6000

	// The MCP endpoint also carries an address budget, counted before the bearer
	// check because CallLimiter cannot see a probe that carries no valid token.
	// Ten times the per-principal numbers, so it bounds probing without pacing a
	// fleet of agents that share one NAT address.
	mcpSourceCallLimit  = 6000
	mcpSourceCallGlobal = 60000

	// A signed-in caller may make this many ledger-writing calls a minute to one
	// method, and the whole deployment this many across every covered method.
	// Deliberately far above normal use (a bulk import is thousands of rows, not
	// thousands of calls a minute) because the budget exists to bound a loop, not
	// to pace work; a tool that drives the API can otherwise grow a ledger that
	// is never pruned.
	principalMethodLimit = 3000
	principalGlobalLimit = 30000
	// Principal keys are (principal, procedure) pairs — a set the server chooses,
	// not the caller — so this ceiling only has to cover the deployment's user
	// count times the covered method count.
	principalLimiterCapacity = 16384
)

// WindowLimiter is an in-memory fixed-window counter: a key's window opens with its
// first allowed request and lasts throttleWindow, so a full quota can land at the end
// of one window and another at the start of the next. A request is counted only when
// it is allowed, so hammering a refused key cannot keep pushing its window forward.
type WindowLimiter struct {
	mu      sync.Mutex
	sources map[string]loginAttempt
	global  loginAttempt

	sourceLimit int
	globalLimit int
	window      time.Duration
	capacity    int
}

// Every budget counts over the same window; the source and global dimensions
// share it so both expire together.
func newWindowLimiter(sourceLimit, globalLimit int) *WindowLimiter {
	return &WindowLimiter{
		sources:     map[string]loginAttempt{},
		sourceLimit: sourceLimit,
		globalLimit: globalLimit,
		window:      throttleWindow,
		capacity:    limiterCapacity,
	}
}

func newDeviceLoginCreateLimiter() *WindowLimiter {
	return newWindowLimiter(deviceLoginSourceLimit, deviceLoginGlobalLimit)
}

func newDeviceLoginLookupLimiter() *WindowLimiter {
	return newWindowLimiter(deviceLoginLookupLimit, deviceLoginLookupGlobal)
}

func newMCPCallLimiter() *WindowLimiter {
	return newWindowLimiter(mcpCallLimit, mcpCallGlobal)
}

// newMCPSourceCallLimiter bounds how often one address may reach the MCP
// endpoint, including requests that never get a principal because they carry no
// or an invalid token.
func newMCPSourceCallLimiter() *WindowLimiter {
	return newWindowLimiter(mcpSourceCallLimit, mcpSourceCallGlobal)
}

// newLogoutRequestLimiter bounds anonymous Logout requests, each of which writes a
// permanent ledger row for a revocation that is a no-op after the first call.
func newLogoutRequestLimiter() *WindowLimiter {
	return newWindowLimiter(logoutRequestSourceLimit, logoutRequestGlobalLimit)
}

// newPrincipalRequestLimiter bounds the ledger-writing calls of a signed-in
// caller. Its key carries both the principal and the procedure, and it is one
// limiter rather than one per method, so the single global counter spans every
// covered method: the two dimensions are "this principal on this method" and
// "this deployment on all of them".
func newPrincipalRequestLimiter() *WindowLimiter {
	limiter := newWindowLimiter(principalMethodLimit, principalGlobalLimit)
	limiter.capacity = principalLimiterCapacity
	return limiter
}

// newLoginRequestLimiter bounds anonymous Login requests, each of which spends a
// bcrypt comparison. It runs before the per-account limiter, so an unknown email
// still costs a budget.
func newLoginRequestLimiter() *WindowLimiter {
	return newWindowLimiter(loginRequestSourceLimit, loginRequestGlobalLimit)
}

// newCreateUserRequestLimiter bounds anonymous CreateUser requests, each of which
// spends a bcrypt hash and (before it) an email-existence lookup.
func newCreateUserRequestLimiter() *WindowLimiter {
	return newWindowLimiter(createUserRequestSourceLimit, createUserRequestGlobalLimit)
}

// newSSOStateRequestLimiter bounds anonymous CreateSSOState requests, each of
// which inserts one nonce into the bounded state cache. Sizing that cache above
// what this budget can admit while a nonce is still usable (see ssoStateCapacity)
// is what keeps a caller that only mints nonces from evicting the state of someone
// who is halfway through a login.
func newSSOStateRequestLimiter() *WindowLimiter {
	return newWindowLimiter(ssoStateSourceLimit, ssoStateGlobalLimit)
}

// Allow counts one request from source and reports whether it may proceed.
func (l *WindowLimiter) Allow(source string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if windowExceeded(l.global, l.globalLimit, l.window, now) {
		return false
	}
	attempt := l.sources[source]
	if windowExceeded(attempt, l.sourceLimit, l.window, now) {
		return false
	}

	l.global = windowBump(l.global, l.window, now)
	l.pruneLocked(now)
	l.sources[source] = windowBump(attempt, l.window, now)
	return true
}

// windowExceeded reports whether a window is still open and already full.
func windowExceeded(attempt loginAttempt, limit int, window time.Duration, now time.Time) bool {
	if now.Sub(attempt.first) >= window {
		return false
	}
	return attempt.count >= limit
}

// windowBump counts one hit, starting a new window when the old one elapsed.
func windowBump(attempt loginAttempt, window time.Duration, now time.Time) loginAttempt {
	if attempt.first.IsZero() || now.Sub(attempt.first) >= window {
		return loginAttempt{count: 1, first: now}
	}
	attempt.count++
	return attempt
}

// pruneLocked drops elapsed windows and, at capacity, the oldest one.
func (l *WindowLimiter) pruneLocked(now time.Time) {
	if len(l.sources) < l.capacity {
		return
	}
	for source, attempt := range l.sources {
		if now.Sub(attempt.first) >= l.window {
			delete(l.sources, source)
		}
	}
	if len(l.sources) < l.capacity {
		return
	}

	var oldestSource string
	var oldest time.Time
	for source, attempt := range l.sources {
		if oldestSource == "" || attempt.first.Before(oldest) {
			oldestSource, oldest = source, attempt.first
		}
	}
	delete(l.sources, oldestSource)
}
