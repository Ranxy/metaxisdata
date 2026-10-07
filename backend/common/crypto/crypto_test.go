package crypto

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key, err := GenerateKey()
	require.NoError(t, err)
	require.Len(t, key, KeySize)
	c, err := NewCipher(key)
	require.NoError(t, err)
	return c
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Parallel()

	c := newTestCipher(t)
	for _, plaintext := range []string{"hunter2", "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n", "sk-live-0123456789", strings.Repeat("x", 4096)} {
		encrypted, err := c.Encrypt(plaintext)
		require.NoError(t, err)
		require.True(t, strings.HasPrefix(encrypted, Prefix), "ciphertext must carry the version prefix")
		require.NotContains(t, encrypted, plaintext)

		decrypted, err := c.Decrypt(encrypted)
		require.NoError(t, err)
		require.Equal(t, plaintext, decrypted)
	}
}

// An empty credential stays empty: it is the "not set" state, not a value with a
// ciphertext of its own.
func TestEncryptDecryptEmptyStaysEmpty(t *testing.T) {
	t.Parallel()

	c := newTestCipher(t)
	encrypted, err := c.Encrypt("")
	require.NoError(t, err)
	require.Empty(t, encrypted)

	decrypted, err := c.Decrypt("")
	require.NoError(t, err)
	require.Empty(t, decrypted)
}

// A fresh nonce per value is what keeps equal credentials from producing equal
// ciphertexts, which is what let an earlier scheme reveal a reused password.
func TestEncryptUsesAFreshNonce(t *testing.T) {
	t.Parallel()

	c := newTestCipher(t)
	first, err := c.Encrypt("same-password")
	require.NoError(t, err)
	second, err := c.Encrypt("same-password")
	require.NoError(t, err)
	require.NotEqual(t, first, second)
}

func TestDecryptRejectsTheWrongKey(t *testing.T) {
	t.Parallel()

	encrypted, err := newTestCipher(t).Encrypt("hunter2")
	require.NoError(t, err)

	_, err = newTestCipher(t).Decrypt(encrypted)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed authentication")
}

func TestDecryptRejectsAValueThatWasNotWrittenByThisScheme(t *testing.T) {
	t.Parallel()

	c := newTestCipher(t)
	// The retired obfuscation scheme stored bare base64 with no prefix.
	for _, legacy := range []string{"aGVsbG8=", "not base64 at all!", Prefix + "!!!not base64!!!"} {
		_, err := c.Decrypt(legacy)
		require.Error(t, err, "value %q must not decrypt to garbage", legacy)
	}

	// A prefix with a body too short to hold a nonce and a tag.
	_, err := c.Decrypt(Prefix + base64.StdEncoding.EncodeToString([]byte("short")))
	require.Error(t, err)
	require.Contains(t, err.Error(), "too short")
}

func TestDecryptRejectsEditedCiphertext(t *testing.T) {
	t.Parallel()

	c := newTestCipher(t)
	encrypted, err := c.Encrypt("hunter2")
	require.NoError(t, err)

	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encrypted, Prefix))
	require.NoError(t, err)

	// Flip the last bit of the tag and the first bit of the nonce in turn.
	for _, index := range []int{len(raw) - 1, 0} {
		edited := make([]byte, len(raw))
		copy(edited, raw)
		edited[index] ^= 0x01
		_, err := c.Decrypt(Prefix + base64.StdEncoding.EncodeToString(edited))
		require.Error(t, err)
		require.Contains(t, err.Error(), "failed authentication")
	}
}

func TestIsCiphertext(t *testing.T) {
	t.Parallel()

	require.True(t, IsCiphertext(Prefix+"AAAA"))
	require.False(t, IsCiphertext("aGVsbG8="))
	require.False(t, IsCiphertext(""))
}

func TestNewCipherRejectsTheWrongKeySize(t *testing.T) {
	t.Parallel()

	_, err := NewCipher([]byte("too-short"))
	require.Error(t, err)
}

func TestParseKey(t *testing.T) {
	t.Parallel()

	raw := strings.Repeat("k", KeySize)
	for name, encoded := range map[string]string{
		"raw":             raw,
		"base64":          base64.StdEncoding.EncodeToString([]byte(raw)),
		"base64 unpadded": base64.RawStdEncoding.EncodeToString([]byte(raw)),
		"hex":             hex.EncodeToString([]byte(raw)),
	} {
		key, err := ParseKey(encoded)
		require.NoError(t, err, name)
		require.Equal(t, raw, string(key), name)
	}

	for name, value := range map[string]string{
		"empty":      "",
		"blank":      "   ",
		"too short":  "abc",
		"wrong size": base64.StdEncoding.EncodeToString([]byte("short")),
		// Sixteen random bytes printed as hex are 32 characters: taking those
		// characters as a raw key would silently halve the key's entropy.
		"16 bytes as hex": "b3c4d5e6f708192a3b4c5d6e7f8091a2",
	} {
		_, err := ParseKey(value)
		require.Error(t, err, name)
	}
}

func TestParseKeys(t *testing.T) {
	t.Parallel()

	keys, err := ParseKeys("")
	require.NoError(t, err)
	require.Empty(t, keys)

	// Not hex digits, so these are the raw spelling of a 32-byte key.
	first := strings.Repeat("k", KeySize)
	second := strings.Repeat("q", KeySize)
	keys, err = ParseKeys(" " + first + " , " + second + " ,")
	require.NoError(t, err)
	require.Len(t, keys, 2)
	require.Equal(t, first, string(keys[0]))
	require.Equal(t, second, string(keys[1]))

	_, err = ParseKeys(first + ",oops")
	require.Error(t, err)
}
