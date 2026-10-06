package auth

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	errs "github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// AccessTokenAudience returns the audience an access token minted for the user
// API must carry in the given release mode.
func AccessTokenAudience(mode common.ReleaseMode) string {
	return fmt.Sprintf(AccessTokenAudienceFmt, mode)
}

// VerifyAccessToken validates an access token's signature, algorithm, issuer,
// audience and expiry. It performs no database lookup, so callers that need the
// principal record must still load it.
func VerifyAccessToken(accessTokenStr, secret string, mode common.ReleaseMode) (*AccessTokenIdentity, error) {
	return VerifyAccessTokenFor(accessTokenStr, secret, AccessTokenAudience(mode))
}

// VerifyAccessTokenFor is VerifyAccessToken with an explicit audience, for a
// resource server whose tokens carry an audience of their own (the MCP endpoint
// does). Audience checking stays inside the verifier on purpose: a caller that
// verified a signature and then forgot the audience comparison would accept a
// token minted for a different resource.
func VerifyAccessTokenFor(accessTokenStr, secret, audience string) (*AccessTokenIdentity, error) {
	claims, err := parseVerifiedClaims(accessTokenStr, secret)
	if err != nil {
		return nil, err
	}
	if !audienceContains(claims.Audience, audience) {
		return nil, errs.Errorf(
			"invalid access token, audience mismatch, got %q, expected %q. you may send request to the wrong environment",
			claims.Audience,
			audience,
		)
	}
	return identityFromClaims(claims)
}

// VerifyAccessTokenProvenance validates that a token was issued by this server —
// signature, key id, issuer and expiry — without requiring a particular audience.
//
// Logout needs it: an MCP token carries the endpoint's resource identifier as its
// audience rather than the user API's, and it must still be revocable, or a leaked
// MCP token has no self-service remedy at all. The signature is what stops a
// caller from flooding the revocation cache with forged strings and evicting
// genuine entries, so requiring an audience here would cost that protection
// nothing and buy the only way to revoke an MCP token.
func VerifyAccessTokenProvenance(accessTokenStr, secret string) (*AccessTokenIdentity, error) {
	claims, err := parseVerifiedClaims(accessTokenStr, secret)
	if err != nil {
		return nil, err
	}
	return identityFromClaims(claims)
}

