package oauth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The two grants the token endpoint implements.
const (
	grantTypeAuthorizationCode = "authorization_code"
	grantTypeRefreshToken      = "refresh_token"
)

// tokenRequest is a parsed /oauth/token request.
type tokenRequest struct {
	GrantType    string
	ClientID     string
	Code         string
	RedirectURI  string
	CodeVerifier string
	Resource     string
	RefreshToken string
	Scope        string
}

// tokenResponse is the RFC 6749 success body.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// oauthGrant is the consented state one access/refresh token pair is minted
// from. It travels unchanged from the authorization code through every refresh,
// so a refresh re-issues a grant and never widens one.
type oauthGrant struct {
	clientID string
	user     *store.UserMessage
	scopes   []string
	// issuedAt travels from the original consent through every rotation, so the
	// grant can still be ordered against the user's last password change.
	issuedAt time.Time
}

// TokenHandler serves POST /oauth/token for the authorization-code and
// refresh_token grants.
//
// The refresh token is a rotating, opaque credential: each refresh consumes the
// token it was presented and issues a replacement, so a copy stolen from the
// client's storage is worthless the moment the real holder refreshes. That is
// what lets an MCP client outlive its access token without sending the user
// through the browser flow again.
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
		request := tokenRequest{
			GrantType:    r.PostForm.Get("grant_type"),
			ClientID:     r.PostForm.Get("client_id"),
			Code:         r.PostForm.Get("code"),
			RedirectURI:  r.PostForm.Get("redirect_uri"),
			CodeVerifier: r.PostForm.Get("code_verifier"),
			Resource:     r.PostForm.Get("resource"),
			RefreshToken: r.PostForm.Get("refresh_token"),
			Scope:        r.PostForm.Get("scope"),
		}
		// RFC 8707: the resource is required at the token endpoint too, and it
		// must be this deployment's. Checking it here means neither a code nor a
		// refresh token can be redeemed for a token addressed somewhere else.
		if request.Resource != endpoints.Resource {
			writeOAuthError(w, http.StatusBadRequest, "invalid_target", "resource must be "+endpoints.Resource)
			return
		}

		switch request.GrantType {
		case grantTypeAuthorizationCode:
			s.exchangeAuthorizationCode(w, r, request, endpoints)
		case grantTypeRefreshToken:
			s.exchangeRefreshToken(w, r, request, endpoints)
		default:
			writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
		}
	})
}

// exchangeAuthorizationCode redeems an authorization code and issues the first
// token pair of a connection.
func (s *Server) exchangeAuthorizationCode(w http.ResponseWriter, r *http.Request, request tokenRequest, endpoints Endpoints) {
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
	// Consent validates the session, but the account can be deactivated between
	// the approval and this exchange. The refresh grant re-checks the same thing,
	// so refusing here keeps the two from disagreeing about a deactivated user.
	if user.MemberDeleted {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the approving user has been deactivated")
		return
	}
	s.issueGrant(w, r, endpoints, oauthGrant{clientID: grant.ClientID, user: user, scopes: grant.Scopes, issuedAt: s.now()})
}

