package oauth

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
)

// RevokeHandler serves POST /oauth/revoke (RFC 7009).
//
// Revoking either credential ends the whole connection: an access token and its
// refresh token are two halves of one grant, and the grant row is what the
// connected-app page addresses and what the access-token liveness check reads.
// The answer is the same empty 200 whether the token was revoked, had already
// been revoked, was unknown, had expired or was forged — anything else would make
// the endpoint a token oracle. token_type_hint is accepted and ignored because
// the server detects the credential's kind itself, and a client_id that does not
// match the credential's owner is a no-op for the same reason.
func (s *Server) RevokeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpoints, ok := s.endpoints(w, r)
		if !ok {
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "the revocation endpoint is a POST")
			return
		}
		if err := r.ParseForm(); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "the body must be an application/x-www-form-urlencoded form")
			return
		}
		token := r.PostForm.Get("token")
		if token == "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "token is required")
			return
		}
		if err := s.revokeToken(r, endpoints, token, r.PostForm.Get("client_id")); err != nil {
			slog.Error("failed to revoke an OAuth token", clog.WithError(err))
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to revoke the token")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
	})
}

// revokeTarget is the connection a presented credential belongs to.
type revokeTarget struct {
	grantID  int64
	userID   int
	clientID string
}

// revokeToken ends the connection behind the presented credential. A zero target
// is a successful no-op, and its cause is deliberately not reported.
func (s *Server) revokeToken(r *http.Request, endpoints Endpoints, token, clientID string) error {
	target, err := s.revokeTargetFor(r.Context(), endpoints, token, clientID)
	if err != nil {
		return err
	}
	if target.grantID == 0 {
		return nil
	}
	// A client revoking its own credential records the credential's user, so the
	// connected-app page and the ledger say who ended the connection.
	revokedBy := target.userID
	// A false result means the row was already revoked, or pruned between the
	// token's minting and this call. Either way the connection is not usable, so
	// the answer stays the same empty success; the cache mark below still records
	// that this replica must not keep serving a live decision for it.
	if _, err := s.config.tokenStore().RevokeOAuthGrant(r.Context(), target.grantID, s.now(), &revokedBy); err != nil {
		return err
	}
	// Marked after the write, so this replica refuses the connection's access
	// tokens immediately instead of at the end of the liveness cache window.
	if s.config.State != nil {
		s.config.State.GrantRevocationCache.Revoke(strconv.FormatInt(target.grantID, 10), s.now())
	}
	// The ledger names who revoked which connection; the token itself is never
	// written to it.
	if user, err := s.config.tokenStore().GetUserByID(r.Context(), target.userID); err == nil && user != nil {
		recordAuditActor(r, user.Email)
	}
	recordAuditDetail(r, map[string]any{"clientId": target.clientID, "connectedAppId": target.grantID})
	return nil
}

// revokeTargetFor resolves the connection a presented credential belongs to. An
// MCP access token is verified locally, so that path needs no database read;
// anything else is tried as a refresh token digest.
func (s *Server) revokeTargetFor(ctx context.Context, endpoints Endpoints, token, clientID string) (revokeTarget, error) {
	if identity, err := auth.VerifyAccessTokenFor(token, s.config.Secret, endpoints.Resource); err == nil {
		// A token minted before access tokens named their grant has no connection
		// to revoke; it is refused at /mcp and recovers with one refresh anyway.
		if identity.GrantID == 0 || (clientID != "" && clientID != identity.ClientID) {
			return revokeTarget{}, nil
		}
		return revokeTarget{grantID: identity.GrantID, userID: identity.UserID, clientID: identity.ClientID}, nil
	}
	grant, err := s.config.tokenStore().GetOAuthGrantByTokenHash(ctx, auth.HashToken(token))
	if err != nil {
		return revokeTarget{}, err
	}
	if grant == nil || (clientID != "" && clientID != grant.ClientID) {
		return revokeTarget{}, nil
	}
	return revokeTarget{grantID: grant.ID, userID: grant.UserID, clientID: grant.ClientID}, nil
}
