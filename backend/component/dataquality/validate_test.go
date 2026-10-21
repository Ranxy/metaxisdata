package dataquality

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// validationPtr is a local pointer helper. It is named to stay out of the way of
// helpers another test file in this package may define.
func validationPtr[T any](value T) *T {
	return &value
}

// validationColumn builds a column whose kind comes from the type string, so the
// tests exercise the same classification the resolver uses.
func validationColumn(name, columnType string, nullable bool) ColumnInfo {
	return ColumnInfo{Name: name, Type: columnType, Kind: ClassifyColumnType(columnType), Nullable: nullable}
}

// validationTarget builds a resolved target for a table with the columns every
// validation case below names. The schema segment and GUID follow the engine's
// naming convention: the MySQL family has no schema, PostgreSQL/MSSQL do.
func validationTarget(engine storepb.Engine) ResolvedTarget {
	target := ResolvedTarget{
		Target:             Target{GUID: "inst1;app;orders", MetaType: storepb.MetaType_TABLE},
		Engine:             engine,
		InstanceResourceID: "inst1",
		Database:           "app",
		Table:              "orders",
		Columns: []ColumnInfo{
			validationColumn("id", "bigint", false),
			validationColumn("amount", "decimal(10,2)", true),
			validationColumn("name", "varchar(64)", true),
			validationColumn("created_at", "timestamptz", true),
			validationColumn("updated_at", "timestamptz", true),
			validationColumn("active", "boolean", true),
		},
	}
	if !isMySQLWireEngine(engine) {
		target.GUID = "inst1;app;public;orders"
		target.Schema = "public"
	}
	return target
}

func validationRowCountConfig(config *storepb.RowCountConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{RowCount: config}}
}

func validationRowCountChangeConfig(config *storepb.RowCountChangeConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCountChange{RowCountChange: config}}
}

func validationNullPercentConfig(config *storepb.NullPercentConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{NullPercent: config}}
}

func validationUniqueConfig(config *storepb.UniqueConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{Unique: config}}
}

func validationUniqueCombinationConfig(config *storepb.UniqueCombinationConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{UniqueCombination: config}}
}

func validationAcceptedValuesConfig(config *storepb.AcceptedValuesConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{AcceptedValues: config}}
}

func validationRangeConfig(config *storepb.RangeConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{Range: config}}
}

func validationRegexConfig(config *storepb.RegexConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{Regex: config}}
}

func validationStringLengthConfig(config *storepb.StringLengthConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StringLength{StringLength: config}}
}

func validationReferentialIntegrityConfig(config *storepb.ReferentialIntegrityConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{ReferentialIntegrity: config}}
}

func validationCrossColumnConfig(config *storepb.CrossColumnConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{CrossColumn: config}}
}

func validationFreshnessConfig(config *storepb.FreshnessConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{Freshness: config}}
}

func validationCustomSQLConfig(config *storepb.CustomSQLConfig) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{CustomSql: config}}
}

// validationCase is one config and the verdict ValidateConfig should return.
type validationCase struct {
	name    string
	config  *storepb.QualityRuleConfig
	wantErr bool
}

// runValidationCases runs one rule type's table and asserts the error code, not
// just that an error happened: every validation failure is InvalidArgument.
func runValidationCases(t *testing.T, ruleType storepb.QualityRuleType, target ResolvedTarget, cases []validationCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateConfig(target, ruleType, testCase.config)
			if !testCase.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err), "error: %v", err)
		})
	}
}

// A config whose oneof arm names another rule type, or a config with no arm at
// all, must be refused instead of being read as a nil arm at run time.
func TestValidateConfigRejectsConfigTypeMismatch(t *testing.T) {
	t.Parallel()

	target := validationTarget(storepb.Engine_MYSQL)
	rowCount := validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1))})

	err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, rowCount)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	err = ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, nil)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	err = ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, &storepb.QualityRuleConfig{})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	err = ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, rowCount)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// Every rule type validates on_empty, threshold_metric and the filter before its
