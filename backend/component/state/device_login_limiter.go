package state

import (
	"sync"
	"time"
)

// Device login throttling. Two endpoints need different budgets:
//
//   - creating a request is anonymous, allocates memory and drives a human
//     approval, so it is counted per source address with a loose global backstop
//     for the case where every caller shares one address (a reverse proxy that
//     is not listed as trusted);
//   - reading or deciding a request needs a signed-in caller, and is counted per
//     caller so holding an account does not buy unlimited guesses at someone
//     else's user code.
const (
	deviceLoginSourceLimit     = 10
	deviceLoginSourceWindow    = time.Minute
	deviceLoginGlobalLimit     = 100
	deviceLoginGlobalWindow    = time.Minute
	deviceLoginLimiterCapacity = 4096

	deviceLoginLookupLimit    = 60
	deviceLoginLookupWindow   = time.Minute
	deviceLoginLookupGlobal   = 600
	deviceLoginLookupCapacity = 4096
)

// DeviceLoginLimiter is an in-memory sliding-window counter. A request is
// counted only when it is allowed, so hammering a refused key cannot keep
// pushing its window forward.
type DeviceLoginLimiter struct {
	mu      sync.Mutex
	sources map[string]loginAttempt
	global  loginAttempt

	sourceLimit  int
	globalLimit  int
	sourceWindow time.Duration
	globalWindow time.Duration
	capacity     int
}

func newDeviceLoginLimiter(sourceLimit, globalLimit int, sourceWindow, globalWindow time.Duration, capacity int) *DeviceLoginLimiter {
	return &DeviceLoginLimiter{
		sources:      map[string]loginAttempt{},
		sourceLimit:  sourceLimit,
		globalLimit:  globalLimit,
		sourceWindow: sourceWindow,
		globalWindow: globalWindow,
		capacity:     capacity,
	}
}

func newDeviceLoginCreateLimiter() *DeviceLoginLimiter {
	return newDeviceLoginLimiter(deviceLoginSourceLimit, deviceLoginGlobalLimit, deviceLoginSourceWindow, deviceLoginGlobalWindow, deviceLoginLimiterCapacity)
}

func newDeviceLoginLookupLimiter() *DeviceLoginLimiter {
	return newDeviceLoginLimiter(deviceLoginLookupLimit, deviceLoginLookupGlobal, deviceLoginLookupWindow, deviceLoginLookupWindow, deviceLoginLookupCapacity)
}

// Allow counts one request from source and reports whether it may proceed.
func (l *DeviceLoginLimiter) Allow(source string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if windowExceeded(l.global, l.globalLimit, l.globalWindow, now) {
		return false
	}
	attempt := l.sources[source]
	if windowExceeded(attempt, l.sourceLimit, l.sourceWindow, now) {
		return false
	}

	l.global = windowBump(l.global, l.globalWindow, now)
	l.pruneLocked(now)
	l.sources[source] = windowBump(attempt, l.sourceWindow, now)
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
	if len(l.sources) < l.capacity {
		return
	}
	for source, attempt := range l.sources {
		if now.Sub(attempt.first) >= l.sourceWindow {
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
