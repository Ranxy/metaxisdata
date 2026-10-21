package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/version"
	"github.com/Ranxy/metaxisdata/backend/config"
)

// testServers builds the dev and prod echo servers once per test binary.
// configureEchoRouters no longer touches process-wide state: every metric is an
// instrument on the server's private registry, and a nil *metrics.Metrics simply
// disables recording.
var (
	testServersOnce sync.Once
	devServer       *echo.Echo
	prodServer      *echo.Echo
)

func configureEchoRoutersForTest(e *echo.Echo, profile *config.Profile) {
	configureEchoRouters(e, profile, nil)
}

func testServers() {
	testServersOnce.Do(func() {
		devServer = echo.New()
		configureEchoRoutersForTest(devServer, &config.Profile{
			Mode:             common.ReleaseModeDev,
			CORSAllowOrigins: []string{"http://localhost:3000"},
		})
		prodServer = echo.New()
		configureEchoRoutersForTest(prodServer, &config.Profile{Mode: common.ReleaseModeProd})
	})
}

func devTestServer() *echo.Echo {
	testServers()
	return devServer
}

func prodTestServer() *echo.Echo {
	testServers()
	return prodServer
}

func doRequest(e *echo.Echo, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	recorder := httptest.NewRecorder()
	e.ServeHTTP(recorder, req)
	return recorder
}

func TestConfigureEchoRoutersServesHealthz(t *testing.T) {
	t.Parallel()

	recorder := doRequest(prodTestServer(), http.MethodGet, "/healthz", nil)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "OK", recorder.Body.String())
}

// The SPA's user menu reads the build metadata from here, so the route has to
// answer without a session (like /healthz) and carry all three fields even when
// nothing was injected at build time.
func TestConfigureEchoRoutersServesBuildInfo(t *testing.T) {
	t.Parallel()

	recorder := doRequest(prodTestServer(), http.MethodGet, "/api/version", nil)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, version.Version, body["version"])
	require.Equal(t, version.GitCommit, body["git_commit"])
	require.Equal(t, version.BuildTime, body["build_time"])
}

// CORS is an explicit allowlist now. A credentialed policy that echoes any
// origin lets any website issue authenticated requests, which is why the former
// "any origin in dev" default is gone.
func TestConfigureEchoRoutersCORSFollowsTheProfile(t *testing.T) {
	t.Parallel()

	t.Run("allowed origin is echoed", func(t *testing.T) {
		t.Parallel()
		recorder := doRequest(devTestServer(), http.MethodOptions, "/healthz", map[string]string{
			"Origin":                        "http://localhost:3000",
			"Access-Control-Request-Method": http.MethodPost,
		})
		require.Equal(t, "http://localhost:3000", recorder.Header().Get("Access-Control-Allow-Origin"))
		require.Equal(t, "true", recorder.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("unlisted origin gets no CORS headers", func(t *testing.T) {
		t.Parallel()
		recorder := doRequest(devTestServer(), http.MethodOptions, "/healthz", map[string]string{
			"Origin":                        "https://evil.example.com",
			"Access-Control-Request-Method": http.MethodPost,
		})
		require.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
		require.Empty(t, recorder.Header().Get("Access-Control-Allow-Credentials"))
	})

	t.Run("prod emits no CORS headers", func(t *testing.T) {
		t.Parallel()
		recorder := doRequest(prodTestServer(), http.MethodGet, "/healthz", map[string]string{
			"Origin": "https://evil.example.com",
		})
		require.Empty(t, recorder.Header().Get("Access-Control-Allow-Origin"))
		require.Empty(t, recorder.Header().Get("Access-Control-Allow-Credentials"))
	})
}

// pprof is registered unconditionally but gated on RuntimeDebug, which --debug
// sets. Without the flag the endpoints must not answer.
func TestPprofFollowsRuntimeDebug(t *testing.T) {
	t.Parallel()

	debug := &atomic.Bool{}
	e := echo.New()
	registerPprof(e, debug)
	require.Equal(t, http.StatusNotFound, doRequest(e, http.MethodGet, "/debug/pprof/", nil).Code)

	debug.Store(true)
	require.Equal(t, http.StatusOK, doRequest(e, http.MethodGet, "/debug/pprof/", nil).Code)
}

// The build does not bundle the frontend, so the catch-all serves a placeholder
// that says so instead of a blank page.
func TestFrontendPlaceholderIsServed(t *testing.T) {
	t.Parallel()

	recorder := doRequest(prodTestServer(), http.MethodGet, "/some/spa/route", nil)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "does not bundle frontend and backend together")
}

func TestRecoverMiddlewareTurnsPanicsIntoErrors(t *testing.T) {
	t.Parallel()

	e := echo.New()
	e.Use(recoverMiddleware)
	e.GET("/boom", func(echo.Context) error {
		panic("kaboom")
	})

	// A panic must not escape the middleware and take the process down.
	recorder := doRequest(e, http.MethodGet, "/boom", nil)
	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "kaboom")
}

// registerPprof takes the flag by pointer, so a runtime change is picked up.
func TestRegisterPprofUsesThePointer(t *testing.T) {
	t.Parallel()

	debug := &atomic.Bool{}
	e := echo.New()
	registerPprof(e, debug)

	recorder := doRequest(e, http.MethodGet, "/debug/pprof/goroutine", nil)
	require.Equal(t, http.StatusNotFound, recorder.Code)

	debug.Store(true)
	// The payload is a gzipped profile, so only the status is asserted here.
	require.Equal(t, http.StatusOK, doRequest(e, http.MethodGet, "/debug/pprof/goroutine", nil).Code)
}

// The Prometheus endpoint moved off the API port: it is served only by the
// dedicated --metrics-addr listener, which these tests do not start. A GET
// /metrics here falls through to the SPA catch-all instead of exposing metrics.
func TestMetricsNotServedOnAPIPort(t *testing.T) {
	t.Parallel()

	recorder := doRequest(prodTestServer(), http.MethodGet, "/metrics", nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Header().Get(echo.HeaderContentType), "text/html",
		"/metrics must fall through to the SPA, not be served by the metrics handler")
	require.NotContains(t, recorder.Body.String(), "metaxisdata_")
}
