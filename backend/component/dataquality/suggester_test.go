package dataquality

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// analysisTestTarget is the relation the pure-helper tests resolve columns
// against.
func analysisTestTarget() ResolvedTarget {
	return ResolvedTarget{
		Target: Target{
			GUID:     "instances/ins1;shop;public;orders",
			MetaType: storepb.MetaType_TABLE,
		},
		Engine:   storepb.Engine_POSTGRES,
		Database: "shop",
		Schema:   "public",
		Table:    "orders",
		Columns: []ColumnInfo{
			{Name: "email", Type: "text", Kind: ColumnKindString, Nullable: true},
			{Name: "status", Type: "text", Kind: ColumnKindString},
			{Name: "qty", Type: "int", Kind: ColumnKindNumeric},
			{Name: "amount", Type: "decimal(10,2)", Kind: ColumnKindNumeric},
			{Name: "created_at", Type: "timestamp", Kind: ColumnKindTemporal},
		},
	}
}

// Every rule type the natural-language vocabulary allows must convert into a typed
// config, with the request's columns injected where the draft omitted them.
func TestDraftConfigBuildsEveryRuleType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ruleType  storepb.QualityRuleType
		config    string
		columns   []string
		assertion func(t *testing.T, config *storepb.QualityRuleConfig, referenced []string)
	}{
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config:   `{"min_rows":1}`,
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, _ []string) {
				require.Equal(t, int64(1), config.GetRowCount().GetMinRows())
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
			config:   `{"max_increase_pct":10}`,
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, _ []string) {
				require.InDelta(t, 10, config.GetRowCountChange().GetMaxIncreasePct(), 1e-9)
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config:   `{"max_null_pct":5}`,
			columns:  []string{"email"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, referenced []string) {
				require.Equal(t, "email", config.GetNullPercent().GetColumn())
				require.Equal(t, []string{"email"}, referenced)
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:   `{}`,
			columns:  []string{"email"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, referenced []string) {
				require.Equal(t, "email", config.GetUnique().GetColumn())
				require.Equal(t, []string{"email"}, referenced)
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
			config:   `{}`,
			columns:  []string{"email", "status"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, referenced []string) {
				require.Equal(t, []string{"email", "status"}, config.GetUniqueCombination().GetColumns())
				require.Equal(t, []string{"email", "status"}, referenced)
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			config:   `{"values":[1,2],"allow_null":true}`,
			columns:  []string{"status"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, _ []string) {
				// A numeric JSON literal is rendered as the string the proto field
				// expects.
				require.Equal(t, []string{"1", "2"}, config.GetAcceptedValues().GetValues())
				require.True(t, config.GetAcceptedValues().GetAllowNull())
				require.Equal(t, "status", config.GetAcceptedValues().GetColumn())
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
			config:   `{"min":0,"max":100,"inclusive":true}`,
			columns:  []string{"amount"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, _ []string) {
				require.Equal(t, "0", config.GetRange().GetMin())
				require.Equal(t, "100", config.GetRange().GetMax())
				require.True(t, config.GetRange().GetInclusive())
				require.Equal(t, "amount", config.GetRange().GetColumn())
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			config:   `{"pattern":"^[^@]+@[^@]+$"}`,
			columns:  []string{"email"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, _ []string) {
				require.Equal(t, "^[^@]+@[^@]+$", config.GetRegex().GetPattern())
				require.Equal(t, "email", config.GetRegex().GetColumn())
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
			config:   `{"min_len":3,"max_len":64}`,
			columns:  []string{"email"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, _ []string) {
				require.Equal(t, int64(3), config.GetStringLength().GetMinLen())
				require.Equal(t, int64(64), config.GetStringLength().GetMaxLen())
				require.Equal(t, "email", config.GetStringLength().GetColumn())
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
			config:   `{"parent_table":"customers","parent_column":"id"}`,
			columns:  []string{"email"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, referenced []string) {
				require.Equal(t, "email", config.GetReferentialIntegrity().GetChildColumn())
				require.Equal(t, "customers", config.GetReferentialIntegrity().GetParentTable())
				require.Equal(t, "id", config.GetReferentialIntegrity().GetParentColumn())
				require.Equal(t, []string{"email"}, referenced)
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
			config:   `{"op":"GTE"}`,
			columns:  []string{"qty", "amount"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, referenced []string) {
				require.Equal(t, storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GTE,
					config.GetCrossColumn().GetOp())
				require.Equal(t, "qty", config.GetCrossColumn().GetLeftColumn())
				require.Equal(t, "amount", config.GetCrossColumn().GetRightColumn())
				require.Equal(t, []string{"qty", "amount"}, referenced)
			},
		},
		{
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			config:   `{"max_lag_seconds":3600}`,
			columns:  []string{"created_at"},
			assertion: func(t *testing.T, config *storepb.QualityRuleConfig, _ []string) {
				require.Equal(t, "created_at", config.GetFreshness().GetTimestampColumn())
				require.Equal(t, int32(3600), config.GetFreshness().GetMaxLagSeconds())
			},
		},
	}
	for _, test := range tests {
		t.Run(test.ruleType.String(), func(t *testing.T) {
			t.Parallel()
			config, referenced, err := draftConfig(RuleDraftOutput{
				Type:    test.ruleType,
				Config:  json.RawMessage(test.config),
				Columns: test.columns,
			}, test.columns)
			require.NoError(t, err)
			require.NotNil(t, config)
			require.NotNil(t, config.GetConfig(), "the config must carry its oneof arm")
			test.assertion(t, config, referenced)
		})
	}
}

// A draft whose config is not an object, or a rule type outside the vocabulary,
// cannot be converted.
func TestDraftConfigRejectsMalformedConfig(t *testing.T) {
	t.Parallel()

	_, _, err := draftConfig(RuleDraftOutput{
		Type:   storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config: json.RawMessage(`[1,2]`),
	}, nil)
	require.Error(t, err)

	_, _, err = draftConfig(RuleDraftOutput{
		Type:   storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		Config: json.RawMessage(`{"sql":"SELECT 1"}`),
	}, nil)
	require.Error(t, err)
}

// The comparison operator accepts both the short prompt spelling and the proto
// enum name.
func TestDraftConfigNormalizesComparisonOperators(t *testing.T) {
	t.Parallel()

	for _, operator := range []string{"GTE", "gte", "QUALITY_COMPARISON_OPERATOR_GTE"} {
		config, _, err := draftConfig(RuleDraftOutput{
			Type:   storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
			Config: json.RawMessage(`{"op":"` + operator + `"}`),
		}, []string{"qty", "amount"})
		require.NoError(t, err)
		require.Equal(t, storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GTE,
			config.GetCrossColumn().GetOp())
	}
}

// The request's column scope is enforced on both the draft's column list and the
// columns its config actually refers to.
func TestDraftColumnsAllowed(t *testing.T) {
	t.Parallel()

	target := analysisTestTarget()
	require.True(t, draftColumnsAllowed(nil, nil, target), "a draft with no columns is scoped by its config")
	require.True(t, draftColumnsAllowed([]string{"email"}, nil, target))
	require.False(t, draftColumnsAllowed([]string{"invented"}, nil, target))
	require.True(t, draftColumnsAllowed([]string{"email"}, []string{"email"}, target))
	require.False(t, draftColumnsAllowed([]string{"status"}, []string{"email"}, target),
		"a column outside the request scope is refused")
	require.True(t, draftColumnsAllowed([]string{""}, []string{"email"}, target),
		"a blank column is skipped; the strict draft parser rejects it before this point")
}

// Column comments come from the stored metadata, per object type.
func TestColumnComments(t *testing.T) {
	t.Parallel()

	table := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{
		TableMetadata: &storepb.TableMetadata{Columns: []*storepb.ColumnMetadata{
			{Name: "email", Type: "text", Comment: "work email"},
			{Name: "status", Type: "text"},
		}},
	}}
	require.Equal(t, map[string]string{"email": "work email"}, columnComments(table, storepb.MetaType_TABLE))

	view := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{
		ViewMetadata: &storepb.ViewMetadata{Columns: []*storepb.ColumnMetadata{
			{Name: "email", Type: "text", Comment: "view email"},
		}},
	}}
	require.Equal(t, map[string]string{"email": "view email"}, columnComments(view, storepb.MetaType_VIEW))

	require.Nil(t, columnComments(table, storepb.MetaType_DATABASE))
	require.Nil(t, columnComments(&storepb.StoredMetadata{}, storepb.MetaType_TABLE))
}

func TestStringFromJSONValue(t *testing.T) {
	t.Parallel()

	require.Equal(t, "paid", stringFromJSONValue(json.RawMessage(`"paid"`)))
	require.Equal(t, "42", stringFromJSONValue(json.RawMessage(`42`)))
	require.Equal(t, "1.5", stringFromJSONValue(json.RawMessage(`1.5`)))
	require.Equal(t, "true", stringFromJSONValue(json.RawMessage(`true`)))
	require.Empty(t, stringFromJSONValue(json.RawMessage(`null`)), "a JSON null has no string form")
}
