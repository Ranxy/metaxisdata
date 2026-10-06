package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// The anonymous OAuth routes use the same key resolution and the same bounded
// store as ingestion: X-Forwarded-For only counts when the peer is a listed
// proxy, and rotating it cannot open a new budget.
func TestOAuthEndpointMiddlewareIgnoresAnUntrustedForwardedHeader(t *testing.T) {
	t.Parallel()

	e := echo.New()
	e.GET("/oauth/register", func(c echo.Context) error { return c.NoContent(http.StatusOK) }, oauthEndpointMiddleware(nil))

	limited := false
	for i := 0; i < oauthEndpointBurst+5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/oauth/register", nil)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", i%256))
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	require.True(t, limited, "rotating X-Forwarded-For must not open a new budget")
}
