package qualityrunner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The batch-level health intake must speak only about checks that ran without
// concluding: a FAIL or an ERROR already has an execution event, and repeating it as
// "no verdict" would wake the agent twice for one fact.
func TestHealthFindingsOnlyReportsChecksThatRanWithoutConcluding(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	enabled := storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
	rule := func(id int64, title string, state storepb.QualityRuleState) *store.QualityRuleMessage {
		return &store.QualityRuleMessage{ID: id, ResourceID: "r", Title: title, State: state, CreatedAt: now.Add(-30 * 24 * time.Hour)}
	}
	execution := func(ruleID int64, status storepb.QualityStatus, total int64) *store.QualityExecutionMessage {
		return &store.QualityExecutionMessage{ID: 10 + ruleID, RuleID: ruleID, Status: status, TotalRows: total, FinishedAt: now}
	}

	findings := healthFindings([]healthAssessment{
		{Rule: rule(1, "passing", enabled), Execution: execution(1, storepb.QualityStatus_QUALITY_STATUS_PASS, 1)},
		{Rule: rule(2, "failing", enabled), Execution: execution(2, storepb.QualityStatus_QUALITY_STATUS_FAIL, 2)},
		{Rule: rule(3, "erroring", enabled), Execution: execution(3, storepb.QualityStatus_QUALITY_STATUS_ERROR, 0)},
		{Rule: rule(4, "skipped", enabled), Execution: execution(4, storepb.QualityStatus_QUALITY_STATUS_SKIPPED, 0)},
		{Rule: rule(5, "paused", storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED), Execution: execution(5, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 0)},
		{Rule: rule(6, "empty target", enabled), Execution: execution(6, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 0)},
		{Rule: rule(7, "filtered to nothing", enabled), Execution: execution(7, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 4)},
	}, now)

	require.Len(t, findings, 2)

	empty := findings[0]
	require.Equal(t, int64(6), empty.RuleID)
	require.Equal(t, "NO_VERDICT_EMPTY", string(empty.Class))
	require.Contains(t, empty.Summary, "empty target")
	require.Contains(t, empty.Summary, "no verdict")
	require.Equal(t, "health:NO_VERDICT_EMPTY:6:2026-11-26", empty.Key)

	never := findings[1]
	require.Equal(t, int64(7), never.RuleID)
	require.Equal(t, "NEVER_JUDGED", string(never.Class))
	require.Equal(t, "qualityRules/r", never.Rule)
}

// One check's class per day is one event: a target that runs every five minutes must
// not append the same finding 288 times.
func TestHealthFindingKeysAreDailyAndPerCheck(t *testing.T) {
	t.Parallel()

	enabled := storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
	day := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	rule := &store.QualityRuleMessage{ID: 9, ResourceID: "r9", Title: "empty", State: enabled, CreatedAt: day.Add(-30 * 24 * time.Hour)}
	execution := &store.QualityExecutionMessage{ID: 99, RuleID: 9, Status: storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, FinishedAt: day}

	first := healthFindings([]healthAssessment{{Rule: rule, Execution: execution}}, day)
	later := healthFindings([]healthAssessment{{Rule: rule, Execution: execution}}, day.Add(6*time.Hour))
	nextDay := healthFindings([]healthAssessment{{Rule: rule, Execution: execution}}, day.Add(24*time.Hour))
	require.Len(t, first, 1)
	require.Equal(t, first[0].Key, later[0].Key, "the same UTC day reuses the key")
	require.NotEqual(t, first[0].Key, nextDay[0].Key, "a new day is a new event")
}
