package llm

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsContextOverflowBodyMatchesProviderPhrases(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		`{"error":{"code":"context_length_exceeded","message":"This model's maximum context length is 128000 tokens."}}`,
		`{"error":"the prompt is too long for this model"}`,
		`upstream says: too many tokens`,
		`{"message":"request too large"}`,
	} {
		require.True(t, isContextOverflowBody([]byte(body)), body)
	}
	for _, body := range []string{
		`{"error":"invalid api key"}`,
		`{"error":{"code":"rate_limit_exceeded"}}`,
		``,
	} {
		require.False(t, isContextOverflowBody([]byte(body)), body)
	}
}

func TestIsContextOverflowReadsTheClassifier(t *testing.T) {
	t.Parallel()

	require.True(t, IsContextOverflow(&ResponseError{Code: FailureContextOverflow}))
	require.True(t, IsContextOverflow(fmt.Errorf("wrapped: %w", &ResponseError{Code: FailureContextOverflow})))
	require.False(t, IsContextOverflow(&ResponseError{Code: FailureProviderStatus}))
	require.False(t, IsContextOverflow(errors.New("plain error")))
	require.False(t, IsContextOverflow(nil))
}
