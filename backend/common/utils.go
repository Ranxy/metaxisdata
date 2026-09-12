//nolint:revive
package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/nyaruka/phonenumbers"
	"google.golang.org/protobuf/encoding/protojson"
)

// ProtojsonMarshaler is a global protojson marshaler with DiscardUnknown set to true.
//
//nolint:forbidigo
var ProtojsonUnmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}

func TruncateString(str string, limit int) (string, bool) {
	chars := 0
	for i := range str {
		if chars >= limit {
			return str[:i], true
		}
		chars++
	}
	return str, false
}

var letters = []rune("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

// RandomString returns a random string with length n.
func RandomString(n int) (string, error) {
	var sb strings.Builder
	sb.Grow(n)
	for i := 0; i < n; i++ {
		// The reason for using crypto/rand instead of math/rand is that
		// the former relies on hardware to generate random numbers and
		// thus has a stronger source of random numbers.
		randNum, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", err
		}
		if _, err := sb.WriteRune(letters[randNum.Uint64()]); err != nil {
			return "", err
		}
	}
	return sb.String(), nil
}

// ValidatePhone validates the phone number.
func ValidatePhone(phone string) error {
	phoneNumber, err := phonenumbers.Parse(phone, "")
	if err != nil {
		return err
	}
	if !phonenumbers.IsValidNumber(phoneNumber) {
		return errors.New("invalid phone number")
	}
	return nil
}

// obfuscateVersion prefixes every ciphertext so the scheme can be rotated
// without having to guess what an existing value is.
const obfuscateVersion = "v1:"

// Obfuscate encrypts src with AES-256-GCM. The key is derived from keyMaterial,
// so it may be any length, and every call uses a fresh random nonce. The
// authentication tag makes a wrong key or a tampered value fail closed instead
// of returning garbage. An empty src stays empty, which is how the store marks
// "no credential set".
func Obfuscate(src, keyMaterial string) (string, error) {
	if src == "" {
		return "", nil
	}
	aead, err := newAEAD(keyMaterial)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to read nonce: %w", err)
	}
	sealed := aead.Seal(nonce, nonce, []byte(src), nil)
	return obfuscateVersion + base64.StdEncoding.EncodeToString(sealed), nil
}

// Unobfuscate reverses Obfuscate. It rejects anything that is not a v1
// ciphertext, including values written by the previous XOR scheme, rather than
// returning plausible-looking plaintext.
func Unobfuscate(dst, keyMaterial string) (string, error) {
	if dst == "" {
		return "", nil
	}
	encoded, ok := strings.CutPrefix(dst, obfuscateVersion)
	if !ok {
		return "", fmt.Errorf("unsupported ciphertext format")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}
	aead, err := newAEAD(keyMaterial)
	if err != nil {
		return "", err
	}
	if len(raw) < aead.NonceSize() {
		return "", fmt.Errorf("ciphertext is too short")
	}
	nonce, ciphertext := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt ciphertext: %w", err)
	}
	return string(plaintext), nil
}

// newAEAD builds the AEAD used for stored credentials. An empty key material is
// an error: without it nothing may be written, since the alternative is storing
// a credential in a form the caller believes is encrypted.
func newAEAD(keyMaterial string) (cipher.AEAD, error) {
	if keyMaterial == "" {
		return nil, fmt.Errorf("encryption key is empty")
	}
	key := sha256.Sum256([]byte(keyMaterial))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