// own parameters, so a bad common field is refused whatever the rule type is.
func TestValidateConfigValidatesCommonFields(t *testing.T) {
	t.Parallel()

	target := validationTarget(storepb.Engine_MYSQL)
	validRowCount := func() *storepb.QualityRuleConfig {
		return validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1))})
	}

	config := validRowCount()
	config.OnEmpty = storepb.QualityEmptyBehavior(9)
	require.Error(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config))

	config = validRowCount()
	config.ThresholdMetric = storepb.QualityThresholdMetric(99)
	require.Error(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config))

	badFilter := &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
		Column: "missing",
		Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
		Values: []string{"1"},
	}}}
	config = validRowCount()
	config.Filter = badFilter
	require.Error(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config))

	badValueFilter := &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
		Column: "id",
		Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
		Values: []string{"not-a-number"},
	}}}
	config = validRowCount()
	config.Filter = badValueFilter
	require.Error(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config))

	clauses := make([]*storepb.QualityFilterClause, 0, MaxFilterClauses+1)
	for range MaxFilterClauses + 1 {
		clauses = append(clauses, &storepb.QualityFilterClause{
			Column: "id",
			Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
			Values: []string{"1"},
		})
	}
	config = validRowCount()
	config.Filter = &storepb.QualityFilter{Clauses: clauses}
	require.Error(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config))

	// A legal filter on the same config passes: the column exists and the value
	// converts to the column's numeric type.
	config = validRowCount()
	config.Filter = &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
		Column: "id",
		Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GTE,
		Values: []string{"42"},
	}}}
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config))

	// Without a dialect there is no way to render a filter, and the rule cannot be
	// compiled for that engine anyway.
	unknownEngine := validationTarget(storepb.Engine_ENGINE_UNSPECIFIED)
	config = validRowCount()
	config.Filter = &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
		Column: "id",
		Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ,
		Values: []string{"1"},
	}}}
	require.Error(t, ValidateConfig(unknownEngine, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, config))
}

func TestValidateRowCount(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "min only", config: validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1))})},
		{name: "min max", config: validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1)), MaxRows: validationPtr(int64(10))})},
		{name: "exact", config: validationRowCountConfig(&storepb.RowCountConfig{ExactRows: validationPtr(int64(5))})},
		{name: "warn inside fail", config: validationRowCountConfig(&storepb.RowCountConfig{
			MinRows:     validationPtr(int64(1)),
			MaxRows:     validationPtr(int64(10)),
			WarnMinRows: validationPtr(int64(2)),
			WarnMaxRows: validationPtr(int64(9)),
		})},
		{name: "no bound", config: validationRowCountConfig(&storepb.RowCountConfig{}), wantErr: true},
		{name: "warn bound only", config: validationRowCountConfig(&storepb.RowCountConfig{WarnMinRows: validationPtr(int64(1))}), wantErr: true},
		{name: "exact with min", config: validationRowCountConfig(&storepb.RowCountConfig{ExactRows: validationPtr(int64(5)), MinRows: validationPtr(int64(1))}), wantErr: true},
		{name: "min above max", config: validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(10)), MaxRows: validationPtr(int64(1))}), wantErr: true},
		{name: "negative exact", config: validationRowCountConfig(&storepb.RowCountConfig{ExactRows: validationPtr(int64(-1))}), wantErr: true},
		{name: "negative min", config: validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(-1))}), wantErr: true},
		{name: "warn min below min", config: validationRowCountConfig(&storepb.RowCountConfig{
			MinRows:     validationPtr(int64(5)),
			WarnMinRows: validationPtr(int64(4)),
		}), wantErr: true},
		{name: "warn max above max", config: validationRowCountConfig(&storepb.RowCountConfig{
			MaxRows:     validationPtr(int64(5)),
			WarnMaxRows: validationPtr(int64(6)),
		}), wantErr: true},
		{name: "warn min above warn max", config: validationRowCountConfig(&storepb.RowCountConfig{
			WarnMinRows: validationPtr(int64(9)),
			WarnMaxRows: validationPtr(int64(3)),
		}), wantErr: true},
		{name: "missing config", config: validationRowCountConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, validationTarget(storepb.Engine_MYSQL), cases)
}

func TestValidateRowCountChange(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "increase only", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{MaxIncreasePct: validationPtr(10.0)})},
		{name: "decrease only", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{MaxDecreasePct: validationPtr(10.0)})},
		{name: "warn inside fail", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{
			MaxIncreasePct:  validationPtr(20.0),
			WarnIncreasePct: validationPtr(10.0),
			MaxDecreasePct:  validationPtr(20.0),
			WarnDecreasePct: validationPtr(10.0),
		})},
		{name: "warn only", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{WarnIncreasePct: validationPtr(10.0)})},
		{name: "no bound", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{}), wantErr: true},
		{name: "negative", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{MaxIncreasePct: validationPtr(-1.0)}), wantErr: true},
		{name: "above 1000", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{MaxIncreasePct: validationPtr(1001.0)}), wantErr: true},
		{name: "nan", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{MaxIncreasePct: validationPtr(math.NaN())}), wantErr: true},
		{name: "warn above fail", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{
			MaxIncreasePct:  validationPtr(10.0),
			WarnIncreasePct: validationPtr(20.0),
		}), wantErr: true},
		{name: "warn decrease above fail", config: validationRowCountChangeConfig(&storepb.RowCountChangeConfig{
			MaxDecreasePct:  validationPtr(10.0),
			WarnDecreasePct: validationPtr(20.0),
		}), wantErr: true},
		{name: "missing config", config: validationRowCountChangeConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, validationTarget(storepb.Engine_MYSQL), cases)
}

