package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/config"
)

func csrfTestServer() *echo.Echo {
	e := echo.New()
	e.Use(csrfProtectionMiddleware(&config.Profile{CORSAllowOrigins: []string{"http://localhost:3000"}}))
	handler := func(c echo.Context) error {
		return c.String(http.StatusOK, "ok")
	}
	e.POST("/write", handler)
	e.GET("/write", handler)
	return e
}

// The session cookie is attached automatically by the browser, so a
// state-changing request that carries it must come from a trusted origin.
func TestCSRFProtectionMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		headers    map[string]string
		wantStatus int
	}{
		{"cross-site fetch is rejected", http.MethodPost,
			map[string]string{"Cookie": "access-token=x", "Sec-Fetch-Site": "cross-site"}, http.StatusForbidden},
		{"same-origin fetch passes", http.MethodPost,
			map[string]string{"Cookie": "access-token=x", "Sec-Fetch-Site": "same-origin"}, http.StatusOK},
		{"same-site fetch passes", http.MethodPost,
			map[string]string{"Cookie": "access-token=x", "Sec-Fetch-Site": "same-site"}, http.StatusOK},
		{"direct navigation passes", http.MethodPost,
			map[string]string{"Cookie": "access-token=x", "Sec-Fetch-Site": "none"}, http.StatusOK},
		{"cross-site origin is rejected", http.MethodPost,
			map[string]string{"Cookie": "access-token=x", "Origin": "https://evil.example.com"}, http.StatusForbidden},
		{"allowlisted origin passes", http.MethodPost,
			map[string]string{"Cookie": "access-token=x", "Origin": "http://localhost:3000"}, http.StatusOK},
		{"cross-site referer is rejected", http.MethodPost,
			map[string]string{"Cookie": "access-token=x", "Referer": "https://evil.example.com/form"}, http.StatusForbidden},
		{"read is never blocked", http.MethodGet,
			map[string]string{"Cookie": "access-token=x", "Sec-Fetch-Site": "cross-site"}, http.StatusOK},
		{"a bearer token is exempt", http.MethodPost,
			map[string]string{"Cookie": "access-token=x", "Authorization": "Bearer t", "Sec-Fetch-Site": "cross-site"}, http.StatusOK},
		{"no session cookie is exempt", http.MethodPost,
			map[string]string{"Sec-Fetch-Site": "cross-site"}, http.StatusOK},
	}

	e := csrfTestServer()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tc.method, "/write", nil)
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			recorder := httptest.NewRecorder()
			e.ServeHTTP(recorder, req)
			require.Equal(t, tc.wantStatus, recorder.Code, "body: %s", recorder.Body.String())
		})
	}
}

func TestIsTrustedRequestOriginFallsBackToTheHost(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "http://metaxis.example.com/write", nil)
	req.Host = "metaxis.example.com"
	req.Header.Set("Origin", "http://metaxis.example.com")
	require.True(t, isTrustedRequestOrigin(req, nil))

	req.Header.Set("Origin", "http://other.example.com")
	require.False(t, isTrustedRequestOrigin(req, nil))

	// The opaque origin of sandboxed iframes and data: URLs is never trusted.
	req.Header.Set("Origin", "null")
	require.False(t, isTrustedRequestOrigin(req, nil))

	// Non-browser clients send neither Origin nor Referer; they cannot be CSRF
	// vectors, and bearer tokens are checked separately.
	req.Header.Del("Origin")
	require.True(t, isTrustedRequestOrigin(req, nil))
}
