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
	// openLineageIngestionTimeout bounds one ingestion request.
	openLineageIngestionTimeout = 60 * time.Second
)

// openLineageIngestionMiddleware rate-limits and time-bounds the OpenLineage
// ingestion routes. They are plain Echo routes and therefore skip the Connect
// interceptors, which is where the rest of the API gets its protections.
func openLineageIngestionMiddleware(trustedProxies []string) echo.MiddlewareFunc {
	store := newBoundedRateLimiterStore(openLineageIngestionRate, openLineageIngestionBurst)
	return openLineageIngestionMiddlewareWithStore(store, trustedProxies)
}

// openLineageIngestionMiddlewareWithStore is the middleware itself. The store is
// a parameter so a test can drive it with a frozen clock and assert exact
// budget boundaries instead of racing the token-bucket refill.
func openLineageIngestionMiddlewareWithStore(store *boundedRateLimiterStore, trustedProxies []string) echo.MiddlewareFunc {
	limiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: store,
		// Key by the ingestion key so one noisy producer cannot exhaust another's
		// budget; the raw key is never stored, only its digest. A request without
		// a key falls back to the resolved client address, not to Echo's RealIP,
		// which believes X-Forwarded-For from anyone.
		IdentifierExtractor: func(c echo.Context) (string, error) {
			if key := apiv1.ExtractIngestionKey(c.Request()); key != "" {
				sum := sha256.Sum256([]byte(key))
				return hex.EncodeToString(sum[:]), nil
			}
			return rateLimitSourceKey(c, trustedProxies), nil
		},
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
		},
	})
	timeout := middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: openLineageIngestionTimeout,
	})
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return limiter(timeout(next))
	}
}
