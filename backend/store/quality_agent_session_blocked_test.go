package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A session may park only on the runtime: a budget window, a transient internal error, a concurrency
// limit. Everything else is a decision, which belongs to a case — a parked session silenced every
// target it covered and nothing could wake it.
func TestInfrastructureBlocksAreTheRetryableSet(t *testing.T) {
	t.Parallel()

	for _, code := range []string{"token-budget", "wake-budget", "internal-error", "concurrency-limit"} {
		require.True(t, QualityAgentSessionBlockedCodeInfrastructure(code), code)
	}
	for _, code := range []string{
		"RULES_NEVER_EVALUATED", "needs-data", "AWAITING_DRAFT_REVIEW", "EXECUTIONS_NOT_EVALUATED", "",
	} {
		require.False(t, QualityAgentSessionBlockedCodeInfrastructure(code), code)
	}
}
