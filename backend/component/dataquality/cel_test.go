package dataquality

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
)

// celTestColumns is one column of every kind a CEL expression may read.
func celTestColumns() []ColumnInfo {
	return []ColumnInfo{
		{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
		{Name: "amount", Type: "decimal(10,2)", Kind: ColumnKindNumeric},
		{Name: "email", Type: "varchar(255)", Kind: ColumnKindString, Nullable: true},
		{Name: "status", Type: "varchar(32)", Kind: ColumnKindString, Nullable: true},
		{Name: "active", Type: "boolean", Kind: ColumnKindBoolean, Nullable: true},
		{Name: "created_at", Type: "timestamp", Kind: ColumnKindTemporal, Nullable: true},
	}
}

// celRuleConfig builds the config one CEL rule is saved with.
func celRuleConfig(expression string, columns []string, sampleRows int32) *storepb.QualityRuleConfig {
	return &storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_CelExpression{
			CelExpression: &storepb.CelExpressionConfig{
				Expression: expression,
				Columns:    columns,
				SampleRows: sampleRows,
			},
		},
	}
}

// celTestTarget extends the compiler's golden target with a boolean column, which
// a CEL expression needs so the boolean kind has a case.
func celTestTarget(engine storepb.Engine) ResolvedTarget {
	target := compileTestTarget(engine)
	target.Columns = append(target.Columns, ColumnInfo{Name: "active", Type: "boolean", Kind: ColumnKindBoolean, Nullable: true})
	return target
}

func TestCompileCelExpressionSampleSQL(t *testing.T) {
	t.Parallel()

	config := celRuleConfig(`amount > 100.0.0 && status == "paid"`, []string{"amount", "status"}, 500)
	tests := []struct {
		engine  storepb.Engine
		wantSQL string
	}{
		{
			engine:  storepb.Engine_MYSQL,
			wantSQL: "SELECT `amount`, `status` FROM `shop`.`orders` LIMIT 500",
		},
		{
			engine:  storepb.Engine_STARROCKS,
			wantSQL: "SELECT `amount`, `status` FROM `shop`.`orders` LIMIT 500",
		},
		{
			engine:  storepb.Engine_POSTGRES,
			wantSQL: `SELECT "amount", "status" FROM "public"."orders" LIMIT 500`,
		},
		{
			engine: storepb.Engine_MSSQL,
			wantSQL: "SELECT [amount], [status] FROM [public].[orders] " +
				"ORDER BY (SELECT NULL) OFFSET 0 ROWS FETCH NEXT 500 ROWS ONLY",
		},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			query, err := Compile(CompileInput{
				Dialect: mustDialect(t, test.engine),
				Target:  celTestTarget(test.engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
				Config:  config,
			})
			require.NoError(t, err)
			require.Equal(t, test.wantSQL, query.SQL)
			require.Nil(t, query.Args)
			require.Equal(t, []string{"amount", "status"}, query.Columns)
			require.Equal(t, QuerySample, query.Kind)
		})
	}
}

// The sample query reuses the filter, so the rule reads the same rows the filter
// describes rather than the whole table.
func TestCompileCelExpressionCarriesTheFilter(t *testing.T) {
	t.Parallel()

	query, err := Compile(CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  celTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
		Config:  celRuleConfig("amount > 100.0", []string{"amount"}, 100),
		Filter:  Filter{Clauses: []FilterClause{{Column: "status", Op: FilterEQ, Values: []string{"paid"}}}},
	})
	require.NoError(t, err)
	require.Equal(t, "SELECT `amount` FROM `shop`.`orders` WHERE `status` = ? LIMIT 100", query.SQL)
	require.Equal(t, []any{"paid"}, query.Args)
}

