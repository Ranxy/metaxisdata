package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// refreshTokenLength is the number of characters in an opaque refresh token.
// The generator's alphabet carries 62 symbols, so 43 characters hold more than
// 256 bits, which is what keeps a token unguessable. The same length the device
// login and OAuth authorization codes use.
const refreshTokenLength = 43

// GenerateRefreshToken mints an opaque refresh token. The value is the
// credential itself; only its digest is ever stored.
func GenerateRefreshToken() (string, error) {
	return common.RandomString(refreshTokenLength)
}

// HashToken returns the SHA-256 hex digest a refresh token is stored and looked
// up by. A digest is enough because the token is a high-entropy random string,
// not a password: there is nothing to brute force and no need for a slow KDF.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// GetRefreshTokenDuration is how long a newly issued refresh token stays usable.
func GetRefreshTokenDuration(_ context.Context, _ *store.Store) time.Duration {
	return DefaultRefreshTokenDuration
}

// GetRefreshTokenCookie builds the web session's refresh cookie. Passing
// token="" clears it, which is what Logout sets.
//
// The cookie is HttpOnly and Secure/SameSite follow the same server-side
// external-URL rule as the access-token cookie, so page JavaScript can never
// read the credential and a cross-site request cannot make the browser send it.
// expiresAt is the session's absolute expiry, not "now plus a duration": a
// rotated token inherits the original deadline, so the cookie must not outlive
// the session it stands for.
func GetRefreshTokenCookie(ctx context.Context, stores *store.Store, token string, expiresAt time.Time) *http.Cookie {
	if token == "" {
		return &http.Cookie{
			Name:    RefreshTokenCookieName,
			Value:   "",
			Expires: time.Unix(0, 0),
			Path:    "/",
		}
	}
	secure, sameSite := cookieSecurity(ctx, stores)
	return &http.Cookie{
		Name:     RefreshTokenCookieName,
		Value:    token,
		Expires:  expiresAt,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	}
}

// GetRefreshTokenFromCookie reads the refresh token a browser sent. A request
// with no such cookie returns an empty string; the caller decides whether that
// is a refusal.
func GetRefreshTokenFromCookie(headers http.Header) string {
	for _, cookieHeader := range headers.Values("Cookie") {
		header := http.Header{}
		header.Add("Cookie", cookieHeader)
		request := http.Request{Header: header}
		if cookie, _ := request.Cookie(RefreshTokenCookieName); cookie != nil {
			return cookie.Value
		}
	}
	return ""
}
