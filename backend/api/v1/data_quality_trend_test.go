package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The requested window follows the documented bounds: zero and negatives mean the
// default, and no window may exceed the execution retention's daily ceiling.
func TestQualityTrendLimit(t *testing.T) {
	t.Parallel()

	require.Equal(t, dataquality.TrendDefaultPoints, qualityTrendLimit(0))
	require.Equal(t, dataquality.TrendDefaultPoints, qualityTrendLimit(-5))
	require.Equal(t, 1, qualityTrendLimit(1))
	require.Equal(t, dataquality.TrendDefaultPoints, qualityTrendLimit(int32(dataquality.TrendDefaultPoints)))
	require.Equal(t, dataquality.TrendMaxPoints, qualityTrendLimit(int32(dataquality.TrendMaxPoints)))
	require.Equal(t, dataquality.TrendMaxPoints, qualityTrendLimit(int32(dataquality.TrendMaxPoints)+1))
	require.Equal(t, dataquality.TrendMaxPoints, qualityTrendLimit(1000))
}

// The trend enums mirror each other by design, but the mapping is explicit so a
// new unit or notice cannot silently become UNSPECIFIED on the wire.
func TestConvertQualityTrendEnums(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		from dataquality.TrendUnit
		to   v1pb.QualityTrendUnit
	}{
		{dataquality.TrendUnitUnspecified, v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_UNSPECIFIED},
		{dataquality.TrendUnitCount, v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_COUNT},
		{dataquality.TrendUnitPercent, v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_PERCENT},
		{dataquality.TrendUnitSeconds, v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_SECONDS},
		{dataquality.TrendUnitNumber, v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_NUMBER},
	} {
		require.Equal(t, test.to, convertQualityTrendUnit(test.from))
	}

	for _, test := range []struct {
		from dataquality.TrendNotice
		to   v1pb.QualityTrendNotice
	}{
		{dataquality.TrendNoticeUnspecified, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNSPECIFIED},
		{dataquality.TrendNoticeNoJudgedExecution, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_NO_JUDGED_EXECUTION},
		{dataquality.TrendNoticeEditedSinceLastRun, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_EDITED_SINCE_LAST_RUN},
		{dataquality.TrendNoticeUnitNotComparable, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNIT_NOT_COMPARABLE},
		{dataquality.TrendNoticeUnsupportedRuleType, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNSUPPORTED_RULE_TYPE},
		{dataquality.TrendNoticeInsufficientHistory, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_INSUFFICIENT_HISTORY},
		{dataquality.TrendNoticeUnsupportedConfig, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNSUPPORTED_CONFIG},
	} {
		require.Equal(t, test.to, convertQualityTrendNotice(test.from))
	}

	for _, test := range []struct {
		from dataquality.TrendSuggestionMethod
		to   v1pb.QualityThresholdSuggestionMethod
	}{
		{dataquality.TrendSuggestionMethodUnspecified, v1pb.QualityThresholdSuggestionMethod_QUALITY_THRESHOLD_SUGGESTION_METHOD_UNSPECIFIED},
		{dataquality.TrendSuggestionMethodP95P99, v1pb.QualityThresholdSuggestionMethod_QUALITY_THRESHOLD_SUGGESTION_METHOD_P95_P99},
		{dataquality.TrendSuggestionMethodObservedRange, v1pb.QualityThresholdSuggestionMethod_QUALITY_THRESHOLD_SUGGESTION_METHOD_OBSERVED_RANGE},
	} {
		require.Equal(t, test.to, convertQualityTrendSuggestionMethod(test.from))
	}
}

// A check that was never judged is a trend with no points and an explanatory
// notice, not an error: this is a read model over an execution history that may
// legitimately be empty.
func TestBuildQualityRuleTrendEmptyHistory(t *testing.T) {
	t.Parallel()

	rule := &store.QualityRuleMessage{
		ID:         7,
		ResourceID: "rule123",
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Revision:   1,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
			RowCount: &storepb.RowCountConfig{MinRows: qualityTestPtr(int64(5))},
		}},
	}

	trend := buildQualityRuleTrend("ws1", rule, nil, nil)
	require.NotNil(t, trend)
	require.Equal(t, "workspaces/ws1/qualityRules/rule123", trend.GetRule())
	require.Equal(t, v1pb.QualityTrendUnit_QUALITY_TREND_UNIT_COUNT, trend.GetUnit())
	require.Empty(t, trend.GetPoints())
	require.Nil(t, trend.GetThresholds(), "a band needs a judged execution to take the snapshot from")
	require.Equal(t, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_NO_JUDGED_EXECUTION, trend.GetBandNotice())
	require.Equal(t, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_INSUFFICIENT_HISTORY, trend.GetSuggestion().GetNotice())
	require.False(t, trend.GetHistoryRestarted())
}

