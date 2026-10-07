package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	authpkg "github.com/Ranxy/metaxisdata/backend/api/auth"
	oauthpkg "github.com/Ranxy/metaxisdata/backend/api/oauth"
	"github.com/Ranxy/metaxisdata/backend/mcp"
)

// recordingLimiter is a per-key source budget that remembers the keys it was
// asked about, so a test can assert which address a request was counted against.
type recordingLimiter struct {
	limit int
	calls []string
	seen  map[string]int
}

func (l *recordingLimiter) Allow(key string, _ time.Time) bool {
	if l.seen == nil {
		l.seen = map[string]int{}
	}
	l.calls = append(l.calls, key)
	l.seen[key]++
	return l.seen[key] <= l.limit
}

// mcpProbe is a request to /mcp that carries no credential, which is exactly the
// probe the per-principal budget never sees.
func mcpProbe(remoteAddr string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	request.RemoteAddr = remoteAddr
	return request
}

// TestMCPSourceLimiterBoundsProbesBeforeTheBearerCheck pins the gap the
// per-principal budget leaves: an unauthenticated request is refused during
// identity resolution, so only a request-level budget can bound it.
func TestMCPSourceLimiterBoundsProbesBeforeTheBearerCheck(t *testing.T) {
	t.Parallel()

	limiter := &recordingLimiter{limit: 1}
	server := mcp.NewServer(mcp.Config{
		Endpoints:         enabledEndpoints(),
		SourceCallLimiter: limiter,
	})
	handler := server.Handler(authpkg.NewTokenAuthenticator(verifierUsers{}, verifierSecret, nil))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, mcpProbe("203.0.113.9:5555"))
	require.Equal(t, http.StatusUnauthorized, recorder.Code, "the first probe reaches the bearer check")
	require.Equal(t, []string{"ip:203.0.113.9"}, limiter.calls)

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, mcpProbe("203.0.113.9:5555"))
	require.Equal(t, http.StatusTooManyRequests, recorder.Code)
	require.Equal(t, "60", recorder.Header().Get("Retry-After"))
	require.Len(t, limiter.calls, 2)

	// Another address has its own budget.
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, mcpProbe("203.0.113.10:5555"))
	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

// TestMCPSourceLimiterUsesTheResolvedClientAddress pins that the key is the same
// trusted-proxy resolution the audit row uses: a listed proxy's forwarded address
// is believed, and an unlisted peer's is ignored.
func TestMCPSourceLimiterUsesTheResolvedClientAddress(t *testing.T) {
	t.Parallel()

	limiter := &recordingLimiter{limit: 10}
	server := mcp.NewServer(mcp.Config{
		Endpoints:         enabledEndpoints(),
		SourceCallLimiter: limiter,
		TrustedProxies:    []string{"10.0.0.1"},
	})
	handler := server.Handler(authpkg.NewTokenAuthenticator(verifierUsers{}, verifierSecret, nil))

	trusted := mcpProbe("10.0.0.1:5555")
	trusted.Header.Set("X-Forwarded-For", "198.51.100.7")
	handler.ServeHTTP(httptest.NewRecorder(), trusted)

	untrusted := mcpProbe("203.0.113.9:5555")
	untrusted.Header.Set("X-Forwarded-For", "198.51.100.7")
	handler.ServeHTTP(httptest.NewRecorder(), untrusted)

	require.Equal(t, []string{"ip:198.51.100.7", "ip:203.0.113.9"}, limiter.calls)
}

// TestMCPSourceLimiterIsSkippedWhenTheSurfaceIsOff pins the order: a disabled
// endpoint answers 404 without spending the budget of a caller that is only
// probing for a surface that is not there.
func TestMCPSourceLimiterIsSkippedWhenTheSurfaceIsOff(t *testing.T) {
	t.Parallel()

	limiter := &recordingLimiter{limit: 1}
	server := mcp.NewServer(mcp.Config{
		Endpoints: func(context.Context) (oauthpkg.Endpoints, bool, error) {
			return oauthpkg.Endpoints{}, false, nil
		},
		SourceCallLimiter: limiter,
	})
	handler := server.Handler(authpkg.NewTokenAuthenticator(verifierUsers{}, verifierSecret, nil))

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, mcpProbe("203.0.113.9:5555"))
	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Empty(t, limiter.calls, "a switched-off surface is not counted")
}
