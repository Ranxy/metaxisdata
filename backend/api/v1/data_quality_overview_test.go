package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestQualityOverviewTrendDays pins the clamp: a missing window is the default,
// a larger one is capped, and a negative one cannot shrink the chart to nothing.
func TestQualityOverviewTrendDays(t *testing.T) {
	t.Parallel()

	require.Equal(t, 7, qualityOverviewTrendDays(0))
	require.Equal(t, 7, qualityOverviewTrendDays(-3))
	require.Equal(t, 14, qualityOverviewTrendDays(14))
	require.Equal(t, 30, qualityOverviewTrendDays(31))
}

// TestFillQualityTrend pins that a quiet day is a zero point rather than a gap,
// and that the points are ordered oldest first.
func TestFillQualityTrend(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC)
	trend := []store.QualityTrendDay{
		{Date: "2026-10-18", Counts: &storepb.QualityStatusCounts{Total: 3, Pass: 2, Fail: 1}},
		{Date: "2026-10-20", Counts: &storepb.QualityStatusCounts{Total: 1, Error: 1}},
	}
	points := fillQualityTrend(start, 3, trend)
	require.Len(t, points, 3)
	require.Equal(t, "2026-10-18", points[0].GetDate())
	require.Equal(t, int32(2), points[0].GetPass())
	require.Equal(t, "2026-10-19", points[1].GetDate())
	require.Equal(t, int32(0), points[1].GetTotal())
	require.Equal(t, "2026-10-20", points[2].GetDate())
	require.Equal(t, int32(1), points[2].GetError())
	require.Equal(t, int32(1), points[2].GetTotal())
}

// qualityOverviewRule builds one enabled rule with a dimension, severity, owner
// and the typed config whose checked column the coverage numerator reads.
func qualityOverviewRule(id int64, targetGUID string, dimension storepb.QualityDimension,
	severity storepb.QualitySeverity, owner int32, config *storepb.QualityRuleConfig) *store.QualityRuleMessage {
	rule := &store.QualityRuleMessage{
		ID:         id,
		TargetGUID: targetGUID,
		TargetType: storepb.MetaType_TABLE,
		Dimension:  dimension,
		Severity:   severity,
		State:      storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		Config:     config,
	}
	if owner > 0 {
		rule.OwnerID = &owner
	}
	return rule
}

// qualityOverviewConfig is the config arm the coverage numerator reads a column
// from; the empty column is a table-level row-count rule that covers none.
func qualityOverviewConfig(column string) *storepb.QualityRuleConfig {
	if column == "" {
		minRows := int64(1)
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
			RowCount: &storepb.RowCountConfig{MinRows: &minRows},
		}}
	}
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: column},
	}}
}

