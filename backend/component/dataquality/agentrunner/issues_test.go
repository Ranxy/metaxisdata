package agentrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/tools/quality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The problem ledger is the record a FAIL lives in; the read has to carry the issue id
// the model reports, the rule it belongs to, and its current status.
func TestListIssuesProjectsTheProblemLedger(t *testing.T) {
	t.Parallel()

	executionID := int64(6451)
	stores := newFakeStore()
	stores.rules = []*store.QualityRuleMessage{{
		ID:         7,
		ResourceID: "MG95jGF8dc8U",
		Title:      "mail must look like a valid email address when present",
	}}
	stores.issues = []*store.QualityIssueMessage{{
		ResourceID:          "Rkur80IfwLi9",
		RuleID:              7,
		Status:              storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		Severity:            storepb.QualitySeverity_QUALITY_SEVERITY_WARNING,
		OccurrenceCount:     6,
		FirstSeenAt:         time.Date(2026, 11, 28, 4, 34, 32, 0, time.UTC),
		LastSeenAt:          time.Date(2026, 11, 28, 12, 11, 37, 0, time.UTC),
		LastSeenExecutionID: &executionID,
	}}
	engine := &Engine{store: stores, now: func() time.Time { return time.Unix(10_000, 0).UTC() }}

	result, err := engine.listIssues(context.Background(), quality.AgentSession{TargetGUID: "t"}, `{}`)
	require.NoError(t, err)
	require.Contains(t, result, `"issue":"qualityIssues/Rkur80IfwLi9"`)
	require.Contains(t, result, `"rule":"qualityRules/MG95jGF8dc8U"`)
	require.Contains(t, result, `"ruleTitle":"mail must look like a valid email address when present"`)
	require.Contains(t, result, `"status":"OPEN"`)
	require.Contains(t, result, `"severity":"WARNING"`)
	require.Contains(t, result, `"occurrenceCount":6`)
	require.Contains(t, result, `"lastSeenExecution":"qualityExecutions/6451"`)
	require.NotNil(t, stores.lastIssueFind)
	require.True(t, stores.lastIssueFind.OpenOnly, "OPEN means the unresolved problems, acknowledged included")
	require.Equal(t, 10, *stores.lastIssueFind.Limit)
}

// A resolved read is an explicit status filter, not the open predicate; the two are
// different questions and the ledger's own definition of open has to survive.
func TestListIssuesFiltersByStatus(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := &Engine{store: stores, now: func() time.Time { return time.Unix(10_000, 0).UTC() }}

	_, err := engine.listIssues(context.Background(), quality.AgentSession{TargetGUID: "t"}, `{"status":"RESOLVED","limit":3}`)
	require.NoError(t, err)
	require.False(t, stores.lastIssueFind.OpenOnly)
	require.Equal(t, []storepb.QualityIssueStatus{storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED}, stores.lastIssueFind.Statuses)
	require.Equal(t, 3, *stores.lastIssueFind.Limit)

	_, err = engine.listIssues(context.Background(), quality.AgentSession{TargetGUID: "t"}, `{"limit":50}`)
	require.NoError(t, err)
	require.Equal(t, maxAgentIssues, *stores.lastIssueFind.Limit)
}

func TestListIssuesRejectsAnUnknownStatus(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := &Engine{store: stores, now: func() time.Time { return time.Unix(10_000, 0).UTC() }}

	_, err := engine.listIssues(context.Background(), quality.AgentSession{TargetGUID: "t"}, `{"status":"BOGUS"}`)
	require.ErrorContains(t, err, "unknown issue status")
}

// The lead reads one named target's ledger; the target session reads its own.
func TestListIssuesLeadReadsTheNamedTarget(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.subagents = map[string]*store.QualityAgentSessionMessage{
		"orders": {TargetGUID: "t"},
	}
	engine := &Engine{store: stores, now: func() time.Time { return time.Unix(10_000, 0).UTC() }}
	lead := &store.QualityAgentSessionMessage{
		ID: 2, ResourceID: "lead", WorkspaceID: "ws", AgentID: 1,
		Role: storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_LEAD,
	}

	_, err := engine.executeTool(context.Background(), lead, stores.agent, "list_issues", `{"target":"orders"}`, &wakeControl{})
	require.NoError(t, err)
	require.NotNil(t, stores.lastIssueFind)
	require.Equal(t, "t", *stores.lastIssueFind.TargetGUID)
}
