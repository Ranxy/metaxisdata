package v1

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// setWebRefreshCookie starts a brand-new web session: the token is stored with
// the session's issue time and a fresh absolute deadline, and the cookie carries
// that same deadline.
func (s *AuthService) setWebRefreshCookie(ctx context.Context, responseHeader http.Header, user *store.UserMessage) error {
	now := time.Now()
	expiresAt := now.Add(auth.GetRefreshTokenDuration(ctx, s.store))
	token, err := s.storeWebSession(ctx, user, now, expiresAt)
	if err != nil {
		return err
	}
	responseHeader.Add("Set-Cookie", auth.GetRefreshTokenCookie(ctx, s.store, token, expiresAt).String())
	return nil
}

// storeWebSession mints a refresh token and stores its digest, returning the
// plaintext for the cookie. It is the only place the plaintext exists outside
// the browser's cookie jar.
//
// issuedAt and expiresAt are parameters rather than "now" and "now plus a
// duration" because a rotation carries the original values forward: a session
// has one issue time and one deadline, and refreshing replaces its credential
// without extending either.
func (s *AuthService) storeWebSession(ctx context.Context, user *store.UserMessage, issuedAt, expiresAt time.Time) (string, error) {
	token, err := auth.GenerateRefreshToken()
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to generate a refresh token"))
	}
	if err := s.store.CreateWebRefreshToken(ctx, &store.WebRefreshToken{
		TokenHash: auth.HashToken(token),
		UserID:    user.ID,
		ExpiresAt: expiresAt,
		IssuedAt:  issuedAt,
	}); err != nil {
		return "", connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to store the refresh token"))
	}
	return token, nil
}

// Refresh rotates the web session. The browser calls it when its access token is
// gone or expired, so it deliberately needs no credential of its own: the
// refresh cookie is the credential.
//
// It consumes the presented token atomically before issuing anything, which
// makes rotation single-use; the replacement inherits the session's original
// issue time and deadline, so refreshing cannot extend a session past its
// absolute lifetime. A user who has since been deactivated, or whose password
// changed after the session was established, is refused.
func (s *AuthService) Refresh(ctx context.Context, req *connect.Request[v1pb.RefreshRequest]) (*connect.Response[v1pb.RefreshResponse], error) {
	refreshToken := auth.GetRefreshTokenFromCookie(req.Header())
	if refreshToken == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("refresh token not found"))
	}
	// The atomic delete is the rotation gate: two concurrent refreshes of one
	// session race here and only the caller that deletes the row may issue.
	stored, err := s.store.ConsumeWebRefreshToken(ctx, auth.HashToken(refreshToken))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to consume the refresh token"))
	}
	if stored == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("invalid refresh token"))
	}
	if !time.Now().Before(stored.ExpiresAt) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("refresh token expired"))
	}
	// The principal is re-read on every refresh rather than trusted from the
	// cookie, so a deactivated account loses its session at the next rotation
	// instead of waiting out the refresh token's lifetime.
	user, err := s.store.GetUserByID(ctx, stored.UserID)
	if err != nil || user == nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("the session's user no longer exists"))
	}
	if user.MemberDeleted {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("user has been deactivated by administrators"))
	}
	// A session established before the last password change must not be renewed:
	// that would resurrect exactly the session the change retired. The rotated
	// token carries the original issue time, so the rule holds however often the
	// session is refreshed.
	if last := user.Profile.GetLastChangePasswordTime(); last != nil && auth.TokenPredatesPasswordChange(stored.IssuedAt, last.AsTime()) {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.Errorf("the session predates the last password change"))
	}

	accessToken, err := auth.GenerateAccessToken(user.Name, user.ID, s.profile.Mode, s.secret, auth.GetTokenDuration(ctx, s.store))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Errorf("failed to generate an access token"))
	}
	rotated, err := s.storeWebSession(ctx, user, stored.IssuedAt, stored.ExpiresAt)
	if err != nil {
		return nil, err
	}

	resp := connect.NewResponse(&v1pb.RefreshResponse{})
	resp.Header().Add("Set-Cookie", auth.GetTokenCookie(ctx, s.store, accessToken).String())
	resp.Header().Add("Set-Cookie", auth.GetRefreshTokenCookie(ctx, s.store, rotated, stored.ExpiresAt).String())
	return resp, nil
}
