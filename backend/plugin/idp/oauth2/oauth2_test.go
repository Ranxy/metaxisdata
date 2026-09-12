package oauth2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestNewIdentityProvider(t *testing.T) {
	tests := []struct {
		name        string
		config      *storepb.OAuth2IdentityProviderConfig
		containsErr string
	}{
		{
			name:        "nil config does not panic",
			config:      nil,
			containsErr: "the oauth2 config is empty",
		},
		{
			name: "nil field mapping does not panic",
			config: &storepb.OAuth2IdentityProviderConfig{
				ClientId:     "test-client-id",
				ClientSecret: "test-client-secret",
				TokenUrl:     "https://example.com/token",
				UserInfoUrl:  "https://example.com/api/user",
			},
			containsErr: `the field "fieldMapping" is empty but required`,
		},
		{
			name: "no tokenUrl",
			config: &storepb.OAuth2IdentityProviderConfig{
				ClientId:     "test-client-id",
				ClientSecret: "test-client-secret",
				AuthUrl:      "",
				TokenUrl:     "",
				UserInfoUrl:  "https://example.com/api/user",
				FieldMapping: &storepb.FieldMapping{
					Identifier: "login",
				},
			},
			containsErr: `the field "tokenUrl" is empty but required`,
		},
		{
			name: "no userInfoUrl",
			config: &storepb.OAuth2IdentityProviderConfig{
				ClientId:     "test-client-id",
				ClientSecret: "test-client-secret",
				AuthUrl:      "",
				TokenUrl:     "https://example.com/token",
				UserInfoUrl:  "",
				FieldMapping: &storepb.FieldMapping{
					Identifier: "login",
				},
			},
			containsErr: `the field "userInfoUrl" is empty but required`,
		},
		{
			name: "no field mapping identifier",
			config: &storepb.OAuth2IdentityProviderConfig{
				ClientId:     "test-client-id",
				ClientSecret: "test-client-secret",
				AuthUrl:      "",
				TokenUrl:     "https://example.com/token",
				UserInfoUrl:  "https://example.com/api/user",
				FieldMapping: &storepb.FieldMapping{
					Identifier: "",
				},
			},
			containsErr: `the field "fieldMapping.identifier" is empty but required`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewIdentityProvider(test.config)
			assert.ErrorContains(t, err, test.containsErr)
		})
	}
}

func newMockServer(t *testing.T, tls bool, code, accessToken string, userinfo []byte) *httptest.Server {
	mux := http.NewServeMux()

	var rawIDToken string
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		vals, err := url.ParseQuery(string(body))
		require.NoError(t, err)

		require.Equal(t, code, vals.Get("code"))
		require.Equal(t, "authorization_code", vals.Get("grant_type"))

		w.Header().Set("Content-Type", "application/json")
		err = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  accessToken,
			"token_type":    "Bearer",
			"refresh_token": "test-refresh-token",
			"expires_in":    3600,
			"id_token":      rawIDToken,
		})
		require.NoError(t, err)
	})
	mux.HandleFunc("/oauth2/userinfo", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write(userinfo)
		require.NoError(t, err)
	})

	var s *httptest.Server
	if tls {
		s = httptest.NewTLSServer(mux)
	} else {
		s = httptest.NewServer(mux)
	}

	return s
}

func TestIdentityProvider(t *testing.T) {
	ctx := context.Background()

	const (
		testClientID    = "test-client-id"
		testCode        = "test-code"
		testAccessToken = "test-access-token"
		testSubject     = "123456789"
		testName        = "John Doe"
		testEmail       = "john.doe@example.com"
	)
	userInfo, err := json.Marshal(
		map[string]any{
			"sub":   testSubject,
			"name":  testName,
			"email": testEmail,
		},
	)
	require.NoError(t, err)

	s := newMockServer(t, false, testCode, testAccessToken, userInfo)

	oauth2, err := NewIdentityProvider(
		&storepb.OAuth2IdentityProviderConfig{
			ClientId:     testClientID,
			ClientSecret: "test-client-secret",
			TokenUrl:     fmt.Sprintf("%s/oauth2/token", s.URL),
			UserInfoUrl:  fmt.Sprintf("%s/oauth2/userinfo", s.URL),
			FieldMapping: &storepb.FieldMapping{
				Identifier:  "sub",
				DisplayName: "name",
			},
		},
	)
	require.NoError(t, err)

	redirectURL := "https://example.com/oauth/callback"
	oauthToken, err := oauth2.ExchangeToken(ctx, redirectURL, testCode, "")
	require.NoError(t, err)
	require.Equal(t, testAccessToken, oauthToken)

	userInfoResult, _, err := oauth2.UserInfo(oauthToken)
	require.NoError(t, err)

	wantUserInfo := &storepb.IdentityProviderUserInfo{
		Identifier:  testSubject,
		DisplayName: testName,
	}
	assert.Equal(t, wantUserInfo, userInfoResult)
}