// The read is newest first and the chart is oldest first: the response flips the
// order exactly once, and the band comes from the newest execution when it is the
// current revision.
func TestBuildQualityRuleTrendPointsOldestFirst(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.October, 30, 9, 0, 0, 0, time.UTC)
	failValue := 100.0
	rule := &store.QualityRuleMessage{
		ID:         7,
		ResourceID: "rule123",
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Revision:   1,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
			RowCount: &storepb.RowCountConfig{MaxRows: qualityTestPtr(int64(100))},
		}},
	}
	executions := []*store.QualityExecutionMessage{
		{
			ID: 3, RuleRevision: 1, Status: storepb.QualityStatus_QUALITY_STATUS_FAIL,
			ObservedValue: qualityTestPtr(120.0), TotalRows: 120, StartedAt: base.Add(2 * time.Hour),
			Thresholds: &storepb.QualityThresholds{FailValue: &failValue},
		},
		{
			ID: 2, RuleRevision: 1, Status: storepb.QualityStatus_QUALITY_STATUS_PASS,
			ObservedValue: qualityTestPtr(110.0), TotalRows: 110, StartedAt: base.Add(time.Hour),
		},
		{
			ID: 1, RuleRevision: 1, Status: storepb.QualityStatus_QUALITY_STATUS_PASS,
			ObservedValue: qualityTestPtr(100.0), TotalRows: 100, StartedAt: base,
		},
	}
	revisions := []*store.QualityRuleRevisionMessage{
		{Revision: 1, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, Config: rule.Config},
	}

	trend := buildQualityRuleTrend("ws1", rule, executions, revisions)
	require.Len(t, trend.GetPoints(), 3)
	require.Equal(t, "workspaces/ws1/qualityExecutions/1", trend.GetPoints()[0].GetExecution())
	require.Equal(t, "workspaces/ws1/qualityExecutions/2", trend.GetPoints()[1].GetExecution())
	require.Equal(t, "workspaces/ws1/qualityExecutions/3", trend.GetPoints()[2].GetExecution())
	require.Equal(t, 100.0, trend.GetPoints()[0].GetValue())
	require.Equal(t, 120.0, trend.GetPoints()[2].GetValue())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, trend.GetPoints()[2].GetStatus())
	require.Equal(t, int32(1), trend.GetPoints()[2].GetRuleRevision())
	require.Equal(t, base, trend.GetPoints()[0].GetStartTime().AsTime())

	require.NotNil(t, trend.GetThresholds(), "the newest point is the current revision, so its snapshot is the band")
	require.Equal(t, 100.0, trend.GetThresholds().GetFailValue())
	require.Equal(t, v1pb.QualityTrendNotice_QUALITY_TREND_NOTICE_UNSPECIFIED, trend.GetBandNotice())
}

// A rule type change bumps the revision without rewriting older executions: the
// trend drops the points the previous check type judged and says the history
// restarted, so the chart never mixes two units on one line.
func TestBuildQualityRuleTrendDropsEarlierRuleType(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.October, 30, 9, 0, 0, 0, time.UTC)
	rule := &store.QualityRuleMessage{
		ID:         7,
		ResourceID: "rule123",
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Revision:   2,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
			RowCount: &storepb.RowCountConfig{MinRows: qualityTestPtr(int64(5))},
		}},
	}
	executions := []*store.QualityExecutionMessage{
		{
			ID: 2, RuleRevision: 2, Status: storepb.QualityStatus_QUALITY_STATUS_PASS,
			ObservedValue: qualityTestPtr(10.0), TotalRows: 10, StartedAt: base.Add(time.Hour),
		},
		{
			ID: 1, RuleRevision: 1, Status: storepb.QualityStatus_QUALITY_STATUS_PASS,
			ObservedValue: qualityTestPtr(3.0), FailedRows: 3, TotalRows: 10, StartedAt: base,
		},
	}
	revisions := []*store.QualityRuleRevisionMessage{
		{Revision: 2, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, Config: rule.Config},
		{Revision: 1, RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE},
	}

	trend := buildQualityRuleTrend("ws1", rule, executions, revisions)
	require.Len(t, trend.GetPoints(), 1, "a unique-check point is not a row count")
	require.Equal(t, "workspaces/ws1/qualityExecutions/2", trend.GetPoints()[0].GetExecution())
	require.True(t, trend.GetHistoryRestarted())
}

// The revision read resolves the distinct revisions of the window only: reading
// the whole history once per point would be wasted work.
func TestTrendRevisionNumbers(t *testing.T) {
	t.Parallel()

	require.Empty(t, trendRevisionNumbers(nil))
	require.Empty(t, trendRevisionNumbers([]*store.QualityExecutionMessage{nil}))
	require.Equal(t, []int32{3, 2}, trendRevisionNumbers([]*store.QualityExecutionMessage{
		{ID: 1, RuleRevision: 3},
		{ID: 2, RuleRevision: 2},
		{ID: 3, RuleRevision: 3},
		{ID: 4, RuleRevision: 3},
	}))
}
