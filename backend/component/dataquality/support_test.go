package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// AllowedRuleTypes is what the prompt vocabulary and the agent's table context
// show a model, so it must be the pipeline's own answer rather than a second
// hand-kept list: a type is offered exactly when it can run on that object type
// and engine. A view target once drew range and null_percent drafts that were
// refused only after the model had spent a round trip on them.
func TestAllowedRuleTypesFollowsTheObjectAndEngineGates(t *testing.T) {
	t.Parallel()

	table := AllowedRuleTypes(storepb.MetaType_TABLE, storepb.Engine_POSTGRES)
	require.Contains(t, table, storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE)
	require.Contains(t, table, storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX)
	require.NotContains(t, table, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED,
		"UNSPECIFIED names no rule")
	for _, ruleType := range table {
		ok, reason := Supported(storepb.Engine_POSTGRES, ruleType)
		require.Truef(t, ok, "the list offered %s, which this engine refuses: %s", ruleType, reason)
		require.Truef(t, RuleTypeForTarget(storepb.MetaType_TABLE, ruleType), "the list offered %s on a table", ruleType)
	}

	// A view has no per-column registry rows, so the column-level types are gone
	// while the table-level ones stay.
	for _, view := range []storepb.MetaType{storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW} {
		allowed := AllowedRuleTypes(view, storepb.Engine_POSTGRES)
		require.Subsetf(t, table, allowed, "a %s accepts a subset of what a table accepts", view)
		for _, refused := range []storepb.QualityRuleType{
			storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		} {
			require.NotContainsf(t, allowed, refused, "%s cannot run on a %s", refused, view)
		}
		require.Contains(t, allowed, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			"a view can still be counted")
	}

	// The engine gate is applied too: T-SQL has no regular-expression operator.
	mssql := AllowedRuleTypes(storepb.MetaType_TABLE, storepb.Engine_MSSQL)
	require.NotContains(t, mssql, storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX)
	require.Contains(t, mssql, storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		"one refused type must not empty the list")

	require.Empty(t, AllowedRuleTypes(storepb.MetaType_UNSPECIFIED, storepb.Engine_POSTGRES),
		"an object type no rule may watch offers nothing")
}
