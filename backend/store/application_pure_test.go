package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplicationSQLKeyDigest(t *testing.T) {
	t.Parallel()

	// The digest is the lookup column, so it must be deterministic and it must not be the
	// key itself.
	require.Equal(t, applicationSQLKeyDigest("asql_abc"), applicationSQLKeyDigest("asql_abc"))
	require.NotEqual(t, applicationSQLKeyDigest("asql_abc"), applicationSQLKeyDigest("asql_abd"))
	require.NotContains(t, applicationSQLKeyDigest("asql_abc"), "abc")
	require.Len(t, applicationSQLKeyDigest("asql_abc"), 64)
}

func TestMaskApplicationSQLAPIKey(t *testing.T) {
	t.Parallel()

	// The mask shows enough of the head to tell which endpoint a key belongs to, enough of the
	// tail to tell two keys apart, and nothing in between.
	key := applicationSQLKeyPrefix + strings.Repeat("a", 20) + "SECRETMIDDLE" + strings.Repeat("b", 39)
	masked := maskApplicationSQLAPIKey(key)

	require.True(t, strings.HasPrefix(masked, key[:13]))
	require.True(t, strings.HasSuffix(masked, key[len(key)-4:]))
	require.Contains(t, masked, "*")
	require.NotContains(t, masked, "SECRETMIDDLE")
	require.NotEqual(t, key, masked)

	require.Equal(t, "*", maskApplicationSQLAPIKey("x"))
}