// The overview's coverage and score fields are one pure assembly: the coverage
// denominator comes from the registry, the numerator from the enabled rules, the
// score from each rule's latest judged result weighted by severity, and ERROR
// leaves a rule unscored rather than a zero.
func TestAssembleQualityScoreOverview(t *testing.T) {
	t.Parallel()

	orders := "inst-1;shop;;orders"
	events := "inst-1;shop;;events"
	users := "inst-2;shop;;users"
	rules := []*store.QualityRuleMessage{
		qualityOverviewRule(1, orders, storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			storepb.QualitySeverity_QUALITY_SEVERITY_WARNING, 7, qualityOverviewConfig("")),
		qualityOverviewRule(2, orders, storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, 7, qualityOverviewConfig("email")),
		qualityOverviewRule(3, events, storepb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS,
			storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL, 0, qualityOverviewConfig("id")),
		qualityOverviewRule(4, users, storepb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			storepb.QualitySeverity_QUALITY_SEVERITY_WARNING, 0, qualityOverviewConfig("")),
	}
	rules[1].RuleType = storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT
	rules[2].RuleType = storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE
	latest := map[int64]*store.QualityExecutionMessage{
		1: {RuleID: 1, Status: storepb.QualityStatus_QUALITY_STATUS_PASS},
		2: {RuleID: 2, Status: storepb.QualityStatus_QUALITY_STATUS_FAIL, FailedRows: 2, TotalRows: 10},
		3: {RuleID: 3, Status: storepb.QualityStatus_QUALITY_STATUS_PASS},
		4: {RuleID: 4, Status: storepb.QualityStatus_QUALITY_STATUS_ERROR},
	}
	coverage := []*store.QualityCoverageRow{
		{InstanceResourceID: "inst-1", InstanceTitle: "Shop", Environment: "prod", TableCount: 5, ColumnCount: 20},
		{InstanceResourceID: "inst-2", InstanceTitle: "Analytics", TableCount: 2, ColumnCount: 8},
	}
	environments := []*storepb.EnvironmentSetting_Environment{{Id: "prod", Title: "Production"}}
	existing := map[store.MetaGUIDKey]struct{}{
		{GUID: orders, ObjectType: storepb.MetaType_TABLE}: {},
		{GUID: events, ObjectType: storepb.MetaType_TABLE}: {},
		{GUID: users, ObjectType: storepb.MetaType_TABLE}:  {},
	}

	overview := assembleQualityScoreOverview(rules, existing, latest, coverage, environments)

	require.Equal(t, int32(3), overview.GetMonitoredTableCount())
	require.Equal(t, int32(2), overview.GetCoveredColumnCount(), "only a column-checking rule covers a column")
	require.Equal(t, int32(7), overview.GetTotalTableCount())
	require.Equal(t, int32(28), overview.GetTotalColumnCount())
	require.Equal(t, int32(2), overview.GetJudgedTableCount(), "the ERROR rule leaves its target unjudged")
	require.Equal(t, int32(3), overview.GetScoredRuleCount())
	require.Equal(t, int32(1), overview.GetUnscoredRuleCount())

	require.NotNil(t, overview.QualityScore, "three judged checks produce a score")
	require.InDelta(t, 100*5.6/6, overview.GetQualityScore(), 1e-9)

	require.Len(t, overview.GetCoverageByInstance(), 2)
	first := overview.GetCoverageByInstance()[0]
	require.Equal(t, "inst-1", first.GetKey())
	require.Equal(t, "Shop", first.GetTitle())
	require.Equal(t, int32(5), first.GetTotalTableCount())
	require.Equal(t, int32(2), first.GetMonitoredTableCount())
	require.Equal(t, int32(20), first.GetTotalColumnCount())
	require.Equal(t, int32(2), first.GetCoveredColumnCount())
	second := overview.GetCoverageByInstance()[1]
	require.Equal(t, "inst-2", second.GetKey())
	require.Equal(t, int32(1), second.GetMonitoredTableCount())
	require.Equal(t, int32(0), second.GetCoveredColumnCount())
	require.Equal(t, int32(8), second.GetTotalColumnCount())

	require.Len(t, overview.GetCoverageByEnvironment(), 2)
	unassigned := overview.GetCoverageByEnvironment()[0]
	require.Empty(t, unassigned.GetKey(), "an unassigned environment is a row with an empty key")
	require.Empty(t, unassigned.GetTitle())
	require.Equal(t, int32(2), unassigned.GetTotalTableCount())
	require.Equal(t, int32(1), unassigned.GetMonitoredTableCount())
	assigned := overview.GetCoverageByEnvironment()[1]
	require.Equal(t, "prod", assigned.GetKey())
	require.Equal(t, "Production", assigned.GetTitle())
	require.Equal(t, int32(5), assigned.GetTotalTableCount())
	require.Equal(t, int32(2), assigned.GetMonitoredTableCount())
	require.Equal(t, int32(2), assigned.GetCoveredColumnCount())

	require.Len(t, overview.GetScoreByDimension(), 3)
	completeness := overview.GetScoreByDimension()[0]
	require.Equal(t, v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, completeness.GetDimension())
	require.Equal(t, int32(2), completeness.GetRuleCount())
	require.Equal(t, int32(2), completeness.GetScoredRuleCount())
	require.Equal(t, int32(1), completeness.GetFailingRuleCount())
	require.InDelta(t, 100*2.6/3, completeness.GetQualityScore(), 1e-9)
	validity := overview.GetScoreByDimension()[1]
	require.Nil(t, validity.QualityScore, "a dimension whose only rule errored has no score")
	require.Zero(t, validity.GetScoredRuleCount())
	uniqueness := overview.GetScoreByDimension()[2]
	require.Equal(t, int32(1), uniqueness.GetScoredRuleCount())
	require.InDelta(t, 100.0, uniqueness.GetQualityScore(), 1e-9)

	require.Len(t, overview.GetScoreByOwner(), 2)
	unowned := overview.GetScoreByOwner()[0]
	require.Empty(t, unowned.GetOwner())
	require.Equal(t, int32(2), unowned.GetRuleCount())
	require.Equal(t, int32(1), unowned.GetScoredRuleCount())
	require.InDelta(t, 100.0, unowned.GetQualityScore(), 1e-9)
	owner := overview.GetScoreByOwner()[1]
	require.Equal(t, "users/7", owner.GetOwner())
	require.Equal(t, int32(2), owner.GetRuleCount())
	require.Equal(t, int32(2), owner.GetScoredRuleCount())
	require.Equal(t, int32(1), owner.GetFailingRuleCount())
}

