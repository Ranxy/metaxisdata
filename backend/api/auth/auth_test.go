package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	errs "github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

func TestGetTokenFromHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		headers map[string]string
		want    string
		wantErr bool
	}{
		{name: "bearer", headers: map[string]string{"Authorization": "Bearer abc"}, want: "abc"},
		{name: "bearer is case insensitive", headers: map[string]string{"Authorization": "bearer abc"}, want: "abc"},
		{name: "extra whitespace", headers: map[string]string{"Authorization": "Bearer   abc"}, want: "abc"},
		{name: "missing token", headers: map[string]string{"Authorization": "Bearer"}, wantErr: true},
		{name: "wrong scheme", headers: map[string]string{"Authorization": "Basic abc"}, wantErr: true},
		{name: "no credentials", headers: map[string]string{}, want: ""},
		{name: "cookie fallback", headers: map[string]string{"Cookie": "access-token=xyz"}, want: "xyz"},
		{name: "authorization wins over cookie", headers: map[string]string{"Authorization": "Bearer abc", "Cookie": "access-token=xyz"}, want: "abc"},
		{name: "unrelated cookie is ignored", headers: map[string]string{"Cookie": "other=1"}, want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			headers := http.Header{}
			for k, v := range tc.headers {
				headers.Set(k, v)
			}
			got, err := GetTokenFromHeaders(headers)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestAudienceContains(t *testing.T) {
	t.Parallel()

	audience := jwt.ClaimStrings{"mt.user.access.dev", "other"}
	require.True(t, audienceContains(audience, "mt.user.access.dev"))
	require.False(t, audienceContains(audience, "mt.user.access.prod"))
	require.False(t, audienceContains(audience, "MT.USER.ACCESS.DEV"))
	require.False(t, audienceContains(nil, "mt.user.access.dev"))
}

// Token parsing must reject anything that is not an HS256 token signed with the
// configured key, issued by this server, carrying the right audience and an
// expiry. The signing key used to be a hard-coded constant, so these checks are
// the other half of that fix.
func TestGeneratedTokensRoundTrip(t *testing.T) {
	t.Parallel()

	const secret = "0123456789abcdef0123456789abcdef"
	audience := "mt.user.access.dev"

	token, err := GenerateAccessToken("alice@example.com", 101, common.ReleaseModeDev, secret, time.Hour)
	require.NoError(t, err)

	claims := &claimsMessage{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(token *jwt.Token) (any, error) {
		if token.Header["kid"] != keyID {
			return nil, errs.Errorf("unexpected kid %v", token.Header["kid"])
		}
		return []byte(secret), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
	)
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	require.Equal(t, "alice@example.com", claims.Name)
	require.Equal(t, "101", claims.Subject)
	require.True(t, audienceContains(claims.Audience, audience))

	t.Run("wrong key is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := jwt.ParseWithClaims(token, &claimsMessage{}, func(*jwt.Token) (any, error) {
			return []byte("another-key-another-key-another"), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
		require.Error(t, err)
	})

	t.Run("wrong audience is rejected", func(t *testing.T) {
		t.Parallel()
		prodToken, err := GenerateAccessToken("alice@example.com", 101, common.ReleaseModeProd, secret, time.Hour)
		require.NoError(t, err)
		prodClaims := &claimsMessage{}
		_, err = jwt.ParseWithClaims(prodToken, prodClaims, func(*jwt.Token) (any, error) {
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
		require.NoError(t, err)
		require.False(t, audienceContains(prodClaims.Audience, audience))
	})

	t.Run("api token expires within the hour", func(t *testing.T) {
		t.Parallel()
		apiToken, err := GenerateAPIToken("bot", 1, common.ReleaseModeDev, secret)
		require.NoError(t, err)
		apiClaims := &claimsMessage{}
		_, err = jwt.ParseWithClaims(apiToken, apiClaims, func(*jwt.Token) (any, error) {
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
		require.NoError(t, err)
		require.WithinDuration(t, time.Now().Add(apiTokenDuration), apiClaims.ExpiresAt.Time, time.Minute)
	})
}

// VerifyAccessToken is what both the auth interceptor and Logout rely on. Logout
// in particular must reject a token it cannot verify, otherwise an
// unauthenticated caller could flood the revocation cache and evict genuine
// entries.
func TestVerifyAccessToken(t *testing.T) {
	t.Parallel()

	const secret = "0123456789abcdef0123456789abcdef"

	t.Run("valid token yields the identity", func(t *testing.T) {
		t.Parallel()
		token, err := GenerateAccessToken("alice@example.com", 101, common.ReleaseModeDev, secret, time.Hour)
		require.NoError(t, err)
		identity, err := VerifyAccessToken(token, secret, common.ReleaseModeDev)
		require.NoError(t, err)
		require.Equal(t, 101, identity.UserID)
		require.WithinDuration(t, time.Now(), identity.IssuedAt, time.Minute)
	})

	t.Run("wrong key is rejected", func(t *testing.T) {
		t.Parallel()
		token, err := GenerateAccessToken("alice@example.com", 101, common.ReleaseModeDev, secret, time.Hour)
		require.NoError(t, err)
		_, err = VerifyAccessToken(token, "another-key-another-key-another", common.ReleaseModeDev)
		require.Error(t, err)
	})

	t.Run("wrong audience is rejected", func(t *testing.T) {
		t.Parallel()
		token, err := GenerateAccessToken("alice@example.com", 101, common.ReleaseModeProd, secret, time.Hour)
		require.NoError(t, err)
		_, err = VerifyAccessToken(token, secret, common.ReleaseModeDev)
		require.Error(t, err)
	})

	t.Run("expired token is rejected", func(t *testing.T) {
		t.Parallel()
		token, err := GenerateAccessToken("alice@example.com", 101, common.ReleaseModeDev, secret, -time.Minute)
		require.NoError(t, err)
		_, err = VerifyAccessToken(token, secret, common.ReleaseModeDev)
		require.ErrorIs(t, err, jwt.ErrTokenExpired)
	})

	t.Run("unsigned token is rejected", func(t *testing.T) {
		t.Parallel()
		unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, &claimsMessage{
			RegisteredClaims: jwt.RegisteredClaims{
				Audience:  jwt.ClaimStrings{"mt.user.access.dev"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				Issuer:    issuer,
				Subject:   "101",
			},
		})
		tokenString, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
		require.NoError(t, err)
		_, err = VerifyAccessToken(tokenString, secret, common.ReleaseModeDev)
		require.Error(t, err)
	})

	// The iat claim has second granularity, so without a per-token id two
	// logins in the same second yield the same string and Logout cannot tell
	// the sessions apart.
	t.Run("tokens minted together are distinct", func(t *testing.T) {
		t.Parallel()
		first, err := GenerateAccessToken("alice@example.com", 101, common.ReleaseModeDev, secret, time.Hour)
		require.NoError(t, err)
		second, err := GenerateAccessToken("alice@example.com", 101, common.ReleaseModeDev, secret, time.Hour)
		require.NoError(t, err)
		require.NotEqual(t, first, second)
	})
}

func TestTokenPredatesPasswordChange(t *testing.T) {
	t.Parallel()

	now := time.Now()
	require.False(t, tokenPredatesPasswordChange(time.Time{}, now), "tokens without an iat claim are not judged")
	require.False(t, tokenPredatesPasswordChange(now, time.Time{}), "users who never changed their password are not judged")
	require.True(t, tokenPredatesPasswordChange(now.Add(-time.Hour), now))
	require.False(t, tokenPredatesPasswordChange(now.Add(time.Hour), now))
	// The iat claim carries sub-second precision, so the ordering around the
	// change instant is exact.
	require.True(t, tokenPredatesPasswordChange(now.Add(-time.Millisecond), now))
	require.False(t, tokenPredatesPasswordChange(now.Add(time.Millisecond), now))
	require.False(t, tokenPredatesPasswordChange(now, now))
}

func TestIsAuthenticationAllowed(t *testing.T) {
	t.Parallel()

	allow := &common.AuthContext{AllowWithoutCredential: true}
	deny := &common.AuthContext{}

	// Reflection handlers are registered without the auth interceptor, so this
	// check never sees them; the prefix must not be blanket-exempted here.
	require.False(t, IsAuthenticationAllowed("/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", deny))
	require.True(t, IsAuthenticationAllowed("/metaxisdata.v1.AuthService/Authenticate", allow))
	require.False(t, IsAuthenticationAllowed("/metaxisdata.v1.UserService/ListUsers", deny))
}

// The permission/audit/allow_without_credential method options are the only
// input to the authorization and audit interceptors, so read them back through
// the registered descriptors.
func TestGetAuthContextReadsMethodOptions(t *testing.T) {
	t.Parallel()

	t.Run("write method carries a permission", func(t *testing.T) {
		t.Parallel()
		authCtx, err := getAuthContext("/metaxisdata.v1.InstanceService/UpdateInstance")
		require.NoError(t, err)
		require.Equal(t, "metaxisdata.instances.update", authCtx.Permission)
		require.False(t, authCtx.AllowWithoutCredential)
	})

	t.Run("login is public and audited", func(t *testing.T) {
		t.Parallel()
		authCtx, err := getAuthContext("/metaxisdata.v1.UserService/CreateUser")
		require.NoError(t, err)
		require.True(t, authCtx.AllowWithoutCredential)
		require.True(t, authCtx.Audit)
		require.Empty(t, authCtx.Permission)
	})

	t.Run("get current user requires credentials", func(t *testing.T) {
		t.Parallel()
		// It used to claim allow_without_credential while returning
		// Unauthenticated for every anonymous call.
		authCtx, err := getAuthContext("/metaxisdata.v1.UserService/GetCurrentUser")
		require.NoError(t, err)
		require.False(t, authCtx.AllowWithoutCredential)
	})

	t.Run("malformed names are rejected", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"", "ListUsers", "/metaxisdata.v1.UserService/NoSuchMethod", "/metaxisdata.v1.NoSuchService/ListUsers", "a/b/c/d"} {
			_, err := getAuthContext(name)
			require.Errorf(t, err, "expected %q to be rejected", name)
		}
	})
}

func TestGetTokenCookie(t *testing.T) {
	t.Parallel()

	t.Run("empty token expires the cookie", func(t *testing.T) {
		t.Parallel()
		cookie := GetTokenCookie(context.Background(), nil, "")
		require.Equal(t, AccessTokenCookieName, cookie.Name)
		require.Empty(t, cookie.Value)
		require.True(t, cookie.Expires.Before(time.Now()))
	})

	t.Run("an unconfigured deployment gets SameSite lax", func(t *testing.T) {
		t.Parallel()
		cookie := GetTokenCookie(context.Background(), nil, "token")
		require.False(t, cookie.Secure)
		require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
		require.True(t, cookie.HttpOnly)
	})
}

// The cookie's Secure/SameSite policy used to come from the client-controlled
// Origin header, so a caller could ask for SameSite=None from plain http.
func TestCookieSecurityComesFromTheServerSideURL(t *testing.T) {
	t.Parallel()

	secure, sameSite := cookieSecurityForExternalURL("")
	require.False(t, secure)
	require.Equal(t, http.SameSiteLaxMode, sameSite)

	secure, sameSite = cookieSecurityForExternalURL("http://metaxis.example.com")
	require.False(t, secure)
	require.Equal(t, http.SameSiteLaxMode, sameSite)

	// A cross-origin SPA needs SameSite=None, which only a deliberately
	// configured https deployment may select.
	secure, sameSite = cookieSecurityForExternalURL("https://metaxis.example.com")
	require.True(t, secure)
	require.Equal(t, http.SameSiteNoneMode, sameSite)
}

func TestGatewayResponseModifierCopiesSetCookie(t *testing.T) {
	t.Parallel()

	ctx := runtime.NewServerMetadataContext(context.Background(), runtime.ServerMetadata{
		// gRPC metadata keys are lowercased, and MD.Get lowercases its argument.
		HeaderMD: map[string][]string{"set-cookie": {"access-token=abc; Path=/"}},
	})
	recorder := httptest.NewRecorder()

	require.NoError(t, (&GatewayResponseModifier{}).Modify(ctx, recorder, nil))
	require.Equal(t, "access-token=abc; Path=/", recorder.Header().Get("Set-Cookie"))

	// Without gateway metadata it must fail rather than silently dropping the
	// cookies.
	require.Error(t, (&GatewayResponseModifier{}).Modify(context.Background(), recorder, nil))
}

func TestRestrictedAccessToken(t *testing.T) {
	t.Parallel()

	const secret = "test-secret-test-secret-test-secret"

	t.Run("restriction survives a round trip", func(t *testing.T) {
		t.Parallel()
		token, err := GenerateRestrictedAccessToken("alice@example.com", 101, common.ReleaseModeDev, secret, time.Hour, TokenRestrictionResetPassword)
		require.NoError(t, err)

		identity, err := VerifyAccessToken(token, secret, common.ReleaseModeDev)
		require.NoError(t, err)
		require.Equal(t, 101, identity.UserID)
		require.Equal(t, TokenRestrictionResetPassword, identity.Restriction)
	})

	t.Run("a full-access token carries no restriction", func(t *testing.T) {
		t.Parallel()
		token, err := GenerateAccessToken("alice@example.com", 101, common.ReleaseModeDev, secret, time.Hour)
		require.NoError(t, err)

		identity, err := VerifyAccessToken(token, secret, common.ReleaseModeDev)
		require.NoError(t, err)
		require.Empty(t, identity.Restriction)
	})

	t.Run("only the password reset RPCs are reachable", func(t *testing.T) {
		t.Parallel()
		require.True(t, TokenRestrictionResetPassword.allows("/metaxisdata.v1.UserService/UpdateUser"))
		require.True(t, TokenRestrictionResetPassword.allows("/metaxisdata.v1.AuthService/Logout"))
		require.False(t, TokenRestrictionResetPassword.allows("/metaxisdata.v1.DatabaseService/ListDatabases"))
		require.False(t, TokenRestrictionResetPassword.allows("/metaxisdata.v1.UserService/DeleteUser"))
	})

	t.Run("an unrestricted token reaches everything", func(t *testing.T) {
		t.Parallel()
		require.True(t, TokenRestriction("").allows("/metaxisdata.v1.UserService/DeleteUser"))
	})

	t.Run("an unknown restriction reaches nothing", func(t *testing.T) {
		t.Parallel()
		require.False(t, TokenRestriction("no-such-restriction").allows("/metaxisdata.v1.AuthService/Logout"))
	})
}
