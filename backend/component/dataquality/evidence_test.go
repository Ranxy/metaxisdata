package dataquality

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The projection is the whole difference between "execution 4339 errored" and
// "check 460 bound a value the database refused": it must carry the check, the
// stable failure class and the engine's own message, and must not invent a value
// or a finish time the execution does not have.
func TestBuildExecutionEvidence(t *testing.T) {
	t.Parallel()

	finished := time.Date(2026, 11, 26, 6, 8, 55, 0, time.UTC)
	rules := []*store.QualityRuleMessage{{
		ID:         460,
		ResourceID: "r460",
		Title:      "performed_at must be recent and not in the future",
	}}
	executions := []*store.QualityExecutionMessage{{
		ID:           4339,
		RuleID:       460,
		RuleRevision: 3,
		Status:       storepb.QualityStatus_QUALITY_STATUS_ERROR,
		ErrorCode:    ErrCodeBadValue,
		ErrorMessage: `invalid input syntax for type timestamp with time zone: "now - 1 day"`,
		Trigger:      storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		Attempt:      2,
		StartedAt:    finished.Add(-12 * time.Millisecond),
		FinishedAt:   finished,
		DurationMS:   12,
	}, {
		ID:        4340,
		RuleID:    999,
		Status:    storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		StartedAt: finished,
	}}

	evidence := BuildExecutionEvidence(executions, rules)
	require.Len(t, evidence, 2)

	first := evidence[0]
	require.Equal(t, "qualityExecutions/4339", first.Execution)
	require.Equal(t, "qualityRules/r460", first.Rule)
	require.Equal(t, "performed_at must be recent and not in the future", first.Title)
	require.Equal(t, "ERROR", first.Status)
	require.Equal(t, ErrCodeBadValue, first.ErrorCode)
	require.Contains(t, first.ErrorMessage, "now - 1 day")
	require.Equal(t, int32(3), first.Revision)
	require.Equal(t, "MANUAL", first.Trigger)
	require.Equal(t, int32(2), first.Attempt)
	require.Equal(t, finished, first.FinishedAt)
	require.Nil(t, first.ObservedValue)

	// An execution whose rule row is gone still renders: the execution is the fact.
	second := evidence[1]
	require.Empty(t, second.Rule)
	require.True(t, second.FinishedAt.IsZero())
	require.Equal(t, "SKIPPED", second.Status)
}

// A driver message is bounded at 1 KiB in the row; the evidence keeps the sentence
// that names the fault and drops the rest, so one verbose failure cannot crowd the
// tool result.
func TestBuildExecutionEvidenceTruncatesTheMessage(t *testing.T) {
	t.Parallel()

	message := strings.Repeat("x", maxEvidenceErrorMessageBytes*2)
	evidence := BuildExecutionEvidence([]*store.QualityExecutionMessage{{
		ID:           1,
		Status:       storepb.QualityStatus_QUALITY_STATUS_ERROR,
		ErrorMessage: message,
	}}, nil)
	require.Len(t, evidence, 1)
	require.LessOrEqual(t, len(evidence[0].ErrorMessage), maxEvidenceErrorMessageBytes)
}
