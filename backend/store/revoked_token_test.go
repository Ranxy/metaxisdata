package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The jti comes from outside the process (a token this server signed, but still
// a caller-supplied string), so the revocation queries must keep it bound.
func TestRevocationQueriesBindTheTokenID(t *testing.T) {
	t.Parallel()

	require.Contains(t, isTokenRevokedQuery, "WHERE jti = $1")
	require.Contains(t, revokeTokenQuery, "VALUES ($1, $2)")
	require.NotContains(t, revokeTokenQuery, "%s", "query must not contain format verbs")

	// The prune is bounded by a bound timestamp, not by anything a caller sends.
	require.Contains(t, deleteExpiredRevokedTokensQuery, "WHERE expires_at < $1")
}
