package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// openLineageIngestionTestServer mounts the middleware built on store with the
// given trusted proxies. httptest requests carry 192.0.2.1:1234 as their peer
// address, so a listed proxy in these tests is "192.0.2.1".
func openLineageIngestionTestServer(store *boundedRateLimiterStore, trustedProxies []string) *echo.Echo {
	e := echo.New()
	g := e.Group("/api/v1/lineage", openLineageIngestionMiddlewareWithStore(store, trustedProxies))
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

// rotatedForwardedFor is a different address per request: what a caller would
// send to escape a bucket keyed on a header it controls.
func rotatedForwardedFor(i int) string {
	return fmt.Sprintf("203.0.113.%d", i%256)
}

// Ingestion skips the Connect interceptor chain, so this middleware is the only
// thing bounding its request rate. This exercises the production constructor;
// the exact boundaries are asserted on a frozen clock below, because the token
// refill would otherwise make a boundary assertion timing-dependent.
func TestOpenLineageIngestionMiddlewareRateLimits(t *testing.T) {
	t.Parallel()

	e := echo.New()
	g := e.Group("/api/v1/lineage", openLineageIngestionMiddleware(nil))
	g.POST("", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	// 50 rps would need tens of seconds to absorb this many spare requests, so a
	// ceiling that never appears means it is not enforced at all.
	limited := false
	for i := 0; i < openLineageIngestionBurst+2000; i++ {
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
// reach a ceiling (M14). One bucket for the whole run is the property.
func TestOpenLineageIngestionMiddlewareIgnoresAnUntrustedForwardedHeader(t *testing.T) {
	t.Parallel()

	store := newFrozenRateLimiterStore(openLineageIngestionRate, openLineageIngestionBurst)
	e := openLineageIngestionTestServer(store, nil)

	for i := 0; i < openLineageIngestionBurst; i++ {
		require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "", rotatedForwardedFor(i)))
	}
	require.Equal(t, http.StatusTooManyRequests, ingestOpenLineageEvent(e, "", rotatedForwardedFor(openLineageIngestionBurst)))
	require.Len(t, store.visitors, 1, "every key-less request lands in the resolved address's bucket")
}

// A listed proxy's forwarding chain is believed, exactly as the audit record and
// the other anonymous routes read it: one address per producer, and the bucket
// of one producer is not the bucket of another.
func TestOpenLineageIngestionMiddlewareBelievesAListedProxy(t *testing.T) {
	t.Parallel()

	store := newFrozenRateLimiterStore(openLineageIngestionRate, openLineageIngestionBurst)
	e := openLineageIngestionTestServer(store, []string{"192.0.2.1"})

	for i := 0; i < openLineageIngestionBurst; i++ {
		require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "", "203.0.113.1"))
	}
	require.Equal(t, http.StatusTooManyRequests, ingestOpenLineageEvent(e, "", "203.0.113.1"))
	require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "", "203.0.113.2"))
}

// Pinned boundary, not a bug: when every hop in the chain is a listed proxy,
// audit.ClientAddress falls back to the leftmost (furthest upstream) entry — the
// one the caller wrote. A trusted-proxy entry that covers the client itself
// ("0.0.0.0/0", or the client's own CIDR) therefore reopens a caller-chosen
// bucket here, exactly as it makes the audit address caller-chosen; the fix's
// guarantee is conditional on the entry list naming proxies rather than client
// ranges. Pinned so the disclosure in docs/security-posture.md stays true.
func TestOpenLineageIngestionMiddlewareTrustAllProxiesLetsTheCallerChooseTheBucket(t *testing.T) {
	t.Parallel()

	store := newFrozenRateLimiterStore(openLineageIngestionRate, openLineageIngestionBurst)
	e := openLineageIngestionTestServer(store, []string{"0.0.0.0/0"})

	for i := 0; i < openLineageIngestionBurst+20; i++ {
		require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "", rotatedForwardedFor(i)+", 192.0.2.1"))
	}
	require.Greater(t, len(store.visitors), openLineageIngestionBurst, "each leftmost value is its own bucket")
}
