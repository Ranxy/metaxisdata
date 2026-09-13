package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// Ingestion skips the Connect interceptor chain, so this middleware is the only
// thing bounding its request rate.
func TestOpenLineageIngestionMiddlewareRateLimits(t *testing.T) {
	t.Parallel()

	e := echo.New()
	g := e.Group("/api/v1/lineage", openLineageIngestionMiddleware())
	g.POST("", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	do := func(key string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/lineage", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	limited := false
	for i := 0; i < openLineageIngestionBurst+5; i++ {
		if do("key-a") == http.StatusTooManyRequests {
			limited = true
			break
		}
	}
	require.True(t, limited, "the burst budget must be enforced")

	// The limiter is keyed by the ingestion key, so another producer keeps its
	// own budget.
	require.Equal(t, http.StatusOK, do("key-b"))
}
