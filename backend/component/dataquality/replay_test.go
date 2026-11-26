package dataquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// completedRun is one judged execution, as the runner recorded it: a check over rows records the
// value it measured, and one that found nothing to judge records none.
func completedRun(id int64, status storepb.QualityStatus, observed float64, observe bool) *store.QualityExecutionMessage {
	execution := &store.QualityExecutionMessage{
		ID:        id,
		State:     storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		Status:    status,
		TotalRows: 1000,
	}
	if observe {
		value := observed
		execution.ObservedValue = &value
	}
	return execution
}

// replayNullPercent is one null-percent check at one strictness, built for a replay.
func replayNullPercent(maxPercent float64, onEmpty storepb.QualityEmptyBehavior) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
			Column:     "email",
			MaxNullPct: &maxPercent,
		}},
		OnEmpty: onEmpty,
	}
}

// The replay is the only thing standing between a tune-level delegation and "turn the red green":
// a change that would make a failing run pass has to be found by judging the run again, and the
// cases below are the shapes that must be refused.
func TestReplayRuleRefusesAChangeThatTurnsAFailingRunIntoAPass(t *testing.T) {
	t.Parallel()

	t.Run("a looser threshold fails the replay", func(t *testing.T) {
		t.Parallel()
		strict := replayNullPercent(10, storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED)
		loose := replayNullPercent(80, storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED)
		failing := completedRun(11, storepb.QualityStatus_QUALITY_STATUS_FAIL, 50, true)
		failing.FailedRows = 500
		passing := completedRun(12, storepb.QualityStatus_QUALITY_STATUS_PASS, 2, true)
		passing.FailedRows = 20
		runs := []*store.QualityExecutionMessage{failing, passing}
		// The same runs against the strict config are what the check recorded: 50 > 10 fails.
		require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, Evaluate(RuleSnapshot{
			Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, Config: strict,
		}, Observation{FailedRows: 500, TotalRows: 1000}, timeZero()).Status,
			"the strict definition is what recorded the FAIL this run came from")

		replay := ReplayRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, loose, runs)
		require.True(t, replay.Available)
		require.Equal(t, 2, replay.Considered)
		require.Equal(t, []string{"qualityExecutions/11"}, replay.Loosened)
	})

	t.Run("a run that reached no verdict turning green fails the replay", func(t *testing.T) {
		t.Parallel()
		// The recorded run is an empty target that refused to pass; a definition that makes an empty
		// target pass is the "empty table scores 100" change, and it is not a repair either.
		empty := completedRun(21, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 0, false)
		empty.TotalRows = 0
		runs := []*store.QualityExecutionMessage{empty}
		replay := ReplayRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			replayNullPercent(10, storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS), runs)
		require.Equal(t, []string{"qualityExecutions/21"}, replay.Loosened)
	})

	t.Run("a freshness run replays from the lag it recorded", func(t *testing.T) {
		t.Parallel()
		loose := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{
			Freshness: &storepb.FreshnessConfig{TimestampColumn: "performed_at", MaxLagSeconds: 86400 * 30},
		}}
		runs := []*store.QualityExecutionMessage{
			completedRun(31, storepb.QualityStatus_QUALITY_STATUS_FAIL, 86400*3, true),
		}
		replay := ReplayRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, loose, runs)
		require.True(t, replay.Available)
		require.Equal(t, []string{"qualityExecutions/31"}, replay.Loosened,
			"the newest timestamp decides only whether there was one; the lag is the recorded value")
	})
}

// A change that leaves every red run red is a repair: the replay allows it, and tightening is
// never refused for turning something green.
func TestReplayRuleAllowsAChangeThatKeepsFailingRunsRed(t *testing.T) {
	t.Parallel()

	// 50% nulls: this run failed at 1% and still fails at 10%.
	stricter := replayNullPercent(1, storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED)
	failing := completedRun(41, storepb.QualityStatus_QUALITY_STATUS_FAIL, 5, true)
	failing.FailedRows = 500
	passing := completedRun(42, storepb.QualityStatus_QUALITY_STATUS_PASS, 0, true)
	runs := []*store.QualityExecutionMessage{failing, passing}
	replay := ReplayRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, stricter, runs)
	require.True(t, replay.Available)
	require.Equal(t, 2, replay.Considered)
	require.Empty(t, replay.Loosened)

	// The definition that recorded the run replays to itself, which is the round trip the
	// comparison rests on: a FAIL stays a FAIL when nothing about the definition moved.
	same := ReplayRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, replayNullPercent(10,
		storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED), runs)
	require.Empty(t, same.Loosened)
}

