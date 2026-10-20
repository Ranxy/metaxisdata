package dataquality

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// capabilityColumn builds a column the same way the resolver does.
func capabilityColumn(name, columnType string) ColumnInfo {
	return ColumnInfo{Name: name, Type: columnType, Kind: ClassifyColumnType(columnType)}
}

// capabilityTarget appends the named columns to the shared validation relation.
func capabilityTarget(engine storepb.Engine, columns ...ColumnInfo) ResolvedTarget {
	target := validationTarget(engine)
	target.Columns = append(target.Columns, columns...)
	return target
}

// TestSupportsText pins which engine types have regex operators and a character
// length. The lists are deny-lists: an unknown type keeps its historical
// allowance so a user-defined text type does not lose the rules it supports.
func TestSupportsText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		engine     storepb.Engine
		columnType string
		want       bool
	}{
		{name: "postgres varchar", engine: storepb.Engine_POSTGRES, columnType: "character varying(255)", want: true},
		{name: "postgres text", engine: storepb.Engine_POSTGRES, columnType: "text", want: true},
		{name: "postgres name", engine: storepb.Engine_POSTGRES, columnType: "name", want: true},
		{name: "postgres citext", engine: storepb.Engine_POSTGRES, columnType: "public.citext", want: true},
		{name: "postgres jsonb", engine: storepb.Engine_POSTGRES, columnType: "jsonb", want: false},
		{name: "postgres json", engine: storepb.Engine_POSTGRES, columnType: "json", want: false},
		{name: "postgres xml", engine: storepb.Engine_POSTGRES, columnType: "xml", want: false},
		{name: "postgres uuid", engine: storepb.Engine_POSTGRES, columnType: "uuid", want: false},
		{name: "postgres bytea", engine: storepb.Engine_POSTGRES, columnType: "bytea", want: false},
		{name: "postgres interval", engine: storepb.Engine_POSTGRES, columnType: "interval", want: false},
		{name: "postgres array", engine: storepb.Engine_POSTGRES, columnType: "_int4", want: false},
		{name: "postgres enum", engine: storepb.Engine_POSTGRES, columnType: "public.mood", want: false},
		{name: "mysql varchar", engine: storepb.Engine_MYSQL, columnType: "varchar(64)", want: true},
		{name: "mysql json", engine: storepb.Engine_MYSQL, columnType: "json", want: true},
		{name: "mysql geometry", engine: storepb.Engine_MYSQL, columnType: "geometry", want: true},
		{name: "mssql nvarchar", engine: storepb.Engine_MSSQL, columnType: "nvarchar(64)", want: true},
		{name: "mssql text", engine: storepb.Engine_MSSQL, columnType: "text", want: true},
		{name: "mssql xml", engine: storepb.Engine_MSSQL, columnType: "xml", want: false},
		{name: "mssql uniqueidentifier", engine: storepb.Engine_MSSQL, columnType: "uniqueidentifier", want: false},
		{name: "mssql varbinary", engine: storepb.Engine_MSSQL, columnType: "varbinary(max)", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, supportsText(capabilityColumn("col", test.columnType), test.engine))
		})
	}
}

