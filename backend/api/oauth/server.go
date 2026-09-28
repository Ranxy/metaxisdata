package oauth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/audit"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// SPA paths the browser flow uses.
const (
	// ConsentPath is the page that shows a pending request and records the
	// decision.
	ConsentPath = "/oauth/consent"
	// SignInPath is the SPA's sign-in page; it honours a `redirect` query
	// parameter and comes back with the browser where it left off.
	SignInPath = "/login"
)

// ServerConfig is everything the OAuth 2.1 protocol endpoints need.
type ServerConfig struct {
	Stores *store.Store
	State  *state.State
	// Tokens applies the same token rules the ConnectRPC API uses; the browser
	// endpoints authenticate the session cookie through it.
	Tokens *auth.TokenAuthenticator
	Mode   common.ReleaseMode
	// Secret signs the access tokens issued by the token endpoint.
	Secret string
	// TrustedProxies decides whether a forwarded address may be believed.
	TrustedProxies []string
	// Endpoints resolves the deployment's issuer and resource per request.
	Endpoints EndpointsFunc
}

// Server serves the OAuth 2.1 protocol endpoints. They are plain HTTP, because
// OAuth clients do not speak ConnectRPC; that also means they sit outside the
// ConnectRPC interceptor chain and do their own authentication, rate limiting
// and error rendering.
type Server struct {
	config ServerConfig
	now    func() time.Time
}

// NewServer returns a server for the configured deployment.
func NewServer(config ServerConfig) *Server {
	return &Server{config: config, now: time.Now}
}

// endpoints resolves the deployment's identifiers for one request, answering the
// request itself when the surface is off or misconfigured. A false return means
// a response has already been written.
func (s *Server) endpoints(w http.ResponseWriter, r *http.Request) (Endpoints, bool) {
	resolved, enabled, err := s.config.Endpoints(r.Context())
	if err != nil {
		writeUnavailable(w)
		return Endpoints{}, false
	}
	if !enabled {
		http.NotFound(w, r)
		return Endpoints{}, false
	}
	return resolved, true
}

// sessionUser resolves the signed-in user from the browser's session cookie.
//
// These endpoints are outside the ConnectRPC interceptor chain, so they
// authenticate themselves — with the same token rules and the same user-API
// audience the SPA's own requests use, because the browser carries the same
// cookie. A signed-in user who has been deactivated mid-flow is refused here.
func (s *Server) sessionUser(r *http.Request) (*store.UserMessage, error) {
	token, err := auth.GetTokenFromHeaders(r.Header)
	if err != nil {
		return nil, err
	}
	user, _, err := s.config.Tokens.Resolve(r.Context(), token, auth.AccessTokenAudience(s.config.Mode))
	if err != nil {
		return nil, err
	}
	return user, nil
}

// requestIP is the client address as the consent page and the audit trail see
// it, which means the trusted-proxy rules apply.
func (s *Server) requestIP(r *http.Request) string {
	return audit.BuildRequestMetadata(r.Header, r.RemoteAddr, s.config.TrustedProxies).GetIp()
}

// consentURL is the page the browser is sent to once a request is pending.
func consentURL(endpoints Endpoints, requestID string) string {
	return endpoints.Issuer + ConsentPath + "?request_id=" + url.QueryEscape(requestID)
}

// signInURL sends the browser to the SPA's sign-in page and back afterwards.
func signInURL(endpoints Endpoints, returnTo string) string {
	return endpoints.Issuer + SignInPath + "?redirect=" + url.QueryEscape(returnTo)
}

// writeJSON writes a response with the headers an OAuth response needs: token
// and metadata responses must never be stored by a shared cache.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// oauthError is the RFC 6749 error body.
type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description,omitempty"`
}

// writeOAuthError answers a protocol request with an RFC 6749 error.
func writeOAuthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, oauthError{Error: code, Description: description})
}

// writeBrowserError renders a failure for the user agent itself. It is used only
// where redirecting is not safe: before the client and its redirect URI have
// been validated, and on the completion endpoint, whose target is only known
// once the store hands the finished request back.
func writeBrowserError(w http.ResponseWriter, status int, code, description string) {
	http.Error(w, code+": "+description, status)
}
