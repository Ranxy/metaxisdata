package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The token hash is a digest of a string from outside the process, so every
// query must keep it bound, and the consume must stay scoped to the client that
// owns the grant -- a hash leaked from one registration must not be redeemable
// by another.
func TestOAuthRefreshTokenQueriesBindTheHashAndScopeTheClient(t *testing.T) {
	t.Parallel()

	require.Contains(t, getOAuthRefreshTokenQuery, "WHERE token_hash = $1 AND client_id = $2")
	require.Contains(t, consumeOAuthRefreshTokenQuery, "WHERE token_hash = $1 AND client_id = $2")
	require.Contains(t, consumeOAuthRefreshTokenQuery, "RETURNING token_hash",
		"the single-use gate relies on the delete reporting the row it claimed")
	require.Contains(t, deleteOAuthRefreshTokensByUserAndClientQuery, "WHERE user_id = $1 AND client_id = $2")
	require.Contains(t, deleteExpiredOAuthRefreshTokensQuery, "WHERE expires_at < $1")

	for _, query := range []string{
		createOAuthRefreshTokenQuery,
		getOAuthRefreshTokenQuery,
		consumeOAuthRefreshTokenQuery,
		deleteOAuthRefreshTokensByUserAndClientQuery,
		deleteExpiredOAuthRefreshTokensQuery,
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
