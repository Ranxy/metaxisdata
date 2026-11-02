package dataquality

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// templateFilter is a filter the scope-clearing and refusal tests share.
func templateFilter() *storepb.QualityFilter {
	return &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
		Column: "id",
		Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GT,
		Values: []string{"0"},
	}}}
}

// templateScopeCase is one rule type's contract: the config a user could write in
// a template, and the config the template must store after the target scope is
// cleared.
type templateScopeCase struct {
	input *storepb.QualityRuleConfig
	want  *storepb.QualityRuleConfig
}

// templateScopeCases pins the cleared shape of every rule type. The contract test
// walks ruleTypeSpecs and fails when a type has no row here, so adding a rule type
// forces a decision about which of its parameters are target-bound.
var templateScopeCases = map[storepb.QualityRuleType]templateScopeCase{
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(5)}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(5)}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_RowCountChange{RowCountChange: &storepb.RowCountChangeConfig{
				MaxIncreasePct:  proto.Float64(10),
				WarnIncreasePct: proto.Float64(5),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_RowCountChange{RowCountChange: &storepb.RowCountChangeConfig{
				MaxIncreasePct:  proto.Float64(10),
				WarnIncreasePct: proto.Float64(5),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
				Column:        "email",
				MaxNullCount:  proto.Int64(5),
				WarnNullCount: proto.Int64(2),
				NullValues:    []string{"n/a"},
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: &storepb.NullPercentConfig{
				MaxNullCount:  proto.Int64(5),
				WarnNullCount: proto.Int64(2),
				NullValues:    []string{"n/a"},
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{
				Column:        "id",
				CountNulls:    true,
				FailThreshold: proto.Float64(10),
				WarnThreshold: proto.Float64(5),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{
				CountNulls:    true,
				FailThreshold: proto.Float64(10),
				WarnThreshold: proto.Float64(5),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_UniqueCombination{UniqueCombination: &storepb.UniqueCombinationConfig{
				Columns:       []string{"tenant_id", "id"},
				AllColumns:    true,
				CountNulls:    true,
				FailThreshold: proto.Float64(10),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_UniqueCombination{UniqueCombination: &storepb.UniqueCombinationConfig{
				CountNulls:    true,
				FailThreshold: proto.Float64(10),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_AcceptedValues{AcceptedValues: &storepb.AcceptedValuesConfig{
				Column:        "status",
				Values:        []string{"active", "paused"},
				AllowNull:     true,
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_AcceptedValues{AcceptedValues: &storepb.AcceptedValuesConfig{
				Values:        []string{"active", "paused"},
				AllowNull:     true,
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{
				Column:        "age",
				Min:           proto.String("0"),
				Max:           proto.String("150"),
				Inclusive:     true,
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Range{Range: &storepb.RangeConfig{
				Min:           proto.String("0"),
				Max:           proto.String("150"),
				Inclusive:     true,
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Regex{Regex: &storepb.RegexConfig{
				Column:        "email",
				Pattern:       `^.+@.+$`,
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Regex{Regex: &storepb.RegexConfig{
				Pattern:       `^.+@.+$`,
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_StringLength{StringLength: &storepb.StringLengthConfig{
				Column:        "code",
				MinLen:        proto.Int64(2),
				MaxLen:        proto.Int64(8),
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_StringLength{StringLength: &storepb.StringLengthConfig{
				MinLen:        proto.Int64(2),
				MaxLen:        proto.Int64(8),
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
				ChildColumn:   "account_id",
				ParentTable:   "accounts",
				ParentColumn:  "id",
				AllowNull:     true,
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
				AllowNull:     true,
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CrossColumn{CrossColumn: &storepb.CrossColumnConfig{
				LeftColumn:    "starts_at",
				RightColumn:   "ends_at",
				Op:            storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LT,
				Tolerance:     proto.Float64(0.5),
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CrossColumn{CrossColumn: &storepb.CrossColumnConfig{
				Op:            storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LT,
				Tolerance:     proto.Float64(0.5),
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Freshness{Freshness: &storepb.FreshnessConfig{
				TimestampColumn: "updated_at",
				MaxLagSeconds:   3600,
				WarnLagSeconds:  proto.Int32(600),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Freshness{Freshness: &storepb.FreshnessConfig{
				MaxLagSeconds:  3600,
				WarnLagSeconds: proto.Int32(600),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CustomSql{CustomSql: &storepb.CustomSQLConfig{
				Sql:           "SELECT 1",
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CustomSql{CustomSql: &storepb.CustomSQLConfig{
				Sql:           "SELECT 1",
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CelExpression{CelExpression: &storepb.CelExpressionConfig{
				Expression:    "true",
				Columns:       []string{"email"},
				SampleRows:    100,
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CelExpression{CelExpression: &storepb.CelExpressionConfig{
				Expression:    "true",
				SampleRows:    100,
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{
				ExpectedColumns: []*storepb.SchemaDriftColumn{{Name: "id", Type: "bigint"}},
				CheckTypes:      proto.Bool(true),
				CheckOrder:      true,
				FailThreshold:   proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_SchemaDrift{SchemaDrift: &storepb.SchemaDriftConfig{
				CheckTypes:    proto.Bool(true),
				CheckOrder:    true,
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_GroupCheck{GroupCheck: &storepb.GroupCheckConfig{
				Columns:         []string{"tenant_id", "status"},
				MinRowsPerGroup: proto.Int64(1),
				MaxRowsPerGroup: proto.Int64(10),
				FailThreshold:   proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_GroupCheck{GroupCheck: &storepb.GroupCheckConfig{
				MinRowsPerGroup: proto.Int64(1),
				MaxRowsPerGroup: proto.Int64(10),
				FailThreshold:   proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Aggregate{Aggregate: &storepb.AggregateConfig{
				Column:    "amount",
				Function:  storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
				Min:       proto.Float64(0),
				Max:       proto.Float64(100),
				WarnMin:   proto.Float64(1),
				Inclusive: true,
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_Aggregate{Aggregate: &storepb.AggregateConfig{
				Function:  storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
				Min:       proto.Float64(0),
				Max:       proto.Float64(100),
				WarnMin:   proto.Float64(1),
				Inclusive: true,
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CrossTable{CrossTable: &storepb.CrossTableConfig{
				RightTable:    "orders",
				LeftFunction:  storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
				LeftColumn:    "total",
				RightFunction: storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
				RightColumn:   "amount",
				Tolerance:     proto.Float64(0.1),
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_CrossTable{CrossTable: &storepb.CrossTableConfig{
				LeftFunction:  storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
				RightFunction: storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
				Tolerance:     proto.Float64(0.1),
				FailThreshold: proto.Float64(1),
			}},
		},
	},
	storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE: {
		input: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_StandardCode{StandardCode: &storepb.StandardCodeConfig{
				Column:        "credit_code",
				Standard:      storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
				AllowNull:     true,
				SampleRows:    100,
				CodeTable:     "",
				FailThreshold: proto.Float64(1),
			}},
			Filter: templateFilter(),
		},
		want: &storepb.QualityRuleConfig{
			Config: &storepb.QualityRuleConfig_StandardCode{StandardCode: &storepb.StandardCodeConfig{
				Standard:      storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
				AllowNull:     true,
				SampleRows:    100,
				FailThreshold: proto.Float64(1),
			}},
		},
	},
}

// The clear function is the only thing standing between a user's editor state and
// a template that names a table. Walking ruleTypeSpecs means a new rule type
// cannot slip through: the missing table row fails the test.
func TestClearRuleConfigTargetScopeContract(t *testing.T) {
	t.Parallel()

	for _, spec := range ruleTypeSpecs {
		t.Run(spec.Type.String(), func(t *testing.T) {
			t.Parallel()

			test, ok := templateScopeCases[spec.Type]
			require.True(t, ok, "add a cleared-config expectation for %s to templateScopeCases", spec.Type)

			got := cloneRuleConfig(test.input)
			ClearRuleConfigTargetScope(got)
			require.True(t, proto.Equal(test.want, got), "clear %s:\n got: %v\nwant: %v", spec.Type, got, test.want)

			// The cleared config must also satisfy the template validator; otherwise
			// the save path would refuse what the clear function just produced.
			err := ValidateRuleTemplateConfig(spec.Type, cloneRuleConfig(got))
			if spec.Type == storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT {
				require.Error(t, err)
				require.Contains(t, err.Error(), "schema drift template is not supported")
				return
			}
			if spec.Type == storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL {
				require.Error(t, err)
				require.Contains(t, err.Error(), "custom SQL template is not supported")
				return
			}
			require.NoError(t, err, "the cleared %s config is a valid template", spec.Type)
		})
	}
}

func TestValidateRuleTemplateConfigRefusals(t *testing.T) {
	t.Parallel()

	validUnique := func() *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{
			FailThreshold: proto.Float64(1),
		}}}
	}
	tests := []struct {
		name      string
		ruleType  storepb.QualityRuleType
		config    *storepb.QualityRuleConfig
		wantError string
	}{
		{
			name:      "unspecified rule type",
			ruleType:  storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED,
			config:    validUnique(),
			wantError: "a rule type is required",
		},
		{
			name:      "unknown rule type",
			ruleType:  storepb.QualityRuleType(99),
			config:    validUnique(),
			wantError: "unknown rule type",
		},
		{
			name:     "schema drift",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_SchemaDrift{
				SchemaDrift: &storepb.SchemaDriftConfig{CheckOrder: true},
			}},
			wantError: "schema drift template is not supported",
		},
		{
			name:      "empty config",
			ruleType:  storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:    &storepb.QualityRuleConfig{},
			wantError: "the rule config is empty",
		},
		{
			name:     "arm mismatch",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
				RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(1)},
			}},
			wantError: "not for the template's",
		},
		{
			name:     "nil arm message",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
				Unique: nil,
			}},
			wantError: "config is required",
		},
		{
			name:     "column names a target",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: &storepb.QualityRuleConfig{
				Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "id"}},
			},
			wantError: "templates do not name columns",
		},
		{
			name:     "filter names a target",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: &storepb.QualityRuleConfig{
				Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{}},
				Filter: templateFilter(),
			},
			wantError: "templates do not carry a filter",
		},
		{
			name:     "empty filter is not a filter",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: &storepb.QualityRuleConfig{
				Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{}},
				Filter: &storepb.QualityFilter{},
			},
		},
		{
			name:     "all_columns names a target",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{
				UniqueCombination: &storepb.UniqueCombinationConfig{AllColumns: true},
			}},
			wantError: "templates do not name columns",
		},
		{
			name:     "related table names a target",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{
				ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{ParentTable: "accounts"},
			}},
			wantError: "templates do not name columns",
		},
		{
			name:     "cel columns name a target",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CelExpression{
				CelExpression: &storepb.CelExpressionConfig{Expression: "true", Columns: []string{"email"}},
			}},
			wantError: "templates do not name columns",
		},
		{
			name:     "cross table names a target",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossTable{
				CrossTable: &storepb.CrossTableConfig{
					RightTable:    "orders",
					LeftFunction:  storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
					RightFunction: storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM,
				},
			}},
			wantError: "templates do not name columns",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateRuleTemplateConfig(test.ruleType, test.config)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantError)
		})
	}
}

func TestValidateRuleTemplateConfigBounds(t *testing.T) {
	t.Parallel()

	unique := func(mutate func(*storepb.QualityRuleConfig)) *storepb.QualityRuleConfig {
		config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{
			FailThreshold: proto.Float64(1),
		}}}
		if mutate != nil {
			mutate(config)
		}
		return config
	}
	rowCount := func(minRows *int64) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
			RowCount: &storepb.RowCountConfig{MinRows: minRows},
		}}
	}
	freshness := func(maxLag int32, warnLag *int32) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{
			Freshness: &storepb.FreshnessConfig{MaxLagSeconds: maxLag, WarnLagSeconds: warnLag},
		}}
	}
	aggregate := func(function storepb.QualityAggregateFunction, percentile *float64, minimum *float64) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Aggregate{
			Aggregate: &storepb.AggregateConfig{Function: function, Percentile: percentile, Min: minimum},
		}}
	}
	crossTable := func(left, right storepb.QualityAggregateFunction, tolerance *float64) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossTable{
			CrossTable: &storepb.CrossTableConfig{LeftFunction: left, RightFunction: right, Tolerance: tolerance},
		}}
	}
	cel := func(sampleRows int32) *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CelExpression{
			CelExpression: &storepb.CelExpressionConfig{Expression: "true", SampleRows: sampleRows},
		}}
	}
	customSQL := func() *storepb.QualityRuleConfig {
		return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{
			CustomSql: &storepb.CustomSQLConfig{Sql: "SELECT 1", FailThreshold: proto.Float64(1)},
		}}
	}

	tests := []struct {
		name      string
		ruleType  storepb.QualityRuleType
		config    *storepb.QualityRuleConfig
		wantError string
	}{
		{name: "negative fail threshold", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:    unique(func(c *storepb.QualityRuleConfig) { c.GetUnique().FailThreshold = proto.Float64(-1) }),
			wantError: "must not be negative"},
		{name: "warn above fail", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:    unique(func(c *storepb.QualityRuleConfig) { c.GetUnique().WarnThreshold = proto.Float64(2) }),
			wantError: "warning threshold 2 exceeds failure threshold 1"},
		{name: "percent threshold above 100", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
				c.GetUnique().FailThreshold = proto.Float64(101)
			}),
			wantError: "above 100 percent"},
		{name: "percent threshold unsupported", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: func() *storepb.QualityRuleConfig {
				config := rowCount(proto.Int64(1))
				config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
				return config
			}(),
			wantError: "PERCENT does not apply"},
		{name: "unknown threshold metric", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.ThresholdMetric = storepb.QualityThresholdMetric(99)
			}),
			wantError: "unknown threshold metric"},
		{name: "unknown empty behavior", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.OnEmpty = storepb.QualityEmptyBehavior(99)
			}),
			wantError: "unknown on_empty behavior"},
		{name: "default timeout", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) { c.TimeoutSeconds = 0 })},
		{name: "maximum timeout", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) { c.TimeoutSeconds = 300 })},
		{name: "timeout above maximum", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:    unique(func(c *storepb.QualityRuleConfig) { c.TimeoutSeconds = 301 }),
			wantError: "timeout_seconds must be between 1 and 300"},
		{name: "negative timeout", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:    unique(func(c *storepb.QualityRuleConfig) { c.TimeoutSeconds = -1 }),
			wantError: "timeout_seconds must be between 1 and 300"},
		{name: "sampling max_rows zero", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_MaxRows{MaxRows: 0}}
			}),
			wantError: "sampling max_rows must be between 1 and 10000000"},
		{name: "sampling max_rows above maximum", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_MaxRows{MaxRows: maxSampledRows + 1}}
			}),
			wantError: "sampling max_rows must be between"},
		{name: "sampling percent below range", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_Percent{Percent: 0.5}}
			}),
			wantError: "sampling percent must be between 1 and 99"},
		{name: "sampling percent above range", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_Percent{Percent: 99.5}}
			}),
			wantError: "sampling percent must be between"},
		{name: "sampling percent NaN", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_Percent{Percent: math.NaN()}}
			}),
			wantError: "sampling percent must be between"},
		{name: "empty sampling object means no sampling", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.Sampling = &storepb.QualityRuleSampling{}
			})},
		{name: "sampling an unsupported type", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: func() *storepb.QualityRuleConfig {
				config := rowCount(proto.Int64(1))
				config.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_MaxRows{MaxRows: 10}}
				return config
			}(),
			wantError: "cannot be sampled"},
		{name: "sampling a type with its own sample_rows", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
			config: func() *storepb.QualityRuleConfig {
				config := cel(0)
				config.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_MaxRows{MaxRows: 10}}
				return config
			}(),
			wantError: "own sample_rows setting"},
		{name: "custom SQL template refused", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			config:    customSQL(),
			wantError: "custom SQL template is not supported"},
		{name: "row count without a bound", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config:    rowCount(nil),
			wantError: "at least one of min_rows, max_rows or exact_rows"},
		{name: "row count change without a bound", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
			config:    &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCountChange{RowCountChange: &storepb.RowCountChangeConfig{}}},
			wantError: "at least one change bound"},
		{name: "null percent without a threshold family", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{},
			}},
			wantError: "needs max_null_count or max_null_pct"},
		{name: "null percent mixes families", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{MaxNullCount: proto.Int64(1), MaxNullPct: proto.Float64(1)},
			}},
			wantError: "not both"},
		{name: "null percent warn above fail", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{MaxNullCount: proto.Int64(2), WarnNullCount: proto.Int64(3)},
			}},
			wantError: "exceeds max_null_count"},
		{name: "null percent above 100", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{MaxNullPct: proto.Float64(101)},
			}},
			wantError: "outside the accepted 0–100 range"},
		{name: "freshness without a lag", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			config:    freshness(0, nil),
			wantError: "max_lag_seconds must be greater than zero"},
		{name: "freshness warn lag not smaller", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			config:    freshness(100, proto.Int32(100)),
			wantError: "must be smaller than max_lag_seconds"},
		{name: "aggregate count", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
			config:    aggregate(storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_COUNT, nil, proto.Float64(1)),
			wantError: "aggregate COUNT duplicates row_count"},
		{name: "aggregate percentile without a value", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
			config:    aggregate(storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE, nil, proto.Float64(1)),
			wantError: "needs a percentile"},
		{name: "aggregate percentile outside range", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
			config:    aggregate(storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_PERCENTILE, proto.Float64(101), proto.Float64(1)),
			wantError: "outside the accepted 0–100 range"},
		{name: "aggregate without a bound", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE,
			config:    aggregate(storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, nil, nil),
			wantError: "needs min or max"},
		{name: "cross table without functions", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
			config:    crossTable(storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_UNSPECIFIED, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, nil),
			wantError: "left function: a cross-table function is required"},
		{name: "cross table negative tolerance", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE,
			config:    crossTable(storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, storepb.QualityAggregateFunction_QUALITY_AGGREGATE_FUNCTION_SUM, proto.Float64(-1)),
			wantError: "must be a non-negative finite number"},
		{name: "cel sample above maximum", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
			config:    cel(200000),
			wantError: "above the maximum"},
		{name: "standard code without a standard", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StandardCode{
				StandardCode: &storepb.StandardCodeConfig{FailThreshold: proto.Float64(1)},
			}},
			wantError: "needs a standard"},
		{name: "custom code table without a reference", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StandardCode{
				StandardCode: &storepb.StandardCodeConfig{
					Standard:      storepb.QualityStandardCode_QUALITY_STANDARD_CODE_CUSTOM_CODE_TABLE,
					FailThreshold: proto.Float64(1),
				},
			}},
			wantError: "requires a code_table reference"},
		{name: "standard code sample above maximum", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STANDARD_CODE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StandardCode{
				StandardCode: &storepb.StandardCodeConfig{
					Standard:      storepb.QualityStandardCode_QUALITY_STANDARD_CODE_UNIFIED_SOCIAL_CREDIT_CODE,
					SampleRows:    200000,
					FailThreshold: proto.Float64(1),
				},
			}},
			wantError: "above the maximum"},
		{name: "valid sampling", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_MaxRows{MaxRows: maxSampledRows}}
			})},
		{name: "valid percent sampling", ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: unique(func(c *storepb.QualityRuleConfig) {
				c.Sampling = &storepb.QualityRuleSampling{Method: &storepb.QualityRuleSampling_Percent{Percent: 99}}
			})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateRuleTemplateConfig(test.ruleType, test.config)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantError)
		})
	}
}
