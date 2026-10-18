package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The token hash is a digest of a string from outside the process, so every
// query must keep it bound, and the rotation must stay scoped to the client that
// owns the grant -- a hash leaked from one registration must not be redeemable
// by another.
func TestOAuthGrantQueriesBindTheHashAndScopeTheClient(t *testing.T) {
	t.Parallel()

	require.Contains(t, getOAuthGrantQuery, "WHERE token_hash = $1 AND client_id = $2")
	require.Contains(t, rotateOAuthGrantQuery, "UPDATE oauth_refresh_token")
	require.Contains(t, rotateOAuthGrantQuery, "WHERE token_hash = $1 AND client_id = $2")
	require.Contains(t, rotateOAuthGrantQuery, "revoked_at IS NULL",
		"a revocation that commits first must stop a rotation")
	require.Contains(t, rotateOAuthGrantQuery, "expires_at > $5",
		"the gate must not rotate an expired grant")
	require.Contains(t, rotateOAuthGrantQuery, "RETURNING id",
		"the single-use gate relies on the update reporting the row it claimed, and on the id surviving the rotation")
	require.Contains(t, revokeOAuthGrantQuery, "WHERE id = $1 AND revoked_at IS NULL")
	require.Contains(t, revokeOAuthGrantQuery, "RETURNING id")
	require.Contains(t, revokeOAuthGrantsByUserAndClientQuery, "WHERE user_id = $1 AND client_id = $2")
	require.Contains(t, revokeOAuthGrantsByUserAndClientQuery, "revoked_at IS NULL")
	require.Contains(t, isOAuthGrantLiveQuery, "id = $1 AND user_id = $2 AND client_id = $3",
		"the live check must bind the token's own user and client, not just the grant id")
	require.Contains(t, isOAuthGrantLiveQuery, "revoked_at IS NULL")
	require.Contains(t, isOAuthGrantLiveQuery, "expires_at > NOW()")
	require.Contains(t, touchOAuthGrantQuery, "last_tool_call_at < $3",
		"the tool-call touch is throttled by a predicate, never an unconditional write")
	require.Contains(t, deleteExpiredOAuthGrantsQuery, "WHERE expires_at < $1")

	for _, query := range []string{
		createOAuthGrantQuery,
		getOAuthGrantQuery,
		rotateOAuthGrantQuery,
		revokeOAuthGrantQuery,
		revokeOAuthGrantsByUserAndClientQuery,
		isOAuthGrantLiveQuery,
		touchOAuthGrantQuery,
		deleteExpiredOAuthGrantsQuery,
	} {
		require.NotContains(t, query, "%s", "query must not contain format verbs")
	}
}

func TestWebRefreshTokenQueriesUseTheAtomicConsume(t *testing.T) {
	t.Parallel()

	require.Contains(t, consumeWebRefreshTokenQuery, "WHERE token_hash = $1")
	require.Contains(t, consumeWebRefreshTokenQuery, "RETURNING",
		"rotation must claim and read the row in one statement")
	require.Contains(t, deleteWebRefreshTokenQuery, "WHERE token_hash = $1")
	require.Contains(t, deleteExpiredWebRefreshTokensQuery, "WHERE expires_at < $1")

	for _, query := range []string{
		createWebRefreshTokenQuery,
		consumeWebRefreshTokenQuery,
		deleteWebRefreshTokenQuery,
		deleteExpiredWebRefreshTokensQuery,
	} {
		require.NotContains(t, query, "%s", "query must not contain format verbs")
	}
}

// The translated filter numbers its placeholders from $1, so the user scope must
// be appended after the filter's arguments. Appending it first would collide
// with the filter's $1 (a 500 when the types differ, or a silently ignored
// predicate when they happen to match).
func TestListConnectedAppsFilterArgsPrecedeTheUserScope(t *testing.T) {
	t.Parallel()

	filter := &ListResourceFilter{
		Where: "(CASE WHEN g.expires_at <= $1 THEN 'EXPIRED' END) = $2",
		Args:  []any{"now", "ACTIVE"},
	}
	userID := 7
	where, args := listConnectedAppsWhere(&FindOAuthGrantMessage{UserID: &userID, Filter: filter})
	require.Equal(t, "TRUE AND (CASE WHEN g.expires_at <= $1 THEN 'EXPIRED' END) = $2 AND g.user_id = $3", where)
	require.Equal(t, []any{"now", "ACTIVE", 7}, args)

	where, args = listConnectedAppsWhere(&FindOAuthGrantMessage{UserID: &userID})
	require.Equal(t, "TRUE AND g.user_id = $1", where)
	require.Equal(t, []any{7}, args)
}
