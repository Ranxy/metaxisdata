package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// The insert carries the context a failure diagnostic needs to be correlated with
// the call: the feature kind, the caller's correlation id and the classifier code.
func TestInsertLLMDebugLogQueryBindsTheContextColumns(t *testing.T) {
	t.Parallel()

	require.Contains(t, insertLLMDebugLogQuery, "INSERT INTO llm_debug_log")
	for _, column := range []string{"provider", "model", "kind", "correlation_id", "error_code", "request_body", "response_body", "created_at"} {
		require.Contains(t, insertLLMDebugLogQuery, column)
	}
	args := []any{"p", "m", "dq_rule_draft", "corr-1", "TRUNCATED", "{}", "{}", time.Now()}
	requirePlaceholdersMatchArgs(t, insertLLMDebugLogQuery, args)
}

// The retrieval path is the indexed correlation id, newest row first: a retried
// write must not make the read ambiguous.
func TestGetLLMDebugLogByCorrelationQueryUsesTheIndexedPath(t *testing.T) {
	t.Parallel()

	require.Contains(t, getLLMDebugLogByCorrelationQuery, "WHERE correlation_id = $1")
	require.Contains(t, getLLMDebugLogByCorrelationQuery, "ORDER BY id DESC")
	require.Contains(t, getLLMDebugLogByCorrelationQuery, "LIMIT 1")
	requirePlaceholdersMatchArgs(t, getLLMDebugLogByCorrelationQuery, []any{"corr-1"})
}

// The classifier is stamped onto the row after the failure is classified: the
// asynchronous writer may have written it first, so the update keys on the indexed
// correlation id instead of the row id.
func TestSetLLMDebugLogErrorCodeQueryUsesTheCorrelationID(t *testing.T) {
	t.Parallel()

	require.Contains(t, setLLMDebugLogErrorCodeQuery, "UPDATE llm_debug_log")
	require.Contains(t, setLLMDebugLogErrorCodeQuery, "SET error_code = $2")
	require.Contains(t, setLLMDebugLogErrorCodeQuery, "WHERE correlation_id = $1")
	requirePlaceholdersMatchArgs(t, setLLMDebugLogErrorCodeQuery, []any{"corr-1", "TRUNCATED"})
}

// A nil entry and an empty correlation id are refused before the store touches the
// database, so a caller cannot write a row nothing can find.
func TestLLMDebugLogRejectsUnusableInput(t *testing.T) {
	t.Parallel()

	stores := &Store{}
	err := stores.InsertLLMDebugLog(context.Background(), nil)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	entry, err := stores.GetLLMDebugLogByCorrelation(context.Background(), "  ")
	require.NoError(t, err)
	require.Nil(t, entry)

	// Updating without an id or a code is a no-op, not an error and not a write.
	require.NoError(t, stores.SetLLMDebugLogErrorCode(context.Background(), "  ", "TRUNCATED"))
	require.NoError(t, stores.SetLLMDebugLogErrorCode(context.Background(), "corr-1", "  "))
}
