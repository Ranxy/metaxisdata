package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// audienceTestSecret is long enough for the deployment's minimum and is only
	// ever used inside this file.
	audienceTestSecret = "0123456789abcdef0123456789abcdef"
	// mcpTestResource is an MCP token's audience: the endpoint's canonical
	// resource identifier, not a fixed string.
	mcpTestResource = "https://mx.example.com/mcp"
	mcpTestScope    = "metaxisdata.mcp.read"
)

// fakeUsers is a UserStore with a single principal, so the token rules can be
// exercised without a database.
type fakeUsers struct {
	user *store.UserMessage
}

func (f fakeUsers) GetUserByID(_ context.Context, id int) (*store.UserMessage, error) {
	if f.user == nil || f.user.ID != id {
		return nil, nil
	}
	return f.user, nil
}

func TestResolveKeepsAudiencesApart(t *testing.T) {
	t.Parallel()

	token, err := GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, audienceTestSecret, time.Hour)
	require.NoError(t, err)

	_, _, err = NewTokenAuthenticator(nil, audienceTestSecret, nil).Resolve(context.Background(), token, mcpTestResource)
	require.ErrorIs(t, err, ErrTokenInvalid, "a user-API token must not resolve for another audience")
}

func TestMCPTokensAreNotInterchangeableWithUserTokens(t *testing.T) {
	t.Parallel()

	mcpToken, err := GenerateMCPAccessToken("user@example.com", 7, mcpTestResource, mcpTestScope, audienceTestSecret, time.Hour)
	require.NoError(t, err)
	_, err = VerifyAccessToken(mcpToken, audienceTestSecret, common.ReleaseModeDev)
	require.Error(t, err, "an MCP token must not verify for the user API")

	identity, err := VerifyAccessTokenFor(mcpToken, audienceTestSecret, mcpTestResource)
	require.NoError(t, err)
	require.Equal(t, 7, identity.UserID)
	require.Equal(t, []string{mcpTestScope}, identity.Scopes, "the token's own scope is what the resource server enforces")
	require.False(t, identity.ExpiresAt.IsZero(), "a resource server hands this expiry to its transport")

	userToken, err := GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, audienceTestSecret, time.Hour)
	require.NoError(t, err)
	_, err = VerifyAccessTokenFor(userToken, audienceTestSecret, mcpTestResource)
	require.Error(t, err, "a user token must not verify for MCP")

	identity, err = VerifyAccessTokenFor(userToken, audienceTestSecret, AccessTokenAudience(common.ReleaseModeDev))
	require.NoError(t, err)
	require.Empty(t, identity.Scopes, "a user-API token carries no scope")
}

// TestResolveAppliesTheUserRulesForTheMCPAudience is the reason the resolution
// chain is shared: an MCP token must stop working the moment the user behind it
// is deactivated, exactly like a web session.
func TestResolveAppliesTheUserRulesForTheMCPAudience(t *testing.T) {
	t.Parallel()

	token, err := GenerateMCPAccessToken("user@example.com", 7, mcpTestResource, mcpTestScope, audienceTestSecret, time.Hour)
	require.NoError(t, err)

	active := NewTokenAuthenticator(fakeUsers{user: &store.UserMessage{ID: 7, Email: "user@example.com"}}, audienceTestSecret, nil)
	user, identity, err := active.Resolve(context.Background(), token, mcpTestResource)
	require.NoError(t, err)
	require.Equal(t, 7, user.ID)
	require.Equal(t, 7, identity.UserID)

	deactivated := NewTokenAuthenticator(fakeUsers{user: &store.UserMessage{ID: 7, MemberDeleted: true}}, audienceTestSecret, nil)
	_, _, err = deactivated.Resolve(context.Background(), token, mcpTestResource)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrTokenInvalid, "the token itself is fine; the principal behind it is not")
}

// TestMCPTokensKeepARevocationPath pins the reason Logout checks provenance
// rather than an audience: an MCP token's audience is the endpoint's resource
// identifier, and it must still be revocable — while an unsigned string must not
// be, or the revocation cache could be flooded with forgeries.
func TestMCPTokensKeepARevocationPath(t *testing.T) {
	t.Parallel()

	mcpToken, err := GenerateMCPAccessToken("user@example.com", 7, mcpTestResource, mcpTestScope, audienceTestSecret, time.Hour)
	require.NoError(t, err)
	identity, err := VerifyAccessTokenProvenance(mcpToken, audienceTestSecret)
	require.NoError(t, err)
	require.Equal(t, 7, identity.UserID)

	userToken, err := GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, audienceTestSecret, time.Hour)
	require.NoError(t, err)
	_, err = VerifyAccessTokenProvenance(userToken, audienceTestSecret)
	require.NoError(t, err)

	_, err = VerifyAccessTokenProvenance(mcpToken, "another-key-another-key-another")
	require.Error(t, err, "a token this server did not sign must never reach the revocation cache")
}

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