func TestCompileCelExpressionSampleRows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sampleRows int32
		wantLimit  string
		wantErr    bool
	}{
		{name: "unset reads as the default", sampleRows: 0, wantLimit: "LIMIT 10000"},
		{name: "explicit", sampleRows: 42, wantLimit: "LIMIT 42"},
		{name: "at the maximum", sampleRows: maxCELSampleRows, wantLimit: "LIMIT 100000"},
		{name: "above the maximum", sampleRows: maxCELSampleRows + 1, wantErr: true},
		{name: "negative", sampleRows: -1, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			query, err := Compile(CompileInput{
				Dialect: mustDialect(t, storepb.Engine_MYSQL),
				Target:  celTestTarget(storepb.Engine_MYSQL),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
				Config:  celRuleConfig("amount > 0.0", []string{"amount"}, test.sampleRows),
			})
			if test.wantErr {
				require.Error(t, err)
				require.Equal(t, common.Invalid, common.ErrorCode(err))
				return
			}
			require.NoError(t, err)
			require.Contains(t, query.SQL, test.wantLimit)
		})
	}
}

func TestCompileCelExpressionColumns(t *testing.T) {
	t.Parallel()

	base := func(columns []string) *storepb.QualityRuleConfig {
		return celRuleConfig("amount > 0.0", columns, 10)
	}
	tests := []struct {
		name    string
		columns []string
		wantErr bool
	}{
		{name: "one column", columns: []string{"amount"}},
		{name: "duplicates collapse", columns: []string{"amount", "amount"}},
		{name: "no column", columns: nil, wantErr: true},
		{name: "unknown column", columns: []string{"missing"}, wantErr: true},
		{name: "blank column", columns: []string{"  "}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := base(test.columns)
			query, err := Compile(CompileInput{
				Dialect: mustDialect(t, storepb.Engine_MYSQL),
				Target:  celTestTarget(storepb.Engine_MYSQL),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
				Config:  config,
			})
			if test.wantErr {
				require.Error(t, err)
				require.Equal(t, common.Invalid, common.ErrorCode(err))
				return
			}
			require.NoError(t, err)
			for _, column := range test.columns {
				require.Contains(t, query.SQL, strings.TrimSpace(column))
			}
		})
	}
}

func TestCelColumnsRejectsTooManyColumns(t *testing.T) {
	t.Parallel()

	target := ResolvedTarget{
		Target:   Target{GUID: "inst;app;orders", MetaType: storepb.MetaType_TABLE},
		Engine:   storepb.Engine_MYSQL,
		Database: "app",
		Table:    "orders",
	}
	names := make([]string, 0, maxCELColumns+1)
	for index := 0; index <= maxCELColumns; index++ {
		name := "c" + strings.Repeat("x", index)
		names = append(names, name)
		target.Columns = append(target.Columns, ColumnInfo{Name: name, Type: "int", Kind: ColumnKindNumeric})
	}
	_, err := celColumns(target, names)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	_, err = celColumns(target, names[:maxCELColumns])
	require.NoError(t, err)
}

