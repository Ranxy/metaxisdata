package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// A label proposal is identified by its column: a column has at most one semantic
// label, so two analyses of the same column must converge on one queue row.
func TestLabelSuggestionSignatureIdentifiesTheColumn(t *testing.T) {
	t.Parallel()

	require.Equal(t, LabelSuggestionSignature("email"), LabelSuggestionSignature(" email "))
	require.NotEqual(t, LabelSuggestionSignature("email"), LabelSuggestionSignature("phone"))
	require.Equal(t, "label|email", LabelSuggestionSignature("email"))
	// Separators in an identifier cannot make two different columns collide.
	require.NotEqual(t, LabelSuggestionSignature("a"), LabelSuggestionSignature("label|a"))
}

// A rule proposal is identified by its type and the columns it covers. Thresholds,
// titles and rationales are not part of the identity: "a uniqueness rule on email"
// is one proposal whatever tolerance the model suggests.
func TestRuleSignatureIgnoresPresentationAndThresholds(t *testing.T) {
	t.Parallel()

	base := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{
			Unique: &storepb.UniqueConfig{Column: "email"},
		},
	}
	tighterFail, tighterWarn := 1.0, 0.5
	stricter := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{
			Unique: &storepb.UniqueConfig{Column: "email", FailThreshold: &tighterFail, WarnThreshold: &tighterWarn},
		},
	}
	require.Equal(t, RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, base),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, stricter))

	// A different rule type on the same column is a different proposal.
	nullPercent := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_NullPercent{
			NullPercent: &storepb.NullPercentConfig{Column: "email"},
		},
	}
	require.NotEqual(t, RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, base),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, nullPercent))
}

// The column set is order-insensitive, so the same combination proposed in another
// order is still one proposal.
func TestRuleSignatureSortsItsColumnSet(t *testing.T) {
	t.Parallel()

	first := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_UniqueCombination{
			UniqueCombination: &storepb.UniqueCombinationConfig{Columns: []string{"tenant_id", "email"}},
		},
	}
	second := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_UniqueCombination{
			UniqueCombination: &storepb.UniqueCombinationConfig{Columns: []string{"email", "tenant_id", "email"}},
		},
	}
	require.Equal(t, RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION, first),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION, second))
}

// Config values that make a proposal a genuinely different rule stay in the
// identity, so two distinct rules are never collapsed into one queue row.
func TestRuleSignatureIncludesSemanticDiscriminators(t *testing.T) {
	t.Parallel()

	referential := func(parent string) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_ReferentialIntegrity{
				ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
					ChildColumn: "customer_id", ParentTable: parent, ParentColumn: "id",
				},
			},
		}
	}
	require.NotEqual(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, referential("customers")),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, referential("accounts")))

	cross := func(function storepb.QualityAggregateFunction) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Aggregate{
				Aggregate: &storepb.AggregateConfig{Column: "amount", Function: function},
			},
		}
	}
	require.NotEqual(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, cross(storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM)),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, cross(storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_AVG)))
}

// A draft that a saved rule already covers must not be queued for review again.
func TestRuleSignaturesCollectSavedRuleIdentities(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{
			Unique: &storepb.UniqueConfig{Column: "email"},
		},
	}
	signature := RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, config)
	signatures := RuleSignatures([]*store.QualityRuleMessage{
		{RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, Config: config},
	})
	require.Contains(t, signatures, signature)
	require.Len(t, signatures, 1)
}

// Previous decisions suppress re-proposals until the caller asks to regenerate.
// A rejected label may come back only after the label is cleared (the fact guard
// covers that), but a rejected rule draft stays rejected.
func TestSuggestionDecisionSuppressionIsKindAware(t *testing.T) {
	t.Parallel()

	require.True(t, SuggestionDecisionSuppressesRuleDraft(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED))
	require.True(t, SuggestionDecisionSuppressesRuleDraft(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED))
	require.True(t, SuggestionDecisionSuppressesLabel(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_REJECTED))
	require.False(t, SuggestionDecisionSuppressesLabel(storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_ACCEPTED))
	for _, status := range []storepb.QualitySuggestionStatus{
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_UNSPECIFIED,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING,
		storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_SUPERSEDED,
	} {
		require.False(t, SuggestionDecisionSuppressesRuleDraft(status))
		require.False(t, SuggestionDecisionSuppressesLabel(status))
	}
}

