package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// openLineageIngestionTestServer mounts the middleware built on the two stores
// with the given trusted proxies. httptest requests carry 192.0.2.1:1234 as their
// peer address, so a listed proxy in these tests is "192.0.2.1".
func openLineageIngestionTestServer(keyStore, sourceStore *boundedRateLimiterStore, trustedProxies []string) *echo.Echo {
	e := echo.New()
	g := e.Group("/api/v1/lineage", openLineageIngestionMiddlewareWithStore(keyStore, sourceStore, trustedProxies))
	g.POST("", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	return e
}

// unfrozenRateLimiterStore is a store whose budget no test depends on: the
// dimension under test should be the only one that can refuse.
func unfrozenRateLimiterStore() *boundedRateLimiterStore {
	return newBoundedRateLimiterStore(rate.Limit(1e6), 1e6)
}

func ingestOpenLineageEvent(e *echo.Echo, key, forwardedFor string) int {
	return ingestOpenLineageEventFrom(e, key, "", forwardedFor)
}

// ingestOpenLineageEventFrom sends one request, optionally from a chosen peer
// address, so a test can show which dimension refused it.
func ingestOpenLineageEventFrom(e *echo.Echo, key, remoteAddr, forwardedFor string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/lineage", nil)
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
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
	// own budget. It comes from another address as well: this loop has spent the
	// shared peer's address budget too, and a real producer on its own address is
	// not paced by that — the deployment-wide bucket this used to be was removed
	// precisely because it made every other producer pay for one caller.
	require.Equal(t, http.StatusOK, ingestOpenLineageEventFrom(e, "key-b", "198.51.100.9:5555", ""))
}

// The key dimension is what keeps one producer's flood out of another's budget.
func TestOpenLineageIngestionMiddlewareKeysTheBudgetByIngestionKey(t *testing.T) {
	t.Parallel()

	keyStore := newFrozenRateLimiterStore(openLineageIngestionRate, openLineageIngestionBurst)
	e := openLineageIngestionTestServer(keyStore, unfrozenRateLimiterStore(), nil)

	for i := 0; i < openLineageIngestionBurst; i++ {
		require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "key-a", ""), "request %d is inside the burst", i+1)
	}
	require.Equal(t, http.StatusTooManyRequests, ingestOpenLineageEvent(e, "key-a", ""))
	require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "key-b", ""), "another key has its own budget")
}

// An invented key is rejected without a password check, so a caller can present a
// fresh one per request and buy a fresh burst each time. The key dimension cannot
// bound that; the resolved client address can, and it is the one thing the caller
// cannot choose. The producer on another address must stay unaffected — that is
// what a deployment-wide shared bucket got wrong (it let this caller deny every
// other producer) and why there is no such bucket.
func TestOpenLineageIngestionMiddlewareBoundsOneAddressWhateverKeyItPresents(t *testing.T) {
	t.Parallel()

	sourceStore := newFrozenRateLimiterStore(openLineageIngestionSourceRate, openLineageIngestionSourceBurst)
	e := openLineageIngestionTestServer(unfrozenRateLimiterStore(), sourceStore, nil)

	for i := 0; i < openLineageIngestionSourceBurst; i++ {
		require.Equal(t, http.StatusOK,
			ingestOpenLineageEvent(e, fmt.Sprintf("forged-%d", i), ""),
			"a forged key still gets its own key bucket, so the address budget is what refuses here")
	}
	require.Equal(t, http.StatusTooManyRequests, ingestOpenLineageEvent(e, "forged-final", ""))

	require.Equal(t, http.StatusOK, ingestOpenLineageEventFrom(e, "real-key", "198.51.100.9:5555", ""),
		"one caller spending its address must not refuse every other producer on the route")
}

