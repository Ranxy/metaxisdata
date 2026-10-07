package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	apiv1 "github.com/Ranxy/metaxisdata/backend/api/v1"
)

const (
	// openLineageIngestionRate is the sustained requests-per-second budget of one
	// ingestion key (or of the resolved client address for key-less requests).
	openLineageIngestionRate = 50
	// openLineageIngestionBurst allows short bursts above the sustained rate.
	openLineageIngestionBurst = 100
	// openLineageIngestionSourceRate is the budget of one client address,
	// whatever credentials it presents. The key dimension cannot bound a caller
	// that invents a key per request — an unknown key is rejected without a
	// password check, so forged keys are cheap and each one opens a fresh
	// bucket — and the address is the one thing it cannot choose. It is set well
	// above one producer's budget so a real fan-in behind one address (a NAT, a
	// shared Spark gateway) is not paced by it.
	openLineageIngestionSourceRate = 500
	// openLineageIngestionSourceBurst allows short bursts above that rate.
	openLineageIngestionSourceBurst = 1000
	// openLineageIngestionTimeout bounds one ingestion request.
	openLineageIngestionTimeout = 60 * time.Second
)

// openLineageIngestionMiddleware rate-limits and time-bounds the OpenLineage
// ingestion routes. They are plain Echo routes and therefore skip the Connect
// interceptors, which is where the rest of the API gets its protections.
func openLineageIngestionMiddleware(trustedProxies []string) echo.MiddlewareFunc {
	keyStore := newBoundedRateLimiterStore(openLineageIngestionRate, openLineageIngestionBurst)
	sourceStore := newBoundedRateLimiterStore(openLineageIngestionSourceRate, openLineageIngestionSourceBurst)
	return openLineageIngestionMiddlewareWithStore(keyStore, sourceStore, trustedProxies)
}

// openLineageIngestionMiddlewareWithStore is the middleware itself. The stores
// are parameters so a test can drive them with a frozen clock and assert exact
// budget boundaries instead of racing the token-bucket refill.
func openLineageIngestionMiddlewareWithStore(keyStore, sourceStore *boundedRateLimiterStore, trustedProxies []string) echo.MiddlewareFunc {
	deny := func(c echo.Context, _ string, _ error) error {
		return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
	}
	// Key by the ingestion key so one noisy producer cannot exhaust another's
	// budget; the raw key is never stored, only its digest. A request without a
	// key falls back to the resolved client address, not to Echo's RealIP,
	// which believes X-Forwarded-For from anyone.
	keyLimiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: keyStore,
		IdentifierExtractor: func(c echo.Context) (string, error) {
			if key := apiv1.ExtractIngestionKey(c.Request()); key != "" {
				sum := sha256.Sum256([]byte(key))
				return hex.EncodeToString(sum[:]), nil
			}
			return rateLimitSourceKey(c, trustedProxies), nil
		},
		DenyHandler: deny,
	})
	// The address dimension is also counted for a request that presents a key:
	// it is the only identifier a caller cannot invent, so it is what bounds a
	// caller rotating forged keys. A shared deployment-wide bucket was tried
	// first and removed — a rotating caller exhausted it and denied every other
	// producer, which is worse than the rate it bounded.
	sourceLimiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store:               sourceStore,
		IdentifierExtractor: func(c echo.Context) (string, error) { return rateLimitSourceKey(c, trustedProxies), nil },
		DenyHandler:         deny,
	})
	timeout := middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: openLineageIngestionTimeout,
	})
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return sourceLimiter(keyLimiter(timeout(next)))
	}
}