// A column whose name is not a CEL identifier can never be referenced by an
// expression, so it is refused with a message about the name instead of a parse
// error the user cannot act on.
func TestCelColumnsRejectsNonIdentifierNames(t *testing.T) {
	t.Parallel()

	target := ResolvedTarget{
		Target:   Target{GUID: "inst;app;orders", MetaType: storepb.MetaType_TABLE},
		Engine:   storepb.Engine_MYSQL,
		Database: "app",
		Table:    "orders",
		Columns:  []ColumnInfo{{Name: "order-id", Type: "int", Kind: ColumnKindNumeric}},
	}
	_, err := celColumns(target, []string{"order-id"})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

func TestCompileCELAcceptsTheRuleLanguage(t *testing.T) {
	t.Parallel()

	tests := []string{
		`amount > 0.0`,
		`amount > 0.0 && status == "paid"`,
		`is_null(email) || email.startsWith("a")`,
		`coalesce(amount, 0.0) > 10.0`,
		`coalesce(status, "unknown") == "unknown"`,
		`qty % 2 == 0`,
		`size(email) > 3`,
		`email.matches("^[a-z]+@")`,
		`status.to_lower() == "paid"`,
		`status.to_upper() == "PAID"`,
		`email.startsWith("a") && email.endsWith(".com")`,
		`created_at > timestamp("2026-01-01T00:00:00Z")`,
		`active`,
		`!active`,
	}
	columns := append(celTestColumns(), ColumnInfo{Name: "qty", Type: "int", Kind: ColumnKindNumeric})
	for _, expression := range tests {
		t.Run(expression, func(t *testing.T) {
			t.Parallel()
			program, err := CompileCEL(columns, expression)
			require.NoError(t, err)
			require.NotNil(t, program)
		})
	}
}

func TestCompileCELRejectsInvalidExpressions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		expression string
	}{
		{name: "empty", expression: "  "},
		{name: "too long", expression: "amount > 0.0 && " + strings.Repeat("amount > 0.0 && ", maxCELExpressionLength/14)},
		{name: "unknown variable", expression: "missing > 0"},
		{name: "non-boolean result", expression: "amount + 1"},
		{name: "unknown function", expression: "not_a_function(amount) > 0"},
		{name: "invalid regex", expression: `email.matches("[")`},
		{name: "non-constant regex", expression: `email.matches(status)`},
		{name: "syntax error", expression: "amount >"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := CompileCEL(celTestColumns(), test.expression)
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err), "error: %v", err)
		})
	}
}

// Each column kind is bound as the CEL type its values can be compared as; the
// engine's modifier suffixes must not change that.
func TestCelColumnType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		column ColumnInfo
		want   *cel.Type
	}{
		{column: ColumnInfo{Name: "id", Type: "bigint unsigned", Kind: ColumnKindNumeric}, want: cel.IntType},
		{column: ColumnInfo{Name: "qty", Type: "int", Kind: ColumnKindNumeric}, want: cel.IntType},
		{column: ColumnInfo{Name: "amount", Type: "decimal(10,2)", Kind: ColumnKindNumeric}, want: cel.DoubleType},
		{column: ColumnInfo{Name: "name", Type: "varchar(64)", Kind: ColumnKindString}, want: cel.StringType},
		{column: ColumnInfo{Name: "active", Type: "boolean", Kind: ColumnKindBoolean}, want: cel.BoolType},
		{column: ColumnInfo{Name: "created_at", Type: "timestamptz", Kind: ColumnKindTemporal}, want: cel.TimestampType},
	}
	for _, test := range tests {
		got, err := celColumnType(test.column)
		require.NoError(t, err, test.column.Name)
		require.True(t, got.IsExactType(test.want), "%s mapped to %s, want %s", test.column.Name, got, test.want)
	}

	_, err := celColumnType(ColumnInfo{Name: "raw", Kind: ColumnKindUnknown})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// A column type this build cannot map onto a CEL type is refused rather than
