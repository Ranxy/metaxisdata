package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

const oauthTestRoute = "/oauth/register"

func oauthTestHandler(c echo.Context) error { return c.NoContent(http.StatusOK) }

func getOAuthTestRoute(e *echo.Echo, route, forwardedFor string) int {
	req := httptest.NewRequest(http.MethodGet, route, nil)
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code
}

// The OAuth routes use the same key resolution and the same bounded store as
// ingestion: X-Forwarded-For only counts when the peer is a listed proxy, so
// rotating it cannot open a new budget. This exercises the production
// constructor; 10 rps would need minutes to absorb the spare requests below.
func TestOAuthEndpointMiddlewareRateLimits(t *testing.T) {
	t.Parallel()

	e := echo.New()
	e.GET(oauthTestRoute, oauthTestHandler, oauthEndpointMiddleware(nil))

	limited := false
	for i := 0; i < oauthEndpointBurst+2000; i++ {
		if getOAuthTestRoute(e, oauthTestRoute, "") == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	require.True(t, limited, "the burst budget must be enforced")
}

// One address, one bucket, whatever the caller writes in X-Forwarded-For.
func TestOAuthEndpointMiddlewareIgnoresAnUntrustedForwardedHeader(t *testing.T) {
	t.Parallel()

	store := newFrozenRateLimiterStore(oauthEndpointRate, oauthEndpointBurst)
	e := echo.New()
	e.GET(oauthTestRoute, oauthTestHandler, oauthEndpointMiddlewareWithStore(store, nil))

	for i := 0; i < oauthEndpointBurst; i++ {
		require.Equal(t, http.StatusOK, getOAuthTestRoute(e, oauthTestRoute, rotatedForwardedFor(i)))
	}
	require.Equal(t, http.StatusTooManyRequests, getOAuthTestRoute(e, oauthTestRoute, rotatedForwardedFor(oauthEndpointBurst)))
	require.Len(t, store.visitors, 1, "every request lands in the resolved address's bucket")
}

// Each anonymous OAuth route carries its own per-address budget: sharing one
// across the four would tighten the surface four-fold, which is the open I2
// question in .agents/docs/security-open-items.md. One route's traffic therefore
// does not spend another's budget.
func TestOAuthEndpointsCarrySeparateBudgets(t *testing.T) {
	t.Parallel()

	e := echo.New()
	e.GET(oauthTestRoute, oauthTestHandler, oauthEndpointMiddleware(nil))
	e.GET("/oauth/token", oauthTestHandler, oauthEndpointMiddleware(nil))

	limited := false
	for i := 0; i < oauthEndpointBurst+2000; i++ {
		if getOAuthTestRoute(e, oauthTestRoute, "") == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	require.True(t, limited, "the exhausted route must be limited")
	require.Equal(t, http.StatusOK, getOAuthTestRoute(e, "/oauth/token", ""), "another route keeps its own budget")
}
