package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/state"
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
	// mcpTestClient is the OAuth registration an MCP token is issued to.
	mcpTestClient = "client-test"
)

// fakeUsers is a UserStore with a single principal, so the token rules can be
// exercised without a database.
type fakeUsers struct {
	user    *store.UserMessage
	revoked map[string]bool
}

func (f fakeUsers) GetUserByID(_ context.Context, id int) (*store.UserMessage, error) {
	if f.user == nil || f.user.ID != id {
		return nil, nil
	}
	return f.user, nil
}

func (f fakeUsers) IsTokenRevoked(_ context.Context, tokenID string) (bool, error) {
	return f.revoked[tokenID], nil
}

// countingUsers records how often the persistent revocation table is read, so a
// test can show the cache is in front of it.
type countingUsers struct {
	fakeUsers
	lookups int
}

func (u *countingUsers) IsTokenRevoked(ctx context.Context, tokenID string) (bool, error) {
	u.lookups++
	return u.fakeUsers.IsTokenRevoked(ctx, tokenID)
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

	mcpToken, err := GenerateMCPAccessToken("user@example.com", 7, mcpTestClient, mcpTestResource, mcpTestScope, audienceTestSecret, time.Hour)
	require.NoError(t, err)
	_, err = VerifyAccessToken(mcpToken, audienceTestSecret, common.ReleaseModeDev)
	require.Error(t, err, "an MCP token must not verify for the user API")

	identity, err := VerifyAccessTokenFor(mcpToken, audienceTestSecret, mcpTestResource)
	require.NoError(t, err)
	require.Equal(t, 7, identity.UserID)
	require.Equal(t, []string{mcpTestScope}, identity.Scopes, "the token's own scope is what the resource server enforces")
	require.Equal(t, mcpTestClient, identity.ClientID, "Logout revokes a client's grants by this claim")
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

	token, err := GenerateMCPAccessToken("user@example.com", 7, mcpTestClient, mcpTestResource, mcpTestScope, audienceTestSecret, time.Hour)
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

	mcpToken, err := GenerateMCPAccessToken("user@example.com", 7, mcpTestClient, mcpTestResource, mcpTestScope, audienceTestSecret, time.Hour)
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

// M4: revocation is keyed by the token's jti and read from the persistent table,
// so a revoked token stays revoked however many other tokens are revoked (the old
// bounded LRU let any account holder evict an entry by logging in and out).
func TestResolveRefusesAPersistentlyRevokedToken(t *testing.T) {
	t.Parallel()

	token, err := GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, audienceTestSecret, time.Hour)
	require.NoError(t, err)
	identity, err := VerifyAccessTokenProvenance(token, audienceTestSecret)
	require.NoError(t, err)
	require.NotEmpty(t, identity.TokenID, "every token this server signs carries a jti")

	users := fakeUsers{
		user:    &store.UserMessage{ID: 7, Email: "user@example.com"},
		revoked: map[string]bool{identity.TokenID: true},
	}
	_, _, err = NewTokenAuthenticator(users, audienceTestSecret, nil).Resolve(context.Background(), token, AccessTokenAudience(common.ReleaseModeDev))
	require.ErrorIs(t, err, ErrTokenRevoked)
}

// M4: the process-local cache only saves a table read. A decision read once is
// reused, and a revocation performed here is refused immediately without a read.
func TestResolveCachesRevocationDecisions(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)

	token, err := GenerateAccessToken("user@example.com", 7, common.ReleaseModeDev, audienceTestSecret, time.Hour)
	require.NoError(t, err)
	identity, err := VerifyAccessTokenProvenance(token, audienceTestSecret)
	require.NoError(t, err)

	users := &countingUsers{fakeUsers: fakeUsers{user: &store.UserMessage{ID: 7, Email: "user@example.com"}}}
	authenticator := NewTokenAuthenticator(users, audienceTestSecret, stateCfg)
	audience := AccessTokenAudience(common.ReleaseModeDev)

	_, _, err = authenticator.Resolve(context.Background(), token, audience)
	require.NoError(t, err)
	_, _, err = authenticator.Resolve(context.Background(), token, audience)
	require.NoError(t, err)
	require.Equal(t, 1, users.lookups, "a fresh decision is not read twice")

	// A revocation this process just performed is refused without waiting for the
	// next table read, let alone a replica's cache to expire.
	stateCfg.TokenRevocationCache.Revoke(identity.TokenID, time.Now())
	_, _, err = authenticator.Resolve(context.Background(), token, audience)
	require.ErrorIs(t, err, ErrTokenRevoked)
	require.Equal(t, 1, users.lookups)
}
