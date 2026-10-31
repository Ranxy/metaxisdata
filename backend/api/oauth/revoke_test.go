package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// postRevoke drives the revocation handler with one form body.
func postRevoke(t *testing.T, server *Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	server.RevokeHandler().ServeHTTP(recorder, request)
	return recorder
}

// revokeForm is the request a client sends to end one connection.
func revokeForm(token, clientID string) url.Values {
	form := url.Values{"token": {token}}
	if clientID != "" {
		form.Set("client_id", clientID)
	}
	return form
}

// mcpAccessToken mints the access token a client would hold after the token
// exchange, so the revocation handler can be exercised without a database. The
// credential belongs to testUserID, the user every revocation test acts as.
func mcpAccessToken(t *testing.T, clientID string, grantID int64, lifetime time.Duration) string {
	t.Helper()

	token, err := auth.GenerateMCPAccessToken("user@example.com", testUserID, clientID, grantID, testResource, MCPReadScope, tokenTestSecret, lifetime)
	require.NoError(t, err)
	return token
}

// testUserID is the signed-in user the revocation tests act as.
const testUserID = 7

func TestRevokeEndpointRejectsBadRequests(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{Endpoints: testEndpoints})

	t.Run("a GET is refused", func(t *testing.T) {
		t.Parallel()

		recorder := httptest.NewRecorder()
		server.RevokeHandler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/oauth/revoke", nil))
		require.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
		require.Equal(t, http.MethodPost, recorder.Header().Get("Allow"))
	})

	t.Run("a missing token is an invalid request", func(t *testing.T) {
		t.Parallel()

		recorder := postRevoke(t, server, url.Values{"client_id": {"client-1"}})
		require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "invalid_request")
	})

	t.Run("a malformed form is an invalid request", func(t *testing.T) {
		t.Parallel()

		request := httptest.NewRequest(http.MethodPost, "/oauth/revoke", strings.NewReader("token=%zz"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		server.RevokeHandler().ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "invalid_request")
	})

	t.Run("a switched-off surface does not exist", func(t *testing.T) {
		t.Parallel()

		disabled := NewServer(ServerConfig{Endpoints: func(context.Context) (Endpoints, bool, error) {
			return Endpoints{}, false, nil
		}})
		recorder := postRevoke(t, disabled, revokeForm("some-token", ""))
		require.Equal(t, http.StatusNotFound, recorder.Code)
	})

	t.Run("a misconfigured surface is unavailable", func(t *testing.T) {
		t.Parallel()

		unavailable := NewServer(ServerConfig{Endpoints: func(context.Context) (Endpoints, bool, error) {
			return Endpoints{}, false, errors.New("external_url is missing")
		}})
		recorder := postRevoke(t, unavailable, revokeForm("some-token", ""))
		require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	})
}

func TestRevokeRefreshTokenEndsTheConnection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{
		byTokenHash: &store.OAuthGrant{ID: 42, TokenHash: auth.HashToken("refresh-1"), ClientID: "client-1", UserID: 7},
		user:        &store.UserMessage{ID: 7, Name: "user@example.com", Email: "user@example.com"},
	}
	server, stateCfg := newTokenTestServer(t, fake, now)

	recorder := postRevoke(t, server, revokeForm("refresh-1", "client-1"))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Empty(t, recorder.Body.String(), "RFC 7009: a successful revocation has an empty body")
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))

	// The presented value is looked up by digest, exactly as it is stored.
	require.Equal(t, auth.HashToken("refresh-1"), fake.lastHashLookup)
	require.Equal(t, []int64{42}, fake.revokedIDs)
	require.Equal(t, 7, *fake.revokedBys[0], "self-service revocation records the credential's user")
	revoked, fresh := stateCfg.GrantRevocationCache.Lookup("42", now)
	require.True(t, fresh)
	require.True(t, revoked, "the replica that revoked must refuse the connection's access tokens immediately")
}

func TestRevokeAccessTokenEndsTheConnection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{user: &store.UserMessage{ID: 7, Name: "user@example.com", Email: "user@example.com"}}
	server, stateCfg := newTokenTestServer(t, fake, now)

	token := mcpAccessToken(t, "client-1", 42, auth.MCPAccessTokenDuration)
	recorder := postRevoke(t, server, revokeForm(token, "client-1"))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Empty(t, recorder.Body.String())

	require.Equal(t, []int64{42}, fake.revokedIDs, "the token's gid names the connection to end")
	require.Equal(t, 7, *fake.revokedBys[0])
	require.Zero(t, fake.byTokenHashCalls, "a locally verified access token needs no refresh-digest lookup")
	revoked, _ := stateCfg.GrantRevocationCache.Lookup("42", now)
	require.True(t, revoked)
}

