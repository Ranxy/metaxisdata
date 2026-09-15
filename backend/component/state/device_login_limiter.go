package state

import (
	"sync"
	"time"
)

// Device login throttling. CreateDeviceLogin is anonymous, allocates memory and
// drives a human approval, so calls are counted per source address and, as a
// backstop, in total.
const (
	deviceLoginSourceLimit     = 10
	deviceLoginSourceWindow    = time.Minute
	deviceLoginGlobalLimit     = 100
	deviceLoginGlobalWindow    = time.Minute
	deviceLoginLimiterCapacity = 4096
)

// DeviceLoginLimiter is an in-memory sliding-window counter for device login
// creation. The per-source bucket keeps one caller from driving the approval
// flow; the global bucket caps the endpoint even when the source address is
// shared, which is what happens behind a reverse proxy that is not listed as
// trusted (and would otherwise starve every user at once).
type DeviceLoginLimiter struct {
	mu      sync.Mutex
	sources map[string]loginAttempt
	global  loginAttempt
}

func newDeviceLoginLimiter() *DeviceLoginLimiter {
	return &DeviceLoginLimiter{sources: map[string]loginAttempt{}}
}

// Allow counts one request from source and reports whether it may proceed. A
// refused request is not counted, so the window cannot be extended by hammering
// it.
func (l *DeviceLoginLimiter) Allow(source string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if windowExceeded(l.global, deviceLoginGlobalLimit, deviceLoginGlobalWindow, now) {
		return false
	}
	attempt := l.sources[source]
	if windowExceeded(attempt, deviceLoginSourceLimit, deviceLoginSourceWindow, now) {
		return false
	}

	l.global = windowBump(l.global, deviceLoginGlobalWindow, now)
	l.pruneLocked(now)
	l.sources[source] = windowBump(attempt, deviceLoginSourceWindow, now)
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
func (l *DeviceLoginLimiter) pruneLocked(now time.Time) {
	if len(l.sources) < deviceLoginLimiterCapacity {
		return
	}
	for source, attempt := range l.sources {
		if now.Sub(attempt.first) >= deviceLoginSourceWindow {
			delete(l.sources, source)
		}
	}
	if len(l.sources) < deviceLoginLimiterCapacity {
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
