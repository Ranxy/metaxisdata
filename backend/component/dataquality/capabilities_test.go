package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// capabilityColumn builds a column the way the resolver does.
func capabilityColumn(name, columnType string) ColumnInfo {
	return ColumnInfo{Name: name, Type: columnType, Kind: ClassifyColumnType(columnType)}
}

// capabilityTarget appends the named columns to the shared validation relation.
func capabilityTarget(engine storepb.Engine, columns ...ColumnInfo) ResolvedTarget {
	target := validationTarget(engine)
	target.Columns = append(target.Columns, columns...)
	return target
}

// capabilitySummary is the fact surface the capability matrix pins. The derived
// ordering/binding flags (orderable, valueComparison) have dedicated tests so a row
// does not have to repeat them.
type capabilitySummary struct {
	comparison, grouping, countDistinct, regex, length, extrema, text, noColumnCount, noNullTest, binary bool
}

func summarizeCapabilities(capabilities columnCapabilities) capabilitySummary {
	return capabilitySummary{
		comparison: capabilities.comparison, grouping: capabilities.grouping,
		countDistinct: capabilities.countDistinct, regex: capabilities.regex,
		length: capabilities.length, extrema: capabilities.extrema, text: capabilities.text,
		noColumnCount: capabilities.noColumnCount, noNullTest: capabilities.noNullTest,
		binary: capabilities.binary,
	}
}