// The F2 regression: a caller that invents an ingestion key per request must not
// be able to refuse *other* producers. A deployment-wide bucket shared by every
// identifier did exactly that — the rotating caller exhausted it and every
// producer on the route got 429 — so what bounds the caller is its own address,
// and a producer elsewhere is untouched. This runs the production constructor,
// so it measures the shipped budgets.
func TestOpenLineageIngestionMiddlewareOneCallerCannotDenyAnotherProducer(t *testing.T) {
	t.Parallel()

	e := echo.New()
	g := e.Group("/api/v1/lineage", openLineageIngestionMiddleware(nil))
	g.POST("", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	limited := false
	for i := 0; i < openLineageIngestionSourceBurst+2000; i++ {
		if ingestOpenLineageEventFrom(e, fmt.Sprintf("forged-%d", i), "203.0.113.9:5555", "") == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	require.True(t, limited, "one address must be bounded however many keys it invents")

	require.Equal(t, http.StatusOK, ingestOpenLineageEventFrom(e, "real-key", "198.51.100.9:5555", ""),
		"one caller spending its own address budget must not refuse another producer")
}

// The address dimension is the only thing that bounds a caller inventing keys, so
// its key has to be the resolved address and never a header the caller writes.
// Pinned on the source store with a rotating X-Forwarded-For: Echo's RealIP would
// open a fresh bucket per request, which is exactly the M14 hole this dimension
// exists to close, and nothing else in this file would notice the change.
func TestOpenLineageIngestionMiddlewareSourceBudgetIgnoresForwardedFor(t *testing.T) {
	t.Parallel()

	sourceStore := newFrozenRateLimiterStore(openLineageIngestionSourceRate, openLineageIngestionSourceBurst)
	e := openLineageIngestionTestServer(unfrozenRateLimiterStore(), sourceStore, nil)

	for i := 0; i < openLineageIngestionSourceBurst; i++ {
		require.Equal(t, http.StatusOK,
			ingestOpenLineageEvent(e, fmt.Sprintf("forged-%d", i), rotatedForwardedFor(i)),
			"request %d is inside the address burst", i+1)
	}
	require.Equal(t, http.StatusTooManyRequests,
		ingestOpenLineageEvent(e, "forged-final", rotatedForwardedFor(openLineageIngestionSourceBurst)))
	require.Len(t, sourceStore.visitors, 1,
		"every rotated header must land in the resolved address's bucket")
}

// A key-less request is budgeted by the address the trusted-proxy rules resolve,
// never by X-Forwarded-For itself: Echo's RealIP believes that header from
// anyone, so a caller rotating it would open a new bucket per request and never
// reach a ceiling (M14). One bucket for the whole run is the property.
func TestOpenLineageIngestionMiddlewareIgnoresAnUntrustedForwardedHeader(t *testing.T) {
	t.Parallel()

	keyStore := newFrozenRateLimiterStore(openLineageIngestionRate, openLineageIngestionBurst)
	e := openLineageIngestionTestServer(keyStore, unfrozenRateLimiterStore(), nil)

	for i := 0; i < openLineageIngestionBurst; i++ {
		require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "", rotatedForwardedFor(i)))
	}
	require.Equal(t, http.StatusTooManyRequests, ingestOpenLineageEvent(e, "", rotatedForwardedFor(openLineageIngestionBurst)))
	require.Len(t, keyStore.visitors, 1, "every key-less request lands in the resolved address's bucket")
}

// A listed proxy's forwarding chain is believed, exactly as the audit record and
// the other anonymous routes read it: one address per producer, and the bucket
// of one producer is not the bucket of another.
func TestOpenLineageIngestionMiddlewareBelievesAListedProxy(t *testing.T) {
	t.Parallel()

	keyStore := newFrozenRateLimiterStore(openLineageIngestionRate, openLineageIngestionBurst)
	e := openLineageIngestionTestServer(keyStore, unfrozenRateLimiterStore(), []string{"192.0.2.1"})

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
// ranges. Pinned so the disclosure in docs/security-posture.md stays true. Both
// dimensions fall back to the same caller-chosen value, which is why this shows
// up in the key store.
func TestOpenLineageIngestionMiddlewareTrustAllProxiesLetsTheCallerChooseTheBucket(t *testing.T) {
	t.Parallel()

	keyStore := newFrozenRateLimiterStore(openLineageIngestionRate, openLineageIngestionBurst)
	e := openLineageIngestionTestServer(keyStore, unfrozenRateLimiterStore(), []string{"0.0.0.0/0"})

	for i := 0; i < openLineageIngestionBurst+20; i++ {
		require.Equal(t, http.StatusOK, ingestOpenLineageEvent(e, "", rotatedForwardedFor(i)+", 192.0.2.1"))
	}
	require.Greater(t, len(keyStore.visitors), openLineageIngestionBurst, "each leftmost value is its own bucket")
}
