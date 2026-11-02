package v1

import (
	"context"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/api/oauth"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// ExchangeRefreshToken rotates a CLI session from the refresh token in the body.
//
// It is the CLI's counterpart to the browser's Refresh: the session is the same
// rotating connected-app grant, but the credential arrives in the body because the
// CLI has no cookie jar, and the pair is returned in the body rather than as
// HttpOnly cookies. Every refusal answers Unauthenticated — the recovery is always
// a new device login, and distinguishing "unknown" from "expired" or "revoked"
// would only let a caller probe which token hashes exist.
func (s *AuthService) ExchangeRefreshToken(ctx context.Context, req *connect.Request[v1pb.ExchangeRefreshTokenRequest]) (*connect.Response[v1pb.ExchangeRefreshTokenResponse], error) {
	if req.Msg.GetRefreshToken() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("refresh_token is required"))
	}

	now := time.Now()
	refreshDuration := auth.GetRefreshTokenDuration(ctx, s.store)
	grantID, user, refreshToken, expiresAt, err := oauth.RotateCLIGrant(ctx, s.store, s.stateCfg, req.Msg.GetRefreshToken(), now, refreshDuration)
	if err != nil {
		switch {
		case errors.Is(err, oauth.ErrGrantNotRefreshable),
			errors.Is(err, oauth.ErrGrantUserMissing),
			errors.Is(err, oauth.ErrGrantUserDeactivated),
			errors.Is(err, oauth.ErrGrantPredatesPasswordChange):
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("the session is no longer valid; sign in again"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to rotate the CLI session"))
		}
	}

	tokenDuration := auth.GetTokenDuration(ctx, s.store)
	accessToken, err := auth.GenerateCLIAccessToken(user.Name, user.ID, oauth.CLIClientID, grantID, s.profile.Mode, s.secret, tokenDuration)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("failed to generate an access token"))
	}
	return connect.NewResponse(&v1pb.ExchangeRefreshTokenResponse{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		ExpiresIn:        int64(tokenDuration.Seconds()),
		RefreshExpiresIn: int64(expiresAt.Sub(now).Seconds()),
	}), nil
}