// TestCapabilityMatrix pins the per-engine type surface. Each field is a separate
// question because engines answer differently: comparison, grouping,
// COUNT(DISTINCT), regex, character length, MIN/MAX and text rendering.
func TestCapabilityMatrix(t *testing.T) {
	t.Parallel()

	text := summarizeCapabilities(textCapabilities)
	tests := []struct {
		name       string
		engine     storepb.Engine
		columnType string
		want       capabilitySummary
	}{
		// PostgreSQL.
		{name: "pg text", engine: storepb.Engine_POSTGRES, columnType: "character varying(64)", want: text},
		{name: "pg name", engine: storepb.Engine_POSTGRES, columnType: "name", want: text},
		{name: "pg citext", engine: storepb.Engine_POSTGRES, columnType: "public.citext", want: text},
		{name: "pg jsonb", engine: storepb.Engine_POSTGRES, columnType: "jsonb",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true}},
		{name: "pg json", engine: storepb.Engine_POSTGRES, columnType: "json", want: capabilitySummary{}},
		{name: "pg xml", engine: storepb.Engine_POSTGRES, columnType: "xml", want: capabilitySummary{}},
		{name: "pg uuid", engine: storepb.Engine_POSTGRES, columnType: "uuid",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true}},
		{name: "pg bytea", engine: storepb.Engine_POSTGRES, columnType: "bytea",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, binary: true}},
		{name: "pg interval", engine: storepb.Engine_POSTGRES, columnType: "interval",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, extrema: true}},
		{name: "pg inet", engine: storepb.Engine_POSTGRES, columnType: "inet",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, extrema: true}},
		{name: "pg macaddr has no MIN", engine: storepb.Engine_POSTGRES, columnType: "macaddr",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true}},
		{name: "pg macaddr8 has no MIN", engine: storepb.Engine_POSTGRES, columnType: "macaddr8",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true}},
		{name: "pg bit is a bit string", engine: storepb.Engine_POSTGRES, columnType: "bit(8)",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true}},
		{name: "pg int4range", engine: storepb.Engine_POSTGRES, columnType: "int4range",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true}},
		{name: "pg matview text array", engine: storepb.Engine_POSTGRES, columnType: "text[]",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, extrema: true}},
		{name: "pg matview integer array", engine: storepb.Engine_POSTGRES, columnType: "integer[]",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, extrema: true}},
		{name: "pg matview varchar array", engine: storepb.Engine_POSTGRES, columnType: "character varying[]",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, extrema: true}},
		{name: "pg matview char array", engine: storepb.Engine_POSTGRES, columnType: "character(4)[]",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, extrema: true}},
		{name: "pg table citext array", engine: storepb.Engine_POSTGRES, columnType: "_citext",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, extrema: true}},
		{name: "pg box compares but does not group", engine: storepb.Engine_POSTGRES, columnType: "box",
			want: capabilitySummary{comparison: true}},
		{name: "pg point has no equality", engine: storepb.Engine_POSTGRES, columnType: "point", want: capabilitySummary{}},
		{name: "pg polygon has no equality", engine: storepb.Engine_POSTGRES, columnType: "polygon", want: capabilitySummary{}},
		{name: "pg PostGIS geometry", engine: storepb.Engine_POSTGRES, columnType: "public.geometry", want: capabilitySummary{}},
		{name: "pg user-defined types keep the rule allowance", engine: storepb.Engine_POSTGRES, columnType: "public.mood",
			want: capabilitySummary{comparison: true, grouping: true}},
		{name: "pg domain over json keeps the rule allowance", engine: storepb.Engine_POSTGRES,
			columnType: "dqprobe.myjson", want: capabilitySummary{comparison: true, grouping: true}},
		{name: "pg text array", engine: storepb.Engine_POSTGRES, columnType: "_text",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, extrema: true}},
		{name: "pg json array has no equality", engine: storepb.Engine_POSTGRES, columnType: "_json", want: capabilitySummary{}},
		{name: "pg point array has no equality", engine: storepb.Engine_POSTGRES, columnType: "_point", want: capabilitySummary{}},
		// SQL Server.
		{name: "mssql nvarchar", engine: storepb.Engine_MSSQL, columnType: "nvarchar(64)", want: capabilitySummary{
			comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, text: true,
		}},
		{name: "mssql legacy text shapes but cannot aggregate", engine: storepb.Engine_MSSQL, columnType: "text",
			want: capabilitySummary{text: true, noColumnCount: true}},
		{name: "mssql xml", engine: storepb.Engine_MSSQL, columnType: "xml", want: capabilitySummary{}},
		{name: "mssql varbinary", engine: storepb.Engine_MSSQL, columnType: "varbinary(max)", want: capabilitySummary{
			comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, binary: true,
		}},
		{name: "mssql uniqueidentifier", engine: storepb.Engine_MSSQL, columnType: "uniqueidentifier", want: capabilitySummary{
			comparison: true, grouping: true, countDistinct: true, length: true, extrema: true, binary: true,
		}},
		{name: "mssql geometry", engine: storepb.Engine_MSSQL, columnType: "geometry", want: capabilitySummary{}},
		{name: "mssql hierarchyid compares, groups and orders", engine: storepb.Engine_MSSQL, columnType: "hierarchyid",
			want: capabilitySummary{comparison: true, grouping: true, countDistinct: true, extrema: true, binary: true}},
		// MySQL family.
		{name: "mysql varchar", engine: storepb.Engine_MYSQL, columnType: "varchar(64)", want: text},
		{name: "mysql json cannot compare", engine: storepb.Engine_MYSQL, columnType: "json",
			want: capabilitySummary{grouping: true, countDistinct: true, regex: true, length: true, extrema: true}},
		{name: "mysql geometry", engine: storepb.Engine_MYSQL, columnType: "geometry",
			want: capabilitySummary{grouping: true, countDistinct: true, binary: true}},
		{name: "mysql geomcollection", engine: storepb.Engine_MYSQL, columnType: "geomcollection",
			want: capabilitySummary{grouping: true, countDistinct: true, binary: true}},
		{name: "mysql blob", engine: storepb.Engine_MYSQL, columnType: "blob", want: capabilitySummary{
			comparison: true, grouping: true, countDistinct: true, regex: true, length: true, extrema: true, binary: true,
		}},
		// StarRocks.
		{name: "starrocks varchar", engine: storepb.Engine_STARROCKS, columnType: "varchar(64)", want: text},
		{name: "starrocks json compares but does not group", engine: storepb.Engine_STARROCKS, columnType: "json",
			want: capabilitySummary{comparison: true, length: true}},
		{name: "starrocks array groups", engine: storepb.Engine_STARROCKS, columnType: "array<int(11)>",
			want: capabilitySummary{grouping: true, countDistinct: true}},
		{name: "starrocks array of json cannot group", engine: storepb.Engine_STARROCKS, columnType: "array<json>",
			want: capabilitySummary{}},
		{name: "starrocks nested struct json array cannot group", engine: storepb.Engine_STARROCKS,
			columnType: "array<struct<a json>>", want: capabilitySummary{}},
		{name: "starrocks nested map json array cannot group", engine: storepb.Engine_STARROCKS,
			columnType: "array<map<varchar(64),json>>", want: capabilitySummary{}},
		{name: "starrocks array of variant cannot group", engine: storepb.Engine_STARROCKS,
			columnType: "array<variant>", want: capabilitySummary{}},
		{name: "starrocks struct field named json is not a json element", engine: storepb.Engine_STARROCKS,
			columnType: "array<struct<`json` int(11)>>",
			want:       capabilitySummary{grouping: true, countDistinct: true}},
		{name: "starrocks map", engine: storepb.Engine_STARROCKS, columnType: "map<varchar,int>", want: capabilitySummary{}},
		{name: "starrocks bitmap has no null test", engine: storepb.Engine_STARROCKS, columnType: "bitmap",
			want: capabilitySummary{countDistinct: true, noNullTest: true, binary: true}},
		{name: "starrocks varbinary measures but does not order", engine: storepb.Engine_STARROCKS, columnType: "varbinary",
			want: capabilitySummary{grouping: true, countDistinct: true, length: true, binary: true}},
		// Doris.
		{name: "doris varchar", engine: storepb.Engine_DORIS, columnType: "varchar(64)", want: text},
		{name: "doris json groups but cannot compare", engine: storepb.Engine_DORIS, columnType: "json",
			want: capabilitySummary{grouping: true, regex: true, length: true}},
		{name: "doris array", engine: storepb.Engine_DORIS, columnType: "array<int(11)>", want: capabilitySummary{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			column := ColumnInfo{
				Name: "col",
				Type: test.columnType,
				Kind: classifyColumnTypeForEngine(test.engine, test.columnType),
			}
			require.Equal(t, test.want, summarizeCapabilities(capabilitiesFor(test.engine, column)))
		})
	}
}

