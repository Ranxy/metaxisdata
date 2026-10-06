package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// openLineageIngestionTestServer mounts the middleware being tested with the
// given trusted proxies. httptest requests carry 192.0.2.1:1234 as their peer
// address, so a listed proxy in these tests is "192.0.2.1".
func openLineageIngestionTestServer(trustedProxies []string) *echo.Echo {
	e := echo.New()
	g := e.Group("/api/v1/lineage", openLineageIngestionMiddleware(trustedProxies))
	g.POST("", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	return e
}

func ingestOpenLineageEvent(e *echo.Echo, key, forwardedFor string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/lineage", nil)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code
}

// Ingestion skips the Connect interceptor chain, so this middleware is the only
// thing bounding its request rate.
func TestOpenLineageIngestionMiddlewareRateLimits(t *testing.T) {
	t.Parallel()

	e := openLineageIngestionTestServer(nil)

	limited := false
	for i := 0; i < openLineageIngestionBurst+5; i++ {
		if ingestOpenLineageEvent(e, "key-a", "") == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	require.True(t, limited, "the burst budget must be enforced")

	// The limiter is keyed by the ingestion key, so another producer keeps its
	// own budget.
	require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "key-b", ""))
}

// A key-less request is budgeted by the address the trusted-proxy rules resolve,
// never by X-Forwarded-For itself: Echo's RealIP believes that header from
// anyone, so a caller rotating it would open a new bucket per request and never
// reach a ceiling (M14).
func TestOpenLineageIngestionMiddlewareIgnoresAnUntrustedForwardedHeader(t *testing.T) {
	t.Parallel()

	e := openLineageIngestionTestServer(nil)

	limited := false
	for i := 0; i < openLineageIngestionBurst+5; i++ {
		if ingestOpenLineageEvent(e, "", fmt.Sprintf("203.0.113.%d", i%256)) == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	require.True(t, limited, "rotating X-Forwarded-For must not open a new budget")
}

// A listed proxy's forwarding chain is believed, exactly as the audit record and
// the other anonymous routes read it: one address per producer, and the bucket
// of one producer is not the bucket of another.
func TestOpenLineageIngestionMiddlewareBelievesAListedProxy(t *testing.T) {
	t.Parallel()

	e := openLineageIngestionTestServer([]string{"192.0.2.1"})

	for i := 0; i < openLineageIngestionBurst; i++ {
		require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "", "203.0.113.1"))
	}
	require.Equal(t, http.StatusTooManyRequests, ingestOpenLineageEvent(e, "", "203.0.113.1"))
	require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "", "203.0.113.2"))
}
