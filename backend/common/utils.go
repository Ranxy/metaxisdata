//nolint:revive
package common

import (
	"crypto/rand"
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

// resourceIDFirstLetters and resourceIDLetters spell the alphabet a resource id
// may use: lowercase letters and digits, and the first character must be a
// letter so the id matches IsValidResourceID. RandomString cannot be used for a
// resource id: its alphabet includes uppercase letters and can start with a
// digit, which the name parsers reject.
var (
	resourceIDFirstLetters = []rune("abcdefghijklmnopqrstuvwxyz")
	resourceIDLetters      = []rune("abcdefghijklmnopqrstuvwxyz0123456789")
)

// RandomResourceID returns a random resource id of length n that
// IsValidResourceID accepts.
func RandomResourceID(n int) (string, error) {
	if n < 2 {
		return "", errors.New("a resource id needs at least two characters")
	}
	var sb strings.Builder
	sb.Grow(n)
	for i := 0; i < n; i++ {
		alphabet := resourceIDLetters
		if i == 0 {
			alphabet = resourceIDFirstLetters
		}
		randNum, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		if _, err := sb.WriteRune(alphabet[randNum.Uint64()]); err != nil {
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