// bound as an arbitrary value.
func TestCompileCELRejectsAnUnmappableColumn(t *testing.T) {
	t.Parallel()

	_, err := CompileCEL([]ColumnInfo{{Name: "raw", Type: "", Kind: ColumnKindUnknown}}, "raw == 1")
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// A CEL row is the normalized sample values in column order; the expression reads
// a variable per column.
func celRow(id int64, amount any, email any, status any, active any, createdAt any) []any {
	return []any{id, amount, email, status, active, createdAt}
}

func TestEvalCELRowsCountsViolations(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	program, err := CompileCEL(celTestColumns(), "amount > 100.0")
	require.NoError(t, err)

	rows := [][]any{
		celRow(1, 50.0, "a@b.com", "paid", true, createdAt),
		celRow(2, 150.0, "b@b.com", "paid", true, createdAt),
		celRow(3, 200.0, "c@b.com", "open", false, createdAt),
	}
	// The expression is the condition a row must satisfy: a row that evaluates to
	// false is a violation.
	violations, evaluated, err := EvalCELRows(program, celTestColumns(), rows)
	require.NoError(t, err)
	require.Equal(t, int64(1), violations)
	require.Equal(t, int64(3), evaluated)
}

// A NULL is a CEL null: is_null sees it, and a guard in front of a member call
// keeps the evaluation of a null from becoming a runtime error.
func TestEvalCELRowsTreatsNullAsCelNull(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	program, err := CompileCEL(celTestColumns(), `is_null(email)`)
	require.NoError(t, err)
	rows := [][]any{
		celRow(1, 1.0, nil, nil, true, createdAt),
		celRow(2, 2.0, "a@b.com", "paid", true, createdAt),
		celRow(3, 3.0, nil, nil, nil, createdAt),
	}
	violations, evaluated, err := EvalCELRows(program, celTestColumns(), rows)
	require.NoError(t, err)
	require.Equal(t, int64(1), violations)
	require.Equal(t, int64(3), evaluated)

	guarded, err := CompileCEL(celTestColumns(), `is_null(email) || email.startsWith("a")`)
	require.NoError(t, err)
	violations, evaluated, err = EvalCELRows(guarded, celTestColumns(), rows)
	require.NoError(t, err)
	require.Equal(t, int64(0), violations)
	require.Equal(t, int64(3), evaluated)
}

func TestEvalCELRowsTypesAndHelpers(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	rows := [][]any{
		celRow(2, nil, "a@b.com", "PAID", int64(1), createdAt),
		celRow(3, 5.0, "b@b.com", "paid", int64(0), "2026-10-02 00:00:00"),
	}
	tests := []struct {
		name       string
		expression string
		violations int64
	}{
		{name: "null coalesce", expression: `coalesce(amount, 0.0) > 10.0`, violations: 2},
		{name: "integer modulo", expression: `id % 2 == 0`, violations: 1},
		{name: "boolean from bit", expression: "active", violations: 1},
		{name: "lower", expression: `status.to_lower() == "paid"`, violations: 0},
		{name: "upper", expression: `status.to_upper() == "PAID"`, violations: 0},
		{name: "regex", expression: `email.matches("^[a-z]+@")`, violations: 0},
		{name: "endsWith", expression: `email.endsWith(".com")`, violations: 0},
		{name: "size", expression: `size(email) > 5`, violations: 0},
		{name: "timestamp compare", expression: `created_at > timestamp("2026-10-01T12:00:00Z")`, violations: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			program, err := CompileCEL(celTestColumns(), test.expression)
			require.NoError(t, err)
			violations, evaluated, err := EvalCELRows(program, celTestColumns(), rows)
			require.NoError(t, err)
			require.Equal(t, int64(2), evaluated)
			require.Equal(t, test.violations, violations)
		})
	}
}

// The same numeric value arrives in different Go types depending on the driver, so
// the conversion accepts the normalized alternatives instead of rejecting a valid
// row.
func TestEvalCELRowsConvertsNormalizedWidths(t *testing.T) {
	t.Parallel()

	createdAt := "2026-10-01 00:00:00"
	program, err := CompileCEL(celTestColumns(), `id % 2 == 0 && amount > 6.0 && active`)
	require.NoError(t, err)
	// id arrives as a float, amount as an integer, active as the string a loosely
	// typed driver reports.
	rows := [][]any{
		{float64(2), int64(7), "a@b.com", "paid", "true", createdAt},
	}
	violations, evaluated, err := EvalCELRows(program, celTestColumns(), rows)
	require.NoError(t, err)
	require.Equal(t, int64(0), violations)
	require.Equal(t, int64(1), evaluated)
}