// A check with no replayable run is unverifiable rather than safe: an ERROR or a SKIPPED execution
// recorded no observation, so nothing can be re-judged and the change has to be a proposal.
func TestReplayRuleIsUnavailableWithoutAReplayableRun(t *testing.T) {
	t.Parallel()

	config := replayNullPercent(10, storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_NOT_EVALUATED)
	ruleType := storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT

	require.False(t, ReplayRule(ruleType, config, nil).Available, "a never-judged check has nothing to replay")

	unfinished := completedRun(51, storepb.QualityStatus_QUALITY_STATUS_ERROR, 0, false)
	unfinished.State = storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING
	skipped := &store.QualityExecutionMessage{
		ID: 52, State: storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		Status: storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
	}
	replay := ReplayRule(ruleType, config, []*store.QualityExecutionMessage{unfinished, skipped})
	require.False(t, replay.Available)
	require.Zero(t, replay.Considered)

	require.False(t, ReplayRule(ruleType, nil, []*store.QualityExecutionMessage{
		completedRun(53, storepb.QualityStatus_QUALITY_STATUS_PASS, 0, true),
	}).Available, "no definition, nothing to judge")
}

// The observation is rebuilt from the row alone, so every field a judge reads has to be one the row
// keeps: the value, the row counts it came from, the baseline a comparison used and whether there
// was a timestamp at all.
func TestReplayObservationRebuildsWhatTheJudgeReads(t *testing.T) {
	t.Parallel()

	baseline := 100.0
	execution := completedRun(61, storepb.QualityStatus_QUALITY_STATUS_FAIL, 40, true)
	execution.TotalRows = 400
	execution.FailedRows = 160
	execution.Sampled = true
	execution.SampleRows = 400
	execution.BaselineValue = &baseline
	execution.ObservedKind = storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO

	observation, ok := ReplayObservation(execution)
	require.True(t, ok)
	require.Equal(t, 40.0, observation.Observed)
	require.Equal(t, int64(160), observation.FailedRows)
	require.Equal(t, int64(400), observation.TotalRows)
	require.True(t, observation.Sampled)
	require.Equal(t, int64(400), observation.SampleRows)
	require.Equal(t, &baseline, observation.Baseline)
	require.False(t, observation.ObservedNull)
	require.NotNil(t, observation.MaxTimestamp, "the row held a value, so a timestamp was there to read")

	nullRun := completedRun(62, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 0, false)
	nullObservation, ok := ReplayObservation(nullRun)
	require.True(t, ok)
	require.True(t, nullObservation.ObservedNull)

	freshRun := completedRun(63, storepb.QualityStatus_QUALITY_STATUS_PASS, 60, true)
	freshObservation, ok := ReplayObservation(freshRun)
	require.True(t, ok)
	require.NotNil(t, freshObservation.MaxTimestamp)
}

// Every patchable field is judged by the replay, so a config the patcher can produce must be one the
// evaluator accepts: the two halves share the same rule types.
func TestReplayRuleJudgesEveryPatchableConfig(t *testing.T) {
	t.Parallel()

	config := everyValueFieldConfig()
	patched, err := PatchField(config, configFieldRangeMin, "2024-01-01T00:00:00Z")
	require.NoError(t, err)
	require.NotNil(t, patched.GetRange())

	runs := []*store.QualityExecutionMessage{completedRun(71, storepb.QualityStatus_QUALITY_STATUS_PASS, 0, true)}
	replay := ReplayRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE, patched, runs)
	require.True(t, replay.Available)
	require.False(t, proto.Equal(config, patched))
}

// timeZero is the instant the evaluator ignores; the judge reads the observation, not the clock.
func timeZero() (zero time.Time) { return zero }
