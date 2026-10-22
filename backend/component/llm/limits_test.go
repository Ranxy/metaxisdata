package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Zero means the server default; an explicit value is clamped to the documented
// bounds when read, so a stored out-of-band number cannot make a request
// unbounded or absurdly small.
func TestNormalizeMaxOutputTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value int32
		want  int
	}{
		{name: "zero is the default", value: 0, want: DefaultMaxOutputTokens},
		{name: "negative is the default", value: -5, want: DefaultMaxOutputTokens},
		{name: "below the minimum clamps up", value: MinMaxOutputTokens - 1, want: MinMaxOutputTokens},
		{name: "minimum is kept", value: MinMaxOutputTokens, want: MinMaxOutputTokens},
		{name: "an explicit value is kept", value: 4096, want: 4096},
		{name: "maximum is kept", value: MaxMaxOutputTokens, want: MaxMaxOutputTokens},
		{name: "above the maximum clamps down", value: MaxMaxOutputTokens + 1, want: MaxMaxOutputTokens},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, NormalizeMaxOutputTokens(test.value))
		})
	}
}

// A profile write accepts zero (default) and the documented range only.
func TestValidateMaxOutputTokens(t *testing.T) {
	t.Parallel()

	for _, valid := range []int32{0, MinMaxOutputTokens, 4096, MaxMaxOutputTokens} {
		require.NoError(t, ValidateMaxOutputTokens(valid), "value %d", valid)
	}
	for _, invalid := range []int32{-1, MinMaxOutputTokens - 1, MaxMaxOutputTokens + 1} {
		err := ValidateMaxOutputTokens(invalid)
		require.Error(t, err, "value %d", invalid)
		require.Contains(t, err.Error(), "max_output_tokens")
	}
}
