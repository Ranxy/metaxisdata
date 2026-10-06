package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
)

// wantContentSecurityPolicy is the policy the SPA was built against. Asserting
// the whole map (rather than a few directives) makes every relaxation a
// deliberate change, and reading it off a served response means removing the
// middleware cannot leave the test green.
var wantContentSecurityPolicy = map[string]string{
	"default-src":     "'self'",
	"script-src":      "'self'",
	"style-src":       "'self' 'unsafe-inline'",
	"img-src":         "'self' data:",
	"worker-src":      "'self'",
	"object-src":      "'none'",
	"base-uri":        "'none'",
	"form-action":     "'self'",
	"frame-ancestors": "'none'",
}

// The SPA carries the HttpOnly session cookie, so a clickjacker must not be able
// to frame it (the /device approval page is the interesting one) and a script
// the bundle did not ship must not run in its origin.
//
// The middleware is registered on the echo instance, not per route, which is why
// a ConnectRPC path and the REST gateway are in the target list: if either of
// them answered without the headers, the API would be a document-free hole in
// the policy. Those two are not registered on this echo (grpc routes are wired
// separately), so their status is not asserted — only that the middleware ran.
// The real wiring is checked against a running server in the integration suite.
func TestSecurityHeadersAreServed(t *testing.T) {
	t.Parallel()

	servers := map[string]*echo.Echo{
		"dev":  devTestServer(),
		"prod": prodTestServer(),
	}
	targets := []struct {
		name       string
		method     string
		target     string
		wantStatus int
	}{
		{name: "SPA document", method: http.MethodGet, target: "/some/spa/route", wantStatus: http.StatusOK},
		{name: "client-side route", method: http.MethodGet, target: "/device?user_code=ABCD-EFGH", wantStatus: http.StatusOK},
		{name: "healthz", method: http.MethodGet, target: "/healthz", wantStatus: http.StatusOK},
		{name: "ConnectRPC path", method: http.MethodPost, target: "/metaxisdata.v1.UserService/GetUser"},
		{name: "REST gateway path", method: http.MethodGet, target: "/v1/openlineage/runs"},
	}

	for serverName, e := range servers {
		for _, target := range targets {
			t.Run(serverName+"/"+target.name, func(t *testing.T) {
				t.Parallel()

				recorder := doRequest(e, target.method, target.target, nil)
				if target.wantStatus != 0 {
					require.Equal(t, target.wantStatus, recorder.Code)
				}

				headers := recorder.Header()
				require.Equal(t, "nosniff", headers.Get(echo.HeaderXContentTypeOptions))
				require.Equal(t, "DENY", headers.Get(echo.HeaderXFrameOptions))
				// Both are deliberately absent: X-XSS-Protection is deprecated
				// and HSTS would need a trustworthy https signal.
				require.Empty(t, headers.Get("X-XSS-Protection"))
				require.Empty(t, headers.Get(echo.HeaderStrictTransportSecurity))

				require.Equal(t, wantContentSecurityPolicy, parseContentSecurityPolicy(headers.Get(echo.HeaderContentSecurityPolicy)))
			})
		}
	}
}

func parseContentSecurityPolicy(csp string) map[string]string {
	directives := make(map[string]string)
	for _, directive := range strings.Split(csp, ";") {
		name, value, _ := strings.Cut(strings.TrimSpace(directive), " ")
		directives[name] = value
	}
	return directives
}
