package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// The SPA carries the HttpOnly session cookie, so a clickjacker must not be able
// to frame it (the /device approval page is the interesting one) and a script
// the bundle did not ship must not run in its origin.
func TestSecurityHeadersAreServed(t *testing.T) {
	t.Parallel()

	servers := map[string]*echo.Echo{
		"dev":  devTestServer(),
		"prod": prodTestServer(),
	}
	// The document is what matters; the API responses carry the headers too
	// because the middleware is registered once for the whole router.
	targets := map[string]string{
		"SPA document": "/some/spa/route",
		"API response": "/healthz",
	}

	for serverName, e := range servers {
		for targetName, target := range targets {
			t.Run(serverName+"/"+targetName, func(t *testing.T) {
				t.Parallel()

				recorder := doRequest(e, http.MethodGet, target, nil)

				require.Equal(t, http.StatusOK, recorder.Code)
				headers := recorder.Header()
				require.Equal(t, "nosniff", headers.Get(echo.HeaderXContentTypeOptions))
				require.Equal(t, "DENY", headers.Get(echo.HeaderXFrameOptions))
				require.Empty(t, headers.Get("X-XSS-Protection"))

				directives := parseContentSecurityPolicy(headers.Get(echo.HeaderContentSecurityPolicy))
				require.Equal(t, "'self'", directives["default-src"])
				// No inline script and no eval: the bundle is the only script
				// source, which is what stops a `javascript:` link from running
				// even if one reaches an href.
				require.Equal(t, "'self'", directives["script-src"])
				require.Equal(t, "'none'", directives["frame-ancestors"])
				require.Equal(t, "'none'", directives["object-src"])
			})
		}
	}
}

// The relaxations are the ones the SPA needs; anything wider would have to be a
// deliberate change, so pin the whole policy.
func TestContentSecurityPolicyAllowsOnlyWhatTheSPANeeds(t *testing.T) {
	t.Parallel()

	require.Equal(t, map[string]string{
		"default-src":     "'self'",
		"script-src":      "'self'",
		"style-src":       "'self' 'unsafe-inline'",
		"img-src":         "'self' data:",
		"worker-src":      "'self'",
		"object-src":      "'none'",
		"frame-ancestors": "'none'",
	}, parseContentSecurityPolicy(contentSecurityPolicy))
}

func parseContentSecurityPolicy(csp string) map[string]string {
	directives := make(map[string]string)
	for _, directive := range strings.Split(csp, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(directive), " ")
		directives[name] = value
	}
	return directives
}
