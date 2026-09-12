package store

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The credential encryption key should come from METADATA_SECRET_KEY rather than
// the AUTH_SECRET row that the ciphertexts live next to. These paths resolve
// without touching the database, so they stay hermetic.
func TestGetSecretUsesTheConfiguredKey(t *testing.T) {
	t.Parallel()

	key := strings.Repeat("k", minSecretLength)
	s := &Store{}
	WithEncryptionKey(key)(s)

	got, err := s.GetSecret(context.Background())
	require.NoError(t, err)
	require.Equal(t, key, got)

	// The resolved value is cached, so repeat reads do not re-derive it.
	got, err = s.GetSecret(context.Background())
	require.NoError(t, err)
	require.Equal(t, key, got)
}

func TestGetSecretRejectsAShortConfiguredKey(t *testing.T) {
	t.Parallel()

	s := &Store{}
	WithEncryptionKey("too-short")(s)

	// Fail closed: returning the short key would encrypt credentials with a
	// brute-forceable secret, and falling through would silently use the
	// database-stored AUTH_SECRET instead.
	_, err := s.GetSecret(context.Background())
	require.Error(t, err)
}
