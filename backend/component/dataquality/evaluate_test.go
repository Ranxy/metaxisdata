package dataquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestRollupOrdersStatesBySeverity pins the rank table: the enum's numeric order is
// not the attention order, and ERROR must stay distinct from FAIL.
func TestRollupOrdersStatesBySeverity(t *testing.T) {
	t.Parallel()

	statuses := []storepb.QualityStatus{
		storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
	}
	want := []storepb.QualityStatus{
		storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
	}
	for index := range statuses {
		require.Equal(t, want[index], Rollup(statuses[:index+1]), "rollup of the first %d states", index+1)
	}
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, Rollup(nil))
}

func TestRollupCounts(t *testing.T) {
	t.Parallel()

	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, RollupCounts(&storepb.QualityStatusCounts{Total: 0}))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, RollupCounts(&storepb.QualityStatusCounts{Total: 3, Error: 1, Fail: 1}))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, RollupCounts(&storepb.QualityStatusCounts{Total: 3, Fail: 1, Pass: 2}))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, RollupCounts(&storepb.QualityStatusCounts{Total: 2, Warn: 1, Pass: 1}))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, RollupCounts(&storepb.QualityStatusCounts{Total: 2, NotEvaluated: 1, Pass: 1}))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_SKIPPED, RollupCounts(&storepb.QualityStatusCounts{Total: 2, Skipped: 2}))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, RollupCounts(&storepb.QualityStatusCounts{Total: 2, Pass: 2}))
}

func TestCountResultsTalliesSixStates(t *testing.T) {
	t.Parallel()

	counts, outcome := CountResults([]storepb.QualityStatus{
		storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
	})
	require.Equal(t, int32(7), counts.GetTotal())
	require.Equal(t, int32(2), counts.GetPass())
	require.Equal(t, int32(1), counts.GetWarn())
	require.Equal(t, int32(1), counts.GetFail())
	require.Equal(t, int32(1), counts.GetError())
	require.Equal(t, int32(1), counts.GetSkipped())
	require.Equal(t, int32(1), counts.GetNotEvaluated())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, outcome)
}

// rule builds a snapshot for one rule type with its config.
func rule(ruleType storepb.QualityRuleType, config *storepb.QualityRuleConfig) RuleSnapshot {
	return RuleSnapshot{Type: ruleType, Config: config}
}

var testNow = time.Date(2026, time.October, 19, 12, 0, 0, 0, time.UTC)

// TestEvaluateRowCount: zero rows is a value here, not missing data.
func TestEvaluateRowCount(t *testing.T) {
	t.Parallel()

	minRows := int64(1)
	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
		RowCount: &storepb.RowCountConfig{MinRows: &minRows},
	}}
	state := Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config), Observation{TotalRows: 0}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())

	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config), Observation{TotalRows: 5}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())
	require.Equal(t, float64(1), state.GetThresholds().GetFailValue())

	maxRows := int64(10)
	warnMax := int64(8)
	config.Config = &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MaxRows: &maxRows, WarnMaxRows: &warnMax}}
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config), Observation{TotalRows: 9}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config), Observation{TotalRows: 11}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())
}

// TestEvaluateEmptyDataFollowsOnEmpty pins that empty input never defaults to a
// pass and the rule author can override it.
func TestEvaluateEmptyDataFollowsOnEmpty(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
		AcceptedValues: &storepb.AcceptedValuesConfig{Column: "status", Values: []string{"a"}},
	}}
	state := Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, config), Observation{}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, state.GetStatus())

	config.OnEmpty = storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, config), Observation{}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())

	config.OnEmpty = storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_FAIL
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, config), Observation{}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())
}

