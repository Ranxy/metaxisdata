package dataquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func planRules(ruleType storepb.QualityRuleType) []RuleShape {
	return []RuleShape{{Type: ruleType}}
}

func TestDerivePlanUnplanned(t *testing.T) {
	t.Parallel()

	plan := DerivePlan(false, nil, []RuleShape{
		{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT},
		{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS},
	})
	require.Equal(t, PlanUnplanned, plan.State)
	require.Equal(t, CriticalityExperimental, plan.Criticality)
	require.Empty(t, plan.Requirements, "a target with no contract has no declaration to gap against")
	require.Zero(t, plan.GapCount)
}

func TestDerivePlanImportantTemplate(t *testing.T) {
	t.Parallel()

	// Volume is satisfied by a row count; schema stability is missing.
	plan := DerivePlan(true, nil, planRules(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))
	require.Equal(t, PlanPlanned, plan.State)
	require.Equal(t, CriticalityImportant, plan.Criticality)
	require.Equal(t, []PlanRequirement{
		{Property: CoverageVolume, Source: RequirementTemplate, Satisfied: true},
		{Property: CoverageSchemaStability, Source: RequirementTemplate},
	}, plan.Requirements)
	require.Equal(t, 1, plan.GapCount)

	// A schema drift rule closes the gap.
	plan = DerivePlan(true, nil, []RuleShape{
		{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE},
		{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT},
	})
	require.Zero(t, plan.GapCount)
}

func TestDerivePlanCriticalTemplate(t *testing.T) {
	t.Parallel()

	plan := DerivePlan(true, &storepb.QualityContractIntent{
		Criticality: storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_CRITICAL,
	}, []RuleShape{
		{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT},
		{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE},
	})
	require.Equal(t, CriticalityCritical, plan.Criticality)
	require.Equal(t, []CoverageProperty{CoverageFreshness, CoverageVolume, CoverageUniqueness, CoverageSchemaStability},
		[]CoverageProperty{
			plan.Requirements[0].Property,
			plan.Requirements[1].Property,
			plan.Requirements[2].Property,
			plan.Requirements[3].Property,
		})
	require.Equal(t, 2, plan.GapCount, "freshness and schema stability are missing")
}

// A contract with no explicit criticality keeps the S1 bridge to IMPORTANT.
func TestDerivePlanDefaultsCriticality(t *testing.T) {
	t.Parallel()

	plan := DerivePlan(true, nil, nil)
	require.Equal(t, CriticalityImportant, plan.Criticality)
	require.Equal(t, 2, plan.GapCount, "an empty plan gaps both IMPORTANT properties")
}

// An explicit intent criticality replaces the template, and EXPERIMENTAL declares
// nothing so it produces no gap.
func TestDerivePlanIntentCriticality(t *testing.T) {
	t.Parallel()

	plan := DerivePlan(true, &storepb.QualityContractIntent{
		Criticality: storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_EXPERIMENTAL,
	}, nil)
	require.Equal(t, PlanPlanned, plan.State)
	require.Equal(t, CriticalityExperimental, plan.Criticality)
	require.Empty(t, plan.Requirements)
	require.Zero(t, plan.GapCount)
}

// A statement adds a requirement for its scope, and only a rule on that column
// satisfies it.
func TestDerivePlanIntentStatements(t *testing.T) {
	t.Parallel()

	intent := &storepb.QualityContractIntent{
		Statements: []*storepb.QualityContractIntentStatement{
			{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_NULL_RATE, Column: "email"},
		},
	}
	plan := DerivePlan(true, intent, []RuleShape{
		{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT},
		{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, Column: "phone"},
	})
	require.Equal(t, RequirementIntent, plan.Requirements[2].Source)
	require.Equal(t, "email", plan.Requirements[2].Column)
	require.False(t, plan.Requirements[2].Satisfied, "a rule on another column does not satisfy the scope")
	require.True(t, plan.Requirements[0].Satisfied, "a table-level requirement is satisfied by any rule of the property")

	plan = DerivePlan(true, intent, []RuleShape{
		{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, Column: "email"},
	})
	require.True(t, plan.Requirements[2].Satisfied)
}

// A statement that restates a template requirement keeps a single requirement and
// marks it as declared by the intent.
func TestDerivePlanIntentRestatesTemplate(t *testing.T) {
	t.Parallel()

	plan := DerivePlan(true, &storepb.QualityContractIntent{
		Statements: []*storepb.QualityContractIntentStatement{
			{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME},
		},
	}, planRules(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))
	require.Equal(t, []PlanRequirement{
		{Property: CoverageVolume, Source: RequirementIntent, Satisfied: true},
		{Property: CoverageSchemaStability, Source: RequirementTemplate},
	}, plan.Requirements)
	require.Equal(t, 1, plan.GapCount)
}

// An exemption waives the gap without marking the requirement satisfied, and an
// exemption for an undeclared requirement adds nothing.
func TestDerivePlanIntentExemptions(t *testing.T) {
	t.Parallel()

	plan := DerivePlan(true, &storepb.QualityContractIntent{
		Exemptions: []*storepb.QualityContractIntentExemption{
			{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_SCHEMA_STABILITY, Reason: "schema pinned upstream"},
			{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_RANGE, Column: "amount", Reason: "undeclared"},
		},
	}, planRules(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))
	require.Len(t, plan.Requirements, 2, "an exemption never adds a requirement")
	require.True(t, plan.Requirements[1].Exempted)
	require.False(t, plan.Requirements[1].Satisfied)
	require.Zero(t, plan.GapCount)
}

// Expired exemptions stop waiving at read time; the stored intent keeps them.
func TestActiveIntent(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	active := &storepb.QualityContractIntentExemption{Property: storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME, Reason: "temporary"}
	expired := &storepb.QualityContractIntentExemption{
		Property:        storepb.QualityCoverageProperty_QUALITY_COVERAGE_PROPERTY_VOLUME,
		Reason:          "temporary",
		ExpiresAtUnixMs: now.Add(-time.Hour).UnixMilli(),
	}
	intent := &storepb.QualityContractIntent{Exemptions: []*storepb.QualityContractIntentExemption{active, expired}}

	filtered := ActiveIntent(intent, now)
	require.Len(t, filtered.GetExemptions(), 1)
	require.Len(t, intent.GetExemptions(), 2, "the stored intent is not mutated")
}

// Every declared property maps onto at least one rule type.
func TestPropertiesForRuleType(t *testing.T) {
	t.Parallel()

	covered := map[CoverageProperty]bool{}
	for ruleType := storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT; ruleType <= storepb.QualityRuleType_QUALITY_RULE_TYPE_SEGMENT_CHECK; ruleType++ {
		for _, property := range propertiesForRuleType(ruleType) {
			covered[property] = true
		}
	}
	for _, property := range []CoverageProperty{
		CoverageVolume, CoverageFreshness, CoverageUniqueness, CoverageSchemaStability,
		CoverageNullRate, CoverageAcceptedValues, CoverageFormat, CoverageRange,
	} {
		require.True(t, covered[property], "property %d has no rule type", property)
	}
}
