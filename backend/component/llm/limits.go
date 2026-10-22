package llm

import "github.com/pkg/errors"

// The output-token budget is a property of the provider profile: the model is
// what caps output, so a modern model can comfortably accept a large ceiling while
// an older one rejects an oversized request. The bounds keep an accidental value
// from handing an unbounded generation to a background job.
const (
	// DefaultMaxOutputTokens is the budget a profile that sets none runs with.
	DefaultMaxOutputTokens = 32768
	// MinMaxOutputTokens is the smallest explicit budget a profile may set. A
	// value below it would truncate almost every structured answer.
	MinMaxOutputTokens = 256
	// MaxMaxOutputTokens is the largest explicit budget a profile may set.
	MaxMaxOutputTokens = 131072
)

// NormalizeMaxOutputTokens resolves a profile's configured budget: zero means the
// server default, and out-of-range values are clamped to the documented bounds.
// It is used when reading stored profiles, so an out-of-band write cannot make a
// request unbounded or absurdly small.
func NormalizeMaxOutputTokens(value int32) int {
	switch {
	case value <= 0:
		return DefaultMaxOutputTokens
	case value < MinMaxOutputTokens:
		return MinMaxOutputTokens
	case value > MaxMaxOutputTokens:
		return MaxMaxOutputTokens
	default:
		return int(value)
	}
}

// ValidateMaxOutputTokens checks a profile create/update value: zero means the
// default, and an explicit value must be within the supported range.
func ValidateMaxOutputTokens(value int32) error {
	if value == 0 {
		return nil
	}
	if value < MinMaxOutputTokens || value > MaxMaxOutputTokens {
		return errors.Errorf("max_output_tokens must be 0 (server default %d) or between %d and %d",
			DefaultMaxOutputTokens, MinMaxOutputTokens, MaxMaxOutputTokens)
	}
	return nil
}