// A row value the column's type cannot represent is BAD_DATA: skipping it would
// turn a broken sample into a pass.
func TestEvalCELRowsRejectsMismatchedValues(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		row  []any
	}{
		{name: "short row", row: []any{int64(1)}},
		{name: "double column holds text", row: celRow(1, "not-a-number", "a@b.com", "paid", true, createdAt)},
		{name: "int column holds text", row: []any{"not-an-integer", 1.0, "a@b.com", "paid", true, createdAt}},
		{name: "integer column is fractional", row: []any{1.5, 1.0, "a@b.com", "paid", true, createdAt}},
		{name: "string column holds a number", row: celRow(1, 1.0, int64(7), "paid", true, createdAt)},
		{name: "boolean column holds a number", row: celRow(1, 1.0, "a@b.com", "paid", int64(7), createdAt)},
		{name: "timestamp column holds text", row: celRow(1, 1.0, "a@b.com", "paid", true, "not-a-time")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			program, err := CompileCEL(celTestColumns(), "amount > 0.0")
			require.NoError(t, err)
			_, _, err = EvalCELRows(program, celTestColumns(), [][]any{test.row})
			require.Error(t, err)
			require.ErrorIs(t, err, errBadData)
		})
	}
}

// A runtime evaluation error (here: a member call on null) is BAD_DATA, never a
// skipped row.
func TestEvalCELRowsRejectsRuntimeErrors(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)
	program, err := CompileCEL(celTestColumns(), `email.startsWith("a")`)
	require.NoError(t, err)
	_, _, err = EvalCELRows(program, celTestColumns(), [][]any{
		celRow(1, 1.0, nil, "paid", true, createdAt),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, errBadData)
}

// CEL is supported on every dialect because the predicate runs in Go, but only on
// relation types whose column types the registry can vouch for.
func TestCELSupport(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
		storepb.Engine_STARROCKS,
	} {
		ok, reason := Supported(engine, storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION)
		require.True(t, ok, "%s: %s", engine, reason)
		require.Empty(t, reason)
	}
	require.True(t, RuleTypeForTarget(storepb.MetaType_TABLE, storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION))
	require.True(t, RuleTypeForTarget(storepb.MetaType_EXTERNAL_TABLE, storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION))
	require.False(t, RuleTypeForTarget(storepb.MetaType_VIEW, storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION))
	require.False(t, RuleTypeForTarget(storepb.MetaType_MATERIALIZED_VIEW, storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION))
	require.False(t, IsMetadataRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION))
}

// runCelExecution runs one CEL rule through the executor with the sample rows
// the fake queryer returns.
func runCelExecution(t *testing.T, config *storepb.QualityRuleConfig, result *db.QueryResult, opts ExecOptions) (*Execution, *execFakeQueryer, error) {
	t.Helper()
	queryer := &execFakeQueryer{results: []*db.QueryResult{result}}
	execution, err := NewExecutor().Execute(context.Background(), queryer,
		execTestResolution(storepb.Engine_MYSQL),
		execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, config),
		opts)
	return execution, queryer, err
}

func TestExecuteCELRuleCountsTheSampleRows(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 100.0", []string{"amount"}, 100)
	execution, queryer, err := runCelExecution(t, config, &db.QueryResult{
		Columns: []string{"amount"},
		Rows:    [][]any{{float64(50)}, {float64(150)}, {float64(200)}},
	}, ExecOptions{StoreSamples: true, SampleLimit: 20})
	require.NoError(t, err)

	observation := execution.Observation
	require.Equal(t, int64(1), observation.FailedRows)
	require.Equal(t, int64(3), observation.TotalRows)
	require.InDelta(t, 1, observation.Observed, 1e-9)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, observation.ObservedKind)
	// Fewer rows than the cap means the whole filtered table was read.
	require.False(t, observation.Sampled)
	require.Equal(t, int64(3), observation.SampleRows)
	require.False(t, observation.Truncated)

	require.Len(t, queryer.queries, 1, "a CEL rule needs no second sample query")
	require.Equal(t, 100, queryer.options[0].MaxRows, "the cap must equal sample_rows")
	require.Contains(t, queryer.queries[0], "LIMIT 100")
	require.Equal(t, queryer.queries[0], execution.ExecutedSQL)

	require.NotNil(t, execution.Samples)
	require.Len(t, execution.Samples.GetSamples(), 1)
	require.Equal(t, "amount", execution.Samples.GetSamples()[0].GetColumns()[0].GetColumn())
	require.Equal(t, "50", execution.Samples.GetSamples()[0].GetColumns()[0].GetValue())
}

