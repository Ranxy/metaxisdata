package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/config"
	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// TestMetricsHandlerServesThePrivateRegistry pins that the metrics listener
// exposes /metrics from the server's registry without touching the default one.
func TestMetricsHandlerServesThePrivateRegistry(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.NewBuildInfoCollector("test-version", "test-commit"))
	server := &Server{metricsRegistry: registry}

	recorder := httptest.NewRecorder()
	server.metricsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "metaxisdata_build_info")
	require.Contains(t, recorder.Body.String(), "test-version")
	require.Equal(t, http.StatusNotFound, doRequestNoBody(server.metricsHandler(), "/healthz"))
}

// TestMetricsListenerRejectsAnUnusableAddress pins that opting into metrics with
// an address this host cannot bind fails the startup instead of being ignored.
func TestMetricsListenerRejectsAnUnusableAddress(t *testing.T) {
	t.Parallel()

	server := &Server{
		profile:         &config.Profile{MetricsAddr: "127.0.0.1:99999"},
		metricsRegistry: prometheus.NewRegistry(),
	}
	require.Error(t, server.startMetricsServer())
}

// doRequestNoBody performs a request against a plain handler and returns its
// status, for handlers that are not Echo engines.
func doRequestNoBody(handler http.Handler, target string) int {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder.Code
}

// TestMetricsHandlerOnlyServesGet pins that the metrics port is a read endpoint,
// not a second API.
func TestMetricsHandlerOnlyServesGet(t *testing.T) {
	t.Parallel()

	server := &Server{metricsRegistry: prometheus.NewRegistry()}
	recorder := httptest.NewRecorder()
	server.metricsHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	require.Equal(t, "GET, HEAD", recorder.Header().Get("Allow"))
}

// TestMetricsListenerServeAndShutdown pins the opt-in listener lifecycle: it
// serves only while the server runs, and Shutdown closes it.
func TestMetricsListenerServeAndShutdown(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.NewBuildInfoCollector("test-version", "test-commit"))
	server := &Server{
		profile:         &config.Profile{MetricsAddr: "127.0.0.1:0"},
		metricsRegistry: registry,
	}
	require.NoError(t, server.startMetricsServer())
	address := server.metricsListener.Addr().String()

	response, err := http.Get("http://" + address + "/metrics")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())

	require.NoError(t, server.Shutdown(context.Background()))

	response, err = http.Get("http://" + address + "/metrics")
	require.Error(t, err, "the metrics listener must be closed by Shutdown")
	if response != nil {
		require.NoError(t, response.Body.Close())
	}
}

// metricExposition renders a registry as Prometheus text, for tests that assert
// recorded series rather than instrument internals.
func metricExposition(t *testing.T, registry *prometheus.Registry) string {
	t.Helper()
	recorder := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	return recorder.Body.String()
}

// metricLine returns the text line exposing the named metric family, so a test
// can assert its label values without depending on label ordering.
func metricLine(t *testing.T, exposition, name string) string {
	t.Helper()
	for _, line := range strings.Split(exposition, "\n") {
		if strings.HasPrefix(line, name+"{") || strings.HasPrefix(line, name+" ") {
			return line
		}
	}
	t.Fatalf("metric %q not found in exposition:\n%s", name, exposition)
	return ""
}