// TestArrayCapabilitiesFollowTheElementOrdering pins the array rule: an array is
// comparable/groupable/orderable only when its element has a btree ordering operator
// class. The built-in geometric types have = but no btree, so their arrays are not
// (PostgreSQL reports "could not identify a comparison function for type box").
func TestArrayCapabilitiesFollowTheElementOrdering(t *testing.T) {
	t.Parallel()

	textArray := capabilityTarget(storepb.Engine_POSTGRES, capabilityColumn("tags", "_text"))
	selected := profileTestColumns(t, textArray, "tags")
	require.Equal(t, profileFactDistinct|profileFactExtrema, selected[0].Facts)
	require.False(t, capabilitiesFor(storepb.Engine_POSTGRES, capabilityColumn("tags", "_text")).valueComparison,
		"a scalar literal cannot be bound to an array column")

	for _, spelling := range []string{"_box", "box[]", "_line", "line[]", "_lseg", "_path", "_circle", "circle[]"} {
		column := ColumnInfo{Name: "a", Type: spelling, Kind: classifyColumnTypeForEngine(storepb.Engine_POSTGRES, spelling)}
		capabilities := capabilitiesFor(storepb.Engine_POSTGRES, column)
		require.Equal(t, columnCapabilities{}, capabilities, "%s must have no capability", spelling)
	}

	// numeric(10,2)[] and date[] route through the array branch (bound as an array
	// literal, not as a scalar) and keep their profile facts.
	for _, spelling := range []string{"numeric(10,2)[]", "double precision[]", "date[]"} {
		column := ColumnInfo{Name: "a", Type: spelling, Kind: classifyColumnTypeForEngine(storepb.Engine_POSTGRES, spelling)}
		require.Equal(t, ColumnKindString, column.Kind, "%s is bound as an array", spelling)
		capabilities := capabilitiesFor(storepb.Engine_POSTGRES, column)
		require.True(t, capabilities.comparison, spelling)
		require.True(t, capabilities.extrema, spelling)
		require.False(t, capabilities.valueComparison, spelling)
	}
}

