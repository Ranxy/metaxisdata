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
)

// verifierUsers is the UserStore the verifier resolves against, so these tests
// need no database.
type verifierUsers struct{}

func (verifierUsers) GetUserByID(_ context.Context, id int) (*store.UserMessage, error) {
	return &store.UserMessage{ID: id, Email: "user@example.com"}, nil
}

func enabledEndpoints() mcp.EndpointsFunc {
	return func(context.Context) (oauth.Endpoints, bool, error) {
		return oauth.Endpoints{Issuer: verifierIssuer, Resource: verifierResource}, true, nil
	}
}

func TestTokenVerifierResolvesAnMCPToken(t *testing.T) {
	t.Parallel()

	token, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, verifierResource, verifierScope, verifierSecret, time.Hour)
	require.NoError(t, err)

	verify := mcp.NewTokenVerifier(enabledEndpoints(), authpkg.NewTokenAuthenticator(verifierUsers{}, verifierSecret, nil))
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

	authenticator := authpkg.NewTokenAuthenticator(verifierUsers{}, verifierSecret, nil)

	userToken, err := authpkg.GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, verifierSecret, time.Hour)
	require.NoError(t, err)
	otherResourceToken, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, "https://other.example.com/mcp", verifierScope, verifierSecret, time.Hour)
	require.NoError(t, err)
	noScopeToken, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, verifierResource, "", verifierSecret, time.Hour)
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

		token, err := authpkg.GenerateMCPAccessToken("user@example.com", 7, verifierResource, verifierScope, verifierSecret, time.Hour)
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