// TestEvaluateCountUsesMetric: COUNT compares failed rows, PERCENT the ratio.
func TestEvaluateCountUsesMetric(t *testing.T) {
	t.Parallel()

	fail := 10.0
	warn := 5.0
	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
		Unique: &storepb.UniqueConfig{Column: "email", FailThreshold: &fail, WarnThreshold: &warn},
	}}
	obs := Observation{FailedRows: 3, TotalRows: 1000}
	state := Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config), obs, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())

	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config), Observation{FailedRows: 7, TotalRows: 1000}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, state.GetStatus())

	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config), Observation{FailedRows: 11, TotalRows: 1000}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())

	// PERCENT: 7/1000 = 0.7% passes a 5% warn threshold.
	config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	pctFail, pctWarn := 5.0, 1.0
	config.GetUnique().FailThreshold = &pctFail
	config.GetUnique().WarnThreshold = &pctWarn
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config), Observation{FailedRows: 7, TotalRows: 1000}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config), Observation{FailedRows: 20, TotalRows: 1000}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config), Observation{FailedRows: 60, TotalRows: 1000}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())
}

// TestEvaluateDefaultFailThresholdIsZero: a count rule with no explicit threshold
// fails on the first failed row.
func TestEvaluateDefaultFailThresholdIsZero(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
		Unique: &storepb.UniqueConfig{Column: "email"},
	}}
	state := Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config), Observation{FailedRows: 1, TotalRows: 10}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config), Observation{FailedRows: 0, TotalRows: 10}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())
}

func TestEvaluateNullPercent(t *testing.T) {
	t.Parallel()

	maxPct := 5.0
	warnPct := 2.0
	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: &maxPct, WarnNullPct: &warnPct},
	}}
	state := Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config), Observation{FailedRows: 10, TotalRows: 1000}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config), Observation{FailedRows: 30, TotalRows: 1000}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_WARN, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config), Observation{FailedRows: 60, TotalRows: 1000}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())

	maxCount := int64(3)
	config.Config = &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullCount: &maxCount}}
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config), Observation{FailedRows: 3, TotalRows: 10}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config), Observation{FailedRows: 4, TotalRows: 10}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())
}

func TestEvaluateRowCountChange(t *testing.T) {
	t.Parallel()

	increase := 20.0
	decrease := 20.0
	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCountChange{
		RowCountChange: &storepb.RowCountChangeConfig{MaxIncreasePct: &increase, MaxDecreasePct: &decrease},
	}}
	state := Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, config), Observation{TotalRows: 100}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, state.GetStatus())

	baseline := 100.0
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, config), Observation{TotalRows: 110, Baseline: &baseline}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, config), Observation{TotalRows: 130, Baseline: &baseline}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, config), Observation{TotalRows: 70, Baseline: &baseline}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())

	zeroBaseline := 0.0
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, config), Observation{TotalRows: 10, Baseline: &zeroBaseline}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, state.GetStatus())
}

func TestEvaluateFreshness(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{
		Freshness: &storepb.FreshnessConfig{TimestampColumn: "created_at", MaxLagSeconds: 3600},
	}}
	state := Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, config), Observation{}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, state.GetStatus())

	newest := testNow.Add(-30 * time.Minute)
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, config), Observation{Observed: 1800, MaxTimestamp: &newest}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.GetStatus())
	state = Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, config), Observation{Observed: 7200, MaxTimestamp: &newest}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())
}

// TestEvaluateRecordsThresholdSnapshot pins that the snapshot records what the
// verdict was judged on, so an old result stays explainable.
func TestEvaluateRecordsThresholdSnapshot(t *testing.T) {
	t.Parallel()

	fail := 3.0
	config := &storepb.QualityRuleConfig{
		Config:  &storepb.QualityRuleConfig_Regex{Regex: &storepb.RegexConfig{Column: "email", Pattern: "^a", FailThreshold: &fail}},
		OnEmpty: storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS,
	}
	state := Evaluate(rule(storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, config), Observation{FailedRows: 4, TotalRows: 10}, testNow)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.GetStatus())
	require.Equal(t, float64(3), state.GetThresholds().GetFailValue())
	require.Equal(t, storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, state.GetThresholds().GetComparison())
	require.Equal(t, storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS, state.GetThresholds().GetOnEmpty())
}
