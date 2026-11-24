package llm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Zero means the server default; an explicit value is clamped to the documented
// bounds when read, so a stored out-of-band number cannot make a request
// unbounded, absurdly small, or larger than the window.
func TestNormalizeModelLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		contextWindow   int32
		maxOutputTokens int32
		wantContext     int
		wantMaxOutput   int
	}{
		{name: "zero is the defaults", wantContext: DefaultContextWindow, wantMaxOutput: DefaultMaxOutputTokens},
		{name: "negative values are the defaults", contextWindow: -5, maxOutputTokens: -5, wantContext: DefaultContextWindow, wantMaxOutput: DefaultMaxOutputTokens},
		{name: "an explicit pair is kept", contextWindow: 131072, maxOutputTokens: 8192, wantContext: 131072, wantMaxOutput: 8192},
		{name: "small output clamps up", contextWindow: 131072, maxOutputTokens: MinMaxOutputTokens - 1, wantContext: 131072, wantMaxOutput: MinMaxOutputTokens},
		{name: "large output clamps down", contextWindow: MaxContextWindow, maxOutputTokens: MaxMaxOutputTokens + 1, wantContext: MaxContextWindow, wantMaxOutput: MaxMaxOutputTokens},
		{name: "an out-of-band maximum output equal to the window shrinks", contextWindow: 131072, maxOutputTokens: MaxMaxOutputTokens, wantContext: 131072, wantMaxOutput: 131071},
		{name: "context above the ceiling clamps", contextWindow: MaxContextWindow + 1, maxOutputTokens: 4096, wantContext: MaxContextWindow, wantMaxOutput: 4096},
		{name: "an out-of-band output equal to the window shrinks", contextWindow: 4096, maxOutputTokens: 8192, wantContext: 4096, wantMaxOutput: 4095},
		{name: "a tiny window never yields an unbounded output", contextWindow: 100, maxOutputTokens: 256, wantContext: 100, wantMaxOutput: MinMaxOutputTokens},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := NormalizeModelLimits(test.contextWindow, test.maxOutputTokens)
			require.Equal(t, test.wantContext, limits.ContextWindow)
			require.Equal(t, test.wantMaxOutput, limits.MaxOutputTokens)
		})
	}
}

// A model write accepts zero (default) and the documented bounds only, and the
// resolved pair must leave a positive message budget.
func TestValidateModelLimits(t *testing.T) {
	t.Parallel()

	valid := []struct {
		name            string
		contextWindow   int32
		maxOutputTokens int32
	}{
		{name: "both defaults", contextWindow: 0, maxOutputTokens: 0},
		{name: "explicit maximum output against the default window", contextWindow: 0, maxOutputTokens: MaxMaxOutputTokens},
		{name: "explicit pair", contextWindow: 131072, maxOutputTokens: 8192},
		{name: "smallest output", contextWindow: 131072, maxOutputTokens: MinMaxOutputTokens},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidateModelLimits(test.contextWindow, test.maxOutputTokens))
		})
	}

	invalid := []struct {
		name            string
		contextWindow   int32
		maxOutputTokens int32
		wantMessage     string
	}{
		{name: "negative window", contextWindow: -1, wantMessage: "context_window"},
		{name: "window above the ceiling", contextWindow: MaxContextWindow + 1, wantMessage: "context_window"},
		{name: "output below the minimum", contextWindow: 131072, maxOutputTokens: MinMaxOutputTokens - 1, wantMessage: "max_output_tokens"},
		{name: "output above the maximum", contextWindow: 262144, maxOutputTokens: MaxMaxOutputTokens + 1, wantMessage: "max_output_tokens"},
		{name: "output equals the window", contextWindow: 32768, maxOutputTokens: 32768, wantMessage: "context_window"},
		{name: "default output does not fit a small window", contextWindow: 16384, wantMessage: "context_window"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateModelLimits(test.contextWindow, test.maxOutputTokens)
			require.Error(t, err)
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}
}

// The compaction trigger mirrors DSH's compaction-basic policy:
// floor(min(W*0.8, W-O-min(65536, W/8))).
func TestCompactionTrigger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		contextWindow   int
		maxOutputTokens int
		want            int
	}{
		{name: "the pressure bound wins", contextWindow: DefaultContextWindow, maxOutputTokens: DefaultMaxOutputTokens, want: 196608},
		{name: "the window fraction wins", contextWindow: 131072, maxOutputTokens: 8192, want: 104857},
		{name: "no message budget left", contextWindow: 32768, maxOutputTokens: 32768, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, CompactionTrigger(test.contextWindow, test.maxOutputTokens))
		})
	}
}

func TestValidateModelNames(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateModelNames(nil))
	require.NoError(t, ValidateModelNames([]string{"gpt-4o", "gpt-4o-mini"}))

	err := ValidateModelNames([]string{"  "})
	require.Error(t, err)
	require.Contains(t, err.Error(), "models[0]")

	err = ValidateModelNames([]string{"gpt-4o", " gpt-4o "})
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate")
}
