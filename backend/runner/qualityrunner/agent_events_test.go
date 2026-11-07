package qualityrunner

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The event message carries the identifiers and numbers a conclusion must cite, and no
// sample rows.
func TestExecutionEventContentCitesTheExecution(t *testing.T) {
	t.Parallel()

	failed := &ruleExecution{
		result: &store.QualityExecutionMessage{ID: 123, TargetGUID: "target", Status: storepb.QualityStatus_QUALITY_STATUS_FAIL, FailedRows: 12},
		head:   &store.QualityRuleMessage{Title: "orders row count"},
	}
	content := executionEventContent(failed)
	require.Contains(t, content, "qualityExecutions/123")
	require.Contains(t, content, "orders row count")
	require.Contains(t, content, "12 failed rows")
	require.Contains(t, content, "[event]")

	errored := &ruleExecution{
		result: &store.QualityExecutionMessage{ID: 7, Status: storepb.QualityStatus_QUALITY_STATUS_ERROR, ErrorCode: "NO_READ_ONLY_SOURCE"},
	}
	content = executionEventContent(errored)
	require.Contains(t, content, "qualityExecutions/7")
	require.Contains(t, content, "NO_READ_ONLY_SOURCE")
	require.Contains(t, content, "ERROR")
}
