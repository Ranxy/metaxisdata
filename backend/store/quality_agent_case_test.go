package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// A case row must round-trip the nullable subject: a rule-level case carries its rule and
// execution, a target-level case carries neither, and neither may read back as zero.
func TestScanQualityAgentCaseMapsTheNullableSubject(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	ruleID, executionID, previous := int64(460), int64(4339), int64(7)
	nextAction := now.Add(time.Minute)
	values := []any{
		int64(9), "case-res", "ws", int64(101), "inst;app;public;orders", int16(storepb.MetaType_TABLE),
		sql.NullInt64{Int64: ruleID, Valid: true},
		"inst;app;public;orders|460|EXECUTION",
		"instances/main", "EXECUTION", "", "range.min", int16(QualityAgentCaseStateDiagnosed),
		int32(1), int32(2), sql.NullInt64{Int64: executionID, Valid: true},
		"the target database refused a bound value", sql.NullTime{Time: nextAction, Valid: true},
		"", sql.NullInt64{Int64: previous, Valid: true}, now, now,
	}
	message, err := scanQualityAgentCase(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Equal(t, int64(9), message.ID)
	require.Equal(t, storepb.MetaType_TABLE, message.TargetType)
	require.NotNil(t, message.RuleID)
	require.Equal(t, ruleID, *message.RuleID)
	require.Equal(t, QualityAgentCaseStateDiagnosed, message.State)
	require.NotNil(t, message.ExecutionID)
	require.Equal(t, executionID, *message.ExecutionID)
	require.NotNil(t, message.NextActionAt)
	require.Equal(t, nextAction, *message.NextActionAt)
	require.NotNil(t, message.PreviousCase)
	require.Equal(t, "range.min", message.Field)
	require.False(t, message.State.Terminal())
}

// A target-level case has no rule and no execution. Reading those as zero would make the
// subject key "inst;0|0|EXECUTION", which is a different (and false) identity.
func TestScanQualityAgentCaseLeavesAnAbsentSubjectEmpty(t *testing.T) {
	t.Parallel()

	now := time.Unix(1000, 0).UTC()
	values := []any{
		int64(10), "case-res", "ws", int64(101), "inst;app;public;orders", int16(storepb.MetaType_VIEW),
		sql.NullInt64{}, "inst;app;public;orders|-|COVERAGE", "", "COVERAGE", "", "",
		int16(QualityAgentCaseStateOpen), int32(0), int32(2), sql.NullInt64{}, "",
		sql.NullTime{}, "", sql.NullInt64{}, now, now,
	}
	message, err := scanQualityAgentCase(&fakeScanner{values: values})
	require.NoError(t, err)
	require.Nil(t, message.RuleID)
	require.Nil(t, message.ExecutionID)
	require.Nil(t, message.NextActionAt)
	require.Nil(t, message.PreviousCase)
}

// The subject key is what an open case claims, so its shape is the uniqueness invariant:
// the same target and kind with different rules are different subjects, and a missing rule
// is a subject of its own rather than the zero one.
func TestQualityAgentCaseSubjectKey(t *testing.T) {
	t.Parallel()

	ruleID := int64(460)
	require.Equal(t, "t|460|EXECUTION", QualityAgentCaseSubjectKey("t", &ruleID, QualityAgentCaseKindExecution))
	require.Equal(t, "t|-|COVERAGE", QualityAgentCaseSubjectKey("t", nil, "COVERAGE"))
	require.NotEqual(t,
		QualityAgentCaseSubjectKey("t", nil, QualityAgentCaseKindExecution),
		QualityAgentCaseSubjectKey("t", &ruleID, QualityAgentCaseKindExecution))
}

// Every state's label and terminal flag are rendered by the transcript, the report and the
// case view, so both are pinned here: a new state that forgets one is a blank badge.
func TestQualityAgentCaseStateLabels(t *testing.T) {
	t.Parallel()

	cases := map[QualityAgentCaseState]struct {
		name     string
		terminal bool
	}{
		QualityAgentCaseStateOpen:      {"OPEN", false},
		QualityAgentCaseStateDiagnosed: {"DIAGNOSED", false},
		QualityAgentCaseStateActed:     {"ACTED", false},
		QualityAgentCaseStateAwaiting:  {"AWAITING_VERIFICATION", false},
		QualityAgentCaseStateResolved:  {"RESOLVED", true},
		QualityAgentCaseStateFrozen:    {"FROZEN", true},
		QualityAgentCaseStateEscalated: {"ESCALATED", true},
	}
	for state, want := range cases {
		require.Equal(t, want.name, state.Name(), state)
		require.Equal(t, want.terminal, state.Terminal(), state)
	}
	require.Empty(t, QualityAgentCaseStateUnspecified.Name())
	require.False(t, QualityAgentCaseStateUnspecified.Terminal())
}
