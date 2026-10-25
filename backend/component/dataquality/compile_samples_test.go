package dataquality

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// sampleTestEngine is one dialect family the sample matrix runs through.
type sampleTestEngine struct {
	name     string
	engine   storepb.Engine
	tableSQL string
}

// sampleTestEngines covers the three dialect families; StarRocks and TiDB share
// the MySQL one, which the compiler goldens already pin.
func sampleTestEngines() []sampleTestEngine {
	return []sampleTestEngine{
		{name: "mysql", engine: storepb.Engine_MYSQL, tableSQL: "`shop`.`orders`"},
		{name: "postgres", engine: storepb.Engine_POSTGRES, tableSQL: `"public"."orders"`},
		{name: "mssql", engine: storepb.Engine_MSSQL, tableSQL: "[public].[orders]"},
	}
}

// requireSamplePlaceholders asserts that the placeholders in a sample statement
// agree with its argument count, in the dialect's own numbering.
func requireSamplePlaceholders(t *testing.T, engine storepb.Engine, sql string, args []any) {
	t.Helper()
	switch engine {
	case storepb.Engine_POSTGRES:
		if len(args) == 0 {
			require.NotContains(t, sql, "$1")
			return
		}
		require.Contains(t, sql, "$1")
		require.Contains(t, sql, fmt.Sprintf("$%d", len(args)))
		require.NotContains(t, sql, fmt.Sprintf("$%d", len(args)+1))
	case storepb.Engine_MSSQL:
		require.Equal(t, len(args), strings.Count(sql, "@p"))
	default:
		require.Equal(t, len(args), strings.Count(sql, "?"))
	}
}

// expectSampleLimit renders the bound a sample statement must carry for one
// engine.
func expectSampleLimit(engine storepb.Engine, limit int) string {
	if engine == storepb.Engine_MSSQL {
		return fmt.Sprintf("ORDER BY (SELECT NULL) OFFSET 0 ROWS FETCH NEXT %d ROWS ONLY", limit)
	}
	return fmt.Sprintf("LIMIT %d", limit)
}

// sampleColumnsFor is the result shape the executor expects back from each rule
// type's sample statement. A column-checked sample leads with the target's
// primary key, because the key is what identifies the failed row; a grouped
// sample returns only the grouped columns.
func sampleColumnsFor(t *testing.T, name string) []string {
	t.Helper()
	switch name {
	case "null_percent", "string_length":
		return []string{"id", "email"}
	case "unique":
		return []string{"email"}
	case "unique_combination":
		return []string{"amount", "status"}
	case "accepted_values":
		return []string{"id", "status"}
	case "range":
		return []string{"id", "amount"}
	case "cross_column":
		return []string{"id", "qty", "amount"}
	case "referential_integrity":
		return compileTestTarget(storepb.Engine_MYSQL).ColumnNames()
	case "custom_sql":
		return nil
	default:
		t.Fatalf("the golden %q has no sample column expectation", name)
		return nil
	}
}

// sampleArgsFor is the argument list each rule type's sample statement binds;
// the compiler goldens already pin the same values for the aggregate.
func sampleArgsFor(name string) []any {
	switch name {
	case "accepted_values":
		return []any{"paid", "open"}
	case "range":
		return []any{int64(0), int64(100)}
	case "string_length":
		return []any{int64(3), int64(64)}
	default:
		return nil
	}
}

// TestCompileFailureRowsCoversEveryRuleType compiles the failed-row statement for
// every rule type on every dialect and pins its kind, columns, bound and
// placeholder numbering.
func TestCompileFailureRowsCoversEveryRuleType(t *testing.T) {
	t.Parallel()

	for _, golden := range compileRuleGoldens() {
		for _, engineCase := range sampleTestEngines() {
			t.Run(golden.name+"/"+engineCase.name, func(t *testing.T) {
				t.Parallel()
				dialect, err := DialectFor(engineCase.engine)
				require.NoError(t, err)
				input := CompileInput{
					Dialect: dialect,
					Target:  compileTestTarget(engineCase.engine),
					Type:    golden.ruleType,
					Config:  golden.config,
					Filter:  golden.filter,
				}
				query, err := CompileFailureRows(input, 20)
				require.NoError(t, err)

				if query.SQL == "" {
					// row_count, row_count_change and freshness have no failing
					// row: the caller skips the sample rather than showing one.
					require.Nil(t, query.Args)
					require.Nil(t, query.Columns)
					return
				}
				require.Equal(t, QuerySample, query.Kind)
				require.Equal(t, sampleColumnsFor(t, golden.name), query.Columns)
				require.Equal(t, sampleArgsFor(golden.name), query.Args)
				if golden.name != "custom_sql" {
					// custom_sql's sample is the user's own statement, which owns
					// the FROM clause and is not rewritten to the target table.
					require.Contains(t, query.SQL, engineCase.tableSQL)
				}
				require.Contains(t, query.SQL, expectSampleLimit(engineCase.engine, 20))
				requireSamplePlaceholders(t, engineCase.engine, query.SQL, query.Args)
			})
		}
	}
}