func TestValidateNullPercent(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "count fail", config: validationNullPercentConfig(&storepb.NullPercentConfig{Column: "name", MaxNullCount: validationPtr(int64(10))})},
		{name: "count warn", config: validationNullPercentConfig(&storepb.NullPercentConfig{
			Column:        "name",
			MaxNullCount:  validationPtr(int64(10)),
			WarnNullCount: validationPtr(int64(5)),
		})},
		{name: "percent fail", config: validationNullPercentConfig(&storepb.NullPercentConfig{Column: "name", MaxNullPct: validationPtr(20.0)})},
		{name: "percent warn", config: validationNullPercentConfig(&storepb.NullPercentConfig{
			Column:      "name",
			MaxNullPct:  validationPtr(20.0),
			WarnNullPct: validationPtr(10.0),
		})},
		{name: "missing column", config: validationNullPercentConfig(&storepb.NullPercentConfig{Column: "missing", MaxNullCount: validationPtr(int64(1))}), wantErr: true},
		{name: "empty column", config: validationNullPercentConfig(&storepb.NullPercentConfig{MaxNullCount: validationPtr(int64(1))}), wantErr: true},
		{name: "no threshold", config: validationNullPercentConfig(&storepb.NullPercentConfig{Column: "name"}), wantErr: true},
		{name: "both families", config: validationNullPercentConfig(&storepb.NullPercentConfig{
			Column:        "name",
			MaxNullCount:  validationPtr(int64(1)),
			MaxNullPct:    validationPtr(1.0),
			WarnNullCount: validationPtr(int64(0)),
		}), wantErr: true},
		{name: "warn count without fail", config: validationNullPercentConfig(&storepb.NullPercentConfig{
			Column:        "name",
			WarnNullCount: validationPtr(int64(1)),
		}), wantErr: true},
		{name: "negative count", config: validationNullPercentConfig(&storepb.NullPercentConfig{Column: "name", MaxNullCount: validationPtr(int64(-1))}), wantErr: true},
		{name: "count warn above fail", config: validationNullPercentConfig(&storepb.NullPercentConfig{
			Column:        "name",
			MaxNullCount:  validationPtr(int64(1)),
			WarnNullCount: validationPtr(int64(2)),
		}), wantErr: true},
		{name: "percent above 100", config: validationNullPercentConfig(&storepb.NullPercentConfig{Column: "name", MaxNullPct: validationPtr(101.0)}), wantErr: true},
		{name: "negative percent", config: validationNullPercentConfig(&storepb.NullPercentConfig{Column: "name", MaxNullPct: validationPtr(-1.0)}), wantErr: true},
		{name: "percent warn above fail", config: validationNullPercentConfig(&storepb.NullPercentConfig{
			Column:      "name",
			MaxNullPct:  validationPtr(10.0),
			WarnNullPct: validationPtr(20.0),
		}), wantErr: true},
		{name: "missing config", config: validationNullPercentConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, validationTarget(storepb.Engine_MYSQL), cases)
}

