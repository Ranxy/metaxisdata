package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// everyValueFieldConfig is a config with every value-bearing field populated, which is what the
// diagnosis reads to name a field a driver refused.
func everyValueFieldConfig() *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{
			Column: "performed_at",
			Min:    stringPointer("now - 1 day"),
			Max:    stringPointer("now"),
		}},
		Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
			Column: "kind", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
			Values: []string{"audit"},
		}}},
	}
}

func stringPointer(value string) *string { return &value }

// The two halves of a field repair have to agree: every field a bound-value diagnosis can name is
// a field a proposal can set, and the other way round. A new value-bearing config field that only
// one side learns about turns a repairable failure into a dead end, which is exactly the failure
// this whole surface exists to remove.
func TestPatchFieldCanSetEveryFieldTheDiagnosisNames(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
			Column: "email", NullValues: []string{"", "N/A"},
		}},
		Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
			{Column: "kind", Values: []string{"audit"}},
			{Column: "channel", Values: []string{"web", "app"}},
		}},
	}
	named := configFieldValues(config)
	require.NotEmpty(t, named)

	// The list-valued shapes are patched in their own cases below; here every name has to be
	// settable, which is the invariant.
	for _, field := range named {
		patched, err := PatchField(config, field.path, "replaced")
		require.NoError(t, err, field.path)
		require.NotSame(t, config, patched, field.path)
	}

	for _, field := range []string{configFieldRangeMin, configFieldRangeMax, configFieldRegex, configFieldExpression} {
		single := &storepb.QualityRuleConfig{}
		switch field {
		case configFieldRangeMin, configFieldRangeMax:
			single.Config = &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{Column: "c"}}
		case configFieldRegex:
			single.Config = &storepb.QualityRuleConfig_Regex{Regex: &storepb.RegexConfig{Column: "c"}}
		case configFieldExpression:
			single.Config = &storepb.QualityRuleConfig_CelExpression{CelExpression: &storepb.CelExpressionConfig{Expression: "true"}}
		default:
			t.Fatalf("unexpected field %q", field)
		}
		_, err := PatchField(single, field, "x")
		require.NoError(t, err, field)
	}
}

// A patch is a copy: a proposal is validated exactly because nothing has been written, so the
// check's own config must leave the call unchanged.
func TestPatchFieldLeavesTheCallersConfigAlone(t *testing.T) {
	t.Parallel()

	config := everyValueFieldConfig()
	original := config.GetRange().GetMin()
	patched, err := PatchField(config, configFieldRangeMin, "2024-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Equal(t, "2024-01-01T00:00:00Z", patched.GetRange().GetMin())
	require.Equal(t, original, config.GetRange().GetMin())

	patched, err = PatchField(config, "filter.clauses[0].values[0]", "alert")
	require.NoError(t, err)
	require.Equal(t, []string{"alert"}, patched.GetFilter().GetClauses()[0].GetValues())
	require.Equal(t, []string{"audit"}, config.GetFilter().GetClauses()[0].GetValues())
}

// A patch that names no field of this check is a model writing its own definition, so the closed
// list is checked rather than trusted.
func TestPatchFieldRefusesFieldsThisBuildCannotSet(t *testing.T) {
	t.Parallel()

	config := everyValueFieldConfig()
	for _, field := range []string{
		"",
		"range",
		"range.fail_threshold",
		"sampling.percent",
		"filter.clauses[0]",
		"filter.clauses[9].values[0]",
		"accepted_values.values[0]",
	} {
		_, err := PatchField(config, field, "x")
		require.Error(t, err, field)
	}
}