// parseVerifiedClaims checks everything about a token except its audience.
func parseVerifiedClaims(accessTokenStr, secret string) (*claimsMessage, error) {
	claims := &claimsMessage{}
	if _, err := jwt.ParseWithClaims(accessTokenStr, claims, func(t *jwt.Token) (any, error) {
		if kid, ok := t.Header["kid"].(string); ok {
			if kid == keyID {
				return []byte(secret), nil
			}
		}
		return nil, errs.Errorf("unexpected access token kid=%v", t.Header["kid"])
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
	); err != nil {
		return nil, err
	}
	return claims, nil
}

func identityFromClaims(claims *claimsMessage) (*AccessTokenIdentity, error) {
	principalID, err := strconv.Atoi(claims.Subject)
	if err != nil {
		return nil, errs.Wrapf(err, "malformed ID %s in the access token", claims.Subject)
	}
	identity := &AccessTokenIdentity{
		UserID:      principalID,
		TokenID:     claims.ID,
		Scopes:      strings.Fields(claims.Scope),
		Restriction: TokenRestriction(claims.Restriction),
	}
	if claims.ExpiresAt != nil {
		identity.ExpiresAt = claims.ExpiresAt.Time
	}
	if claims.IssuedAtNanos != 0 {
		identity.IssuedAt = time.Unix(0, claims.IssuedAtNanos)
	} else if claims.IssuedAt != nil {
		identity.IssuedAt = claims.IssuedAt.Time
	}
	return identity, nil
}

// Token failures a TokenAuthenticator reports. They are plain errors rather
// than ConnectRPC errors so that each entry point can map them onto its own
// transport's error shape while applying the same rules.
var (
	// ErrTokenMissing means the request carried no bearer token.
	ErrTokenMissing = errs.New("access token not found")
	// ErrTokenRevoked means the token was revoked, or has expired.
	ErrTokenRevoked = errs.New("access token expired")
	// ErrTokenInvalid means the token did not verify for the expected audience.
	ErrTokenInvalid = errs.New("failed to parse claim")
)

// UserStore is the part of the user store a TokenAuthenticator needs. *store.Store
// implements it; the interface exists so the token rules can be exercised without
// a database.
type UserStore interface {
	GetUserByID(ctx context.Context, id int) (*store.UserMessage, error)
	// IsTokenRevoked consults the persistent revocation records a Logout writes.
	IsTokenRevoked(ctx context.Context, tokenID string) (bool, error)
}

// TokenAuthenticator turns a bearer token into the user it was issued to.
//
// It exists so every entry point applies the same rules in the same order:
// signature, algorithm, issuer, audience and expiry, then the persistent
// revocation record (cached in front of the table), the principal lookup,
// deactivation, and the password-change cutoff. A second entry point that only
// verified the signature would keep accepting tokens for a user who has since
// been deactivated, had their password changed, or logged out.
type TokenAuthenticator struct {
	users    UserStore
	secret   string
	stateCfg *state.State
}

// NewTokenAuthenticator returns an authenticator backed by the given user store.
// stateCfg may be nil, in which case every revocation check reads the store; a
// nil users skips the check entirely (only tests do that).
func NewTokenAuthenticator(users UserStore, secret string, stateCfg *state.State) *TokenAuthenticator {
	return &TokenAuthenticator{users: users, secret: secret, stateCfg: stateCfg}
}

// Resolve validates accessTokenStr for the given audience and returns the
// principal it belongs to. The returned error is one of the sentinel errors
// above, or a descriptive error naming the user the token belongs to.
func (a *TokenAuthenticator) Resolve(ctx context.Context, accessTokenStr, audience string) (*store.UserMessage, *AccessTokenIdentity, error) {
	if accessTokenStr == "" {
		return nil, nil, ErrTokenMissing
	}
	identity, err := VerifyAccessTokenFor(accessTokenStr, a.secret, audience)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, nil, ErrTokenRevoked
		}
		return nil, nil, ErrTokenInvalid
	}
	revoked, err := a.tokenRevoked(ctx, identity.TokenID)
	if err != nil {
		return nil, nil, err
	}
	if revoked {
		return nil, nil, ErrTokenRevoked
	}

	user, err := a.users.GetUserByID(ctx, identity.UserID)
	if err != nil {
		return nil, nil, errs.Errorf("failed to find user ID %d in the access token", identity.UserID)
	}
	if user == nil {
		return nil, nil, errs.Errorf("user ID %d not exists in the access token", identity.UserID)
	}
	if user.MemberDeleted {
		return nil, nil, errs.Errorf("user ID %d has been deactivated by administrators", user.ID)
	}
	// A token minted before the last password change must not survive it. The
	// comparison uses persisted state, so it holds across replicas. Both
	// timestamps come from this process's clock and the iat claim carries
	// sub-second precision, so the ordering is exact.
	if lastChange := user.Profile.GetLastChangePasswordTime(); lastChange != nil {
		if tokenPredatesPasswordChange(identity.IssuedAt, lastChange.AsTime()) {
			return nil, nil, errs.Errorf("access token of user ID %d was issued before the last password change", user.ID)
		}
	}
	return user, identity, nil
}

// tokenRevoked reports whether the token id is in the persistent revocation
// record a Logout writes. The table is the authority: the process-local cache in
// front of it only saves a read, and an eviction or expiry costs one lookup
// rather than an accepted revoked token. A read failure fails closed — a
// revocation table that cannot be consulted must not clear a token.
func (a *TokenAuthenticator) tokenRevoked(ctx context.Context, tokenID string) (bool, error) {
	if tokenID == "" || a.users == nil {
		// A token without a jti cannot be revoked; every token this server signs
		// carries one.
		return false, nil
	}
	now := time.Now()
	if a.stateCfg != nil {
		if revoked, fresh := a.stateCfg.TokenRevocationCache.Lookup(tokenID, now); fresh {
			return revoked, nil
		}
	}
	revoked, err := a.users.IsTokenRevoked(ctx, tokenID)
	if err != nil {
		return false, errs.Wrapf(err, "failed to read the revocation record for token %q", tokenID)
	}
	if a.stateCfg != nil {
		a.stateCfg.TokenRevocationCache.Remember(tokenID, revoked, now)
	}
	return revoked, nil
}
