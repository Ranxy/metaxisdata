package server

import (
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"

	"github.com/Ranxy/metaxisdata/backend/component/audit"
)

const (
	// rateLimiterCapacity bounds how many identifiers one rate limiter tracks.
	// Echo's RateLimiterMemoryStore has no ceiling, and the identifier of an
	// anonymous route is partly caller-chosen, so without this the map grows for
	// as long as a caller keeps presenting new ones (M14).
	rateLimiterCapacity = 4096
	// rateLimiterExpiresIn is how long an idle bucket is kept before a sweep
	// drops it, matching echo's RateLimiterMemoryStore default.
	rateLimiterExpiresIn = 3 * time.Minute
)

// rateLimitSourceKey is the budget key of a request from an unauthenticated
// caller: the address the trusted-proxy rules resolve it to. Echo's RealIP
// trusts X-Forwarded-For unconditionally, so keying on that would let one caller
// open a new bucket per request by rewriting the header (M14).
func rateLimitSourceKey(c echo.Context, trustedProxies []string) string {
	if ip := audit.ClientAddress(c.Request().Header, c.Request().RemoteAddr, trustedProxies); ip != "" {
		return ip
	}
	// net/http always fills RemoteAddr, so this is unreachable in practice; one
	// shared bucket is the safe answer if it is ever reached.
	return "unknown"
}

// boundedRateLimiterStore is one token bucket per identifier, with a hard
// ceiling on the number of tracked identifiers. Every anonymous route keyed by
// an ingestion key lets the caller choose that identifier, so the memory a
// limiter may take must come from the server, not from the caller.
//
// Reaching the ceiling drops one bucket to make room. That can only hand its
// owner a fresh budget, never deny it, so the ceiling bounds the memory a
// caller's identifiers may cost and cannot become a denial of service of its
// own — as long as making room stays constant-time, which is why the victim is
// arbitrary rather than the oldest. The identifier ceiling does not bound the
// rate of a caller that rotates identifiers freely; where that matters the route
// adds a second dimension keyed by the resolved client address, which the caller
// cannot choose (see openLineageIngestionMiddleware). A deployment-wide bucket
// shared by every identifier was tried and removed: a caller rotating keys
// exhausted it and denied every other producer on the route, which is a worse
// failure than the rate it was meant to bound.
type boundedRateLimiterStore struct {
	mu       sync.Mutex
	visitors map[string]*boundedVisitor

	rate      rate.Limit
	burst     int
	capacity  int
	expiresIn time.Duration

	lastCleanup time.Time
	// timeNow is a test seam; production keeps time.Now.
	timeNow func() time.Time
}

type boundedVisitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// The middleware takes a RateLimiterStore; this is where a signature drift would
// surface.
var _ middleware.RateLimiterStore = (*boundedRateLimiterStore)(nil)

func newBoundedRateLimiterStore(limit rate.Limit, burst int) *boundedRateLimiterStore {
	store := &boundedRateLimiterStore{
		visitors:  make(map[string]*boundedVisitor),
		rate:      limit,
		burst:     burst,
		capacity:  rateLimiterCapacity,
		expiresIn: rateLimiterExpiresIn,
		timeNow:   time.Now,
	}
	store.lastCleanup = store.timeNow()
	return store
}

// Allow implements middleware.RateLimiterStore.
func (s *boundedRateLimiterStore) Allow(identifier string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.timeNow()
	if now.Sub(s.lastCleanup) >= s.expiresIn {
		s.pruneLocked(now)
		s.lastCleanup = now
	}

	visitor, ok := s.visitors[identifier]
	if !ok {
		if len(s.visitors) >= s.capacity {
			s.evictLocked()
		}
		visitor = &boundedVisitor{limiter: rate.NewLimiter(s.rate, s.burst)}
		s.visitors[identifier] = visitor
	}
	visitor.lastSeen = now
	return visitor.limiter.AllowN(now, 1), nil
}

// pruneLocked drops every bucket that has been idle for at least expiresIn.
func (s *boundedRateLimiterStore) pruneLocked(now time.Time) {
	for identifier, visitor := range s.visitors {
		if now.Sub(visitor.lastSeen) >= s.expiresIn {
			delete(s.visitors, identifier)
		}
	}
}

// evictLocked drops one bucket to make room for a new one, in constant time.
//
// Which bucket goes does not matter: eviction only ever recreates a bucket with
// a full burst, so every choice is permissive. It must not be a search for the
// oldest one, though — a caller that invents a new identifier per request would
// then force a walk of the whole map, under the mutex every other request needs,
// on exactly the path this ceiling exists to bound.
func (s *boundedRateLimiterStore) evictLocked() {
	for identifier := range s.visitors {
		delete(s.visitors, identifier)
		return
	}
}
