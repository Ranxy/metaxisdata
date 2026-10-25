package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// nullPercentConfig builds a null-percent rule config with custom missing values.
func nullPercentConfig(column string, nullValues []string) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{
			Column:     column,
			MaxNullPct: proto.Float64(0),
			NullValues: nullValues,
		},
	}}
}

// TestNullPercentCustomValuesSQL pins the custom missing-value predicate in the
// aggregate and the sample, including how the values are bound around the filter.
func TestNullPercentCustomValuesSQL(t *testing.T) {
	t.Parallel()

	config := nullPercentConfig("status", []string{"", "N/A"})
	filter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
	}}
	tests := []struct {
		name         string
		engine       storepb.Engine
		filter       Filter
		wantSQL      string
		wantArgs     []any
		wantSample   string
		wantSampleAg []any
	}{
		{
			name:   "mysql no filter",
			engine: storepb.Engine_MYSQL,
			wantSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `status` IS NULL OR `status` IN (?, ?) " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
			wantArgs: []any{"", "N/A"},
			wantSample: "SELECT `id`, `status` FROM `shop`.`orders` " +
				"WHERE (`status` IS NULL OR `status` IN (?, ?)) LIMIT 20",
			wantSampleAg: []any{"", "N/A"},
		},
		{
			name:   "starrocks shares the mysql rendering",
			engine: storepb.Engine_STARROCKS,
			wantSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `status` IS NULL OR `status` IN (?, ?) " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
			wantArgs: []any{"", "N/A"},
			wantSample: "SELECT `id`, `status` FROM `shop`.`orders` " +
				"WHERE (`status` IS NULL OR `status` IN (?, ?)) LIMIT 20",
			wantSampleAg: []any{"", "N/A"},
		},
		{
			name:   "postgres no filter",
			engine: storepb.Engine_POSTGRES,
			wantSQL: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN "status" IS NULL OR "status" IN ($1, $2) ` +
				`THEN 1 ELSE 0 END) AS failed_rows FROM "public"."orders"`,
			wantArgs: []any{"", "N/A"},
			wantSample: `SELECT "id", "status" FROM "public"."orders" ` +
				`WHERE ("status" IS NULL OR "status" IN ($1, $2)) LIMIT 20`,
			wantSampleAg: []any{"", "N/A"},
		},
		{
			name:   "mssql no filter",
			engine: storepb.Engine_MSSQL,
			wantSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN [status] IS NULL OR [status] IN (@p1, @p2) " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM [public].[orders]",
			wantArgs: []any{"", "N/A"},
			wantSample: "SELECT [id], [status] FROM [public].[orders] " +
				"WHERE ([status] IS NULL OR [status] IN (@p1, @p2)) " +
				"ORDER BY (SELECT NULL) OFFSET 0 ROWS FETCH NEXT 20 ROWS ONLY",
			wantSampleAg: []any{"", "N/A"},
		},
		{
			name:   "mysql with filter",
			engine: storepb.Engine_MYSQL,
			filter: filter,
			// A positional dialect binds in text order: the IN list precedes the
			// WHERE clause.
			wantSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `status` IS NULL OR `status` IN (?, ?) " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders` WHERE `status` = ?",
			wantArgs: []any{"", "N/A", "paid"},
			wantSample: "SELECT `id`, `status` FROM `shop`.`orders` " +
				"WHERE `status` = ? AND (`status` IS NULL OR `status` IN (?, ?)) LIMIT 20",
			wantSampleAg: []any{"paid", "", "N/A"},
		},
		{
			name:   "postgres with filter",
			engine: storepb.Engine_POSTGRES,
			filter: filter,
			// A numbered dialect keeps the filter's numbering and numbers the custom
			// values after it.
			wantSQL: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN "status" IS NULL OR "status" IN ($2, $3) ` +
				`THEN 1 ELSE 0 END) AS failed_rows FROM "public"."orders" WHERE "status" = $1`,
			wantArgs: []any{"paid", "", "N/A"},
			wantSample: `SELECT "id", "status" FROM "public"."orders" ` +
				`WHERE "status" = $1 AND ("status" IS NULL OR "status" IN ($2, $3)) LIMIT 20`,
			wantSampleAg: []any{"paid", "", "N/A"},
		},
		{
			name:   "mssql with filter",
			engine: storepb.Engine_MSSQL,
			filter: filter,
			wantSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN [status] IS NULL OR [status] IN (@p2, @p3) " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM [public].[orders] WHERE [status] = @p1",
			wantArgs: []any{"paid", "", "N/A"},
			wantSample: "SELECT [id], [status] FROM [public].[orders] " +
				"WHERE [status] = @p1 AND ([status] IS NULL OR [status] IN (@p2, @p3)) " +
				"ORDER BY (SELECT NULL) OFFSET 0 ROWS FETCH NEXT 20 ROWS ONLY",
			wantSampleAg: []any{"paid", "", "N/A"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			query, err := Compile(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(test.engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
				Config:  config,
				Filter:  test.filter,
			})
			require.NoError(t, err)
			require.Equal(t, test.wantSQL, query.SQL)
			require.Equal(t, test.wantArgs, query.Args)
			require.Equal(t, []string{"total_rows", "failed_rows"}, query.Columns)

			sample, err := CompileFailureRows(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(test.engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
				Config:  config,
				Filter:  test.filter,
			}, 20)
			require.NoError(t, err)
			require.Equal(t, test.wantSample, sample.SQL)
			require.Equal(t, test.wantSampleAg, sample.Args)
			require.Equal(t, []string{"id", "status"}, sample.Columns)
		})
	}
}

// TestNullPercentCustomValuesKeepEmptyShape pins the backwards-compatible shape:
// an empty null_values list compiles exactly as before (no IN list).
func TestNullPercentCustomValuesKeepEmptyShape(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:  nullPercentConfig("status", nil),
	})
	require.NoError(t, err)
	require.Equal(t, "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `status` IS NULL THEN 1 ELSE 0 END) "+
		"AS failed_rows FROM `shop`.`orders`", query.SQL)
	require.Empty(t, query.Args)
}

// TestNullPercentCustomValuesConversion pins the typed binding: values are
// converted with the column's type, so a numeric column's missing value is a
// number and a boolean column's is a bool.
func TestNullPercentCustomValuesConversion(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	numericTarget := compileTestTarget(storepb.Engine_MYSQL)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  numericTarget,
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:  nullPercentConfig("qty", []string{"0", "-1"}),
	})
	require.NoError(t, err)
	require.Equal(t, []any{int64(0), int64(-1)}, query.Args)

	booleanTarget := compileTestTarget(storepb.Engine_MYSQL)
	booleanTarget.Columns = append(booleanTarget.Columns, ColumnInfo{Name: "enabled", Type: "boolean", Kind: ColumnKindBoolean})
	query, err = Compile(CompileInput{
		Dialect: dialect,
		Target:  booleanTarget,
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:  nullPercentConfig("enabled", []string{"true", "false"}),
	})
	require.NoError(t, err)
	require.Equal(t, []any{true, false}, query.Args)

	temporalTarget := compileTestTarget(storepb.Engine_MYSQL)
	query, err = Compile(CompileInput{
		Dialect: dialect,
		Target:  temporalTarget,
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:  nullPercentConfig("created_at", []string{"1970-01-01 00:00:00"}),
	})
	require.NoError(t, err)
	require.Equal(t, []any{"1970-01-01 00:00:00"}, query.Args)
}

// TestValidateNullPercentCustomValues pins the custom-value validation: at most 20
// values and each one convertible to the column's type.
func TestValidateNullPercentCustomValues(t *testing.T) {
	t.Parallel()

	target := compileTestTarget(storepb.Engine_POSTGRES)
	require.NoError(t, ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		nullPercentConfig("status", []string{"", "N/A", "unknown"})))

	tooMany := make([]string, 0, maxNullValues+1)
	for index := 0; index <= maxNullValues; index++ {
		tooMany = append(tooMany, "v"+string(rune('a'+index)))
	}
	tests := []struct {
		name        string
		config      *storepb.QualityRuleConfig
		wantMessage string
	}{
		{
			name:        "too many values",
			config:      nullPercentConfig("status", tooMany),
			wantMessage: "at most 20 values",
		},
		{
			name:        "non-numeric value for a numeric column",
			config:      nullPercentConfig("qty", []string{"abc"}),
			wantMessage: "not a number",
		},
		{
			name:        "non-boolean value for a boolean column",
			config:      nullPercentConfig("enabled", []string{"maybe"}),
			wantMessage: "not a boolean",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			validationTarget := target
			if test.name == "non-boolean value for a boolean column" {
				validationTarget.Columns = append(append([]ColumnInfo{}, target.Columns...),
					ColumnInfo{Name: "enabled", Type: "boolean", Kind: ColumnKindBoolean})
			}
			err := ValidateConfig(validationTarget, storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, test.config)
			require.Equal(t, common.Invalid, common.ErrorCode(err))
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}

	// The compiler re-checks the same contract when it is called directly.
	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	_, err = Compile(CompileInput{
		Dialect: dialect,
		Target:  target,
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:  nullPercentConfig("status", tooMany),
	})
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}
