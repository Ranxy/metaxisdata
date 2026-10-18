package oauth

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TokenStore is the store surface the token endpoint needs. It exists so the
// refresh and rotation path can be exercised without a database: *store.Store
// implements it, production leaves ServerConfig.TokenStores nil, and a test can
// put a fake behind it.
type TokenStore interface {
	GetOAuthGrant(ctx context.Context, clientID, tokenHash string) (*store.OAuthGrant, error)
	CreateOAuthGrant(ctx context.Context, create *store.OAuthGrant) (int64, error)
	RotateOAuthGrant(ctx context.Context, clientID, oldHash, newHash string, expiresAt, now time.Time) (int64, bool, error)
	RevokeOAuthGrant(ctx context.Context, id int64, revokedAt time.Time, revokedBy *int) (bool, error)
	GetUserByID(ctx context.Context, id int) (*store.UserMessage, error)
	TouchOAuthClient(ctx context.Context, clientID string) error
}

// tokenStore returns the store the token endpoint uses, preferring a test's fake.
func (c *ServerConfig) tokenStore() TokenStore {
	if c.TokenStores != nil {
		return c.TokenStores
	}
	return c.Stores
}

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
// client's storage is worthless the moment the real holder refreshes. The grant
// row is updated in place, which keeps its id stable: the id is what a connected
// app is addressed by and what an access token carries, so revoking it and
// rotating it serialize on one row instead of racing a delete with an insert.
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
	user, err := s.config.tokenStore().GetUserByID(r.Context(), grant.ApprovedByUserID)
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
	consented := oauthGrant{clientID: grant.ClientID, user: user, scopes: grant.Scopes, issuedAt: s.now()}
	// The grant row is created before the access token because the token carries
	// the row's id; a connection without that handle could not be revoked by its
	// owner or by an administrator.
	grantID, refreshToken, err := s.createGrant(r.Context(), consented, endpoints.Resource)
	if err != nil {
		slog.Error("failed to store an OAuth grant", clog.WithError(err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue the refresh token")
		return
	}
	s.writeGrantResponse(w, r, endpoints, consented, grantID, refreshToken)
}

// exchangeRefreshToken rotates a grant. Every check that can be made from the
// stored row runs before the row is rotated, so a request the client can correct
// does not burn the credential it was about to use; the checks that need the
// live principal run after, because a refusal there must end the grant.
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
	stored, err := s.config.tokenStore().GetOAuthGrant(r.Context(), request.ClientID, tokenHash)
	if err != nil {
		slog.Error("failed to look up an OAuth grant", clog.WithError(err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to look up the refresh token")
		return
	}
	if stored == nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is unknown, already used or was issued to another client")
		return
	}
	now := s.now()
	// An expired row is left for the maintenance runner: it is what the
	// connected-apps page shows as EXPIRED, and it can issue nothing.
	if !now.Before(stored.ExpiresAt) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token has expired")
		return
	}
	// A revoked connection is terminal: the access token path refuses its tokens
	// and this path refuses a new pair. Re-authorizing creates a new row.
	if stored.RevokedAt != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the connection has been revoked; authorize again")
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
	// The atomic rotation is the single-use gate: concurrent refreshes of one
	// token race here and only the caller that claims the row may issue. The row
	// id survives, so the connection keeps one identity across rotations.
	grantID, refreshToken, rotated, err := s.rotateGrant(r.Context(), request.ClientID, tokenHash, now)
	if err != nil {
		slog.Error("failed to rotate an OAuth grant", clog.WithError(err))
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to consume the refresh token")
		return
	}
	if !rotated {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the refresh token is unknown, already used or was issued to another client")
		return
	}
	user, err := s.config.tokenStore().GetUserByID(r.Context(), stored.UserID)
	if err != nil || user == nil {
		s.revokeFailedGrant(r.Context(), grantID)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the approving user no longer exists")
		return
	}
	if user.MemberDeleted {
		s.revokeFailedGrant(r.Context(), grantID)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the approving user has been deactivated")
		return
	}
	// A grant issued before the last password change must not mint a fresh access
	// token: that would resurrect exactly the session the change retired. The
	// comparison uses persisted state, so it holds across replicas, just as the
	// access token's own iat cutoff does.
	if last := user.Profile.GetLastChangePasswordTime(); last != nil && auth.TokenPredatesPasswordChange(stored.CreatedAt, last.AsTime()) {
		s.revokeFailedGrant(r.Context(), grantID)
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "the grant predates the last password change; authorize again")
		return
	}
	s.writeGrantResponse(w, r, endpoints, oauthGrant{clientID: request.ClientID, user: user, scopes: ParseScopeList(stored.Scope), issuedAt: stored.CreatedAt}, grantID, refreshToken)
}

