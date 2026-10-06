//nolint:revive
package common

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
	"strings"
	"unicode/utf8"

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

// TruncateUTF8Bytes cuts a string to at most limit bytes without splitting a
// multi-byte rune. Cutting mid-rune yields invalid UTF-8, which protobuf and
// JSON encoders reject outright, so a bound that slices bytes naively can drop
// the very record it was added to protect.
func TruncateUTF8Bytes(str string, limit int) string {
	if len(str) <= limit {
		return str
	}
	end := limit
	for end > 0 && !utf8.RuneStart(str[end]) {
		end--
	}
	return str[:end]
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

// Obfuscate obfuscates a string with a seed string. An empty seed is rejected
// instead of dividing by zero, and an empty input stays empty.
func Obfuscate(src, seed string) (string, error) {
	if src == "" {
		return "", nil
	}
	if seed == "" {
		return "", errors.New("cannot obfuscate with an empty seed")
	}
	srcBytes, seedBytes := []byte(src), []byte(seed)
	obfuscated := make([]byte, len(srcBytes))
	for i, b := range srcBytes {
		obfuscated[i] = b ^ seedBytes[i%len(seedBytes)]
	}
	return base64.StdEncoding.EncodeToString(obfuscated), nil
}

// Unobfuscate unobfuscates a string with a seed string.
func Unobfuscate(dst, seed string) (string, error) {
	if dst == "" {
		return "", nil
	}
	if seed == "" {
		return "", errors.New("cannot unobfuscate with an empty seed")
	}
	obfuscated, err := base64.StdEncoding.DecodeString(dst)
	if err != nil {
		return "", err
	}
	unobfuscated, seedBytes := make([]byte, len(obfuscated)), []byte(seed)
	for i, b := range obfuscated {
		unobfuscated[i] = b ^ seedBytes[i%len(seedBytes)]
	}
	return string(unobfuscated), nil
}