// TestValueComparisonSeparatesScalarsFromArrays pins the rule surface: filters and
// accepted values bind a scalar, so they are refused for array columns even though
// the array itself supports comparison (joins).
func TestValueComparisonSeparatesScalarsFromArrays(t *testing.T) {
	t.Parallel()

	target := capabilityTarget(storepb.Engine_POSTGRES, capabilityColumn("tags", "_text"))
	require.Error(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		rowCountWithJSONFilterOn("tags", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ)))
	require.Error(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{Column: "tags", Values: []string{"a"}})))
	// The array still groups, so uniqueness stays legal.
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		validationUniqueConfig(&storepb.UniqueConfig{Column: "tags"})))
}

// TestValidationRejectsTypesTheEngineCannotRun pins the classes of rule the
// capability checks refuse: text operators on types without them, grouping on
// types without equality, and comparison where a bound string would never match.
func TestValidationRejectsTypesTheEngineCannotRun(t *testing.T) {
	t.Parallel()

	target := capabilityTarget(storepb.Engine_POSTGRES,
		capabilityColumn("payload", "jsonb"),
		capabilityColumn("payload_json", "json"),
		capabilityColumn("doc", "xml"),
		capabilityColumn("uid", "uuid"),
		capabilityColumn("box_col", "box"),
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
			want:     "needs a text type",
		},
		{
			name:     "string_length on uuid",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
			config: validationStringLengthConfig(&storepb.StringLengthConfig{
				Column: "uid", MinLen: validationPtr(int64(10)),
			}),
			want: "needs a text type",
		},
		{
			name:     "unique on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:   validationUniqueConfig(&storepb.UniqueConfig{Column: "payload_json"}),
			want:     "cannot group values of this type",
		},
		{
			name:     "unique on box",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config:   validationUniqueConfig(&storepb.UniqueConfig{Column: "box_col"}),
			want:     "cannot group values of this type",
		},
		{
			name:     "group_check on xml",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_GroupCheck{
				GroupCheck: &storepb.GroupCheckConfig{
					Columns: []string{"doc"}, MinRowsPerGroup: validationPtr(int64(1)),
				},
			}},
			want: "cannot group values of this type",
		},
		{
			name:     "accepted_values on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			config: validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{
				Column: "payload_json", Values: []string{"{}"},
			}),
			want: "cannot compare it with a scalar value",
		},
		{
			name:     "null_percent custom values on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: validationNullPercentConfig(&storepb.NullPercentConfig{
				Column: "payload_json", MaxNullCount: validationPtr(int64(0)), NullValues: []string{"{}"},
			}),
			want: "cannot compare it with a scalar value",
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
			want: "cannot compare it with a scalar value",
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

	// all_columns expands to the whole relation, so one ungroupable column makes
	// the rule unusable even when the author never named it.
	combo := capabilityTarget(storepb.Engine_POSTGRES, capabilityColumn("payload_json", "json"))
	err := ValidateConfig(combo, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{AllColumns: true}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot group values of this type")
}