// TestSupportsEquality pins which engine types can be compared, grouped and
// filtered by value.
func TestSupportsEquality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		engine     storepb.Engine
		columnType string
		want       bool
	}{
		{name: "postgres text", engine: storepb.Engine_POSTGRES, columnType: "text", want: true},
		{name: "postgres jsonb", engine: storepb.Engine_POSTGRES, columnType: "jsonb", want: true},
		{name: "postgres uuid", engine: storepb.Engine_POSTGRES, columnType: "uuid", want: true},
		{name: "postgres bytea", engine: storepb.Engine_POSTGRES, columnType: "bytea", want: true},
		{name: "postgres array", engine: storepb.Engine_POSTGRES, columnType: "_int4", want: true},
		{name: "postgres enum", engine: storepb.Engine_POSTGRES, columnType: "public.mood", want: true},
		{name: "postgres json", engine: storepb.Engine_POSTGRES, columnType: "json", want: false},
		{name: "postgres xml", engine: storepb.Engine_POSTGRES, columnType: "xml", want: false},
		{name: "postgres point", engine: storepb.Engine_POSTGRES, columnType: "point", want: false},
		{name: "postgres postgis geometry", engine: storepb.Engine_POSTGRES, columnType: "public.geometry", want: false},
		{name: "mysql json", engine: storepb.Engine_MYSQL, columnType: "json", want: true},
		{name: "mysql geometry", engine: storepb.Engine_MYSQL, columnType: "geometry", want: true},
		{name: "mssql nvarchar", engine: storepb.Engine_MSSQL, columnType: "nvarchar(64)", want: true},
		{name: "mssql text", engine: storepb.Engine_MSSQL, columnType: "text", want: false},
		{name: "mssql xml", engine: storepb.Engine_MSSQL, columnType: "xml", want: false},
		{name: "mssql geometry", engine: storepb.Engine_MSSQL, columnType: "geometry", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, supportsEquality(capabilityColumn("col", test.columnType), test.engine))
		})
	}
}

// TestValidationRejectsTypesTheEngineCannotRun pins the A and B classes: a rule
// that compiles to operators its column's type does not support is refused at save
// time instead of being saved and failing on every execution.
func TestValidationRejectsTypesTheEngineCannotRun(t *testing.T) {
	t.Parallel()

	target := capabilityTarget(storepb.Engine_POSTGRES,
		capabilityColumn("payload", "jsonb"),
		capabilityColumn("payload_json", "json"),
		capabilityColumn("doc", "xml"),
		capabilityColumn("uid", "uuid"),
	)
	tests := []struct {
		name     string
		ruleType storepb.QualityRuleType
		config   *storepb.QualityRuleConfig
		want     string
	}{
		{
			name:     "regex on jsonb",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			config:   validationRegexConfig(&storepb.RegexConfig{Column: "payload", Pattern: "^x"}),
			want:     "this rule needs a text column",
		},
		{
			name:     "string_length on uuid",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
			config: validationStringLengthConfig(&storepb.StringLengthConfig{
				Column: "uid", MinLen: validationPtr(int64(10)),
			}),
			want: "this rule needs a text column",
		},
		{
			name:     "unique on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:   validationUniqueConfig(&storepb.UniqueConfig{Column: "payload_json"}),
			want:     "the engine cannot compare or group values of this type",
		},
		{
			name:     "unique_combination all columns",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
			config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{
				AllColumns: true,
			}),
			want: "the engine cannot compare or group values of this type",
		},
		{
			name:     "group_check on xml",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_GroupCheck{
				GroupCheck: &storepb.GroupCheckConfig{
					Columns: []string{"doc"}, MinRowsPerGroup: validationPtr(int64(1)),
				},
			}},
			want: "the engine cannot compare or group values of this type",
		},
		{
			name:     "accepted_values on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			config: validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{
				Column: "payload_json", Values: []string{"{}"},
			}),
			want: "the engine cannot compare or group values of this type",
		},
		{
			name:     "null_percent custom values on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: validationNullPercentConfig(&storepb.NullPercentConfig{
				Column: "payload_json", MaxNullCount: validationPtr(int64(0)), NullValues: []string{"{}"},
			}),
			want: "the engine cannot compare or group values of this type",
		},
		{
			name:     "filter on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: func() *storepb.QualityRuleConfig {
				config := validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1))})
				config.Filter = &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
					Column: "payload_json",
					Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
					Values: []string{"{}"},
				}}}
				return config
			}(),
			want: "the engine cannot compare or group values of this type",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateConfig(target, test.ruleType, test.config)
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.want)
		})
	}
}

