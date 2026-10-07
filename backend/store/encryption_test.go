package store

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common/crypto"
)

func newTestCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	cipher, err := crypto.NewCipher(key)
	require.NoError(t, err)
	return cipher
}

func newTestKey(t *testing.T) []byte {
	t.Helper()
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	return key
}

// A store without an installed cipher must refuse to read a credential instead
// of treating an unset cipher as "no encryption".
func TestCredentialCipherWithoutOneFails(t *testing.T) {
	t.Parallel()

	s := &Store{}
	_, err := s.credentialCipher()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}

func TestSetCipherInstallsTheCipher(t *testing.T) {
	t.Parallel()

	cipher := newTestCipher(t)
	s := &Store{}
	s.SetCipher(cipher)

	got, err := s.credentialCipher()
	require.NoError(t, err)
	require.Same(t, cipher, got)
}

func TestUnwrapDataKeyBareKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	got, err := unwrapDataKey(base64.StdEncoding.EncodeToString(key), nil)
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.Equal(t, -1, got.wrappedBy)

	for name, stored := range map[string]string{
		"not base64": "!!!",
		"short":      base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		_, err := unwrapDataKey(stored, nil)
		require.Error(t, err, name)
	}
}

func TestUnwrapDataKeyWrappedKey(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	current := newTestKey(t)
	previous := newTestKey(t)

	wrap := func(t *testing.T, kek, key []byte) string {
		t.Helper()
		kekCipher, err := crypto.NewCipher(kek)
		require.NoError(t, err)
		wrapped, err := kekCipher.Encrypt(string(key))
		require.NoError(t, err)
		return wrapped
	}

	// The current key opens it, and the caller learns it needs no re-wrap.
	got, err := unwrapDataKey(wrap(t, current, key), [][]byte{current, previous})
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.Equal(t, 0, got.wrappedBy)

	// A retired key opens it, and the caller learns it must be re-wrapped.
	got, err = unwrapDataKey(wrap(t, previous, key), [][]byte{current, previous})
	require.NoError(t, err)
	require.Equal(t, key, got.key)
	require.Equal(t, 1, got.wrappedBy)

	// A wrapped key with no key-encryption key configured says so, rather than
	// trying to decode it as a bare key.
	_, err = unwrapDataKey(wrap(t, current, key), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "none is configured")

	_, err = unwrapDataKey(wrap(t, newTestKey(t), key), [][]byte{current, previous})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no configured key-encryption key")
}

// The wrapping round-trip the server performs when it re-wraps a bare data key
// must produce a value this code can open again.
func TestWrappedDataKeyRoundTrips(t *testing.T) {
	t.Parallel()

	key := newTestKey(t)
	kek := newTestKey(t)
	kekCipher, err := crypto.NewCipher(kek)
	require.NoError(t, err)

	wrapped, err := kekCipher.Encrypt(string(key))
	require.NoError(t, err)
	require.True(t, crypto.IsCiphertext(wrapped))
	require.False(t, strings.Contains(wrapped, base64.StdEncoding.EncodeToString(key)))

	got, err := unwrapDataKey(wrapped, [][]byte{kek})
	require.NoError(t, err)
	require.Equal(t, key, got.key)
}
