package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestDerivePlanUnplanned(t *testing.T) {
	t.Parallel()

	plan := DerivePlan(false, 0, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
	})
	require.Equal(t, PlanUnplanned, plan.State)
	require.Equal(t, CriticalityExperimental, plan.Criticality)
	require.Empty(t, plan.Requirements, "a target with no contract has no declaration to gap against")
	require.Zero(t, plan.GapCount)
}

func TestDerivePlanImportant(t *testing.T) {
	t.Parallel()

	// Volume is satisfied by a row count; schema stability is missing.
	plan := DerivePlan(true, CriticalityImportant, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
	})
	require.Equal(t, PlanPlanned, plan.State)
	require.Equal(t, CriticalityImportant, plan.Criticality)
	require.Equal(t, []PlanRequirement{
		{Property: CoverageVolume, Satisfied: true},
		{Property: CoverageSchemaStability, Satisfied: false},
	}, plan.Requirements)
	require.Equal(t, 1, plan.GapCount)

	// A schema drift rule closes the gap.
	plan = DerivePlan(true, CriticalityImportant, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
	})
	require.Equal(t, 0, plan.GapCount)
	require.True(t, plan.Requirements[0].Satisfied)
	require.True(t, plan.Requirements[1].Satisfied)
}

func TestDerivePlanCritical(t *testing.T) {
	t.Parallel()

	plan := DerivePlan(true, CriticalityCritical, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
	})
	require.Equal(t, []PlanRequirement{
		{Property: CoverageFreshness, Satisfied: false},
		{Property: CoverageVolume, Satisfied: true},
		{Property: CoverageUniqueness, Satisfied: true},
		{Property: CoverageSchemaStability, Satisfied: false},
	}, plan.Requirements)
	require.Equal(t, 2, plan.GapCount)
}

func TestDerivePlanDefaultsCriticality(t *testing.T) {
	t.Parallel()

	plan := DerivePlan(true, 0, nil)
	require.Equal(t, CriticalityImportant, plan.Criticality)
	require.Equal(t, 2, plan.GapCount, "an empty plan gaps both IMPORTANT properties")
}