func TestValidateUnique(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "default threshold", config: validationUniqueConfig(&storepb.UniqueConfig{Column: "id"})},
		{name: "explicit thresholds", config: validationUniqueConfig(&storepb.UniqueConfig{
			Column:        "id",
			FailThreshold: validationPtr(2.0),
			WarnThreshold: validationPtr(1.0),
		})},
		{name: "count nulls", config: validationUniqueConfig(&storepb.UniqueConfig{Column: "id", CountNulls: true})},
		{name: "percent metric", config: func() *storepb.QualityRuleConfig {
			config := validationUniqueConfig(&storepb.UniqueConfig{Column: "id", FailThreshold: validationPtr(50.0)})
			config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
			return config
		}()},
		{name: "missing column", config: validationUniqueConfig(&storepb.UniqueConfig{Column: "missing"}), wantErr: true},
		{name: "negative fail", config: validationUniqueConfig(&storepb.UniqueConfig{Column: "id", FailThreshold: validationPtr(-1.0)}), wantErr: true},
		{name: "warn above fail", config: validationUniqueConfig(&storepb.UniqueConfig{
			Column:        "id",
			FailThreshold: validationPtr(1.0),
			WarnThreshold: validationPtr(2.0),
		}), wantErr: true},
		{name: "percent above 100", config: func() *storepb.QualityRuleConfig {
			config := validationUniqueConfig(&storepb.UniqueConfig{Column: "id", FailThreshold: validationPtr(101.0)})
			config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
			return config
		}(), wantErr: true},
		{name: "percent metric", config: func() *storepb.QualityRuleConfig {
			config := validationUniqueConfig(&storepb.UniqueConfig{Column: "id", FailThreshold: validationPtr(50.0)})
			config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
			return config
		}()},
		{name: "missing config", config: validationUniqueConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, validationTarget(storepb.Engine_MYSQL), cases)
}

func TestValidateUniqueCombination(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "two columns", config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{Columns: []string{"id", "name"}})},
		{name: "all columns", config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{AllColumns: true})},
		{name: "all columns with count nulls", config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{AllColumns: true, CountNulls: true})},
		{name: "no columns", config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{}), wantErr: true},
		{name: "all columns with list", config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{
			AllColumns: true,
			Columns:    []string{"id"},
		}), wantErr: true},
		{name: "too many columns", config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{
			Columns: []string{"id", "name", "amount", "created_at", "updated_at", "active", "a", "b", "c", "d", "e"},
		}), wantErr: true},
		{name: "duplicate column", config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{Columns: []string{"id", "id"}}), wantErr: true},
		{name: "missing column", config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{Columns: []string{"id", "missing"}}), wantErr: true},
		{name: "warn above fail", config: validationUniqueCombinationConfig(&storepb.UniqueCombinationConfig{
			Columns:       []string{"id"},
			FailThreshold: validationPtr(1.0),
			WarnThreshold: validationPtr(2.0),
		}), wantErr: true},
		{name: "missing config", config: validationUniqueCombinationConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION, validationTarget(storepb.Engine_MYSQL), cases)
}

func TestValidateAcceptedValues(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "numeric values", config: validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{Column: "id", Values: []string{"1", "2"}})},
		{name: "allow null", config: validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{Column: "name", Values: []string{"a"}, AllowNull: true})},
		{name: "no values", config: validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{Column: "id"}), wantErr: true},
		{name: "too many values", config: validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{
			Column: "id",
			Values: func() []string {
				values := make([]string, 0, compileMaxAcceptedValues+1)
				for i := range compileMaxAcceptedValues + 1 {
					values = append(values, strconv.Itoa(i))
				}
				return values
			}(),
		}), wantErr: true},
		{name: "unconvertible value", config: validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{Column: "id", Values: []string{"1", "abc"}}), wantErr: true},
		{name: "missing column", config: validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{Column: "missing", Values: []string{"1"}}), wantErr: true},
		{name: "warn above fail", config: validationAcceptedValuesConfig(&storepb.AcceptedValuesConfig{
			Column:        "id",
			Values:        []string{"1"},
			FailThreshold: validationPtr(1.0),
			WarnThreshold: validationPtr(2.0),
		}), wantErr: true},
		{name: "missing config", config: validationAcceptedValuesConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES, validationTarget(storepb.Engine_MYSQL), cases)
}

func TestValidateRange(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "numeric bounds", config: validationRangeConfig(&storepb.RangeConfig{Column: "amount", Min: validationPtr("1.5"), Max: validationPtr("9.5")})},
		{name: "min only", config: validationRangeConfig(&storepb.RangeConfig{Column: "amount", Min: validationPtr("0")})},
		{name: "temporal bounds", config: validationRangeConfig(&storepb.RangeConfig{
			Column: "created_at",
			Min:    validationPtr("2026-01-01"),
			Max:    validationPtr("2026-12-31"),
		})},
		{name: "no bounds", config: validationRangeConfig(&storepb.RangeConfig{Column: "amount"}), wantErr: true},
		{name: "string column", config: validationRangeConfig(&storepb.RangeConfig{Column: "name", Min: validationPtr("a")}), wantErr: true},
		{name: "missing column", config: validationRangeConfig(&storepb.RangeConfig{Column: "missing", Min: validationPtr("1")}), wantErr: true},
		{name: "numeric bound not a number", config: validationRangeConfig(&storepb.RangeConfig{Column: "amount", Min: validationPtr("abc")}), wantErr: true},
		{name: "empty bound", config: validationRangeConfig(&storepb.RangeConfig{Column: "amount", Min: validationPtr("  ")}), wantErr: true},
		{name: "min above max", config: validationRangeConfig(&storepb.RangeConfig{Column: "amount", Min: validationPtr("10"), Max: validationPtr("1")}), wantErr: true},
		{name: "missing config", config: validationRangeConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE, validationTarget(storepb.Engine_MYSQL), cases)
}