func TestExecuteCELRuleMarksACappedSample(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 100.0", []string{"amount"}, 2)
	execution, _, err := runCelExecution(t, config, &db.QueryResult{
		Columns: []string{"amount"},
		Rows:    [][]any{{float64(50)}, {float64(150)}},
	}, ExecOptions{})
	require.NoError(t, err)
	// Reading exactly the cap means the query may have more rows: this is a sample.
	require.True(t, execution.Observation.Sampled)
	require.True(t, execution.Sampled)
	require.Equal(t, int64(2), execution.Observation.SampleRows)
}

func TestExecuteCELRuleCarriesTruncation(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 0.0", []string{"amount"}, 2)
	execution, _, err := runCelExecution(t, config, &db.QueryResult{
		Columns:   []string{"amount"},
		Rows:      [][]any{{float64(1)}},
		Truncated: true,
	}, ExecOptions{})
	require.NoError(t, err)
	require.True(t, execution.Observation.Truncated)
	require.True(t, execution.Truncated)
}

// An empty sample follows on_empty: it was never judged, so it is not a pass by
// default.
func TestExecuteCELRuleWithNoRowsFollowsOnEmpty(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 0.0", []string{"amount"}, 100)
	execution, _, err := runCelExecution(t, config, &db.QueryResult{Columns: []string{"amount"}}, ExecOptions{})
	require.NoError(t, err)
	require.Zero(t, execution.Observation.TotalRows)
	require.Zero(t, execution.Observation.FailedRows)

	state := Evaluate(execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, config), execution.Observation, time.Now())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, state.Status)

	config.OnEmpty = storepb.QualityEmptyBehavior_QUALITY_EMPTY_BEHAVIOR_PASS
	state = Evaluate(execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, config), execution.Observation, time.Now())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, state.Status)
}

// A CEL rule measures violations over the rows it evaluated, so a percentage
// threshold divides by that count.
func TestExecuteCELRuleSupportsPercentThresholds(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 100.0", []string{"amount"}, 100)
	config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
	config.GetCelExpression().FailThreshold = protoFloat(10)
	execution, _, err := runCelExecution(t, config, &db.QueryResult{
		Columns: []string{"amount"},
		Rows:    [][]any{{float64(50)}, {float64(150)}, {float64(50)}, {float64(50)}},
	}, ExecOptions{})
	require.NoError(t, err)

	state := Evaluate(execTestSnapshot(storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, config), execution.Observation, time.Now())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, state.Status)
	require.InDelta(t, 10, state.Thresholds.GetFailValue(), 1e-9)
	require.Equal(t, storepb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN, state.Thresholds.GetComparison())
}

func TestExecuteCELRuleLimitsTheFailureSample(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 1000.0", []string{"amount"}, 10)
	execution, _, err := runCelExecution(t, config, &db.QueryResult{
		Columns: []string{"amount"},
		Rows:    [][]any{{float64(150)}, {float64(200)}, {float64(300)}},
	}, ExecOptions{StoreSamples: true, SampleLimit: 2})
	require.NoError(t, err)
	require.NotNil(t, execution.Samples)
	require.Len(t, execution.Samples.GetSamples(), 2)
}

func TestExecuteCELRuleSkipsSamplesWhenNotRequested(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 100.0", []string{"amount"}, 10)
	execution, _, err := runCelExecution(t, config, &db.QueryResult{
		Columns: []string{"amount"},
		Rows:    [][]any{{float64(50)}},
	}, ExecOptions{})
	require.NoError(t, err)
	require.Nil(t, execution.Samples)
}

