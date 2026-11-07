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

// A transient block still collects the event; a paused or needs-human session does not.
func TestSessionAcceptsAgentEvents(t *testing.T) {
	t.Parallel()

	require.True(t, sessionAcceptsAgentEvents(&store.QualityAgentSessionMessage{
		State: storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED,
	}))
	require.True(t, sessionAcceptsAgentEvents(&store.QualityAgentSessionMessage{
		State:       storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED,
		BlockedCode: "token-budget",
	}))
	require.False(t, sessionAcceptsAgentEvents(&store.QualityAgentSessionMessage{
		State:       storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED,
		BlockedCode: "needs-human",
	}))
	require.False(t, sessionAcceptsAgentEvents(&store.QualityAgentSessionMessage{
		State: storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_PAUSED,
	}))
	require.False(t, sessionAcceptsAgentEvents(nil))
}
