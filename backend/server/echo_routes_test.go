package server

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/config"
)

// configureEchoRouters registers the Prometheus middleware, which registers
// process-wide metrics; a second registration only logs. Build one instance per
// mode and share it so the tests stay quiet.
var (
	devServerOnce  sync.Once
	devServer      *echo.Echo
	prodServerOnce sync.Once
	prodServer     *echo.Echo
)

func devTestServer() *echo.Echo {
	devServerOnce.Do(func() {
		devServer = echo.New()
		configureEchoRouters(devServer, &config.Profile{Mode: common.ReleaseModeDev})
	})
	return devServer
}

func prodTestServer() *echo.Echo {
	prodServerOnce.Do(func() {
		prodServer = echo.New()
		configureEchoRouters(prodServer, &config.Profile{Mode: common.ReleaseModeProd})
	})
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

// Dev installs a wide-open CORS middleware; prod installs none, so the browser
// same-origin policy applies. A default build is a dev build, which is why the
// release tag matters.
func TestConfigureEchoRoutersCORSFollowsTheProfile(t *testing.T) {
	t.Parallel()

	t.Run("dev allows any origin with credentials", func(t *testing.T) {
		t.Parallel()
		recorder := doRequest(devTestServer(), http.MethodOptions, "/healthz", map[string]string{
			"Origin":                        "https://evil.example.com",
			"Access-Control-Request-Method": http.MethodPost,
		})
		require.Equal(t, "https://evil.example.com", recorder.Header().Get("Access-Control-Allow-Origin"))
		require.Equal(t, "true", recorder.Header().Get("Access-Control-Allow-Credentials"))
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
