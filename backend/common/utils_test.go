package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Stored credentials used to be "encrypted" with a repeating-key XOR whose seed
// lived in the same database, with no nonce and no integrity check. They are
// now AES-256-GCM values with a version prefix.
func TestObfuscateRoundTrip(t *testing.T) {
	t.Parallel()

	const key = "0123456789abcdef0123456789abcdef"

	for _, plaintext := range []string{"p@ssw0rd", "a", strings.Repeat("x", 4096), "密码🔐"} {
		t.Run(plaintext[:1], func(t *testing.T) {
			t.Parallel()
			ciphertext, err := Obfuscate(plaintext, key)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(ciphertext, "v1:"))
			require.NotContains(t, ciphertext, plaintext)

			decrypted, err := Unobfuscate(ciphertext, key)
			require.NoError(t, err)
			require.Equal(t, plaintext, decrypted)
		})
	}
}

func TestObfuscateUsesAFreshNonce(t *testing.T) {
	t.Parallel()

	const key = "0123456789abcdef0123456789abcdef"
	first, err := Obfuscate("same", key)
	require.NoError(t, err)
	second, err := Obfuscate("same", key)
	require.NoError(t, err)

	// Equal ciphertexts for equal plaintexts leak equality; the random nonce
	// must prevent that.
	require.NotEqual(t, first, second)
}

func TestObfuscateEmptyInputStaysEmpty(t *testing.T) {
	t.Parallel()

	// The store uses "" to mean "no credential set".
	ciphertext, err := Obfuscate("", "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	require.Empty(t, ciphertext)

	plaintext, err := Unobfuscate("", "0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	require.Empty(t, plaintext)
}

func TestUnobfuscateFailsClosed(t *testing.T) {
	t.Parallel()

	const key = "0123456789abcdef0123456789abcdef"
	ciphertext, err := Obfuscate("secret", key)
	require.NoError(t, err)

	t.Run("wrong key", func(t *testing.T) {
		t.Parallel()
		_, err := Unobfuscate(ciphertext, "another-key-another-key-another")
		require.Error(t, err)
	})

	t.Run("tampered ciphertext", func(t *testing.T) {
		t.Parallel()
		// Flip a bit inside the base64 payload; the GCM tag must reject it
		// instead of returning corrupted plaintext.
		raw := []byte(ciphertext)
		if raw[len(raw)-2] == 'A' {
			raw[len(raw)-2] = 'B'
		} else {
			raw[len(raw)-2] = 'A'
		}
		_, err := Unobfuscate(string(raw), key)
		require.Error(t, err)
	})

	t.Run("missing version prefix", func(t *testing.T) {
		t.Parallel()
		// Values written by the old XOR scheme must not be silently mis-read.
		_, err := Unobfuscate("cGFzc3dvcmQ=", key)
		require.ErrorContains(t, err, "unsupported ciphertext format")
	})

	t.Run("empty key", func(t *testing.T) {
		t.Parallel()
		_, err := Unobfuscate(ciphertext, "")
		require.ErrorContains(t, err, "encryption key is empty")
		_, err = Obfuscate("secret", "")
		require.ErrorContains(t, err, "encryption key is empty")
	})

	t.Run("truncated ciphertext", func(t *testing.T) {
		t.Parallel()
		_, err := Unobfuscate("v1:AAAA", key)
		require.Error(t, err)
	})
}
