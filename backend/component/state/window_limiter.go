package state

import (
	"sync"
	"time"
)

// Throttling budgets, all counted over a sliding window. The device login
// endpoints need two different ones:
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
// The MCP budget is per principal rather than per address: agents behind one
// address share it, and every call reads the registry and writes a ledger row.
// It bounds abuse rather than normal use — a client would have to sustain ten
// calls a second to reach it.
const (
	// throttleWindow is the window every budget counts over.
	throttleWindow = time.Minute

	// limiterCapacity bounds the tracked sources in each limiter.
	limiterCapacity = 4096

	deviceLoginSourceLimit  = 10
	deviceLoginGlobalLimit  = 100
	deviceLoginLookupLimit  = 60
	deviceLoginLookupGlobal = 600

	loginRequestSourceLimit = 30
	loginRequestGlobalLimit = 300

	createUserRequestSourceLimit = 10
	createUserRequestGlobalLimit = 100

	mcpCallLimit  = 600
	mcpCallGlobal = 6000
)

// WindowLimiter is an in-memory sliding-window counter. A request is
// counted only when it is allowed, so hammering a refused key cannot keep
// pushing its window forward.
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
