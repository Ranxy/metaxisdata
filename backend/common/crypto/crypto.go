// Package crypto encrypts the credentials the server keeps in its own
// database: the instance and data source credentials, and the LLM provider API
// keys.
//
// The data key is a random 32-byte AES-256 key. A ciphertext is the prefix
// "v1:" followed by the standard base64 of a fresh 12-byte nonce, the encrypted
// bytes and the 16-byte GCM tag. Because every value gets its own nonce, equal
// plaintexts never produce equal ciphertexts, and because the tag authenticates
// the whole value, decrypting with the wrong key or reading an edited value
// fails instead of returning garbage.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const (
	// Prefix versions the ciphertext layout. It is the only version this build
	// writes, and a reader refuses a value without it: a credential written by
	// the retired obfuscation scheme fails as an error rather than decrypting to
	// garbage that the next write would store as the new plaintext.
	Prefix = "v1:"

	// KeySize is the AES-256 key length in bytes.
	KeySize = 32

	// KeyEnvironment names the environment variable that holds the current
	// key-encryption key. Its presence is what keeps a database read or a
	// database backup from being enough to recover the stored credentials: the
	// data key is wrapped with it instead of being kept in the clear.
	KeyEnvironment = "METAXISDATA_ENCRYPTION_KEY"

	// PreviousKeyEnvironment names an optional comma-separated list of retired
	// key-encryption keys. They are only ever used to open a wrapped data key, so
	// a rotation does not require re-encrypting a single stored credential.
	PreviousKeyEnvironment = "METAXISDATA_ENCRYPTION_KEY_PREVIOUS"

	// nonceSize is the GCM nonce length, and tagSize its authentication tag.
	nonceSize = 12
	tagSize   = 16
)

// GenerateKey returns a new random data key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate a random key: %w", err)
	}
	return key, nil
}

// ParseKey parses one key from its configured form: standard base64, hex, or
// the raw 32 bytes. The key comes from an environment variable an operator
// pastes by hand, so all three spellings are accepted.
func ParseKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("the key is empty")
	}
	if len(value) == KeySize {
		return []byte(value), nil
	}
	if decoded, err := base64.StdEncoding.DecodeString(value); err == nil && len(decoded) == KeySize {
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(value); err == nil && len(decoded) == KeySize {
		return decoded, nil
	}
	return nil, fmt.Errorf("the key must be %d bytes, or its base64 or hex encoding", KeySize)
}

// ParseKeys parses a comma-separated key list. Empty entries are skipped, so an
// unset or blank variable yields no keys rather than an error.
func ParseKeys(value string) ([][]byte, error) {
	var keys [][]byte
	for _, entry := range strings.Split(value, ",") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		key, err := ParseKey(entry)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// Cipher encrypts and decrypts values with one data key.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a cipher from a 32-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to build the AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to build the GCM cipher: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt encrypts a value. An empty plaintext stays empty: no credential and
// no ciphertext are the same state.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate a nonce: %w", err)
	}
	sealed := c.aead.Seal(nil, nonce, []byte(plaintext), nil)
	return Prefix + base64.StdEncoding.EncodeToString(append(nonce, sealed...)), nil
}

// Decrypt decrypts a value. An empty ciphertext stays empty. Anything else must
// be a "v1:" ciphertext that authenticates under this key; a value written by
// another scheme, encrypted with another key, truncated or edited is an error.
func (c *Cipher) Decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, Prefix) {
		return "", errors.New("the value is not encrypted in the v1 format")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil {
		return "", fmt.Errorf("failed to decode the ciphertext: %w", err)
	}
	if len(raw) < nonceSize+tagSize {
		return "", errors.New("the ciphertext is too short to be authentic")
	}
	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return "", errors.New("the ciphertext failed authentication: it was not written with this key, or it was edited")
	}
	return string(plaintext), nil
}

// IsCiphertext reports whether a value is stored in the current ciphertext
// format.
func IsCiphertext(value string) bool {
	return strings.HasPrefix(value, Prefix)
}
