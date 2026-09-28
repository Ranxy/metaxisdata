package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// audienceTestSecret is long enough for the deployment's minimum and is only
// ever used inside this file.
const audienceTestSecret = "0123456789abcdef0123456789abcdef"

// TestResolveKeepsAudiencesApart pins the separation the MCP endpoint relies on:
// a token minted for the user API must not resolve for another audience. The
// audience check runs before any store access, which is why no database is
// needed here.
func TestResolveKeepsAudiencesApart(t *testing.T) {
	t.Parallel()

	token, err := GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, audienceTestSecret, time.Hour)
	require.NoError(t, err)

	_, _, err = NewTokenAuthenticator(nil, audienceTestSecret, nil).Resolve(context.Background(), token, MCPAccessTokenAudience(common.ReleaseModeDev))
	require.ErrorIs(t, err, ErrTokenInvalid, "a user-API token must not resolve for another audience")
}

// TestMCPTokensAreNotInterchangeableWithUserTokens pins the audience split in
// both directions: an MCP token must not verify for the user API, and a user
// token must not verify for MCP.
func TestMCPTokensAreNotInterchangeableWithUserTokens(t *testing.T) {
	t.Parallel()

	mcpToken, err := GenerateMCPAccessToken("user@example.com", 7, common.ReleaseModeDev, audienceTestSecret, time.Hour)
	require.NoError(t, err)
	_, err = VerifyAccessToken(mcpToken, audienceTestSecret, common.ReleaseModeDev)
	require.Error(t, err, "an MCP token must not verify for the user API")

	identity, err := VerifyAccessTokenFor(mcpToken, audienceTestSecret, MCPAccessTokenAudience(common.ReleaseModeDev))
	require.NoError(t, err)
	require.Equal(t, 7, identity.UserID)

	userToken, err := GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, audienceTestSecret, time.Hour)
	require.NoError(t, err)
	_, err = VerifyAccessTokenFor(userToken, audienceTestSecret, MCPAccessTokenAudience(common.ReleaseModeDev))
	require.Error(t, err, "a user token must not verify for MCP")
}

// TestResolveReportsAnExpiredTokenAsRevoked keeps the mapping the ConnectRPC
// interceptor has always reported: an expired token is an authentication
// failure, not a parse failure.
func TestResolveReportsAnExpiredTokenAsRevoked(t *testing.T) {
	t.Parallel()

	token, err := GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, audienceTestSecret, -time.Hour)
	require.NoError(t, err)

	_, _, err = NewTokenAuthenticator(nil, audienceTestSecret, nil).Resolve(context.Background(), token, AccessTokenAudience(common.ReleaseModeDev))
	require.ErrorIs(t, err, ErrTokenRevoked)
}

func TestResolveRejectsAMissingToken(t *testing.T) {
	t.Parallel()

	_, _, err := NewTokenAuthenticator(nil, audienceTestSecret, nil).Resolve(context.Background(), "", AccessTokenAudience(common.ReleaseModeDev))
	require.ErrorIs(t, err, ErrTokenMissing)
}