func TestValidateRegex(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "valid pattern", config: validationRegexConfig(&storepb.RegexConfig{Column: "name", Pattern: "^[a-z]+$"})},
		{name: "no column", config: validationRegexConfig(&storepb.RegexConfig{Pattern: "^a$"}), wantErr: true},
		{name: "numeric column", config: validationRegexConfig(&storepb.RegexConfig{Column: "id", Pattern: "^a$"}), wantErr: true},
		{name: "empty pattern", config: validationRegexConfig(&storepb.RegexConfig{Column: "name"}), wantErr: true},
		{name: "blank pattern", config: validationRegexConfig(&storepb.RegexConfig{Column: "name", Pattern: "   "}), wantErr: true},
		{name: "invalid pattern", config: validationRegexConfig(&storepb.RegexConfig{Column: "name", Pattern: "(["}), wantErr: true},
		{name: "missing config", config: validationRegexConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX, validationTarget(storepb.Engine_POSTGRES), cases)
}

func TestValidateStringLength(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "both bounds", config: validationStringLengthConfig(&storepb.StringLengthConfig{Column: "name", MinLen: validationPtr(int64(1)), MaxLen: validationPtr(int64(10))})},
		{name: "min only", config: validationStringLengthConfig(&storepb.StringLengthConfig{Column: "name", MinLen: validationPtr(int64(0))})},
		{name: "max only", config: validationStringLengthConfig(&storepb.StringLengthConfig{Column: "name", MaxLen: validationPtr(int64(10))})},
		{name: "no bounds", config: validationStringLengthConfig(&storepb.StringLengthConfig{Column: "name"}), wantErr: true},
		{name: "negative min", config: validationStringLengthConfig(&storepb.StringLengthConfig{Column: "name", MinLen: validationPtr(int64(-1))}), wantErr: true},
		{name: "negative max", config: validationStringLengthConfig(&storepb.StringLengthConfig{Column: "name", MaxLen: validationPtr(int64(-1))}), wantErr: true},
		{name: "min above max", config: validationStringLengthConfig(&storepb.StringLengthConfig{Column: "name", MinLen: validationPtr(int64(10)), MaxLen: validationPtr(int64(1))}), wantErr: true},
		{name: "numeric column", config: validationStringLengthConfig(&storepb.StringLengthConfig{Column: "id", MinLen: validationPtr(int64(1))}), wantErr: true},
		{name: "missing config", config: validationStringLengthConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH, validationTarget(storepb.Engine_MYSQL), cases)
}

func TestValidateReferentialIntegrity(t *testing.T) {
	t.Parallel()

	valid := func(parentTable string) *storepb.QualityRuleConfig {
		return validationReferentialIntegrityConfig(&storepb.ReferentialIntegrityConfig{
			ChildColumn:  "id",
			ParentTable:  parentTable,
			ParentColumn: "id",
		})
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, validationTarget(storepb.Engine_MYSQL), []validationCase{
		{name: "mysql parent table", config: valid("users")},
		{name: "mysql allow null", config: validationReferentialIntegrityConfig(&storepb.ReferentialIntegrityConfig{
			ChildColumn:  "id",
			ParentTable:  "users",
			ParentColumn: "id",
			AllowNull:    true,
		})},
		{name: "mysql database qualified parent", config: valid("app.users")},
		{name: "mysql other database parent", config: valid("other.users"), wantErr: true},
		{name: "mysql three segments", config: valid("a.b.c"), wantErr: true},
		{name: "mysql empty segment", config: valid("app."), wantErr: true},
		{name: "missing child column", config: validationReferentialIntegrityConfig(&storepb.ReferentialIntegrityConfig{
			ChildColumn:  "missing",
			ParentTable:  "users",
			ParentColumn: "id",
		}), wantErr: true},
		{name: "empty parent column", config: validationReferentialIntegrityConfig(&storepb.ReferentialIntegrityConfig{
			ChildColumn: "id",
			ParentTable: "users",
		}), wantErr: true},
		{name: "warn above fail", config: validationReferentialIntegrityConfig(&storepb.ReferentialIntegrityConfig{
			ChildColumn:   "id",
			ParentTable:   "users",
			ParentColumn:  "id",
			FailThreshold: validationPtr(1.0),
			WarnThreshold: validationPtr(2.0),
		}), wantErr: true},
		{name: "missing config", config: validationReferentialIntegrityConfig(nil), wantErr: true},
	})

	// PostgreSQL requires the schema-qualified form, and the schema must be the
	// rule's own: reaching another schema is a different check the engine cannot
	// express as a same-container join here.
	postgres := validationTarget(storepb.Engine_POSTGRES)
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY, postgres, []validationCase{
		{name: "postgres schema qualified", config: valid("public.users")},
		{name: "postgres bare table", config: valid("users")},
		{name: "postgres other schema", config: valid("other.users"), wantErr: true},
		{name: "postgres three segments", config: valid("public.app.users"), wantErr: true},
	})
}

