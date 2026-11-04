package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func qualityPlanRule() *store.QualityRuleMessage {
	return &store.QualityRuleMessage{
		TargetGUID: "inst-1;shop;public;orders",
		TargetType: storepb.MetaType_TABLE,
		RuleType:   storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
	}
}

func TestQualityPlanViewUnplanned(t *testing.T) {
	t.Parallel()

	plan := qualityPlanView("inst-1;shop;public;orders", storepb.MetaType_TABLE,
		[]*store.QualityRuleMessage{qualityPlanRule()}, nil)
	require.Equal(t, v1pb.QualityPlanState_QUALITY_PLAN_STATE_UNPLANNED, plan.GetState())
	require.Equal(t, v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_EXPERIMENTAL, plan.GetCriticality())
	require.Empty(t, plan.GetRequirements())
	require.Zero(t, plan.GetGapCount())
	require.Equal(t, int32(1), plan.GetCheckCount())
}

func TestQualityPlanViewPlanned(t *testing.T) {
	t.Parallel()

	plan := qualityPlanView("inst-1;shop;public;orders", storepb.MetaType_TABLE,
		[]*store.QualityRuleMessage{qualityPlanRule()}, &store.QualityContractMessage{})
	require.Equal(t, v1pb.QualityPlanState_QUALITY_PLAN_STATE_PLANNED, plan.GetState())
	require.Equal(t, v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_IMPORTANT, plan.GetCriticality())
	require.Equal(t, []*v1pb.QualityCoverageRequirement{
		{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, Satisfied: true, Source: v1pb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_TEMPLATE},
		{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_SCHEMA_STABILITY, Source: v1pb.QualityCoverageSource_QUALITY_COVERAGE_SOURCE_TEMPLATE},
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

// A contract-only target is a plan: declared requirements with no checks are the
// state where gaps matter most.
func TestQualityPlanEntriesIncludesContractOnlyTarget(t *testing.T) {
	t.Parallel()

	rule := qualityPlanRule()
	contractOnly := &store.QualityContractMessage{
		TargetGUID: "inst-1;shop;public;customers",
		TargetType: storepb.MetaType_TABLE,
		Settings: &storepb.QualityContractSettings{
			Intent: &storepb.QualityContractIntent{
				Criticality: storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_CRITICAL,
			},
		},
	}
	entries := qualityPlanEntries([]*store.QualityRuleMessage{rule}, []*store.QualityContractMessage{contractOnly})
	require.Len(t, entries, 2)
	require.Equal(t, rule.TargetGUID, entries[0].guid)
	require.Nil(t, entries[0].contract)
	require.Equal(t, contractOnly.TargetGUID, entries[1].guid)
	require.NotNil(t, entries[1].contract)
	require.Empty(t, entries[1].rules)
}

// A paused check does not satisfy the requirement it would otherwise cover.
func TestQualityPlanViewPausedRule(t *testing.T) {
	t.Parallel()

	paused := qualityPlanRule()
	paused.State = storepb.QualityRuleState_QUALITY_RULE_STATE_PAUSED
	plan := qualityPlanView("inst-1;shop;public;orders", storepb.MetaType_TABLE,
		[]*store.QualityRuleMessage{paused}, &store.QualityContractMessage{})
	require.False(t, plan.GetRequirements()[0].GetSatisfied(), "a paused volume check is not coverage")
	require.Equal(t, int32(2), plan.GetGapCount(), "volume and schema stability are both open")
}

// An exempted requirement is not a gap, so the gap list and the plan's gap count
// never disagree.
func TestQualityCoverageGapsFromPlansSkipsExempted(t *testing.T) {
	t.Parallel()

	plan := &v1pb.QualityPlan{
		Target:      &v1pb.QualityTarget{MetaGuid: "inst-1;shop;public;orders"},
		Criticality: v1pb.QualityAssetCriticality_QUALITY_ASSET_CRITICALITY_IMPORTANT,
		Requirements: []*v1pb.QualityCoverageRequirement{
			{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, Satisfied: true},
			{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_SCHEMA_STABILITY, Exempted: true},
			{Property: v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_UNIQUENESS, Column: "id"},
		},
	}
	gaps := qualityCoverageGapsFromPlans([]*v1pb.QualityPlan{plan})
	require.Len(t, gaps, 1)
	require.Equal(t, v1pb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_UNIQUENESS, gaps[0].GetProperty())
	require.Equal(t, "id", gaps[0].GetColumn())
}
