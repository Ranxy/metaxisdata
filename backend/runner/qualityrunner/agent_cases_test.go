package qualityrunner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func caseRow(state store.QualityAgentCaseState, attempts int32) *store.QualityAgentCaseMessage {
	evidence := int64(4339)
	return &store.QualityAgentCaseMessage{
		ResourceID:   "case-1",
		State:        state,
		FailureClass: string(dataquality.ClassConfigValueInvalid),
		Attempts:     attempts,
		MaxAttempts:  2,
		ExecutionID:  &evidence,
	}
}

func caseExecution(id int64, state storepb.QualityExecutionState, status storepb.QualityStatus) *store.QualityExecutionMessage {
	return &store.QualityExecutionMessage{ID: id, State: state, Status: status}
}

// The verification run decides the case without asking a model: a verdict closes it, the same
// class spends an attempt, a different class re-diagnoses, and a run that has not finished
// waits. Only a case that has a repair pending is verified, so a diagnosed case cannot burn an
// attempt before the agent has changed anything.
func TestCaseResolutionReadsTheVerificationRun(t *testing.T) {
	t.Parallel()

	completed := storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED
	enabled := &store.QualityRuleMessage{State: storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED}
	acted := store.QualityAgentCaseStateActed
	class := dataquality.ClassConfigValueInvalid
	verdict := &dataquality.Diagnosis{Class: dataquality.ClassDataFailure}
	sameClass := &dataquality.Diagnosis{Class: class}
	otherClass := &dataquality.Diagnosis{Class: dataquality.ClassSourceFault}

	cases := []struct {
		name        string
		open        *store.QualityAgentCaseMessage
		rule        *store.QualityRuleMessage
		execution   *store.QualityExecutionMessage
		diagnosis   *dataquality.Diagnosis
		want        dataquality.VerificationOutcome
		wantAttempt int32
	}{
		{
			name: "a verdict closes the case", open: caseRow(acted, 0), rule: enabled,
			execution: caseExecution(4451, completed, storepb.QualityStatus_QUALITY_STATUS_FAIL), diagnosis: verdict,
			want: dataquality.VerifyResolved,
		},
		{
			name: "the same class spends one attempt", open: caseRow(acted, 0), rule: enabled,
			execution: caseExecution(4451, completed, storepb.QualityStatus_QUALITY_STATUS_ERROR), diagnosis: sameClass,
			want: dataquality.VerifyRetry, wantAttempt: 1,
		},
		{
			name: "the last attempt freezes the case", open: caseRow(acted, 1), rule: enabled,
			execution: caseExecution(4451, completed, storepb.QualityStatus_QUALITY_STATUS_ERROR), diagnosis: sameClass,
			want: dataquality.VerifyFrozen, wantAttempt: 2,
		},
		{
			name: "a different class re-diagnoses", open: caseRow(acted, 1), rule: enabled,
			execution: caseExecution(4451, completed, storepb.QualityStatus_QUALITY_STATUS_ERROR), diagnosis: otherClass,
			want: dataquality.VerifyReopen, wantAttempt: 1,
		},
		{
			name: "a paused check cannot be verified", open: caseRow(acted, 0),
			rule:      &store.QualityRuleMessage{State: storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED},
			execution: caseExecution(4451, completed, storepb.QualityStatus_QUALITY_STATUS_PASS), diagnosis: verdict,
			want: dataquality.VerifyEscalated,
		},
		{
			name: "a run that has not finished waits", open: caseRow(acted, 0), rule: enabled,
			execution: caseExecution(4451, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED),
			want:      dataquality.VerifyRearm,
		},
		{
			name: "a terminal case is never reopened", open: caseRow(store.QualityAgentCaseStateFrozen, 2), rule: enabled,
			execution: caseExecution(4451, completed, storepb.QualityStatus_QUALITY_STATUS_PASS), diagnosis: verdict,
			want: dataquality.VerifyRearm, wantAttempt: 2,
		},
		{
			name: "the case's own evidence is not a verification", open: caseRow(acted, 0), rule: enabled,
			execution: caseExecution(4339, completed, storepb.QualityStatus_QUALITY_STATUS_ERROR), diagnosis: sameClass,
			want: dataquality.VerifyRearm,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			verification := caseResolution(test.open, test.rule, test.execution, test.diagnosis)
			require.Equal(t, test.want, verification.Outcome)
			require.Equal(t, test.wantAttempt, verification.Attempts)
			require.NotEmpty(t, verification.Reason)
		})
	}
}

// The transition maps an outcome onto the row: terminal outcomes clear the next look, a retry
// schedules one a minute out so a just-failed case does not spin the three-second tick, and a
// run that has not finished writes nothing.
func TestCaseTransition(t *testing.T) {
	t.Parallel()

	now := time.Unix(10_000, 0).UTC()
	for _, test := range []struct {
		outcome  dataquality.VerificationOutcome
		state    store.QualityAgentCaseState
		wantNext bool
		apply    bool
	}{
		{dataquality.VerifyResolved, store.QualityAgentCaseStateResolved, false, true},
		{dataquality.VerifyFrozen, store.QualityAgentCaseStateFrozen, false, true},
		{dataquality.VerifyEscalated, store.QualityAgentCaseStateEscalated, false, true},
		{dataquality.VerifyRetry, store.QualityAgentCaseStateDiagnosed, true, true},
		{dataquality.VerifyReopen, store.QualityAgentCaseStateDiagnosed, true, true},
		{dataquality.VerifyRearm, store.QualityAgentCaseStateUnspecified, false, false},
	} {
		state, next, apply := caseTransition(dataquality.Verification{Outcome: test.outcome}, now)
		require.Equal(t, test.state, state, string(test.outcome))
		require.Equal(t, test.apply, apply, string(test.outcome))
		if test.wantNext {
			require.NotNil(t, next, string(test.outcome))
			require.WithinDuration(t, now.Add(time.Minute), *next, time.Second)
		} else {
			require.Nil(t, next, string(test.outcome))
		}
	}
}

// The wake prompt renders the case summary, and the model resolves the check by it. A summary
// that did not name the check would leave several open cases on one target indistinguishable.
func TestCaseSummaryNamesTheCheck(t *testing.T) {
	t.Parallel()

	rule := &store.QualityRuleMessage{ID: 460, ResourceID: "r460", Title: "performed_at must be recent"}
	diagnosis := &dataquality.Diagnosis{Class: dataquality.ClassConfigValueInvalid, Summary: "the target database refused a bound value"}
	summary := caseSummary(rule, diagnosis)
	require.Contains(t, summary, `check "performed_at must be recent"`)
	require.Contains(t, summary, "qualityRules/r460")
	require.Contains(t, summary, "refused a bound value")

	require.Empty(t, caseSummary(nil, diagnosis))
	require.Empty(t, caseSummary(rule, nil))
}
