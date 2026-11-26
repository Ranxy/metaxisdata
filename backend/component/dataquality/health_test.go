package dataquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func healthRule(state storepb.QualityRuleState, createdAt time.Time) *store.QualityRuleMessage {
	return &store.QualityRuleMessage{ID: 1, ResourceID: "r", Title: "a check", State: state, CreatedAt: createdAt}
}

func healthExecution(ruleID int64, status storepb.QualityStatus, totalRows int64, finishedAt time.Time) *store.QualityExecutionMessage {
	return &store.QualityExecutionMessage{
		ID: 10 + ruleID, RuleID: ruleID, Status: status, TotalRows: totalRows, FinishedAt: finishedAt,
	}
}

// Health answers one question the failure path cannot: is this check able to conclude
// at all? The sample's audit target had ten executions that were all NOT_EVALUATED on
// an empty table, which the agent could only describe as "the evaluation pipeline seems
// broken". The classes below are the difference between that guess and an answer.
func TestDiagnoseCheckHealth(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	enabled := storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED
	paused := storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED
	old := now.Add(-30 * 24 * time.Hour)
	young := now.Add(-5 * time.Minute)
	cases := []struct {
		name       string
		input      HealthInput
		wantClass  HealthClass
		wantReason string
	}{
		{
			name: "a verdict inside the window is healthy, whatever it says",
			input: HealthInput{Rule: healthRule(enabled, old), Now: now, Executions: []*store.QualityExecutionMessage{
				healthExecution(1, storepb.QualityStatus_QUALITY_STATUS_FAIL, 3, now.Add(-time.Hour)),
			}},
			wantClass: HealthHealthy,
		},
		{
			name: "an empty target is not a broken check",
			input: HealthInput{Rule: healthRule(enabled, old), Now: now, Executions: []*store.QualityExecutionMessage{
				healthExecution(1, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 0, now.Add(-time.Hour)),
			}},
			wantClass:  HealthNoVerdictEmpty,
			wantReason: "no rows",
		},
		{
			name: "ran with rows but still no verdict",
			input: HealthInput{Rule: healthRule(enabled, old), Now: now, Executions: []*store.QualityExecutionMessage{
				healthExecution(1, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 5, now.Add(-time.Hour)),
			}},
			wantClass: HealthNeverJudged,
		},
		{
			name:      "a check that never ran inside its first stall window is pending",
			input:     HealthInput{Rule: healthRule(enabled, young), Now: now, Cadence: time.Hour},
			wantClass: HealthPending,
		},
		{
			name:       "a check that never ran and is past its stall window is stalled",
			input:      HealthInput{Rule: healthRule(enabled, old), Now: now, Cadence: time.Hour},
			wantClass:  HealthStalled,
			wantReason: "never run",
		},
		{
			name: "no completion inside the stall window is stale",
			input: HealthInput{Rule: healthRule(enabled, old), Now: now, Cadence: time.Hour,
				Executions: []*store.QualityExecutionMessage{
					healthExecution(1, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 5, now.Add(-3*time.Hour)),
				}},
			wantClass: HealthStalled,
		},
		{
			name: "a verdict outside the lookback is not a working check",
			input: HealthInput{Rule: healthRule(enabled, old), Now: now,
				Executions: []*store.QualityExecutionMessage{
					healthExecution(1, storepb.QualityStatus_QUALITY_STATUS_PASS, 1, now.Add(-10*24*time.Hour)),
				}},
			wantClass: HealthNeverJudged,
		},
		{
			name: "another rule's executions are ignored",
			input: HealthInput{Rule: healthRule(enabled, old), Now: now,
				Executions: []*store.QualityExecutionMessage{
					healthExecution(2, storepb.QualityStatus_QUALITY_STATUS_PASS, 1, now.Add(-time.Hour)),
				}},
			wantClass: HealthPending,
		},
		{
			name:      "a paused check is not assessed",
			input:     HealthInput{Rule: healthRule(paused, old), Now: now},
			wantClass: "",
		},
		{
			name:      "no reference instant means nothing to say",
			input:     HealthInput{Rule: healthRule(enabled, old)},
			wantClass: "",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			health := DiagnoseCheckHealth(test.input)
			require.Equal(t, test.wantClass, health.Class)
			if test.wantReason != "" {
				require.Contains(t, health.Reason, test.wantReason)
			}
			if health.Class != "" {
				require.NotEmpty(t, health.Reason, "every assessed class explains itself")
			}
		})
	}
}

// A verdict is a conclusion about the data; ERROR and SKIPPED are monitoring states and
// must never be counted as one.
func TestHealthCountsOnlyJudgedVerdicts(t *testing.T) {
	t.Parallel()

	for _, status := range []storepb.QualityStatus{
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
	} {
		require.False(t, isJudgedVerdict(status), status.String())
	}
	for _, status := range []storepb.QualityStatus{
		storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
	} {
		require.True(t, isJudgedVerdict(status), status.String())
	}
}

// The sweep and the batch path must answer the stall question the same way. The sweep reads only
// two instants — when the check was saved and when it last completed — because it starts from the
// schedule rather than from an execution; the batch path holds the executions. One rule, one
// answer, or an agent would be told a check is stalled by one reader and healthy by the other.
func TestDiagnoseStaleCheckAgreesWithTheBatchStallRule(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	cadence := 2 * time.Hour
	cases := []struct {
		name        string
		createdAt   time.Time
		completedAt time.Time
		wantClass   HealthClass
	}{
		{"never ran and old enough to have", now.Add(-30 * 24 * time.Hour), time.Time{}, HealthStalled},
		{"never ran but still young", now.Add(-time.Minute), time.Time{}, HealthPending},
		{"completed inside the window", now.Add(-30 * 24 * time.Hour), now.Add(-time.Hour), ""},
		{"completed outside the window", now.Add(-30 * 24 * time.Hour), now.Add(-5 * time.Hour), HealthStalled},
		{"young rule that has not completed yet", now.Add(-time.Hour), time.Time{}, HealthPending},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			stale := DiagnoseStaleCheck(test.createdAt, test.completedAt, cadence, now)
			require.Equal(t, test.wantClass, stale.Class)

			// The batch reader sees the same instants through an execution list: one NOT_EVALUATED
			// row at the completion instant, or none at all.
			input := HealthInput{Rule: healthRule(storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED, test.createdAt), Now: now, Cadence: cadence}
			if !test.completedAt.IsZero() {
				input.Executions = []*store.QualityExecutionMessage{
					healthExecution(1, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 5, test.completedAt),
				}
			}
			batch := DiagnoseCheckHealth(input)
			if test.wantClass == "" {
				require.NotEqual(t, HealthStalled, batch.Class, "the sweep must not call a working check stalled")
				return
			}
			require.Equal(t, test.wantClass, batch.Class)
		})
	}

	// Without a cadence there is no stall window, so the sweep has nothing to say.
	require.Empty(t, DiagnoseStaleCheck(now.Add(-30*24*time.Hour), time.Time{}, 0, now).Class)
	require.Empty(t, DiagnoseStaleCheck(now.Add(-30*24*time.Hour), time.Time{}, cadence, time.Time{}).Class)
}
