package oauth

import (
	"context"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// CLIClientID is the built-in OAuth client a CLI device login authorizes as.
//
// The CLI is not dynamically registered: its identifier is fixed, so a user's CLI
// sessions are recognizable connected apps rather than one anonymous registration
// per install, and it has no redirect URIs because the device login — not the
// browser authorization-code flow — is its ceremony. The client row exists only so
// the connected-apps list can name the connection; the grant row is the app.
const (
	CLIClientID   = "mxd"
	CLIClientName = "Metaxisdata CLI"
)

// ErrGrantNotRefreshable is every refusal decided from the stored row alone: the
// token is unknown, already consumed by a concurrent refresh, issued to another
// client, revoked, expired or of another kind. They are one sentinel because the
// client's recovery is the same and telling them apart would only let a caller
// probe which token hashes exist.
var ErrGrantNotRefreshable = errors.New("the refresh token is unknown, already used, revoked or expired")

// EnsureCLIClient makes sure the built-in client the CLI sessions point at exists.
// It is idempotent, so the device-login exchange can call it before every grant
// without a startup migration that seeds data.
func EnsureCLIClient(ctx context.Context, stores *store.Store) error {
	return stores.EnsureOAuthClient(ctx, &store.OAuthClient{
		ClientID:                CLIClientID,
		ClientName:              CLIClientName,
		RedirectURIs:            []string{},
		TokenEndpointAuthMethod: "none",
	})
}

// CreateCLIGrant opens one CLI session: a connected app whose refresh credential
// is the pair's rotating half. The resource and scope are empty because the
// session targets the user API, not the MCP endpoint (its access token carries the
// user API audience), and the device facts are kept only for display on the
// management page.
func CreateCLIGrant(ctx context.Context, tokenStore TokenStore, user *store.UserMessage, now time.Time, ttl time.Duration, clientVersion, requestIP string) (int64, string, time.Time, error) {
	token, err := auth.GenerateRefreshToken()
	if err != nil {
		return 0, "", time.Time{}, err
	}
	expiresAt := now.Add(ttl)
	id, err := tokenStore.CreateOAuthGrant(ctx, &store.OAuthGrant{
		TokenHash:     auth.HashToken(token),
		ClientID:      CLIClientID,
		UserID:        user.ID,
		Kind:          store.OAuthGrantKindCLI,
		ExpiresAt:     expiresAt,
		CreatedAt:     now,
		ClientVersion: clientVersion,
		RequestIP:     requestIP,
	})
	if err != nil {
		return 0, "", time.Time{}, err
	}
	return id, token, expiresAt, nil
}

// RotateCLIGrant consumes a CLI session's refresh token and returns the rotated
// pair's refresh half with the principal it belongs to.
//
// The checks that only the stored row can decide run before the rotation, so a
// request the client can correct does not burn the credential; the checks that
// need the live principal run after, because a refusal there must end the
// connection. That is the same order the MCP refresh path uses, and the two share
// the rotation and principal-recheck helpers so they cannot drift.
func RotateCLIGrant(ctx context.Context, tokenStore TokenStore, stateCfg *state.State, refreshToken string, now time.Time, ttl time.Duration) (int64, *store.UserMessage, string, time.Time, error) {
	if refreshToken == "" {
		return 0, nil, "", time.Time{}, ErrGrantNotRefreshable
	}
	tokenHash := auth.HashToken(refreshToken)
	stored, err := tokenStore.GetOAuthGrant(ctx, CLIClientID, tokenHash)
	if err != nil {
		return 0, nil, "", time.Time{}, err
	}
	// The kind is part of the predicate: a grant opened by the OAuth browser flow
	// must not be redeemable here even if its hash somehow reached a CLI.
	if stored == nil || stored.Kind != store.OAuthGrantKindCLI || !now.Before(stored.ExpiresAt) || stored.RevokedAt != nil {
		return 0, nil, "", time.Time{}, ErrGrantNotRefreshable
	}
	// The atomic rotation is the single-use gate: concurrent refreshes of one
	// token race here and only the caller that claims the row may issue.
	grantID, rotatedToken, rotated, expiresAt, err := rotateGrantToken(ctx, tokenStore, CLIClientID, tokenHash, now, ttl)
	if err != nil {
		return 0, nil, "", time.Time{}, err
	}
	if !rotated {
		return 0, nil, "", time.Time{}, ErrGrantNotRefreshable
	}
	user, err := recheckGrantPrincipal(ctx, tokenStore, stateCfg, stored, grantID, now)
	if err != nil {
		return 0, nil, "", time.Time{}, err
	}
	return grantID, user, rotatedToken, expiresAt, nil
}
