package server

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

const (
	// oauthEndpointRate is the sustained requests-per-second budget of one client
	// address on the anonymous OAuth endpoints.
	oauthEndpointRate = 10
	// oauthEndpointBurst allows short bursts above the sustained rate.
	oauthEndpointBurst = 20
	// oauthEndpointTimeout bounds one OAuth protocol request.
	oauthEndpointTimeout = 30 * time.Second
)

// oauthEndpointMiddleware rate-limits and time-bounds the anonymous OAuth
// endpoints (registration and the token exchange). They are plain Echo routes
// and therefore skip the Connect interceptors, which is where the rest of the
// API gets its protections: registration is anonymous by design, and the token
// endpoint is a credential exchange, so both need a ceiling.
func oauthEndpointMiddleware(trustedProxies []string) echo.MiddlewareFunc {
	store := newBoundedRateLimiterStore(oauthEndpointRate, oauthEndpointBurst)
	return oauthEndpointMiddlewareWithStore(store, trustedProxies)
}

// oauthEndpointMiddlewareWithStore is the middleware itself; the store is a
// parameter so a test can freeze its clock.
func oauthEndpointMiddlewareWithStore(store *boundedRateLimiterStore, trustedProxies []string) echo.MiddlewareFunc {
	limiter := middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: store,
		// The trusted-proxy rules decide whether a forwarded address may be
		// believed. Echo's RealIP trusts the header unconditionally, which would
		// let a caller lift its own limit by spoofing it.
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return rateLimitSourceKey(c, trustedProxies), nil
		},
		DenyHandler: func(c echo.Context, _ string, _ error) error {
			return c.JSON(http.StatusTooManyRequests, map[string]string{
				"error":             "temporarily_unavailable",
				"error_description": "too many requests",
			})
		},
	})
	timeout := middleware.TimeoutWithConfig(middleware.TimeoutConfig{
		Timeout: oauthEndpointTimeout,
	})
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return limiter(timeout(next))
	}
}