// A different filter makes a different rule even when type and columns match: the
// proposal is about which rows must hold, not just which column is checked.
func TestRuleSignatureIncludesTheFilter(t *testing.T) {
	t.Parallel()

	withFilter := func(operator storepb.QualityFilterOperator, value string) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Unique{
				Unique: &storepb.UniqueConfig{Column: "email"},
			},
			Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
				{Column: "status", Op: operator, Values: []string{value}},
			}},
		}
	}
	active := RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, withFilter(storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ, "active"))
	deleted := RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, withFilter(storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ, "deleted"))
	require.NotEqual(t, active, deleted)

	// Clause order and value order do not change the identity.
	first := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "email"}},
		Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
			{Column: "status", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IN, Values: []string{"active", "pending"}},
			{Column: "tenant", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ, Values: []string{"t1"}},
		}},
	}
	second := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "email"}},
		Filter: &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
			{Column: "tenant", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ, Values: []string{"t1"}},
			{Column: "status", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IN, Values: []string{"pending", "active"}},
		}},
	}
	require.Equal(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, first),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, second))
}

// Directional rules keep their left/right roles: sorting the column set must not
// turn "a > b" and "b > a" into one proposal.
func TestRuleSignatureKeepsDirectionalRoles(t *testing.T) {
	t.Parallel()

	crossColumn := func(left, right string) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CrossColumn{CrossColumn: &storepb.CrossColumnConfig{
				LeftColumn: left, RightColumn: right,
				Op: storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GT,
			}},
		}
	}
	forward := RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN, crossColumn("a", "b"))
	reverse := RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN, crossColumn("b", "a"))
	require.NotEqual(t, forward, reverse, "cross_column is directional")

	crossTable := func(left, right string) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CrossTable{CrossTable: &storepb.CrossTableConfig{
				LeftColumn: left, RightColumn: right, RightTable: "other",
				LeftFunction:  storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
				RightFunction: storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT,
			}},
		}
	}
	require.NotEqual(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, crossTable("a", "b")),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, crossTable("b", "a")),
		"cross_table sides are not interchangeable")
}

// Semantic config fields that change what a rule judges are part of its identity;
// otherwise one proposal overwrites the other and a saved rule suppresses the
// wrong draft.
func TestRuleSignatureIncludesSemanticConfigValues(t *testing.T) {
	t.Parallel()

	regex := func(pattern string) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{
			Regex: &storepb.RegexConfig{Column: "email", Pattern: pattern},
		}}
	}
	require.NotEqual(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, regex("^.+@.+$")),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, regex("^[a-z]+$")),
		"different regex patterns are different rules")

	accepted := func(values []string, allowNull bool) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
			AcceptedValues: &storepb.AcceptedValuesConfig{Column: "country", Values: values, AllowNull: allowNull},
		}}
	}
	require.NotEqual(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, accepted([]string{"US", "CA"}, false)),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, accepted([]string{"US", "MX"}, false)),
		"different accepted sets are different rules")
	require.Equal(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, accepted([]string{"CA", "US"}, false)),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, accepted([]string{"US", "CA"}, false)),
		"the accepted set is order-insensitive")

	unique := func(countNulls bool) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
			Unique: &storepb.UniqueConfig{Column: "email", CountNulls: countNulls},
		}}
	}
	require.NotEqual(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, unique(false)),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, unique(true)),
		"counting NULLs as a value is a different rule")

	percent := &storepb.QualityRuleConfig{
		Config:          &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "email"}},
		ThresholdMetric: storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT,
	}
	count := &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "email"}},
	}
	require.NotEqual(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, percent),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, count),
		"COUNT and PERCENT thresholds are different units")

	drift := func(columns ...string) *storepb.QualityRuleConfig {
		expected := make([]*storepb.SchemaDriftColumn, 0, len(columns))
		for _, name := range columns {
			expected = append(expected, &storepb.SchemaDriftColumn{Name: name, Type: "text"})
		}
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{
			SchemaDrift: &storepb.SchemaDriftConfig{ExpectedColumns: expected},
		}}
	}
	require.NotEqual(t,
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, drift("a", "b")),
		RuleSignature(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, drift("a", "c")),
		"a different expected schema is a different rule")
}
