package oauth

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
)

// tokenRequest is a parsed /oauth/token request.
type tokenRequest struct {
	ClientID     string
	Code         string
	RedirectURI  string
	CodeVerifier string
	Resource     string
}

// tokenResponse is the RFC 6749 success body.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Scope       string `json:"scope,omitempty"`
}

// TokenHandler serves POST /oauth/token for the authorization-code grant.
//
// There is no refresh token on purpose: the access token lasts as long as a web
// session, and a client that finds it expired runs the flow again. That keeps the
// number of long-lived credentials in the system at zero, at the cost of a
// browser round trip on expiry.
func (s *Server) TokenHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoints, ok := s.endpoints(w, r)
		if !ok {
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "the token endpoint is a POST")
			return
		}
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the body must be an application/x-www-form-urlencoded form")
			return
		}
		if r.PostForm.Get("grant_type") != "authorization_code" {
			writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported")
			return
		}
		request := tokenRequest{
			ClientID:     r.PostForm.Get("client_id"),
			Code:         r.PostForm.Get("code"),
			RedirectURI:  r.PostForm.Get("redirect_uri"),
			CodeVerifier: r.PostForm.Get("code_verifier"),
			Resource:     r.PostForm.Get("resource"),
		}
		// RFC 8707: the resource is required at the token endpoint too, and it
		// must be this deployment's. Checking it here means a code cannot be
		// redeemed for a token addressed somewhere else.
		if request.Resource != endpoints.Resource {
			writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource must be "+endpoints.Resource)
			return
		}
		// Exchange consumes the code whether or not the checks below pass, so a
		// stolen code cannot be replayed after a failed attempt.
		grant, err := s.config.State.OAuthAuthorizationRequestStore.Exchange(request.Code, s.now())
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the authorization code is unknown, expired or already used")
			return
		}
		if grant.ClientID != request.ClientID {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the code was issued to another client")
			return
		}
		if grant.RedirectURI != request.RedirectURI {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
			return
		}
		if !VerifyPKCE(grant.CodeChallenge, request.CodeVerifier) {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the PKCE verifier does not match the challenge")
			return
		}
		user, err := s.config.Stores.GetUserByID(r.Context(), grant.ApprovedByUserID)
		if err != nil || user == nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the approving user no longer exists")
			return
		}
		// The ledger records who received a credential and for which client; the
		// token itself is never part of the row.
		recordAuditActor(r, user.Email)
		recordAuditDetail(r, map[string]any{"clientId": grant.ClientID, "resource": endpoints.Resource})
		duration := auth.GetTokenDuration(r.Context(), s.config.Stores)
		scope := grantedScope(grant.Scopes)
		token, err := auth.GenerateMCPAccessToken(user.Name, user.ID, endpoints.Resource, scope, s.config.Secret, duration)
		if err != nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue the token")
			return
		}
		// A client that has exchanged a code is in use, and last_used_at is what the
		// planned "connected apps" page will read. Best effort: the token is valid
		// whether or not the timestamp landed.
		if err := s.config.Stores.TouchOAuthClient(r.Context(), grant.ClientID); err != nil {
			slog.Warn("failed to record an OAuth client's last use", clog.WithError(err))
		}
		writeJSON(w, http.StatusOK, tokenResponse{
			AccessToken: token,
			TokenType:   "Bearer",
			ExpiresIn:   int64(duration.Seconds()),
			Scope:       scope,
		})
	})
}

// grantedScope renders the granted scopes the way the token response reports
// them, defaulting to the scope the metadata advertises.
func grantedScope(scopes []string) string {
	if len(scopes) == 0 {
		return MCPReadScope
	}
	return strings.Join(scopes, " ")
}
