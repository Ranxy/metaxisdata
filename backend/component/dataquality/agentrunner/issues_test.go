package agentrunner

import (
	"context"
	"encoding/json"
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
		State:      storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED,
	}}
	stores.issues = []*store.QualityIssueMessage{{
		ResourceID:          "Rkur80IfwLi9",
		TargetGUID:          "t;db;public;orders",
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
	var payload struct {
		Target string `json:"target"`
		Issues []struct {
			Target string `json:"target"`
		} `json:"issues"`
	}
	require.NoError(t, json.Unmarshal([]byte(result), &payload))
	require.Equal(t, "t", payload.Target)
	require.Len(t, payload.Issues, 1)
	require.Equal(t, "t;db;public;orders", payload.Issues[0].Target, "a non-leaf read needs the row-level target")
	require.Contains(t, result, `"rule":"qualityRules/MG95jGF8dc8U"`, "an archived rule keeps its issue reference")
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
// different questions and the ledger's own definition of open has to survive. ALL is
// the only value that adds no predicate.
func TestListIssuesFiltersByStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		status   string
		openOnly bool
		statuses []storepb.QualityIssueStatus
	}{
		{"open", "OPEN", true, nil},
		{"acknowledged", "ACKNOWLEDGED", false, []storepb.QualityIssueStatus{storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED}},
		{"resolved", "RESOLVED", false, []storepb.QualityIssueStatus{storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED}},
		{"all", "ALL", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stores := newFakeStore()
			engine := &Engine{store: stores, now: func() time.Time { return time.Unix(10_000, 0).UTC() }}

			_, err := engine.listIssues(context.Background(), quality.AgentSession{TargetGUID: "t"}, `{"status":"`+tc.status+`"}`)
			require.NoError(t, err)
			require.Equal(t, tc.openOnly, stores.lastIssueFind.OpenOnly)
			require.Equal(t, tc.statuses, stores.lastIssueFind.Statuses)
		})
	}
}

func TestListIssuesClampsTheLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		limit string
		want  int
	}{
		{"default", `{}`, 10},
		{"explicit", `{"limit":3}`, 3},
		{"above-max", `{"limit":50}`, maxAgentIssues},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stores := newFakeStore()
			engine := &Engine{store: stores, now: func() time.Time { return time.Unix(10_000, 0).UTC() }}

			_, err := engine.listIssues(context.Background(), quality.AgentSession{TargetGUID: "t"}, tc.limit)
			require.NoError(t, err)
			require.Equal(t, tc.want, *stores.lastIssueFind.Limit)
		})
	}
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
	stores.enrollment = &store.QualityAgentEnrollmentMessage{State: storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE}
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

// A lead may name a target only if it is still in the agent's scope; a raw GUID
// outside the team must not become a cross-agent ledger read.
func TestListIssuesLeadRejectsAnUnenrolledTarget(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	stores.subagents = map[string]*store.QualityAgentSessionMessage{
		"orders": {TargetGUID: "t"},
	}
	shared := &store.QualityAgentSessionMessage{ID: 2, ResourceID: "lead", WorkspaceID: "ws", AgentID: 1,
		Role: storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_LEAD}
	stores.enrollment = &store.QualityAgentEnrollmentMessage{State: storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE}
	engine := &Engine{store: stores, now: func() time.Time { return time.Unix(10_000, 0).UTC() }}

	_, err := engine.executeTool(context.Background(), shared, stores.agent, "list_issues", `{"target":"orders"}`, &wakeControl{})
	require.NoError(t, err)

	// A raw target GUID is accepted when it is enrolled, and the lookup carries the
	// lead's workspace and agent.
	_, err = engine.executeTool(context.Background(), shared, stores.agent, "list_issues", `{"target":"t"}`, &wakeControl{})
	require.NoError(t, err)
	require.Equal(t, "ws", stores.lastEnrollmentWorkspace)
	require.Equal(t, int64(1), stores.lastEnrollmentAgent)
	require.Equal(t, "t", stores.lastEnrollmentTarget)

	stores.enrollment = &store.QualityAgentEnrollmentMessage{State: storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_OUT_OF_SCOPE}
	_, err = engine.executeTool(context.Background(), shared, stores.agent, "list_issues", `{"target":"orders"}`, &wakeControl{})
	require.ErrorContains(t, err, "no longer in this agent's scope")

	stores.enrollment = nil
	_, err = engine.executeTool(context.Background(), shared, stores.agent, "list_issues", `{"target":"orders"}`, &wakeControl{})
	require.ErrorContains(t, err, "is not enrolled")
}
