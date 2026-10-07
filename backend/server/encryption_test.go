package server

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/crypto"
)

// A retired key-encryption key may only open a wrapped data key. A deployment
// that configures one and not the current key is a misconfiguration, and it must
// be reported instead of quietly making the retired key the one the data key is
// wrapped under: the store is never reached here, so a nil one proves the check
// runs before any key material is used.
func TestResolveCredentialCipherRejectsARetiredKeyAlone(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	t.Setenv(crypto.KeyEnvironment, "")
	t.Setenv(crypto.PreviousKeyEnvironment, base64.StdEncoding.EncodeToString(key))

	s := &Server{}
	err = s.resolveCredentialCipher(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), crypto.PreviousKeyEnvironment)
	require.Contains(t, err.Error(), "is set without")
}

func TestResolveCredentialCipherRejectsAnUnusableKey(t *testing.T) {
	t.Setenv(crypto.KeyEnvironment, "not-a-key")
	t.Setenv(crypto.PreviousKeyEnvironment, "")

	s := &Server{}
	err := s.resolveCredentialCipher(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), crypto.KeyEnvironment)
}