// TestCompileFailureRowsWithoutKeyColumns keeps the sample shape independent of
// whether the registry knows a primary key: without one the checked columns are
// all there is, and a checked column that is part of the key is not repeated.
func TestCompileFailureRowsWithoutKeyColumns(t *testing.T) {
	t.Parallel()

	dialect := mustDialect(t, storepb.Engine_MYSQL)
	target := compileTestTarget(storepb.Engine_MYSQL)
	target.KeyColumns = nil
	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "id", MaxNullPct: proto.Float64(5)},
	}}
	query, err := CompileFailureRows(CompileInput{
		Dialect: dialect,
		Target:  target,
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:  config,
	}, 20)
	require.NoError(t, err)
	require.Equal(t, []string{"id"}, query.Columns)
	require.Equal(t, "SELECT `id` FROM `shop`.`orders` WHERE (`id` IS NULL) LIMIT 20", query.SQL)

	// With the key known it leads the sample in key order, and the checked column
	// is not repeated just because it is also part of the key.
	target.KeyColumns = []string{"id", "email"}
	query, err = CompileFailureRows(CompileInput{
		Dialect: dialect,
		Target:  target,
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:  config,
	}, 20)
	require.NoError(t, err)
	require.Equal(t, []string{"id", "email"}, query.Columns)
	require.Equal(t, "SELECT `id`, `email` FROM `shop`.`orders` WHERE (`id` IS NULL) LIMIT 20", query.SQL)
}

// TestCompileFailureRowsShapes pins the complete SQL of the representative rule
// types, so a predicate change fails here instead of being mirrored by the test.
func TestCompileFailureRowsShapes(t *testing.T) {
	t.Parallel()

	goldens := map[string]compileGolden{}
	for _, golden := range compileRuleGoldens() {
		goldens[golden.name] = golden
	}

	tests := []struct {
		name   string
		golden compileGolden
		engine storepb.Engine
		want   string
	}{
		{
			name:   "null percent mysql",
			golden: goldens["null_percent"],
			engine: storepb.Engine_MYSQL,
			want:   "SELECT `id`, `email` FROM `shop`.`orders` WHERE (`email` IS NULL) LIMIT 20",
		},
		{
			name:   "null percent postgres",
			golden: goldens["null_percent"],
			engine: storepb.Engine_POSTGRES,
			want:   `SELECT "id", "email" FROM "public"."orders" WHERE ("email" IS NULL) LIMIT 20`,
		},
		{
			name:   "null percent sql server",
			golden: goldens["null_percent"],
			engine: storepb.Engine_MSSQL,
			want: "SELECT [id], [email] FROM [public].[orders] WHERE ([email] IS NULL) " +
				"ORDER BY (SELECT NULL) OFFSET 0 ROWS FETCH NEXT 20 ROWS ONLY",
		},
		{
			name:   "unique groups the duplicate keys",
			golden: goldens["unique"],
			engine: storepb.Engine_MYSQL,
			want: "SELECT `email` FROM `shop`.`orders` WHERE `email` IS NOT NULL " +
				"GROUP BY `email` HAVING COUNT(*) > 1 LIMIT 20",
		},
		{
			name:   "accepted values",
			golden: goldens["accepted_values"],
			engine: storepb.Engine_MYSQL,
			want: "SELECT `id`, `status` FROM `shop`.`orders` " +
				"WHERE (`status` IS NULL OR `status` NOT IN (?, ?)) LIMIT 20",
		},
		{
			name:   "regex postgres",
			golden: compileRegexGolden(),
			engine: storepb.Engine_POSTGRES,
			want: `SELECT "id", "email" FROM "public"."orders" ` +
				`WHERE ("email" IS NULL OR "email" !~ $1) LIMIT 20`,
		},
		{
			name:   "cross column",
			golden: goldens["cross_column"],
			engine: storepb.Engine_MYSQL,
			want: "SELECT `id`, `qty`, `amount` FROM `shop`.`orders` " +
				"WHERE (NOT (`qty` >= `amount`)) LIMIT 20",
		},
		{
			name:   "custom sql wraps the user statement",
			golden: goldens["custom_sql"],
			engine: storepb.Engine_MYSQL,
			want:   "SELECT * FROM (SELECT * FROM orders WHERE amount < 0) dq LIMIT 20",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			query, err := CompileFailureRows(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(test.engine),
				Type:    test.golden.ruleType,
				Config:  test.golden.config,
				Filter:  test.golden.filter,
			}, 20)
			require.NoError(t, err)
			require.Equal(t, test.want, query.SQL)
		})
	}
}

