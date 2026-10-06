//go:build integration

package runner

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The unit test builds an echo with configureEchoRouters alone, but the routes
// that matter are registered by other functions (Connect handlers, the REST
// gateway, OAuth, the MCP wrapper). This drives the real binary and requires the
// same policy on every one of them: a response without the headers is a document
// - or JSON the browser may render as one - outside the clickjacking and script
// policy. Statuses differ (404 for a disabled MCP surface, 401/400 for an
// unauthenticated call), so only the headers are asserted.
func TestSecurityHeadersRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 10 * time.Second}

	targets := []struct {
		name   string
		method string
		path   string
	}{
		{name: "SPA document", method: http.MethodGet, path: "/"},
		{name: "client-side route", method: http.MethodGet, path: "/device?user_code=ABCD-EFGH"},
		{name: "ConnectRPC path", method: http.MethodPost, path: "/metaxisdata.v1.UserService/GetUser"},
		{name: "REST gateway path", method: http.MethodGet, path: "/v1/openlineage/runs"},
		{name: "OAuth authorize", method: http.MethodGet, path: "/oauth/authorize?client_id=nope&response_type=code"},
		{name: "MCP endpoint", method: http.MethodGet, path: "/mcp"},
	}

	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequestWithContext(ctx, target.method, env.BaseURL+target.path, nil)
			require.NoError(t, err)
			resp, err := httpClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			csp := resp.Header.Get("Content-Security-Policy")
			require.Contains(t, csp, "script-src 'self'")
			require.Contains(t, csp, "frame-ancestors 'none'")
			require.Equal(t, "DENY", resp.Header.Get("X-Frame-Options"))
			require.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
			require.Empty(t, resp.Header.Get("Strict-Transport-Security"))
			require.Empty(t, resp.Header.Get("X-XSS-Protection"))
		})
	}
}