func TestValidateCrossColumn(t *testing.T) {
	t.Parallel()

	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN, validationTarget(storepb.Engine_MYSQL), []validationCase{
		{name: "numeric pair", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "id",
			RightColumn: "amount",
			Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LTE,
		})},
		{name: "temporal pair", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "created_at",
			RightColumn: "updated_at",
			Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LTE,
		})},
		{name: "numeric tolerance", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "id",
			RightColumn: "amount",
			Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
			Tolerance:   validationPtr(0.5),
		})},
		{name: "missing left", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "missing",
			RightColumn: "amount",
			Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
		}), wantErr: true},
		{name: "string column", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "name",
			RightColumn: "amount",
			Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
		}), wantErr: true},
		{name: "mixed kinds", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "amount",
			RightColumn: "created_at",
			Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
		}), wantErr: true},
		{name: "no operator", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "id",
			RightColumn: "amount",
		}), wantErr: true},
		{name: "unknown operator", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "id",
			RightColumn: "amount",
			Op:          storepb.QualityComparisonOperator(9),
		}), wantErr: true},
		{name: "negative tolerance", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "id",
			RightColumn: "amount",
			Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
			Tolerance:   validationPtr(-1.0),
		}), wantErr: true},
		{name: "nan tolerance", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "id",
			RightColumn: "amount",
			Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
			Tolerance:   validationPtr(math.NaN()),
		}), wantErr: true},
		{name: "tolerance on temporal", config: validationCrossColumnConfig(&storepb.CrossColumnConfig{
			LeftColumn:  "created_at",
			RightColumn: "updated_at",
			Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
			Tolerance:   validationPtr(1.0),
		}), wantErr: true},
		{name: "missing config", config: validationCrossColumnConfig(nil), wantErr: true},
	})
}

