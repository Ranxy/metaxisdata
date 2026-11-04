package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func qualityPlanRule(ruleType storepb.QualityRuleType) *store.QualityRuleMessage {
	return &store.QualityRuleMessage{
		TargetGUID: "inst-1;shop;public;orders",
		TargetType: storepb.MetaType_TABLE,
		RuleType:   ruleType,
	}
}

func TestQualityPlanViewUnplanned(t *testing.T) {
	t.Parallel()

	plan := qualityPlanView("inst-1;shop;public;orders", storepb.MetaType_TABLE,
		[]*store.QualityRuleMessage{qualityPlanRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT)}, false)
	require.Equal(t, v1pb.QualityPlanState_QUALITY_PLAN_STATE_UNPLANNED, plan.GetState())
	require.Equal(t, v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_EXPERIMENTAL, plan.GetCriticality())
	require.Empty(t, plan.GetRequirements())
	require.Zero(t, plan.GetGapCount())
	require.Equal(t, int32(1), plan.GetCheckCount())
}

func TestQualityPlanViewPlanned(t *testing.T) {
	t.Parallel()

	plan := qualityPlanView("inst-1;shop;public;orders", storepb.MetaType_TABLE,
		[]*store.QualityRuleMessage{qualityPlanRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT)}, true)
	require.Equal(t, v1pb.QualityPlanState_QUALITY_PLAN_STATE_PLANNED, plan.GetState())
	require.Equal(t, v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_IMPORTANT, plan.GetCriticality())
	require.Equal(t, []*v1pb.QualityCoverageRequirement{
		{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, Satisfied: true},
		{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_SCHEMA_STABILITY, Satisfied: false},
	}, plan.GetRequirements())
	require.Equal(t, int32(1), plan.GetGapCount())
}

func TestQualityPlanMatchesScope(t *testing.T) {
	t.Parallel()

	guid := "inst-1;shop;public;orders"
	require.True(t, qualityPlanMatchesScope(guid, "", "", "", ""))
	require.True(t, qualityPlanMatchesScope(guid, "inst-1", "shop", "public", "ord"))
	require.False(t, qualityPlanMatchesScope(guid, "inst-2", "", "", ""))
	require.False(t, qualityPlanMatchesScope(guid, "", "", "", "customers"))
}
