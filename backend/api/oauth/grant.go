package oauth

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	clog "github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Grant rotation failures that can only be decided by reloading the principal.
// They are sentinels rather than transport errors because the two refresh
// entry points render them differently: the OAuth token endpoint answers RFC 6749
// `invalid_grant`, the ConnectRPC CLI endpoint answers Unauthenticated. The
// decision itself lives here, so the two cannot disagree about a deactivated
// account or a pre-password-change grant.
var (
	// ErrGrantUserMissing means the grant's user no longer exists, or could not be
	// read. The grant is revoked before this is returned.
	ErrGrantUserMissing = errors.New("the grant's user no longer exists")
	// ErrGrantUserDeactivated means the account was soft-deleted. The grant is
	// revoked before this is returned.
	ErrGrantUserDeactivated = errors.New("the grant's user has been deactivated")
	// ErrGrantPredatesPasswordChange means the grant was issued before the user's
	// last password change, so refreshing it would resurrect a retired session. The
	// grant is revoked before this is returned.
	ErrGrantPredatesPasswordChange = errors.New("the grant predates the last password change")
)

// rotateGrantToken mints a replacement refresh token and claims the stored one in
// one atomic statement. A false result means another caller claimed it, the grant
// was revoked, or it expired; the minted token is then simply discarded. The row
// is updated in place, so its id — the connection's stable handle — survives.
func rotateGrantToken(ctx context.Context, tokenStore TokenStore, clientID, oldHash string, now time.Time, ttl time.Duration) (int64, string, bool, time.Time, error) {
	token, err := auth.GenerateRefreshToken()
	if err != nil {
		return 0, "", false, time.Time{}, err
	}
	expiresAt := now.Add(ttl)
	id, rotated, err := tokenStore.RotateOAuthGrant(ctx, clientID, oldHash, auth.HashToken(token), expiresAt, now)
	if err != nil || !rotated {
		return 0, "", rotated, time.Time{}, err
	}
	return id, token, true, expiresAt, nil
}

// recheckGrantPrincipal re-reads the principal after a rotation. The credential is
// already burned by then, so a principal that cannot use it ends the connection
// explicitly: the row is revoked (it is not deleted, so the management page shows
// what ended), and the caller reports the refusal.
//
// A read failure is different: it is not proof that the account is gone, and the
// credential is already burned, so revoking would only destroy a live connection
// (an operator watching the page would see it ended for no reason). The caller
// fails the issuance instead, without touching the row.
func recheckGrantPrincipal(ctx context.Context, tokenStore TokenStore, stateCfg *state.State, stored *store.OAuthGrant, grantID int64, now time.Time) (*store.UserMessage, error) {
	user, err := tokenStore.GetUserByID(ctx, stored.UserID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to re-read the connection's user")
	}
	if user == nil {
		revokeUnusableGrant(ctx, tokenStore, stateCfg, grantID, now)
		return nil, ErrGrantUserMissing
	}
	if user.MemberDeleted {
		revokeUnusableGrant(ctx, tokenStore, stateCfg, grantID, now)
		return nil, ErrGrantUserDeactivated
	}
	// A grant issued before the last password change must not mint a fresh access
	// token: that would resurrect exactly the session the change retired. The
	// comparison uses persisted state, so it holds across replicas, just as the
	// access token's own iat cutoff does.
	if last := user.Profile.GetLastChangePasswordTime(); last != nil && auth.TokenPredatesPasswordChange(stored.CreatedAt, last.AsTime()) {
		revokeUnusableGrant(ctx, tokenStore, stateCfg, grantID, now)
		return nil, ErrGrantPredatesPasswordChange
	}
	return user, nil
}

// revokeUnusableGrant ends a grant whose live principal can no longer use it. The
// consumed row used to be deleted, which ended the connection implicitly; an
// in-place rotation must say so explicitly, or the row would remain listed as an
// active connection that can never refresh. Best effort: the credential is already
// burned, so a failure here only leaves a stale row for the maintenance prune.
func revokeUnusableGrant(ctx context.Context, tokenStore TokenStore, stateCfg *state.State, grantID int64, now time.Time) {
	if _, err := tokenStore.RevokeOAuthGrant(ctx, grantID, now, nil); err != nil {
		slog.Warn("failed to revoke an unusable OAuth grant", clog.WithError(err))
		return
	}
	// Marked after the write commits, so this replica refuses the connection's
	// tokens immediately instead of serving a live decision for the rest of the
	// cache window.
	if stateCfg != nil {
		stateCfg.GrantRevocationCache.Revoke(strconv.FormatInt(grantID, 10), now)
	}
}