func TestExecuteCELRuleRejectsAMismatchedSampleShape(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 100.0", []string{"amount"}, 10)
	_, _, err := runCelExecution(t, config, &db.QueryResult{
		Columns: []string{"status"},
		Rows:    [][]any{{"paid"}},
	}, ExecOptions{})
	require.Error(t, err)
	require.ErrorIs(t, err, errBadData)

	_, _, err = runCelExecution(t, config, &db.QueryResult{
		Columns: []string{"amount", "status"},
		Rows:    [][]any{{float64(1), "paid"}},
	}, ExecOptions{})
	require.Error(t, err)
	require.ErrorIs(t, err, errBadData)
}

func TestExecuteCELRuleRejectsABadRowValue(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 100.0", []string{"amount"}, 10)
	_, _, err := runCelExecution(t, config, &db.QueryResult{
		Columns: []string{"amount"},
		Rows:    [][]any{{"not-a-number"}},
	}, ExecOptions{})
	require.Error(t, err)
	require.ErrorIs(t, err, errBadData)
}

func protoFloat(value float64) *float64 {
	return &value
}

// The save-time pipeline accepts every legal CEL rule and refuses the parameter
// combinations that would fail (or mean nothing) at execution.
func TestValidateCELExpressionConfig(t *testing.T) {
	t.Parallel()

	target := validationTarget(storepb.Engine_MYSQL)
	percent := func(fail, warn *float64) *storepb.QualityRuleConfig {
		config := celRuleConfig("amount > 0.0", []string{"amount"}, 100)
		config.ThresholdMetric = storepb.QualityThresholdMetric_QUALITY_THRESHOLD_METRIC_PERCENT
		config.GetCelExpression().FailThreshold = fail
		config.GetCelExpression().WarnThreshold = warn
		return config
	}
	withExpression := func(expression string) *storepb.QualityRuleConfig {
		return celRuleConfig(expression, []string{"amount"}, 100)
	}
	withColumns := func(columns ...string) *storepb.QualityRuleConfig {
		return celRuleConfig("amount > 0.0", columns, 100)
	}
	withSampleRows := func(rows int32) *storepb.QualityRuleConfig {
		return celRuleConfig("amount > 0.0", []string{"amount"}, rows)
	}

	cases := []validationCase{
		{name: "valid count rule", config: celRuleConfig("amount > 0.0", []string{"amount"}, 100)},
		{name: "valid boolean column", config: celRuleConfig("active", []string{"active"}, 0)},
		{name: "valid string column", config: celRuleConfig(`name.startsWith("a")`, []string{"name"}, 10)},
		{name: "valid percent rule", config: percent(validationPtr(5.0), validationPtr(1.0))},
		{name: "sample rows unset", config: withSampleRows(0)},
		{name: "sample rows at the maximum", config: withSampleRows(maxCELSampleRows)},

		{name: "empty expression", config: withExpression("  "), wantErr: true},
		{
			name:    "expression too long",
			config:  withExpression("amount > 0.0 && " + strings.Repeat("amount > 0.0 && ", maxCELExpressionLength/17)),
			wantErr: true,
		},
		{name: "no columns", config: celRuleConfig("amount > 0.0", nil, 100), wantErr: true},
		{name: "unknown column", config: withColumns("missing"), wantErr: true},
		{name: "undeclared variable", config: celRuleConfig("id > 0", []string{"amount"}, 100), wantErr: true},
		{name: "non-boolean expression", config: withExpression("amount + 1.0"), wantErr: true},
		{name: "unknown function", config: withExpression("not_a_function(amount) > 0.0"), wantErr: true},
		{name: "invalid regex", config: withExpression(`name.matches("[")`), wantErr: true},
		{name: "non-constant regex", config: celRuleConfig(`name.matches(name)`, []string{"name"}, 100), wantErr: true},
		{name: "negative sample rows", config: withSampleRows(-1), wantErr: true},
		{name: "sample rows above the maximum", config: withSampleRows(maxCELSampleRows + 1), wantErr: true},
		{name: "negative fail threshold", config: percent(validationPtr(-1.0), nil), wantErr: true},
		{name: "percent above 100", config: percent(validationPtr(101.0), nil), wantErr: true},
		{name: "warn above fail", config: percent(validationPtr(2.0), validationPtr(5.0)), wantErr: true},
		{name: "invalid filter", config: func() *storepb.QualityRuleConfig {
			config := celRuleConfig("amount > 0.0", []string{"amount"}, 100)
			config.Filter = &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{
				{Column: "missing", Op: storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_EQ, Values: []string{"x"}},
			}}
			return config
		}(), wantErr: true},
	}
	runValidationCases(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, target, cases)
}