// TestValidationAllowsComparableTypes pins that the new checks do not reject the
// types the engines do support: every "string" column is not opaque, and jsonb
// (unlike json) has equality.
func TestValidationAllowsComparableTypes(t *testing.T) {
	t.Parallel()

	target := capabilityTarget(storepb.Engine_POSTGRES,
		capabilityColumn("payload", "jsonb"),
		capabilityColumn("payload_json", "json"),
		capabilityColumn("uid", "uuid"),
		capabilityColumn("email", "public.citext"),
	)
	tests := []struct {
		name     string
		ruleType storepb.QualityRuleType
		config   *storepb.QualityRuleConfig
	}{
		{
			name:     "regex on citext",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			config:   validationRegexConfig(&storepb.RegexConfig{Column: "email", Pattern: "^x"}),
		},
		{
			name:     "string_length on text",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
			config:   validationStringLengthConfig(&storepb.StringLengthConfig{Column: "name", MaxLen: validationPtr(int64(10))}),
		},
		{
			name:     "unique on jsonb",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:   validationUniqueConfig(&storepb.UniqueConfig{Column: "payload"}),
		},
		{
			name:     "unique on uuid",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:   validationUniqueConfig(&storepb.UniqueConfig{Column: "uid"}),
		},
		{
			name:     "accepted_values on jsonb",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			config:   validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{Column: "payload", Values: []string{"{}"}}),
		},
		{
			name:     "filter on jsonb",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: func() *storepb.QualityRuleConfig {
				config := validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1))})
				config.Filter = &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
					Column: "payload",
					Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IN,
					Values: []string{"{}"},
				}}}
				return config
			}(),
		},
		{
			name:     "is null filter on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: func() *storepb.QualityRuleConfig {
				config := validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1))})
				config.Filter = &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
					Column: "payload_json",
					Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NULL,
				}}}
				return config
			}(),
		},
		{
			name:     "plain null_percent on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: validationNullPercentConfig(&storepb.NullPercentConfig{
				Column: "payload_json", MaxNullCount: validationPtr(int64(0)),
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.NoError(t, ValidateConfig(target, test.ruleType, test.config))
		})
	}
}

// TestValidationUsesEngineTextSpellings pins that the MySQL wire family keeps the
// rules its engines actually run: it coerces JSON and spatial values to text.
func TestValidationUsesEngineTextSpellings(t *testing.T) {
	t.Parallel()

	target := capabilityTarget(storepb.Engine_MYSQL,
		capabilityColumn("payload", "json"),
		capabilityColumn("shape", "geometry"),
		validationColumn("year_built", "year", true),
	)
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		validationRegexConfig(&storepb.RegexConfig{Column: "payload", Pattern: "^x"})))
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		validationStringLengthConfig(&storepb.StringLengthConfig{Column: "payload", MaxLen: validationPtr(int64(10))})))
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		validationUniqueConfig(&storepb.UniqueConfig{Column: "shape"})))
	// YEAR is numeric: a range rule is meaningful and a text rule is not.
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		validationRangeConfig(&storepb.RangeConfig{Column: "year_built", Min: validationPtr("2000")})))
}

// TestClassificationCoversEngineSpellings pins the type spellings the classifier
// used to miss: MySQL YEAR and StarRocks/Doris LARGEINT compare and aggregate as
// numbers, and MSSQL DATETIMEOFFSET is a temporal type.
func TestClassificationCoversEngineSpellings(t *testing.T) {
	t.Parallel()

	require.Equal(t, ColumnKindNumeric, ClassifyColumnType("year"))
	require.Equal(t, ColumnKindNumeric, ClassifyColumnType("largeint"))
	require.Equal(t, ColumnKindTemporal, ClassifyColumnType("datetimeoffset"))
	require.Equal(t, ColumnKindNumeric, ClassifyColumnType("YEAR(4)"))
	require.True(t, strings.HasPrefix(ClassifyColumnType("datetime2").String(), "temporal"))
}
