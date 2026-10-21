package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
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