func TestRevokeIsANoOpForAnotherClientsToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("access token", func(t *testing.T) {
		t.Parallel()

		fake := &fakeTokenStore{}
		server, stateCfg := newTokenTestServer(t, fake, now)

		// The token belongs to client-2, the request names client-1: the mismatch
		// is symmetric, and neither client's connection may be touched.
		recorder := postRevoke(t, server, revokeForm(mcpAccessToken(t, "client-2", 42, auth.MCPAccessTokenDuration), "client-1"))
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Empty(t, fake.revokedIDs)

		_, fresh := stateCfg.GrantRevocationCache.Lookup("42", now)
		require.False(t, fresh, "a client_id mismatch must not mark another client's connection")
	})

	t.Run("refresh token", func(t *testing.T) {
		t.Parallel()

		fake := &fakeTokenStore{
			byTokenHash: &store.OAuthGrant{ID: 42, TokenHash: auth.HashToken("refresh-1"), ClientID: "client-1", UserID: 7},
		}
		server, stateCfg := newTokenTestServer(t, fake, now)

		recorder := postRevoke(t, server, revokeForm("refresh-1", "client-2"))
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Empty(t, fake.revokedIDs)

		_, fresh := stateCfg.GrantRevocationCache.Lookup("42", now)
		require.False(t, fresh)
	})
}

// TestRevokeUnknownOrForeignTokensIsANoOp covers the tokens the endpoint must
// answer 200 to without writing anything: anything else would let a caller probe
// which token strings were once valid.
func TestRevokeUnknownOrForeignTokensIsANoOp(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	webToken, err := auth.GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, tokenTestSecret, time.Hour)
	require.NoError(t, err)

	tests := []struct {
		name  string
		token string
	}{
		{name: "unknown token", token: "not-a-token-any-client-ever-saw"},
		{name: "web or CLI audience token", token: webToken},
		{name: "expired MCP access token", token: mcpAccessToken(t, "client-1", 42, -time.Minute)},
		{
			name:  "an MCP access token minted before access tokens named their grant",
			token: mcpAccessToken(t, "client-1", 0, auth.MCPAccessTokenDuration),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeTokenStore{}
			server, _ := newTokenTestServer(t, fake, now)

			recorder := postRevoke(t, server, revokeForm(tc.token, ""))
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Empty(t, recorder.Body.String())
			require.Empty(t, fake.revokedIDs)
		})
	}
}

func TestRevokeAlreadyRevokedTokenStillSucceeds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	fake := &fakeTokenStore{
		byTokenHash:    &store.OAuthGrant{ID: 42, ClientID: "client-1", UserID: 7},
		alreadyRevoked: true,
	}
	server, stateCfg := newTokenTestServer(t, fake, now)

	recorder := postRevoke(t, server, revokeForm("refresh-1", "client-1"))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Empty(t, recorder.Body.String())

	// A false result is the same outcome for the caller, and the cache is still
	// marked so this replica does not keep serving a live decision for the row.
	revoked, fresh := stateCfg.GrantRevocationCache.Lookup("42", now)
	require.True(t, fresh)
	require.True(t, revoked)
}

func TestRevokeStoreFailureIsAnError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	t.Run("lookup failure", func(t *testing.T) {
		t.Parallel()

		fake := &fakeTokenStore{byTokenHashErr: errors.New("store unavailable")}
		server, _ := newTokenTestServer(t, fake, now)

		recorder := postRevoke(t, server, revokeForm("refresh-1", ""))
		require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
		require.Contains(t, recorder.Body.String(), "server_error")
		require.Empty(t, fake.revokedIDs)
	})

	t.Run("revocation write failure", func(t *testing.T) {
		t.Parallel()

		fake := &fakeTokenStore{
			byTokenHash: &store.OAuthGrant{ID: 42, ClientID: "client-1", UserID: 7},
			revokeErr:   errors.New("store unavailable"),
		}
		server, stateCfg := newTokenTestServer(t, fake, now)

		recorder := postRevoke(t, server, revokeForm("refresh-1", "client-1"))
		require.Equal(t, http.StatusInternalServerError, recorder.Code, recorder.Body.String())
		_, fresh := stateCfg.GrantRevocationCache.Lookup("42", now)
		require.False(t, fresh, "a failed write must not claim the connection is revoked")
	})
}
