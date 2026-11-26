package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func verificationExecution(state storepb.QualityExecutionState, status storepb.QualityStatus, totalRows int64) *store.QualityExecutionMessage {
	return &store.QualityExecutionMessage{ID: 4451, State: state, Status: status, TotalRows: totalRows}
}

// Verification is a comparison, not an opinion: the case's class against the class of
// the run that was supposed to fix it. A verdict closes the case, whatever the verdict
// says — a FAIL is a working check.
func TestVerifyCaseReadsTheRunDeterministically(t *testing.T) {
	t.Parallel()

	completed := storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED
	enabled := storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
	cases := []struct {
		name  string
		input VerificationInput
		want  VerificationOutcome
	}{
		{
			name: "a verdict closes the case",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, RuleState: enabled,
				Execution: verificationExecution(completed, storepb.QualityStatus_QUALITY_STATUS_FAIL, 4)},
			want: VerifyResolved,
		},
		{
			name: "the same class spends an attempt",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, Attempts: 0, RuleState: enabled,
				Execution: verificationExecution(completed, storepb.QualityStatus_QUALITY_STATUS_ERROR, 0), NewClass: ClassConfigValueInvalid},
			want: VerifyRetry,
		},
		{
			name: "the attempt budget freezes the case",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, Attempts: 1, RuleState: enabled,
				Execution: verificationExecution(completed, storepb.QualityStatus_QUALITY_STATUS_ERROR, 0), NewClass: ClassConfigValueInvalid},
			want: VerifyFrozen,
		},
		{
			name: "a different class re-diagnoses without spending an attempt",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, Attempts: 1, RuleState: enabled,
				Execution: verificationExecution(completed, storepb.QualityStatus_QUALITY_STATUS_ERROR, 0), NewClass: ClassSourceFault},
			want: VerifyReopen,
		},
		{
			name: "an unfinished run waits and spends nothing",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, RuleState: enabled,
				Execution: verificationExecution(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, 0)},
			want: VerifyRearm,
		},
		{
			name: "an abandoned run waits and spends nothing",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, RuleState: enabled,
				Execution: verificationExecution(storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, 0)},
			want: VerifyRearm,
		},
		{
			name: "an empty target stays an answer when that was the case",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassNoVerdictEmpty, RuleState: enabled,
				Execution: verificationExecution(completed, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 0), NewClass: ClassNoVerdictEmpty},
			want: VerifyResolved,
		},
		{
			name: "an empty target does not resolve a different case",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, RuleState: enabled,
				Execution: verificationExecution(completed, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 0), NewClass: ClassNoVerdictEmpty},
			want: VerifyReopen,
		},
		{
			name: "a paused check cannot be verified, so it escalates",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, RuleState: storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED,
				Execution: verificationExecution(completed, storepb.QualityStatus_QUALITY_STATUS_PASS, 1)},
			want: VerifyEscalated,
		},
		{
			name: "an archived check closes its case",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, RuleState: storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED,
				Execution: verificationExecution(completed, storepb.QualityStatus_QUALITY_STATUS_PASS, 1)},
			want: VerifyResolved,
		},
		{
			name:  "no run yet waits",
			input: VerificationInput{State: store.QualityAgentCaseStateActed, Class: ClassConfigValueInvalid, RuleState: enabled},
			want:  VerifyRearm,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			verification := VerifyCase(test.input)
			require.Equal(t, test.want, verification.Outcome)
			require.NotEmpty(t, verification.Reason, "every outcome explains itself")
			if test.want == VerifyRearm || test.want == VerifyReopen {
				require.Equal(t, test.input.Attempts, verification.Attempts, "waiting and re-diagnosing spend no attempt")
			}
			if test.want == VerifyFrozen {
				require.Equal(t, int32(2), verification.Attempts)
			}
		})
	}
}