// exchangeRefreshToken rotates a grant. Every check that can be made from the
// stored row runs before the row is consumed, so a request the client can
// correct does not burn the credential it was about to use; the checks that need
// the live principal run after, because a refusal there must end the grant.
func (s *Server) exchangeRefreshToken(w http.ResponseWriter, r *http.Request, request tokenRequest, endpoints Endpoints) {
	if request.ClientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id is required")
		return
	}
	if request.RefreshToken == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "refresh_token is required")
		return
	}
	// Every lookup is scoped to the client the grant was issued to, so a hash
	// leaked from one registration cannot be redeemed by another.
	tokenHash := auth.HashToken(request.RefreshToken)
	stored, err := s.config.Stores.GetOAuthRefreshToken(r.Context(), request.ClientID, tokenHash)
	if err != nil {
		slog.Error("failed to look up an OAuth refresh token", clog.WithError(err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to look up the refresh token")
		return
	}
	if stored == nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is unknown, already used or was issued to another client")
		return
	}
	// The response for an expired token is the same invalid_grant a consumed one
	// gets; the row is dropped while refusing it, since Consume is the atomic
	// delete and nothing is being issued.
	if !s.now().Before(stored.ExpiresAt) {
		if _, err := s.config.Stores.ConsumeOAuthRefreshToken(r.Context(), request.ClientID, tokenHash); err != nil {
			slog.Warn("failed to drop an expired OAuth refresh token", clog.WithError(err))
		}
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token has expired")
		return
	}
	// The grant is pinned to the resource the user consented to. A deployment
	// whose external URL moved must not have old grants mint tokens for a
	// resource identifier that no longer exists; the client authorizes again.
	if stored.Resource != endpoints.Resource {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the grant was issued for another resource; authorize again")
		return
	}
	// A refresh re-issues the grant as consented: a request naming a different
	// scope is refused rather than honored. Omitting the parameter inherits the
	// consented scope.
	if requested := ParseScopeList(request.Scope); len(requested) > 0 && strings.Join(requested, " ") != stored.Scope {
		writeOAuthError(w, http.StatusBadRequest, "invalid_scope", "the grant was issued a different scope")
		return
	}
	// The atomic delete is the single-use gate: concurrent refreshes of one
	// token race here and only the caller that deletes the row may issue.
	consumed, err := s.config.Stores.ConsumeOAuthRefreshToken(r.Context(), request.ClientID, tokenHash)
	if err != nil {
		slog.Error("failed to consume an OAuth refresh token", clog.WithError(err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to consume the refresh token")
		return
	}
	if !consumed {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is unknown, already used or was issued to another client")
		return
	}
	user, err := s.config.Stores.GetUserByID(r.Context(), stored.UserID)
	if err != nil || user == nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the approving user no longer exists")
		return
	}
	if user.MemberDeleted {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the approving user has been deactivated")
		return
	}
	// A grant issued before the last password change must not mint a fresh access
	// token: that would resurrect exactly the session the change retired. The
	// comparison uses persisted state, so it holds across replicas, just as the
	// access token's own iat cutoff does.
	if last := user.Profile.GetLastChangePasswordTime(); last != nil && auth.TokenPredatesPasswordChange(stored.IssuedAt, last.AsTime()) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the grant predates the last password change; authorize again")
		return
	}
	s.issueGrant(w, r, endpoints, oauthGrant{clientID: request.ClientID, user: user, scopes: ParseScopeList(stored.Scope), issuedAt: stored.IssuedAt})
}

// issueGrant mints the access token and the rotated refresh token for one
// consented grant, records the audit row and writes the token response. Both
// grants converge here, so the two cannot drift in what they issue or record.
func (s *Server) issueGrant(w http.ResponseWriter, r *http.Request, endpoints Endpoints, grant oauthGrant) {
	// The ledger records who received a credential and for which client; the
	// tokens themselves are never part of the row.
	recordAuditActor(r, grant.user.Email)
	recordAuditDetail(r, map[string]any{"clientId": grant.clientID, "resource": endpoints.Resource})

	duration := auth.GetTokenDuration(r.Context(), s.config.Stores)
	scope := grantedScope(grant.scopes)
	accessToken, err := auth.GenerateMCPAccessToken(grant.user.Name, grant.user.ID, grant.clientID, endpoints.Resource, scope, s.config.Secret, duration)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue the token")
		return
	}
	refreshToken, err := s.storeRefreshToken(r.Context(), grant, endpoints.Resource, scope)
	if err != nil {
		slog.Error("failed to store an OAuth refresh token", clog.WithError(err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue the refresh token")
		return
	}
	// A client that has exchanged a code is in use, and last_used_at is what the
	// planned "connected apps" page will read. Best effort: the token is valid
	// whether or not the timestamp landed.
	if err := s.config.Stores.TouchOAuthClient(r.Context(), grant.clientID); err != nil {
		slog.Warn("failed to record an OAuth client's last use", clog.WithError(err))
	}
	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(duration.Seconds()),
		RefreshToken: refreshToken,
		Scope:        scope,
	})
}

// storeRefreshToken mints a refresh token and stores its digest, returning the
// plaintext for the response body. It is the only place the plaintext exists
// outside the client's own storage.
//
// The expiry is measured from now on every rotation, so an active client's
// connection keeps working without a re-authorization; what it can mint stays
// bounded by the resource it was consented for and by the principal's state at
// each refresh.
func (s *Server) storeRefreshToken(ctx context.Context, grant oauthGrant, resource, scope string) (string, error) {
	token, err := auth.GenerateRefreshToken()
	if err != nil {
		return "", err
	}
	if err := s.config.Stores.CreateOAuthRefreshToken(ctx, &store.OAuthRefreshToken{
		TokenHash: auth.HashToken(token),
		ClientID:  grant.clientID,
		UserID:    grant.user.ID,
		Resource:  resource,
		Scope:     scope,
		ExpiresAt: s.now().Add(auth.GetRefreshTokenDuration(ctx, s.config.Stores)),
		IssuedAt:  grant.issuedAt,
	}); err != nil {
		return "", err
	}
	return token, nil
}

// grantedScope renders the granted scopes the way the token response reports
// them, defaulting to the scope the metadata advertises.
func grantedScope(scopes []string) string {
	if len(scopes) == 0 {
		return MCPReadScope
	}
	return strings.Join(scopes, " ")
}
