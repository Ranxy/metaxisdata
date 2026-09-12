package state

import (
	"sync"
	"time"
)

// Login throttling bounds online password guessing and applies to the pair
// (email, peer address) so neither one account nor one source can be hammered.
const (
	loginAttemptLimit  = 10
	loginAttemptWindow = 5 * time.Minute
	// loginAttemptCapacity bounds the tracked keys; the oldest window entries
	// are pruned first once it is exceeded.
	loginAttemptCapacity = 4096
)

// LoginLimiter is an in-memory sliding-window counter for failed logins. It is
// deliberately process-local: a deployment with several replicas should put a
// shared limiter in front of the API.
type LoginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

type loginAttempt struct {
	count int
	first time.Time
}

func newLoginLimiter() *LoginLimiter {
	return &LoginLimiter{attempts: map[string]loginAttempt{}}
}

// Blocked reports whether the key has exhausted its window and must be refused
// before the password is even checked.
func (l *LoginLimiter) Blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt, ok := l.attempts[key]
	if !ok {
		return false
	}
	if now.Sub(attempt.first) >= loginAttemptWindow {
		delete(l.attempts, key)
		return false
	}
	return attempt.count >= loginAttemptLimit
}

// RecordFailure counts one failed attempt, starting a new window when the old
// one has expired.
func (l *LoginLimiter) RecordFailure(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	attempt, ok := l.attempts[key]
	if !ok || now.Sub(attempt.first) >= loginAttemptWindow {
		l.pruneLocked(now)
		l.attempts[key] = loginAttempt{count: 1, first: now}
		return
	}
	attempt.count++
	l.attempts[key] = attempt
}

// Reset clears the key after a successful login.
func (l *LoginLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// pruneLocked drops expired entries, and the oldest ones if the map still
// exceeds its capacity.
func (l *LoginLimiter) pruneLocked(now time.Time) {
	for key, attempt := range l.attempts {
		if now.Sub(attempt.first) >= loginAttemptWindow {
			delete(l.attempts, key)
		}
	}
	if len(l.attempts) < loginAttemptCapacity {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, attempt := range l.attempts {
		if oldestKey == "" || attempt.first.Before(oldest) {
			oldestKey, oldest = key, attempt.first
		}
	}
	delete(l.attempts, oldestKey)
}