// createGrant stores the first grant row of a connection and returns its id and
// the plaintext refresh token. The expiry is measured from now, and created_at
// is the authorization time the row carries forward across every rotation.
func (s *Server) createGrant(ctx context.Context, grant oauthGrant, resource string) (int64, string, error) {
	token, err := auth.GenerateRefreshToken()
	if err != nil {
		return 0, "", err
	}
	id, err := s.config.tokenStore().CreateOAuthGrant(ctx, &store.OAuthGrant{
		TokenHash: auth.HashToken(token),
		ClientID:  grant.clientID,
		UserID:    grant.user.ID,
		Resource:  resource,
		Scope:     grantedScope(grant.scopes),
		ExpiresAt: s.now().Add(auth.GetRefreshTokenDuration(ctx, s.config.Stores)),
		CreatedAt: grant.issuedAt,
	})
	if err != nil {
		return 0, "", err
	}
	return id, token, nil
}

// rotateGrant mints a replacement refresh token and claims the stored one in one
// atomic statement. A false result means another caller claimed it, the grant was
// revoked, or it expired; the minted token is then simply discarded.
func (s *Server) rotateGrant(ctx context.Context, clientID, oldHash string, now time.Time) (int64, string, bool, error) {
	token, err := auth.GenerateRefreshToken()
	if err != nil {
		return 0, "", false, err
	}
	expiresAt := now.Add(auth.GetRefreshTokenDuration(ctx, s.config.Stores))
	id, rotated, err := s.config.tokenStore().RotateOAuthGrant(ctx, clientID, oldHash, auth.HashToken(token), expiresAt, now)
	if err != nil || !rotated {
		return 0, "", rotated, err
	}
	return id, token, true, nil
}

// revokeFailedGrant ends a grant whose live principal can no longer use it. The
// current code path used to end it by deleting the consumed row; an in-place
// rotation must say so explicitly, or the row would remain listed as an active
// connection that can never refresh. Best effort: the credential is already
// burned, so a failure here only leaves a stale row for the maintenance prune.
func (s *Server) revokeFailedGrant(ctx context.Context, grantID int64) {
	if _, err := s.config.tokenStore().RevokeOAuthGrant(ctx, grantID, s.now(), nil); err != nil {
		slog.Warn("failed to revoke an unusable OAuth grant", clog.WithError(err))
		return
	}
	// Marked after the write commits, so this replica refuses the connection's
	// tokens immediately instead of serving a live decision for the rest of the
	// cache window.
	if s.config.State != nil {
		s.config.State.GrantRevocationCache.Revoke(strconv.FormatInt(grantID, 10), s.now())
	}
}

// writeGrantResponse mints the access token for a stored grant, records the
// audit row and writes the token response. Both grants converge here, so the two
// cannot drift in what they issue, record or address.
func (s *Server) writeGrantResponse(w http.ResponseWriter, r *http.Request, endpoints Endpoints, grant oauthGrant, grantID int64, refreshToken string) {
	// The ledger records who received a credential, for which client, and which
	// connection it belongs to; the tokens themselves are never part of the row.
	recordAuditActor(r, grant.user.Email)
	recordAuditDetail(r, map[string]any{"clientId": grant.clientID, "resource": endpoints.Resource, "connectedAppId": grantID})

	duration := auth.GetTokenDuration(r.Context(), s.config.Stores)
	scope := grantedScope(grant.scopes)
	accessToken, err := auth.GenerateMCPAccessToken(grant.user.Name, grant.user.ID, grant.clientID, grantID, endpoints.Resource, scope, s.config.Secret, duration)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to issue the token")
		return
	}
	// A client that has exchanged a code is in use; last_used_at is per
	// registration, while the grant's own activity is last_tool_call_at. Best
	// effort: the token is valid whether or not the timestamp landed.
	if err := s.config.tokenStore().TouchOAuthClient(r.Context(), grant.clientID); err != nil {
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

// grantedScope renders the granted scopes the way the token response reports
// them, defaulting to the scope the metadata advertises.
func grantedScope(scopes []string) string {
	if len(scopes) == 0 {
		return MCPReadScope
	}
	return strings.Join(scopes, " ")
}