// A workspace with no enabled rule has no coverage numerator and no score at
// all; the denominator still comes from the registry.
func TestAssembleQualityScoreOverviewEmpty(t *testing.T) {
	t.Parallel()

	overview := assembleQualityScoreOverview(nil, nil, nil,
		[]*store.QualityCoverageRow{{InstanceResourceID: "inst-1", TableCount: 3, ColumnCount: 9}}, nil)
	require.Zero(t, overview.GetMonitoredTableCount())
	require.Zero(t, overview.GetCoveredColumnCount())
	require.Zero(t, overview.GetJudgedTableCount())
	require.Zero(t, overview.GetScoredRuleCount())
	require.Nil(t, overview.QualityScore, "no judged check means no score")
	require.Empty(t, overview.GetScoreByDimension())
	require.Empty(t, overview.GetScoreByOwner())
	require.Equal(t, int32(3), overview.GetTotalTableCount())
	require.Equal(t, int32(9), overview.GetTotalColumnCount())
}

// The score's row-level mode is part of the assembly: a rule whose type scores by
// passed rows uses failed_rows/total_rows even when the verdict is FAIL.
func TestAssembleQualityScoreOverviewUsesRowScores(t *testing.T) {
	t.Parallel()

	rule := qualityOverviewRule(1, "inst-1;shop;;orders", storepb.QualityDimension_QUALITY_DIMENSION_UNIQUENESS,
		storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, 0, qualityOverviewConfig("id"))
	rule.RuleType = storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE
	latest := map[int64]*store.QualityExecutionMessage{
		1: {RuleID: 1, Status: storepb.QualityStatus_QUALITY_STATUS_FAIL, FailedRows: 1, TotalRows: 4},
	}
	existing := map[store.MetaGUIDKey]struct{}{
		{GUID: "inst-1;shop;;orders", ObjectType: storepb.MetaType_TABLE}: {},
	}
	overview := assembleQualityScoreOverview([]*store.QualityRuleMessage{rule}, existing, latest,
		[]*store.QualityCoverageRow{{InstanceResourceID: "inst-1", TableCount: 1, ColumnCount: 2}}, nil)
	require.NotNil(t, overview.QualityScore)
	require.InDelta(t, 75.0, overview.GetQualityScore(), 1e-9)
}

// A rule can outlive its target. Its target has no coverage denominator row, so
// it must not count toward coverage, the score, the judged-target count, or any
// breakdown: otherwise coverage could exceed 100%.
func TestAssembleQualityScoreOverviewExcludesOrphanedTargets(t *testing.T) {
	t.Parallel()

	live := "inst-1;shop;;orders"
	orphaned := "inst-1;shop;;dropped"
	rules := []*store.QualityRuleMessage{
		qualityOverviewRule(1, live, storepb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
			storepb.QualitySeverity_QUALITY_SEVERITY_WARNING, 0, qualityOverviewConfig("email")),
		qualityOverviewRule(2, orphaned, storepb.QualityDimension_QUALITY_DIMENSION_VALIDITY,
			storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL, 0, qualityOverviewConfig("id")),
	}
	latest := map[int64]*store.QualityExecutionMessage{
		1: {RuleID: 1, Status: storepb.QualityStatus_QUALITY_STATUS_PASS},
		2: {RuleID: 2, Status: storepb.QualityStatus_QUALITY_STATUS_PASS},
	}
	existing := map[store.MetaGUIDKey]struct{}{
		{GUID: live, ObjectType: storepb.MetaType_TABLE}: {},
	}
	coverage := []*store.QualityCoverageRow{{InstanceResourceID: "inst-1", TableCount: 1, ColumnCount: 2}}

	overview := assembleQualityScoreOverview(rules, existing, latest, coverage, nil)
	require.Equal(t, int32(1), overview.GetMonitoredTableCount(), "the orphaned target is not in the denominator")
	require.Equal(t, int32(1), overview.GetCoveredColumnCount())
	require.Equal(t, int32(1), overview.GetJudgedTableCount())
	require.Equal(t, int32(1), overview.GetScoredRuleCount(), "the orphaned rule is not scored")
	require.Zero(t, overview.GetUnscoredRuleCount(), "the orphaned rule is not part of the score's population")
	require.NotNil(t, overview.QualityScore)
	require.InDelta(t, 100.0, overview.GetQualityScore(), 1e-9)
	require.Len(t, overview.GetScoreByDimension(), 1)
	require.Equal(t, v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS, overview.GetScoreByDimension()[0].GetDimension())
	require.Len(t, overview.GetCoverageByInstance(), 1)
	require.Equal(t, int32(1), overview.GetCoverageByInstance()[0].GetMonitoredTableCount())
	require.LessOrEqual(t, int(overview.GetMonitoredTableCount()), int(overview.GetTotalTableCount()))
}