// TestValidationAllowsSupportedTypes pins that the checks do not reject what the
// engines support: a box can be compared even though it cannot be grouped, and
// jsonb supports grouping where json does not.
func TestValidationAllowsSupportedTypes(t *testing.T) {
	t.Parallel()

	target := capabilityTarget(storepb.Engine_POSTGRES,
		capabilityColumn("payload", "jsonb"),
		capabilityColumn("payload_json", "json"),
		capabilityColumn("uid", "uuid"),
		capabilityColumn("email", "public.citext"),
		capabilityColumn("box_col", "box"),
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
			config:   rowCountWithJSONFilterOn("payload", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IN),
		},
		{
			name:     "filter on box",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config:   rowCountWithJSONFilterOn("box_col", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ),
		},
		{
			name:     "is null filter on json",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config:   rowCountWithJSONFilterOn("payload_json", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NULL),
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

// rowCountWithJSONFilterOn builds a row-count rule whose filter compares one
// column with a value.
func rowCountWithJSONFilterOn(column string, op storepb.QualityFilterOperator) *storepb.QualityRuleConfig {
	clause := &storepb.QualityFilterClause{Column: column, Op: op}
	switch op {
	case storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NULL,
		storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NOT_NULL:
	default:
		clause.Values = []string{`{"k":1}`}
	}
	config := validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1))})
	config.Filter = &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{clause}}
	return config
}

// TestValidationUsesEngineTypeSpellings pins engine-specific allowances and
// rejections: StarRocks compares JSON but cannot group it, Doris groups JSON but
// cannot compare it, MySQL JSON comparison is refused (a bound string never equals
// a JSON document), and SQL Server cannot measure its legacy text type.
func TestValidationUsesEngineTypeSpellings(t *testing.T) {
	t.Parallel()

	starrocks := capabilityTarget(storepb.Engine_STARROCKS, capabilityColumn("payload", "json"))
	require.NoError(t, ValidateConfig(starrocks, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		rowCountWithJSONFilterOn("payload", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ)))
	require.Error(t, ValidateConfig(starrocks, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		validationUniqueConfig(&storepb.UniqueConfig{Column: "payload"})))

	doris := capabilityTarget(storepb.Engine_DORIS, capabilityColumn("payload", "json"))
	require.NoError(t, ValidateConfig(doris, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		validationUniqueConfig(&storepb.UniqueConfig{Column: "payload"})))
	require.Error(t, ValidateConfig(doris, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		rowCountWithJSONFilterOn("payload", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ)))
	require.NoError(t, ValidateConfig(doris, storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		validationStringLengthConfig(&storepb.StringLengthConfig{Column: "payload", MaxLen: validationPtr(int64(10))})))

	mysql := capabilityTarget(storepb.Engine_MYSQL, capabilityColumn("payload", "json"))
	require.Error(t, ValidateConfig(mysql, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		rowCountWithJSONFilterOn("payload", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ)))
	require.NoError(t, ValidateConfig(mysql, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		validationUniqueConfig(&storepb.UniqueConfig{Column: "payload"})))

	mssql := capabilityTarget(storepb.Engine_MSSQL, capabilityColumn("legacy", "text"))
	require.Error(t, ValidateConfig(mssql, storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		validationStringLengthConfig(&storepb.StringLengthConfig{Column: "legacy", MaxLen: validationPtr(int64(10))})))
	require.NoError(t, ValidateConfig(mssql, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		validationNullPercentConfig(&storepb.NullPercentConfig{Column: "legacy", MaxNullCount: validationPtr(int64(0))})))

	// Doris bitmap accepts IS NULL (4.1.4); the capability must not be copied from
	// StarRocks.
	dorisBitmap := capabilityTarget(storepb.Engine_DORIS, capabilityColumn("bm", "bitmap"))
	require.NoError(t, ValidateConfig(dorisBitmap, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		validationNullPercentConfig(&storepb.NullPercentConfig{Column: "bm", MaxNullCount: validationPtr(int64(0))})))
	require.NoError(t, ValidateConfig(dorisBitmap, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		rowCountWithJSONFilterOn("bm", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NULL)))

	// StarRocks bitmap rejects even IS NULL, so a plain null check cannot run.
	bitmap := capabilityTarget(storepb.Engine_STARROCKS, capabilityColumn("bm", "bitmap"))
	require.Error(t, ValidateConfig(bitmap, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		validationNullPercentConfig(&storepb.NullPercentConfig{Column: "bm", MaxNullCount: validationPtr(int64(0))})))
	require.Error(t, ValidateConfig(bitmap, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		rowCountWithJSONFilterOn("bm", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_IS_NULL)))

	// A PostgreSQL user-defined type keeps the historical rule allowance (enums are
	// executable; a composite predicate is a documented pre-existing limitation).
	userDefined := capabilityTarget(storepb.Engine_POSTGRES, capabilityColumn("comp", "dqprobe.mycomp"))
	require.NoError(t, ValidateConfig(userDefined, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		validationUniqueConfig(&storepb.UniqueConfig{Column: "comp"})))

	// PostgreSQL BIT is a bit string, not a boolean: filters bind a string that the
	// engine parses as bits, while text rules are not applicable.
	bit := capabilityTarget(storepb.Engine_POSTGRES, ColumnInfo{
		Name: "b", Type: "bit(8)", Kind: classifyColumnTypeForEngine(storepb.Engine_POSTGRES, "bit(8)"),
	})
	require.NoError(t, ValidateConfig(bit, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		rowCountWithJSONFilterOn("b", storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ)))
	require.Error(t, ValidateConfig(bit, storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		validationStringLengthConfig(&storepb.StringLengthConfig{Column: "b", MaxLen: validationPtr(int64(8))})))
}