// A column whose name is not a CEL identifier is refused by validation rather than
// by a parser error the user cannot act on.
func TestValidateCELExpressionRejectsANonIdentifierColumn(t *testing.T) {
	t.Parallel()

	target := validationTarget(storepb.Engine_MYSQL)
	target.Columns = append(target.Columns, ColumnInfo{Name: "order-id", Type: "int", Kind: ColumnKindNumeric})
	config := celRuleConfig("order-id > 0", []string{"order-id"}, 100)
	err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, config)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// A money column is refused at save: CEL reads the value as a plain number, while
// money arrives as decorated currency text and would turn every row into BAD_DATA
// after the rule was saved.
func TestValidateCELExpressionRejectsMoneyColumns(t *testing.T) {
	t.Parallel()

	target := validationTarget(storepb.Engine_POSTGRES)
	target.Columns = append(target.Columns, ColumnInfo{Name: "balance", Type: "money", Kind: ColumnKindNumeric})
	config := celRuleConfig("balance > 0.0", []string{"balance"}, 100)
	err := ValidateConfig(target, storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION, config)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	require.Contains(t, err.Error(), "money")
}

// A stored money rule still evaluates: the sample value arrives as the decorated
// string the driver renders, which celParseNumber accepts when unambiguous.
func TestExecuteCELRuleReadsMoneyValues(t *testing.T) {
	t.Parallel()

	config := celRuleConfig("amount > 100.0", []string{"amount"}, 10)
	execution, _, err := runCelExecution(t, config, &db.QueryResult{
		Columns: []string{"amount"},
		Rows:    [][]any{{"$1,234.50"}, {"$0.50"}},
	}, ExecOptions{})
	require.NoError(t, err)
	// 1234.50 satisfies the predicate; 0.50 does not and is a violation.
	require.Equal(t, int64(1), execution.Observation.FailedRows)
	require.Equal(t, int64(2), execution.Observation.TotalRows)
}

// celParseNumber accepts a decorated money rendering only when it is unambiguous;
// an ambiguous separator layout is BAD_DATA rather than a silent locale guess.
func TestCelParseNumberStrict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    float64
		wantErr bool
	}{
		{name: "plain decimal", value: "12.5", want: 12.5},
		{name: "scientific", value: "1.5e2", want: 150},
		{name: "currency prefix with grouping", value: "$1,234.50", want: 1234.5},
		{name: "negative currency prefix", value: "-$1.23", want: -1.23},
		{name: "currency after sign", value: "$-1.23", want: -1.23},
		{name: "parenthesized negative", value: "($12.30)", want: -12.3},
		{name: "grouped thousands", value: "1,234", want: 1234},
		{name: "comma decimal with dot grouping", value: "1.234,50", want: 1234.5},
		{name: "trailing currency symbol", value: "1.23€", want: 1.23},
		{name: "ambiguous lone comma", value: "1,5", wantErr: true},
		{name: "ambiguous internal space", value: "1 234,50", wantErr: true},
		{name: "not a number", value: "not-a-number", wantErr: true},
		{name: "nan", value: "NaN", wantErr: true},
		{name: "infinity", value: "Inf", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := celParseNumber(test.value)
			if test.wantErr {
				require.Error(t, err, test.value)
				return
			}
			require.NoError(t, err, test.value)
			require.InDelta(t, test.want, got, 1e-9, test.value)
		})
	}
}