// compileRegexGolden is the regex case the shared goldens leave out: MSSQL has no
// regular-expression operator, so it needs its own matrix.
func compileRegexGolden() compileGolden {
	return compileGolden{
		name:     "regex",
		ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{
			Regex: &storepb.RegexConfig{Column: "email", Pattern: "^[^@]+@[^@]+$"},
		}},
	}
}

// regex sample: the postgres operator differs from the MySQL family's, and MSSQL
// refuses even though validation already rejects the combination.
func TestCompileFailureRowsRegex(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES} {
		t.Run(engine.String(), func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(engine)
			require.NoError(t, err)
			query, err := CompileFailureRows(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
				Config:  compileRegexGolden().config,
			}, 20)
			require.NoError(t, err)
			require.Equal(t, []any{"^[^@]+@[^@]+$"}, query.Args)
			requireSamplePlaceholders(t, engine, query.SQL, query.Args)
			require.Contains(t, query.SQL, " IS NULL OR ")
		})
	}

	dialect, err := DialectFor(storepb.Engine_MSSQL)
	require.NoError(t, err)
	_, err = CompileFailureRows(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MSSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		Config:  compileRegexGolden().config,
	}, 20)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// The rule's filter narrows the sample exactly like it narrows the aggregate, and
// its arguments come before the config arguments because its predicate appears
// first in the statement.
func TestCompileFailureRowsAppliesTheFilterFirst(t *testing.T) {
	t.Parallel()

	filter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
	}}
	goldens := map[string]compileGolden{}
	for _, golden := range compileRuleGoldens() {
		goldens[golden.name] = golden
	}
	accepted := goldens["accepted_values"]
	input := CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    accepted.ruleType,
		Config:  accepted.config,
		Filter:  filter,
	}
	query, err := CompileFailureRows(input, 5)
	require.NoError(t, err)
	require.Equal(t,
		"SELECT `id`, `status` FROM `shop`.`orders` WHERE `status` = ? "+
			"AND (`status` IS NULL OR `status` NOT IN (?, ?)) LIMIT 5",
		query.SQL)
	require.Equal(t, []any{"paid", "paid", "open"}, query.Args)

	postgresInput := input
	postgresInput.Dialect = mustDialect(t, storepb.Engine_POSTGRES)
	postgresInput.Target = compileTestTarget(storepb.Engine_POSTGRES)
	query, err = CompileFailureRows(postgresInput, 5)
	require.NoError(t, err)
	require.Equal(t,
		`SELECT "id", "status" FROM "public"."orders" WHERE "status" = $1 `+
			`AND ("status" IS NULL OR "status" NOT IN ($2, $3)) LIMIT 5`,
		query.SQL)
	require.Equal(t, []any{"paid", "paid", "open"}, query.Args)
}

// The sample limit follows the same normalization as the executor: unset reads as
// the default, a large request is clamped.
func TestCompileFailureRowsNormalizesTheLimit(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
		NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
	}}
	input := CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		Config:  config,
	}
	for _, test := range []struct {
		name  string
		limit int
		want  string
	}{
		{name: "unset reads as the default", limit: 0, want: "LIMIT 20"},
		{name: "negative reads as the default", limit: -3, want: "LIMIT 20"},
		{name: "explicit", limit: 7, want: "LIMIT 7"},
		{name: "clamped at the maximum", limit: 500, want: "LIMIT 100"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			query, err := CompileFailureRows(input, test.limit)
			require.NoError(t, err)
			require.Contains(t, query.SQL, test.want)
		})
	}
}

// A rule type that cannot produce a failed-row statement either returns a zero
// query (the executor builds the sample another way) or is refused so a caller
// cannot silently store an empty sample.
func TestCompileFailureRowsUnsupportedTypes(t *testing.T) {
	t.Parallel()

	// A CEL rule's failing rows are the sampled rows its predicate rejected, so the
	// executor builds that sample itself and there is no second statement.
	cel := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CelExpression{
		CelExpression: &storepb.CelExpressionConfig{Expression: "amount > 0.0", Columns: []string{"amount"}},
	}}
	base := CompileInput{
		Dialect: mustDialect(t, storepb.Engine_MYSQL),
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Config:  cel,
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CEL_EXPRESSION,
	}
	query, err := CompileFailureRows(base, 20)
	require.NoError(t, err)
	require.Empty(t, query.SQL)

	unspecified := base
	unspecified.Type = storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED
	_, err = CompileFailureRows(unspecified, 20)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	nilDialect := base
	nilDialect.Dialect = nil
	_, err = CompileFailureRows(nilDialect, 20)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))

	nilConfig := base
	nilConfig.Config = nil
	_, err = CompileFailureRows(nilConfig, 20)
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
}

// mustDialect resolves one engine's dialect or fails the test.
func mustDialect(t *testing.T, engine storepb.Engine) Dialect {
	t.Helper()
	dialect, err := DialectFor(engine)
	require.NoError(t, err)
	return dialect
}
