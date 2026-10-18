package mcp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"

	authpkg "github.com/Ranxy/metaxisdata/backend/api/auth"
	"github.com/Ranxy/metaxisdata/backend/api/oauth"
	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/mcp"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	verifierSecret   = "0123456789abcdef0123456789abcdef"
	verifierResource = "https://mx.example.com/mcp"
	verifierIssuer   = "https://mx.example.com"
	verifierScope    = "metaxisdata.mcp.read"
	// verifierClient is the OAuth registration a minted MCP token names.
	verifierClient = "client-test"
)

// verifierUsers is the UserStore the verifier resolves against, so these tests
// need no database.
type verifierUsers struct{}

func (verifierUsers) GetUserByID(_ context.Context, id int) (*store.UserMessage, error) {
	return &store.UserMessage{ID: id, Email: "user@example.com"}, nil
}

func (verifierUsers) IsTokenRevoked(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func enabledEndpoints() mcp.EndpointsFunc {
	return func(context.Context) (oauth.Endpoints, bool, error) {
		return oauth.Endpoints{Issuer: verifierIssuer, Resource: verifierResource}, true, nil
	}
}

func TestTokenVerifierResolvesAnMCPToken(t *testing.T) {
	t.Parallel()

	token, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, verifierClient, 42, verifierResource, verifierScope, verifierSecret, time.Hour)
	require.NoError(t, err)

	verify := mcp.NewTokenVerifier(enabledEndpoints(), authpkg.NewTokenAuthenticator(verifierUsers{}, nil, verifierSecret, nil))
	info, err := verify(context.Background(), token, &http.Request{})
	require.NoError(t, err)
	require.Equal(t, "7", info.UserID)
	require.Equal(t, []string{verifierScope}, info.Scopes)
	require.WithinDuration(t, time.Now().Add(time.Hour), info.Expiration, time.Minute)

	// The tool layer reaches the principal through the request, because a tool
	// handler with no principal must refuse rather than run unchecked.
	user, ok := mcp.UserFromRequest(&mcpsdk.CallToolRequest{Extra: &mcpsdk.RequestExtra{TokenInfo: info}})
	require.True(t, ok)
	require.Equal(t, 7, user.ID)
}

func TestTokenVerifierRefusesEverythingElse(t *testing.T) {
	t.Parallel()

	authenticator := authpkg.NewTokenAuthenticator(verifierUsers{}, nil, verifierSecret, nil)

	userToken, err := authpkg.GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, verifierSecret, time.Hour)
	require.NoError(t, err)
	otherResourceToken, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, verifierClient, 42, "https://other.example.com/mcp", verifierScope, verifierSecret, time.Hour)
	require.NoError(t, err)
	noScopeToken, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, verifierClient, 42, verifierResource, "", verifierSecret, time.Hour)
	require.NoError(t, err)

	cases := []struct {
		name  string
		token string
	}{
		{name: "a web token", token: userToken},
		{name: "a token for another resource", token: otherResourceToken},
		{name: "a nonsense token", token: "not-a-token"},
		{name: "an empty token", token: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			verify := mcp.NewTokenVerifier(enabledEndpoints(), authenticator)
			_, err := verify(context.Background(), tc.token, &http.Request{})
			require.ErrorIs(t, err, sdkauth.ErrInvalidToken)
		})
	}

	t.Run("a disabled surface refuses even a valid token", func(t *testing.T) {
		t.Parallel()

		token, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, verifierClient, 42, verifierResource, verifierScope, verifierSecret, time.Hour)
		require.NoError(t, err)

		disabled := func(context.Context) (oauth.Endpoints, bool, error) {
			return oauth.Endpoints{}, false, nil
		}
		_, err = mcp.NewTokenVerifier(disabled, authenticator)(context.Background(), token, &http.Request{})
		require.ErrorIs(t, err, sdkauth.ErrInvalidToken)
	})

	t.Run("a token without the scope is passed through without inventing one", func(t *testing.T) {
		t.Parallel()

		verify := mcp.NewTokenVerifier(enabledEndpoints(), authenticator)
		info, err := verify(context.Background(), noScopeToken, &http.Request{})
		require.NoError(t, err)
		require.Empty(t, info.Scopes, "the transport middleware is what turns a missing scope into a 403")
	})

	t.Run("no request principal means no user", func(t *testing.T) {
		t.Parallel()

		_, ok := mcp.UserFromRequest(&mcpsdk.CallToolRequest{})
		require.False(t, ok)
		_, ok = mcp.UserFromTokenInfo(nil)
		require.False(t, ok)
	})
}

// verifierGrants is the GrantStore the verifier tests resolve against: the only
// live grant is id 42 for user 7 and the test client.
type verifierGrants struct {
	live bool
}

func (g verifierGrants) IsOAuthGrantLive(_ context.Context, grantID int64, userID int, clientID string) (bool, error) {
	if grantID != 42 || userID != 7 || clientID != verifierClient {
		return false, nil
	}
	return g.live, nil
}

func TestTokenVerifierRequiresALiveConnectedApp(t *testing.T) {
	t.Parallel()

	// A token minted before the connected-app claim existed cannot be checked
	// against a connection, so the endpoint refuses it rather than accepting a
	// token that a revocation could not reach.
	legacyToken, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, verifierClient, 0, verifierResource, verifierScope, verifierSecret, time.Hour)
	require.NoError(t, err)
	verify := mcp.NewTokenVerifier(enabledEndpoints(), authpkg.NewTokenAuthenticator(verifierUsers{}, verifierGrants{live: true}, verifierSecret, nil))
	_, err = verify(context.Background(), legacyToken, &http.Request{})
	require.ErrorIs(t, err, sdkauth.ErrInvalidToken)

	grantToken, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, verifierClient, 42, verifierResource, verifierScope, verifierSecret, time.Hour)
	require.NoError(t, err)

	info, err := verify(context.Background(), grantToken, &http.Request{})
	require.NoError(t, err)
	request := &mcpsdk.CallToolRequest{Extra: &mcpsdk.RequestExtra{TokenInfo: info}}
	require.Equal(t, int64(42), mcp.GrantIDFromRequest(request), "the tool path needs the connected app to record activity on it")

	revoked := mcp.NewTokenVerifier(enabledEndpoints(), authpkg.NewTokenAuthenticator(verifierUsers{}, verifierGrants{live: false}, verifierSecret, nil))
	_, err = revoked(context.Background(), grantToken, &http.Request{})
	require.ErrorIs(t, err, sdkauth.ErrInvalidToken)
}