func TestValidateFreshness(t *testing.T) {
	t.Parallel()

	cases := []validationCase{
		{name: "fail lag", config: validationFreshnessConfig(&storepb.FreshnessConfig{TimestampColumn: "created_at", MaxLagSeconds: 3600})},
		{name: "warn lag", config: validationFreshnessConfig(&storepb.FreshnessConfig{
			TimestampColumn: "created_at",
			MaxLagSeconds:   3600,
			WarnLagSeconds:  validationPtr(int32(1800)),
		})},
		{name: "zero lag", config: validationFreshnessConfig(&storepb.FreshnessConfig{TimestampColumn: "created_at"}), wantErr: true},
		{name: "negative lag", config: validationFreshnessConfig(&storepb.FreshnessConfig{TimestampColumn: "created_at", MaxLagSeconds: -1}), wantErr: true},
		{name: "warn above fail", config: validationFreshnessConfig(&storepb.FreshnessConfig{
			TimestampColumn: "created_at",
			MaxLagSeconds:   3600,
			WarnLagSeconds:  validationPtr(int32(3600)),
		}), wantErr: true},
		{name: "negative warn", config: validationFreshnessConfig(&storepb.FreshnessConfig{
			TimestampColumn: "created_at",
			MaxLagSeconds:   3600,
			WarnLagSeconds:  validationPtr(int32(-1)),
		}), wantErr: true},
		{name: "non temporal column", config: validationFreshnessConfig(&storepb.FreshnessConfig{TimestampColumn: "name", MaxLagSeconds: 60}), wantErr: true},
		{name: "missing column", config: validationFreshnessConfig(&storepb.FreshnessConfig{TimestampColumn: "missing", MaxLagSeconds: 60}), wantErr: true},
		{name: "missing config", config: validationFreshnessConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, validationTarget(storepb.Engine_MYSQL), cases)
}

func TestValidateCustomSQL(t *testing.T) {
	t.Parallel()

	validSQL := "SELECT id FROM orders WHERE id < 0"
	cases := []validationCase{
		{name: "select", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{Sql: validSQL})},
		{name: "select with thresholds", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{
			Sql:           validSQL,
			FailThreshold: validationPtr(10.0),
			WarnThreshold: validationPtr(5.0),
		})},
		{name: "empty sql", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{}), wantErr: true},
		{name: "multi statement", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{Sql: "SELECT 1; SELECT 2"}), wantErr: true},
		{name: "ddl", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{Sql: "CREATE TABLE t (id int)"}), wantErr: true},
		{name: "dml", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{Sql: "DELETE FROM orders"}), wantErr: true},
		{name: "explain", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{Sql: "EXPLAIN SELECT 1"}), wantErr: true},
		{name: "select into", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{Sql: "SELECT id INTO @x FROM orders"}), wantErr: true},
		{name: "too long", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{
			Sql: "SELECT '" + strings.Repeat("x", maxCustomSQLBytes) + "'",
		}), wantErr: true},
		{name: "warn above fail", config: validationCustomSQLConfig(&storepb.CustomSQLConfig{
			Sql:           validSQL,
			FailThreshold: validationPtr(1.0),
			WarnThreshold: validationPtr(2.0),
		}), wantErr: true},
		{name: "percent metric", config: func() *storepb.QualityRuleConfig {
			config := validationCustomSQLConfig(&storepb.CustomSQLConfig{Sql: validSQL})
			config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
			return config
		}(), wantErr: true},
		{name: "missing config", config: validationCustomSQLConfig(nil), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL, validationTarget(storepb.Engine_MYSQL), cases)
}

// A CEL rule that compiles to a bool over existing columns is accepted; the
// per-parameter and per-expression refusals are covered in cel_test.go.
func TestValidateCELExpressionIsSupported(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CelExpression{
		CelExpression: &storepb.CelExpressionConfig{Expression: "amount > 0.0", Columns: []string{"amount"}, SampleRows: 100},
	}}
	err := ValidateConfig(validationTarget(storepb.Engine_MYSQL), storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, config)
	require.NoError(t, err)
}

// ValidateRule resolves the target first, so a missing store or config is refused
// before any rule work happens; the parts that need a registry are covered by the
// integration tests of the API layer.
func TestValidateRuleRejectsMissingInputs(t *testing.T) {
	t.Parallel()

	_, err := ValidateRule(t.Context(), nil, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "inst1;app;orders", storepb.MetaType_TABLE, nil, nil)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	config := validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1))})
	_, err = ValidateRule(t.Context(), nil, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "inst1;app;orders", storepb.MetaType_TABLE, config, nil)
	require.Error(t, err)
	// Resolving a target without a store is a server-side fault, not a bad rule.
	require.Equal(t, common.Internal, common.ErrorCode(err))
}

// PERCENT thresholds are only meaningful where the compiled statement also returns
// total_rows. row_count, row_count_change, null_percent (which names its own
// percentage form), freshness and custom SQL have no denominator: custom SQL's
// statement returns failed rows only, so a percentage threshold there would be
// silently meaningless.
func TestValidateConfigRejectsPercentWithoutDenominator(t *testing.T) {
	t.Parallel()

	percent := storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	target := validationTarget(storepb.Engine_MYSQL)

	noDenominator := []struct {
		name     string
		ruleType storepb.QualityRuleType
		config   *storepb.QualityRuleConfig
	}{
		{
			name:     "row count",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config:   validationRowCountConfig(&storepb.RowCountConfig{MinRows: validationPtr(int64(1))}),
		},
		{
			name:     "row count change",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
			config:   validationRowCountChangeConfig(&storepb.RowCountChangeConfig{MaxIncreasePct: validationPtr(10.0)}),
		},
		{
			name:     "null percent",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config:   validationNullPercentConfig(&storepb.NullPercentConfig{Column: "name", MaxNullPct: validationPtr(10.0)}),
		},
		{
			name:     "freshness",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			config:   validationFreshnessConfig(&storepb.FreshnessConfig{TimestampColumn: "created_at", MaxLagSeconds: 60}),
		},
		{
			name:     "custom sql",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			config:   validationCustomSQLConfig(&storepb.CustomSQLConfig{Sql: "SELECT 1"}),
		},
	}
	for _, testCase := range noDenominator {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			testCase.config.ThresholdMetric = percent
			err := ValidateConfig(target, testCase.ruleType, testCase.config)
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
		})
	}

	// A count-type rule does have a denominator, so PERCENT stays legal.
	unique := validationUniqueConfig(&storepb.UniqueConfig{Column: "id", FailThreshold: validationPtr(50.0)})
	unique.ThresholdMetric = percent
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, unique))
}

