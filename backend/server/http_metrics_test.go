package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/audit"
	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// TestHTTPMetricsSkipsNonAPITraffic pins the routes excluded from request
// metrics: health checks, the Prometheus endpoint (which lives on its own
// listener) and pprof sessions would otherwise dominate the series.
func TestHTTPMetricsSkipsNonAPITraffic(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"/healthz", "/metrics", "/debug/pprof/", "/debug/pprof/goroutine"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		require.True(t, skipHTTPMetrics(request), "%s should be skipped", target)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	require.False(t, skipHTTPMetrics(request))
}

// TestHTTPMetricsSkipsGatewayLoopback pins the double-count fix: the REST
// gateway answers /v1/* by dialing this process's own port, and the inner hop
// carries the stamped peer proof. It is not a second client request.
func TestHTTPMetricsSkipsGatewayLoopback(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/metaxisdata.v1.InstanceService/ListInstances", nil)
	request.RemoteAddr = "127.0.0.1:4040"
	require.False(t, skipHTTPMetrics(request))

	// The gateway's /v1 middleware stamps Grpc-Metadata-* headers; the gateway
	// strips that prefix before the Connect handler sees them, which is what the
	// metrics middleware inspects.
	stamped := http.Header{}
	audit.StampGatewayPeer(stamped, "203.0.113.7:51000")
	request.Header.Set(audit.GatewayPeerHeader, stamped.Get("Grpc-Metadata-"+audit.GatewayPeerHeader))
	request.Header.Set(audit.GatewayProofHeader, stamped.Get("Grpc-Metadata-"+audit.GatewayProofHeader))
	require.True(t, skipHTTPMetrics(request))

	// A caller that reaches the Connect route directly cannot forge the proof.
	forged := httptest.NewRequest(http.MethodGet, "/metaxisdata.v1.InstanceService/ListInstances", nil)
	forged.RemoteAddr = "127.0.0.1:4040"
	forged.Header.Set(audit.GatewayPeerHeader, "203.0.113.7")
	forged.Header.Set(audit.GatewayProofHeader, "not-a-proof")
	require.False(t, skipHTTPMetrics(forged))
}

// TestHTTPMetricsRouteLabel pins the bounded route label: the template is used,
// unmatched requests collapse to one value and the SPA catch-all to another.
func TestHTTPMetricsRouteLabel(t *testing.T) {
	t.Parallel()

	template := echo.New()
	template.GET("/api/version", func(c echo.Context) error {
		require.Equal(t, "/api/version", routeLabel(c))
		return c.NoContent(http.StatusOK)
	})
	recorder := httptest.NewRecorder()
	template.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	// Echo leaves Path empty when no route matches; the label collapses it so an
	// arbitrary 404 probe cannot mint a series.
	unmatched := echo.New()
	unmatched.GET("/api/version", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	unmatched.HTTPErrorHandler = func(_ error, c echo.Context) {
		require.Equal(t, "unmatched", routeLabel(c))
		_ = c.NoContent(http.StatusNotFound)
	}
	recorder = httptest.NewRecorder()
	unmatched.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/no-such-route", nil))
	require.Equal(t, http.StatusNotFound, recorder.Code)

	spa := echo.New()
	spa.GET("/*", func(c echo.Context) error {
		require.Equal(t, "spa", routeLabel(c))
		return c.NoContent(http.StatusOK)
	})
	recorder = httptest.NewRecorder()
	spa.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/anything", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
}

// TestHTTPMetricsCountsRecoveredPanics pins that a panicking handler's 500 is
// counted: the recover middleware is installed outside the metrics middleware,
// so the metric must be recorded before the panic unwinds to it.
func TestHTTPMetricsCountsRecoveredPanics(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))

	e := echo.New()
	e.Use(recoverMiddleware)
	e.Use(httpMetricsMiddleware(m))
	e.GET("/panic", func(echo.Context) error { panic("boom") })

	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/panic", nil))
	require.Equal(t, http.StatusInternalServerError, recorder.Code)

	line := metricLine(t, metricExposition(t, registry), "metaxisdata_http_requests_total")
	require.Contains(t, line, `route="/panic"`)
	require.Contains(t, line, `status="500"`)
	require.Contains(t, line, `method="get"`)
}
