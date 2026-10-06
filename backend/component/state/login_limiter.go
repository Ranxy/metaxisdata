package state

import (
	"strings"
	"sync"
	"time"
)

// Login throttling bounds online password guessing with two independent
// counters: one per account and one per source. Keeping them apart is what makes
// each dimension meaningful. A single (email, source) key collapses to the proxy
// address on a deployment behind a reverse proxy, so every client shares one
// bucket and a stranger can lock a victim's account by failing ten times; it also
// lets an attacker start a fresh window per email, so password spraying and the
// bcrypt work behind it are unbounded.
//
// The two counters bound different attacks: the account counter stops guessing
// one password from many sources, and the source counter stops one source from
// trying many accounts. Together they also bound the lockout a single source can
// cause — it can spend at most loginSourceAttemptLimit failures per window, so it
// can lock at most that many accounts divided by the account limit.
//
// The source is the resolved client address (audit.ClientAddress), never the bare
// TCP peer, and an anonymous request spends a source/global budget at the Connect
// entry before it reaches this limiter (see apiv1.ThrottleInterceptor).
const (
	// loginAccountAttemptLimit is how many failures one account tolerates,
	// regardless of where they come from.
	loginAccountAttemptLimit = 10
	// loginSourceAttemptLimit is how many failures one source tolerates across
	// every account it tries.
	loginSourceAttemptLimit = 20
	loginAttemptWindow      = 5 * time.Minute
	// loginAttemptCapacity bounds each map; expired windows are dropped first.
	loginAttemptCapacity = 4096
)

// LoginLimiter is an in-memory sliding-window counter for failed logins. It is
// deliberately process-local: a deployment with several replicas should put a
// shared limiter in front of the API.
type LoginLimiter struct {
	mu       sync.Mutex
	accounts map[string]loginAttempt
	sources  map[string]loginAttempt
}

type loginAttempt struct {
	count int
	first time.Time
}

func newLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		accounts: map[string]loginAttempt{},
		sources:  map[string]loginAttempt{},
	}
}

// Blocked reports whether the account or the source has exhausted its window and
// the request must be refused before the password is even checked.
func (l *LoginLimiter) Blocked(email, source string, now time.Time) bool {
	account := normalizeAccount(email)
	l.mu.Lock()
	defer l.mu.Unlock()
	return attemptBlocked(l.accounts, account, loginAccountAttemptLimit, now) ||
		attemptBlocked(l.sources, source, loginSourceAttemptLimit, now)
}

// RecordFailure counts one failed attempt against both the account and the
// source, starting a new window when the old one has expired.
func (l *LoginLimiter) RecordFailure(email, source string, now time.Time) {
	account := normalizeAccount(email)
	l.mu.Lock()
	defer l.mu.Unlock()
	recordAttemptLocked(l.accounts, account, now, loginAttemptCapacity)
	recordAttemptLocked(l.sources, source, now, loginAttemptCapacity)
}

// ResetAccount clears the account's failures after a successful login. The
// source counter is deliberately not cleared: a caller who knows one password
// must not be able to buy a fresh spraying budget from a shared address.
func (l *LoginLimiter) ResetAccount(email string) {
	account := normalizeAccount(email)
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.accounts, account)
}

// normalizeAccount lower-cases and trims the email so "Alice@x" and "alice@x"
// share one window.
func normalizeAccount(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// attemptBlocked reports whether key's window is still open and already full.
func attemptBlocked(attempts map[string]loginAttempt, key string, limit int, now time.Time) bool {
	attempt, ok := attempts[key]
	if !ok {
		return false
	}
	if now.Sub(attempt.first) >= loginAttemptWindow {
		delete(attempts, key)
		return false
	}
	return attempt.count >= limit
}

// recordAttemptLocked counts one failure, pruning expired entries before adding a
// new key so the map cannot grow without bound.
func recordAttemptLocked(attempts map[string]loginAttempt, key string, now time.Time, capacity int) {
	attempt, ok := attempts[key]
	if !ok || now.Sub(attempt.first) >= loginAttemptWindow {
		pruneAttemptsLocked(attempts, now, capacity)
		attempts[key] = loginAttempt{count: 1, first: now}
		return
	}
	attempt.count++
	attempts[key] = attempt
}

// pruneAttemptsLocked drops expired entries, and the oldest one if the map still
// exceeds its capacity.
func pruneAttemptsLocked(attempts map[string]loginAttempt, now time.Time, capacity int) {
	for key, attempt := range attempts {
		if now.Sub(attempt.first) >= loginAttemptWindow {
			delete(attempts, key)
		}
	}
	if len(attempts) < capacity {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, attempt := range attempts {
		if oldestKey == "" || attempt.first.Before(oldest) {
			oldestKey, oldest = key, attempt.first
		}
	}
	delete(attempts, oldestKey)
}
