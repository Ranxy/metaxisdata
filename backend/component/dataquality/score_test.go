package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestScoreModeForCoversEveryRuleType is the contract test: every rule type in
// the catalog has a score mode, and exactly the twelve row-level types score by
// passed rows while the other six score by verdict. Adding a rule type without
// deciding its mode fails here.
func TestScoreModeForCoversEveryRuleType(t *testing.T) {
	t.Parallel()

	require.Len(t, ruleTypeSpecs, 18, "the catalog carries eighteen rule types")
	rowLevel := map[storepb.QualityRuleType]bool{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT:          true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE:                true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION:    true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES:       true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE:                 true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:                 true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH:         true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY: true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN:          true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION:        true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:          true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK:           true,
	}
	require.Len(t, rowLevel, 12)

	rowCount, statusCount := 0, 0
	for _, spec := range ruleTypeSpecs {
		want := ScoreModeStatus
		if rowLevel[spec.Type] {
			want = ScoreModeRows
			rowCount++
		} else {
			statusCount++
		}
		require.Equal(t, want, ScoreModeFor(spec.Type), spec.Type.String())
	}
	require.Equal(t, 12, rowCount)
	require.Equal(t, 6, statusCount)

	require.Equal(t, ScoreModeNone, ScoreModeFor(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED))
	require.Equal(t, ScoreModeNone, ScoreModeFor(storepb.QualityRuleType(999)))
}

// TestCheckScoreRowLevel pins the passed-row share and the fallback: the score is
// the passed share of the observed rows, clamped, and a check with no rows falls
// through to its verdict.
func TestCheckScoreRowLevel(t *testing.T) {
	t.Parallel()

	score, judged := CheckScore(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityStatus_QUALITY_STATUS_FAIL, 2, 10)
	require.True(t, judged)
	require.InDelta(t, 0.8, score, 1e-9)

	score, judged = CheckScore(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityStatus_QUALITY_STATUS_FAIL, 4, 4)
	require.True(t, judged, "a row-level type scores by rows even when every row failed")
	require.InDelta(t, 0, score, 1e-9)

	score, judged = CheckScore(storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityStatus_QUALITY_STATUS_FAIL, 12, 10)
	require.True(t, judged)
	require.InDelta(t, 0, score, 1e-9, "a score is clamped into [0, 1]")

	// No total rows: the verdict is the fallback, not a division by zero.
	score, judged = CheckScore(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityStatus_QUALITY_STATUS_PASS, 0, 0)
	require.True(t, judged)
	require.InDelta(t, 1, score, 1e-9)
	score, judged = CheckScore(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityStatus_QUALITY_STATUS_WARN, 0, 0)
	require.True(t, judged)
	require.InDelta(t, 0.5, score, 1e-9)

	// An operational failure is not judged, whatever its row counters say.
	for _, status := range []storepb.QualityStatus{
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
		storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
	} {
		score, judged = CheckScore(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, status, 1, 10)
		require.False(t, judged, status.String())
		require.Zero(t, score)
	}
}

// TestCheckScoreByVerdict pins the six verdict-scored types: PASS is 1, WARN is
// 0.5, FAIL is 0, and only the operational states are unjudged.
func TestCheckScoreByVerdict(t *testing.T) {
	t.Parallel()

	for _, ruleType := range []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
	} {
		for status, want := range map[storepb.QualityStatus]float64{
			storepb.QualityStatus_QUALITY_STATUS_PASS: 1,
			storepb.QualityStatus_QUALITY_STATUS_WARN: 0.5,
			storepb.QualityStatus_QUALITY_STATUS_FAIL: 0,
		} {
			score, judged := CheckScore(ruleType, status, 1, 10)
			require.Truef(t, judged, "%s %s", ruleType.String(), status.String())
			require.InDeltaf(t, want, score, 1e-9, "%s %s", ruleType.String(), status.String())
		}
		_, judged := CheckScore(ruleType, storepb.QualityStatus_QUALITY_STATUS_ERROR, 0, 0)
		require.False(t, judged, "%s: an error is not a verdict", ruleType.String())
	}

	_, judged := CheckScore(storepb.QualityRuleType(999), storepb.QualityStatus_QUALITY_STATUS_PASS, 0, 0)
	require.False(t, judged, "a type this build does not know has no score")
}

// TestCheckWeight pins the severity weights the aggregate multiplies by.
func TestCheckWeight(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1.0, CheckWeight(storepb.QualitySeverity_QUALITY_SEVERITY_WARNING))
	require.Equal(t, 2.0, CheckWeight(storepb.QualitySeverity_QUALITY_SEVERITY_ERROR))
	require.Equal(t, 3.0, CheckWeight(storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL))
	require.Equal(t, 1.0, CheckWeight(storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED))
}

// TestScoreAggregateIsWeightedAndUnsetWhenEmpty pins the weighted mean on the
// 0-100 scale and that an empty aggregate has no score instead of 0 or 100.
func TestScoreAggregateIsWeightedAndUnsetWhenEmpty(t *testing.T) {
	t.Parallel()

	empty := &ScoreAggregate{}
	score, judged := empty.Score()
	require.False(t, judged)
	require.Zero(t, score)
	require.Zero(t, empty.Scored())

	aggregate := &ScoreAggregate{}
	aggregate.Add(0.5, storepb.QualitySeverity_QUALITY_SEVERITY_WARNING, true)
	aggregate.Add(1, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, false)
	score, judged = aggregate.Score()
	require.True(t, judged)
	require.InDelta(t, 100*2.5/3, score, 1e-9)
	require.Equal(t, int32(2), aggregate.Scored())
	require.Equal(t, int32(1), aggregate.Failing())
}
