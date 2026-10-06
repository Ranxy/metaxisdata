package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRefreshTokensAreStoredAsDigests(t *testing.T) {
	t.Parallel()

	token, err := GenerateRefreshToken()
	require.NoError(t, err)
	require.Len(t, token, refreshTokenLength, "43 base62 characters carry the entropy the token needs")

	digest := HashToken(token)
	require.NotEqual(t, token, digest, "the plaintext must never be what is stored")
	require.Len(t, digest, 64, "SHA-256 hex")
	require.Equal(t, digest, HashToken(token), "a lookup must be deterministic")
	require.NotEqual(t, digest, HashToken(token+"x"), "one changed character is another digest")
}

func TestRefreshTokenCookieIsHttpOnlyAndClearable(t *testing.T) {
	t.Parallel()

	cleared := GetRefreshTokenCookie(context.Background(), nil, "", time.Time{})
	require.Equal(t, RefreshTokenCookieName, cleared.Name)
	require.Empty(t, cleared.Value)
	require.True(t, cleared.Expires.Before(time.Now()), "clearing must expire the cookie")

	expiresAt := time.Now().Add(time.Hour).Truncate(time.Second)
	cookie := GetRefreshTokenCookie(context.Background(), nil, "the-token", expiresAt)
	require.Equal(t, "the-token", cookie.Value)
	require.True(t, cookie.HttpOnly, "page JavaScript must not read the credential")
	require.Equal(t, "/", cookie.Path)
	require.Equal(t, expiresAt, cookie.Expires, "the cookie dies with the session, not before or after it")
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite, "an http deployment gets Lax")
}

func TestGetRefreshTokenFromCookie(t *testing.T) {
	t.Parallel()

	headers := http.Header{}
	headers.Add("Cookie", "other=1; "+RefreshTokenCookieName+"=the-token")
	require.Equal(t, "the-token", GetRefreshTokenFromCookie(headers))

	require.Empty(t, GetRefreshTokenFromCookie(http.Header{}))
	require.Empty(t, GetRefreshTokenFromCookie(http.Header{"Cookie": []string{"other=1"}}))
}