// Each PERCENT refusal explains itself through the one helper the save-time gate
// and the executor's backstop share: a count rule has no denominator, null_percent
// names its own unit, and the measurement rules bound absolute values.
func TestPercentRefusalMessages(t *testing.T) {
	t.Parallel()

	percent := storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	refused := []struct {
		ruleType storepb.QualityRuleType
		want     string
	}{
		{ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, want: "chooses count or percentage in its own config"},
		{ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, want: "drift items"},
		{ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_AGGREGATE, want: "absolute values"},
		{ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_TABLE, want: "absolute values"},
		{ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, want: "no total-rows denominator"},
		{ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE, want: "no total-rows denominator"},
		{ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS, want: "no total-rows denominator"},
		{ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL, want: "no total-rows denominator"},
		{ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED, want: "no total-rows denominator"},
	}
	for _, test := range refused {
		err := validatePercentSupport(test.ruleType, percent)
		require.Equal(t, common.Invalid, common.ErrorCode(err), test.ruleType.String())
		require.Contains(t, err.Error(), test.want, test.ruleType.String())
	}

	allowed := []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
	}
	for _, ruleType := range allowed {
		require.NoError(t, validatePercentSupport(ruleType, percent), ruleType.String())
	}

	// The same refusal reaches the save path with the accurate null_percent text.
	config := validationNullPercentConfig(&storepb.NullPercentConfig{Column: "name", MaxNullPct: validationPtr(10.0)})
	config.ThresholdMetric = percent
	err := ValidateConfig(validationTarget(storepb.Engine_MYSQL), storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, config)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	require.Contains(t, err.Error(), "chooses count or percentage in its own config")
}

// parentTableName is the pure half of the parent lookup: it decides which registry
// object the config names. The GUID it produces has to be the same path the schema
// syncer registers, or the parent lookup would always miss.
func TestParentTableName(t *testing.T) {
	t.Parallel()

	mysql := validationTarget(storepb.Engine_MYSQL) // Database "app", no schema segment.
	schema, table, err := parentTableName(mysql, "users")
	require.NoError(t, err)
	require.Equal(t, "", schema)
	require.Equal(t, "users", table)
	require.Equal(t, "inst1;app;;users", relatedTargetGUID(mysql, schema, table))

	schema, table, err = parentTableName(mysql, "  app.users  ")
	require.NoError(t, err)
	require.Equal(t, "", schema)
	require.Equal(t, "users", table)

	_, _, err = parentTableName(mysql, "other.users")
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	// Two segments are database.table; a "table.column" spelling is a different
	// database and is refused rather than silently reinterpreted.
	_, _, err = parentTableName(mysql, "users.id")
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, _, err = parentTableName(mysql, "app.")
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	_, _, err = parentTableName(mysql, "app.a.b")
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	_, _, err = parentTableName(mysql, "   ")
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	postgres := validationTarget(storepb.Engine_POSTGRES) // Schema "public".
	schema, table, err = parentTableName(postgres, "users")
	require.NoError(t, err)
	require.Equal(t, "public", schema)
	require.Equal(t, "users", table)
	require.Equal(t, "inst1;app;public;users", relatedTargetGUID(postgres, schema, table))

	schema, table, err = parentTableName(postgres, "public.users")
	require.NoError(t, err)
	require.Equal(t, "public", schema)
	require.Equal(t, "users", table)

	_, _, err = parentTableName(postgres, "other.users")
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	// The qualifier names a container, not a relation: engines treat unquoted
	// identifiers case-insensitively, so casing must not decide whether it matches.
	schema, table, err = parentTableName(mysql, "APP.users")
	require.NoError(t, err)
	require.Equal(t, "", schema)
	require.Equal(t, "users", table)
	schema, _, err = parentTableName(postgres, "PUBLIC.users")
	require.NoError(t, err)
	require.Equal(t, "public", schema, "the canonical schema is returned, not the author's casing")

	// An explicitly empty schema qualifier is still malformed.
	_, _, err = parentTableName(postgres, "users.id")
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}