// TestClassificationCoversEngineSpellings pins the type spellings the classifier
// used to miss: MySQL YEAR, StarRocks/Doris LARGEINT and Doris' DECIMALV3 compare
// and aggregate as numbers, and MSSQL DATETIMEOFFSET is temporal.
func TestClassificationCoversEngineSpellings(t *testing.T) {
	t.Parallel()

	require.Equal(t, ColumnKindNumeric, ClassifyColumnType("year"))
	require.Equal(t, ColumnKindNumeric, ClassifyColumnType("largeint"))
	require.Equal(t, ColumnKindNumeric, ClassifyColumnType("decimalv3(10, 2)"))
	require.Equal(t, ColumnKindNumeric, ClassifyColumnType("decimalv2(10, 2)"))
	require.Equal(t, ColumnKindTemporal, ClassifyColumnType("datetimeoffset"))
	require.Equal(t, ColumnKindNumeric, ClassifyColumnType("YEAR(4)"))
}

// TestFailureRowsAreNotStricterThanTheAggregate pins the invariant that broke a
// plain null_percent rule on a json column: the failed-row sample must compile
// wherever the aggregate did. Only the custom missing values compare the column, so
// only they require comparison support.
func TestFailureRowsAreNotStricterThanTheAggregate(t *testing.T) {
	t.Parallel()

	target := capabilityTarget(storepb.Engine_POSTGRES,
		capabilityColumn("payload_json", "json"),
		capabilityColumn("doc", "xml"),
	)
	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	for _, name := range []string{"payload_json", "doc"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			input := CompileInput{
				Dialect: dialect,
				Target:  target,
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
				Config: validationNullPercentConfig(&storepb.NullPercentConfig{
					Column: name, MaxNullCount: validationPtr(int64(0)),
				}),
			}
			aggregate, err := Compile(input)
			require.NoError(t, err)
			require.NotEmpty(t, aggregate.SQL)
			sample, err := CompileFailureRows(input, 20)
			require.NoError(t, err)
			require.NotEmpty(t, sample.SQL)
		})
	}

	// The custom-null-values arm compares the column, so both the aggregate and the
	// sample must refuse it for a type without comparison support.
	custom := CompileInput{
		Dialect: dialect,
		Target:  target,
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config: validationNullPercentConfig(&storepb.NullPercentConfig{
			Column: "payload_json", MaxNullCount: validationPtr(int64(0)), NullValues: []string{`{}`},
		}),
	}
	_, err = Compile(custom)
	require.Error(t, err)
	_, err = CompileFailureRows(custom, 20)
	require.Error(t, err)
}
