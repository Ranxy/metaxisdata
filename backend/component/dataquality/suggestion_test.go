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