func TestIdentityProvider_SelfSigned(t *testing.T) {
	ctx := context.Background()

	const (
		testClientID    = "test-client-id"
		testCode        = "test-code"
		testAccessToken = "test-access-token"
		testSubject     = "123456789"
		testName        = "John Doe"
		testEmail       = "john.doe@example.com"
	)
	userInfo, err := json.Marshal(
		map[string]any{
			"sub":   testSubject,
			"name":  testName,
			"email": testEmail,
		},
	)
	require.NoError(t, err)

	t.Run("verify TLS", func(t *testing.T) {
		s := newMockServer(t, true, testCode, testAccessToken, userInfo)
		oauth2, err := NewIdentityProvider(
			&storepb.OAuth2IdentityProviderConfig{
				ClientId:     testClientID,
				ClientSecret: "test-client-secret",
				TokenUrl:     fmt.Sprintf("%s/oauth2/token", s.URL),
				UserInfoUrl:  fmt.Sprintf("%s/oauth2/userinfo", s.URL),
				FieldMapping: &storepb.FieldMapping{
					Identifier:  "sub",
					DisplayName: "name",
				},
			},
		)
		require.NoError(t, err)

		redirectURL := "https://example.com/oauth/callback"
		_, err = oauth2.ExchangeToken(ctx, redirectURL, testCode, "")
		assert.ErrorContains(t, err, "x509: certificate signed by unknown authority")
	})

	t.Run("skip TLS verify", func(t *testing.T) {
		s := newMockServer(t, true, testCode, testAccessToken, userInfo)
		oauth2, err := NewIdentityProvider(
			&storepb.OAuth2IdentityProviderConfig{
				ClientId:     testClientID,
				ClientSecret: "test-client-secret",
				TokenUrl:     fmt.Sprintf("%s/oauth2/token", s.URL),
				UserInfoUrl:  fmt.Sprintf("%s/oauth2/userinfo", s.URL),
				FieldMapping: &storepb.FieldMapping{
					Identifier:  "sub",
					DisplayName: "name",
				},
				SkipTlsVerify: true,
			},
		)
		require.NoError(t, err)

		redirectURL := "https://example.com/oauth/callback"
		oauthToken, err := oauth2.ExchangeToken(ctx, redirectURL, testCode, "")
		require.NoError(t, err)
		require.Equal(t, testAccessToken, oauthToken)

		userInfoResult, _, err := oauth2.UserInfo(oauthToken)
		require.NoError(t, err)

		wantUserInfo := &storepb.IdentityProviderUserInfo{
			Identifier:  testSubject,
			DisplayName: testName,
		}
		assert.Equal(t, wantUserInfo, userInfoResult)
	})
}

// PKCE is optional but when the client used it the verifier must reach the
// token endpoint; otherwise the provider rejects the exchange.
func TestExchangeTokenSendsThePKCEVerifier(t *testing.T) {
	t.Parallel()

	var gotVerifier string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		vals, err := url.ParseQuery(string(body))
		require.NoError(t, err)
		gotVerifier = vals.Get("code_verifier")

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}))
	}))
	defer srv.Close()

	provider, err := NewIdentityProvider(&storepb.OAuth2IdentityProviderConfig{
		ClientId:     "id",
		ClientSecret: "secret",
		TokenUrl:     srv.URL,
		UserInfoUrl:  srv.URL,
		FieldMapping: &storepb.FieldMapping{Identifier: "email"},
	})
	require.NoError(t, err)

	token, err := provider.ExchangeToken(context.Background(), "https://example.com/oauth/callback", "code", "verifier-123")
	require.NoError(t, err)
	require.Equal(t, "token", token)
	require.Equal(t, "verifier-123", gotVerifier)
}
