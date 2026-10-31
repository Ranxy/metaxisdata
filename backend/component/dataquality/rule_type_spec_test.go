package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// testSpecFor returns the descriptor of one rule type or fails the test.
func testSpecFor(t *testing.T, ruleType storepb.QualityRuleType) ruleTypeSpec {
	t.Helper()
	for _, spec := range ruleTypeSpecs {
		if spec.Type == ruleType {
			return spec
		}
	}
	t.Fatalf("no rule type spec for %s", ruleType)
	return ruleTypeSpec{}
}

// ruleTypesWith returns the rule types whose descriptor matches, in proto order.
func ruleTypesWith(match func(ruleTypeSpec) bool) []storepb.QualityRuleType {
	types := make([]storepb.QualityRuleType, 0, len(ruleTypeSpecs))
	for _, spec := range ruleTypeSpecs {
		if match(spec) {
			types = append(types, spec.Type)
		}
	}
	return types
}

// TestRuleTypeSpecsCoverTheEnum is the completeness guard of the descriptor table:
// every real enum value has exactly one row, in proto order, and every function
// field a live rule type dispatches through is set.
func TestRuleTypeSpecsCoverTheEnum(t *testing.T) {
	t.Parallel()

	realTypes := 0
	for value := range storepb.QualityRuleType_name {
		if storepb.QualityRuleType(value) == storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
			continue
		}
		realTypes++
	}
	require.Equal(t, 18, realTypes, "the rule type enum has 18 real values")
	require.Len(t, ruleTypeSpecs, realTypes)

	seen := make(map[storepb.QualityRuleType]bool, len(ruleTypeSpecs))
	for index, spec := range ruleTypeSpecs {
		require.False(t, seen[spec.Type], "duplicate spec for %s", spec.Type)
		seen[spec.Type] = true
		// The order is the proto enum order: labelRuleTypes derives its order from
		// the slice, so shuffling the rows would change the prompt vocabulary.
		require.Equal(t, index+1, int(spec.Type), "spec %d is out of proto order: %s", index, spec.Type)

		require.NotNil(t, spec.Arm, "%s has no config arm", spec.Type)
		require.NotNil(t, spec.Compiler, "%s has no compiler", spec.Type)
		require.NotNil(t, spec.Validator, "%s has no validator", spec.Type)
		judgeOrThresholds := 0
		if spec.Judge != nil {
			judgeOrThresholds++
		}
		if spec.Thresholds != nil {
			judgeOrThresholds++
		}
		require.Equal(t, 1, judgeOrThresholds, "%s must set exactly one of judge/thresholds", spec.Type)
	}
	require.False(t, seen[storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED],
		"UNSPECIFIED must not have a spec")
}

// TestRuleTypeSpecContract pins the per-type sets the rest of the package derives
// from the table. These are product contracts, not implementation details: a rule
// type moving between sets is a behavior change and must show up here.
func TestRuleTypeSpecContract(t *testing.T) {
	t.Parallel()

	require.Equal(t, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
	}, ruleTypesWith(func(spec ruleTypeSpec) bool { return spec.Title != "" }),
		"the label-suggestion vocabulary is 14 types, in proto order")

	require.Equal(t, "unique", testSpecFor(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE).Title)
	require.Equal(t, "format", testSpecFor(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX).Title)
	require.Equal(t, "expression", testSpecFor(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION).Title)

	require.Equal(t, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
	}, ruleTypesWith(func(spec ruleTypeSpec) bool { return spec.Views }),
		"a view can only carry the table-level checks")

	require.Equal(t, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
	}, ruleTypesWith(func(spec ruleTypeSpec) bool { return spec.Metadata }),
		"only schema_drift reads the registry instead of the target")

	require.Equal(t, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
	}, ruleTypesWith(func(spec ruleTypeSpec) bool { return spec.Percent }),
		"10 types carry a denominator for a PERCENT threshold")

	require.Equal(t, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
	}, ruleTypesWith(func(spec ruleTypeSpec) bool { return spec.Sample != nil }),
		"11 types return failing rows")

	require.Equal(t, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
	}, ruleTypesWith(func(spec ruleTypeSpec) bool { return spec.Observe != nil }),
		"16 types read an aggregate row; CEL and schema_drift never do")

	require.Equal(t, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
	}, ruleTypesWith(func(spec ruleTypeSpec) bool { return spec.CheckedColumn != nil }),
		"9 types check one named column")

	require.Equal(t, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
	}, ruleTypesWith(func(spec ruleTypeSpec) bool { return spec.LabelDraft != nil }),
		"8 types have a deterministic label draft")

	require.Equal(t, []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
	}, ruleTypesWith(func(spec ruleTypeSpec) bool { return spec.Judge != nil }),
		"6 types judge an observation that is not a count shape")

	// A draft's title and rationale come from the spec title, so every type that
	// has a draft must have one.
	for _, spec := range ruleTypeSpecs {
		if spec.LabelDraft != nil {
			require.NotEmpty(t, spec.Title, "%s has a label draft but no title", spec.Type)
		}
	}

	for _, spec := range ruleTypeSpecs {
		switch spec.Type {
		case storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX:
			require.Equal(t, map[storepb.Engine]string{
				storepb.Engine_MSSQL: "MSSQL has no regular-expression operator; use custom SQL",
			}, spec.EngineRefusals)
		default:
			require.Empty(t, spec.EngineRefusals, "%s must not refuse an engine", spec.Type)
		}
	}
}
