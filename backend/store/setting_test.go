package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// GetSecret must return the already-resolved AUTH_SECRET without touching the
// database: the obfuscation path reads it for every instance/LLM row and must
// not pay a query per read.
func TestGetSecretReturnsTheResolvedSecret(t *testing.T) {
	t.Parallel()

	s := &Store{secret: "resolved-secret"}
	got, err := s.GetSecret(context.Background())
	require.NoError(t, err)
	require.Equal(t, "resolved-secret", got)
}
