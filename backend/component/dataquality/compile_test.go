package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// compileTestGUID is the registry GUID of the golden target. The compilers never
// read it for SQL, but it appears in validation errors.
const compileTestGUID = "instances/ins1;shop;public;orders"

// compileTestTarget is the relation the golden cases observe: a table with at
// least one column of every kind the compilers distinguish. MySQL and StarRocks
// have no schema segment, so the schema is empty there.
func compileTestTarget(engine storepb.Engine) ResolvedTarget {
	// MySQL and StarRocks have no schema segment; PostgreSQL and MSSQL qualify the
	// table with one.
	schema := "public"
	if engine == storepb.Engine_MYSQL || engine == storepb.Engine_STARROCKS {
		schema = ""
	}
	return ResolvedTarget{
		Target: Target{
			GUID:     compileTestGUID,
			MetaType: storepb.MetaType_TABLE,
			Path: &storepb.QualityTargetPath{
				Instance: "instances/ins1",
				Database: "shop",
				Schema:   schema,
				Object:   "orders",
			},
		},
		Engine:             engine,
		InstanceResourceID: "instances/ins1",
		Database:           "shop",
		Schema:             schema,
		Table:              "orders",
		Columns: []ColumnInfo{
			{Name: "id", Type: "bigint", Kind: ColumnKindNumeric},
			{Name: "email", Type: "varchar(255)", Kind: ColumnKindString},
			{Name: "amount", Type: "decimal(10,2)", Kind: ColumnKindNumeric},
			{Name: "created_at", Type: "timestamp", Kind: ColumnKindTemporal},
			{Name: "status", Type: "varchar(32)", Kind: ColumnKindString},
			{Name: "qty", Type: "int", Kind: ColumnKindNumeric},
			{Name: "price", Type: "decimal(12,2)", Kind: ColumnKindNumeric},
			{Name: "updated_at", Type: "datetime", Kind: ColumnKindTemporal},
			{Name: "name", Type: "varchar(100)", Kind: ColumnKindString},
		},
		KeyColumns: []string{"id"},
	}
}

// compileGolden is one rule definition and the complete statement each dialect
// must render for it. The SQL is written out literally rather than rebuilt from
// the dialect helpers, so a quoting or placeholder change actually fails the
// test instead of being mirrored by both sides.
type compileGolden struct {
	name        string
	ruleType    storepb.QualityRuleType
	config      *storepb.QualityRuleConfig
	filter      Filter
	wantArgs    []any
	wantColumns []string
	wantKind    QueryKind
	mysqlSQL    string
	postgresSQL string
	mssqlSQL    string
}

// compileRuleGoldens covers the 13 SQL rule types that share the dialect helpers;
// the v1.1 and metadata types have their own SQL or behavior pinned in
// cel_test.go, schema_drift_test.go, group_check_test.go, aggregate_test.go and
// cross_table_test.go. StarRocks shares the MySQL expectations: the dialects differ
// only by the engine the table is quoted for.
func compileRuleGoldens() []compileGolden {
	return []compileGolden{
		{
			name:     "row_count",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
				RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(1)},
			}},
			wantColumns: []string{"total_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL:    "SELECT COUNT(*) AS total_rows FROM `shop`.`orders`",
			postgresSQL: `SELECT COUNT(*) AS total_rows FROM "public"."orders"`,
			mssqlSQL:    "SELECT COUNT(*) AS total_rows FROM [public].[orders]",
		},
		{
			name:     "row_count_change",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCountChange{
				RowCountChange: &storepb.RowCountChangeConfig{MaxIncreasePct: proto.Float64(10)},
			}},
			wantColumns: []string{"total_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL:    "SELECT COUNT(*) AS total_rows FROM `shop`.`orders`",
			postgresSQL: `SELECT COUNT(*) AS total_rows FROM "public"."orders"`,
			mssqlSQL:    "SELECT COUNT(*) AS total_rows FROM [public].[orders]",
		},
		{
			name:     "null_percent",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullPct: proto.Float64(5)},
			}},
			wantColumns: []string{"total_rows", "failed_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `email` IS NULL THEN 1 ELSE 0 END) " +
				"AS failed_rows FROM `shop`.`orders`",
			postgresSQL: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN "email" IS NULL THEN 1 ELSE 0 END) ` +
				`AS failed_rows FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN [email] IS NULL THEN 1 ELSE 0 END) " +
				"AS failed_rows FROM [public].[orders]",
		},
		{
			name:     "unique",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
				Unique: &storepb.UniqueConfig{Column: "email"},
			}},
			wantColumns: []string{"failed_rows", "duplicate_groups", "total_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL: "SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, " +
				"(SELECT COUNT(*) FROM `shop`.`orders`) AS total_rows FROM " +
				"(SELECT `email`, COUNT(*) AS cnt FROM `shop`.`orders` WHERE `email` IS NOT NULL " +
				"GROUP BY `email` HAVING COUNT(*) > 1) dq",
			postgresSQL: `SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, ` +
				`(SELECT COUNT(*) FROM "public"."orders") AS total_rows FROM ` +
				`(SELECT "email", COUNT(*) AS cnt FROM "public"."orders" WHERE "email" IS NOT NULL ` +
				`GROUP BY "email" HAVING COUNT(*) > 1) dq`,
			mssqlSQL: "SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, " +
				"(SELECT COUNT(*) FROM [public].[orders]) AS total_rows FROM " +
				"(SELECT [email], COUNT(*) AS cnt FROM [public].[orders] WHERE [email] IS NOT NULL " +
				"GROUP BY [email] HAVING COUNT(*) > 1) dq",
		},
		{
			name:     "unique_combination",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{
				UniqueCombination: &storepb.UniqueCombinationConfig{Columns: []string{"amount", "status"}},
			}},
			wantColumns: []string{"failed_rows", "duplicate_groups", "total_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL: "SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, " +
				"(SELECT COUNT(*) FROM `shop`.`orders`) AS total_rows FROM " +
				"(SELECT `amount`, `status`, COUNT(*) AS cnt FROM `shop`.`orders` " +
				"WHERE `amount` IS NOT NULL AND `status` IS NOT NULL " +
				"GROUP BY `amount`, `status` HAVING COUNT(*) > 1) dq",
			postgresSQL: `SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, ` +
				`(SELECT COUNT(*) FROM "public"."orders") AS total_rows FROM ` +
				`(SELECT "amount", "status", COUNT(*) AS cnt FROM "public"."orders" ` +
				`WHERE "amount" IS NOT NULL AND "status" IS NOT NULL ` +
				`GROUP BY "amount", "status" HAVING COUNT(*) > 1) dq`,
			mssqlSQL: "SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, " +
				"(SELECT COUNT(*) FROM [public].[orders]) AS total_rows FROM " +
				"(SELECT [amount], [status], COUNT(*) AS cnt FROM [public].[orders] " +
				"WHERE [amount] IS NOT NULL AND [status] IS NOT NULL " +
				"GROUP BY [amount], [status] HAVING COUNT(*) > 1) dq",
		},
		{
			name:     "accepted_values",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
				AcceptedValues: &storepb.AcceptedValuesConfig{Column: "status", Values: []string{"paid", "open"}},
			}},
			wantArgs:    []any{"paid", "open"},
			wantColumns: []string{"total_rows", "failed_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `status` IS NULL OR `status` NOT IN (?, ?) " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
			postgresSQL: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN "status" IS NULL OR "status" NOT IN ($1, $2) ` +
				`THEN 1 ELSE 0 END) AS failed_rows FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN [status] IS NULL OR [status] NOT IN (@p1, @p2) " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM [public].[orders]",
		},
		{
			name:     "range",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{
				Range: &storepb.RangeConfig{Column: "amount", Min: proto.String("0"), Max: proto.String("100")},
			}},
			wantArgs:    []any{int64(0), int64(100)},
			wantColumns: []string{"total_rows", "failed_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `amount` <= ? OR `amount` >= ? " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
			postgresSQL: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN "amount" <= $1 OR "amount" >= $2 ` +
				`THEN 1 ELSE 0 END) AS failed_rows FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN [amount] <= @p1 OR [amount] >= @p2 " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM [public].[orders]",
		},
		{
			name:     "string_length",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StringLength{
				StringLength: &storepb.StringLengthConfig{Column: "email", MinLen: proto.Int64(3), MaxLen: proto.Int64(64)},
			}},
			wantArgs:    []any{int64(3), int64(64)},
			wantColumns: []string{"total_rows", "failed_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN CHAR_LENGTH(`email`) < ? OR CHAR_LENGTH(`email`) > ? " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
			postgresSQL: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN CHAR_LENGTH("email") < $1 OR CHAR_LENGTH("email") > $2 ` +
				`THEN 1 ELSE 0 END) AS failed_rows FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN LEN([email]) < @p1 OR LEN([email]) > @p2 " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM [public].[orders]",
		},
		{
			name:     "referential_integrity",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{
				ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
					ChildColumn:  "status",
					ParentTable:  "customer_status",
					ParentColumn: "code",
				},
			}},
			wantColumns: []string{"total_rows", "failed_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN c.`status` IS NULL OR p.`code` IS NULL " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM (SELECT * FROM `shop`.`orders`) c " +
				"LEFT JOIN `shop`.`customer_status` p ON c.`status` = p.`code`",
			postgresSQL: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN c."status" IS NULL OR p."code" IS NULL ` +
				`THEN 1 ELSE 0 END) AS failed_rows FROM (SELECT * FROM "public"."orders") c ` +
				`LEFT JOIN "public"."customer_status" p ON c."status" = p."code"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN c.[status] IS NULL OR p.[code] IS NULL " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM (SELECT * FROM [public].[orders]) c " +
				"LEFT JOIN [public].[customer_status] p ON c.[status] = p.[code]",
		},
		{
			name:     "cross_column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{
				CrossColumn: &storepb.CrossColumnConfig{
					LeftColumn:  "qty",
					RightColumn: "amount",
					Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_GTE,
				},
			}},
			wantColumns: []string{"total_rows", "failed_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN NOT (`qty` >= `amount`) " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
			postgresSQL: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN NOT ("qty" >= "amount") ` +
				`THEN 1 ELSE 0 END) AS failed_rows FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN NOT ([qty] >= [amount]) " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM [public].[orders]",
		},
		{
			name:     "freshness",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{
				Freshness: &storepb.FreshnessConfig{
					TimestampColumn: "created_at",
					MaxLagSeconds:   86400,
					WarnLagSeconds:  proto.Int32(3600),
				},
			}},
			wantColumns: []string{"total_rows", "max_ts", "lag_seconds"},
			wantKind:    QueryAggregate,
			mysqlSQL: "SELECT COUNT(*) AS total_rows, MAX(`created_at`) AS max_ts, " +
				"TIMESTAMPDIFF(SECOND, MAX(`created_at`), NOW()) AS lag_seconds FROM `shop`.`orders`",
			postgresSQL: `SELECT COUNT(*) AS total_rows, MAX("created_at") AS max_ts, ` +
				`EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - MAX("created_at"))) AS lag_seconds FROM "public"."orders"`,
			mssqlSQL: "SELECT COUNT(*) AS total_rows, MAX([created_at]) AS max_ts, " +
				"DATEDIFF(SECOND, MAX([created_at]), SYSUTCDATETIME()) AS lag_seconds FROM [public].[orders]",
		},
		{
			name:     "custom_sql",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{
				CustomSql: &storepb.CustomSQLConfig{Sql: "SELECT * FROM orders WHERE amount < 0"},
			}},
			wantColumns: []string{"failed_rows"},
			wantKind:    QueryAggregate,
			mysqlSQL:    "SELECT COUNT(*) AS failed_rows FROM (SELECT * FROM orders WHERE amount < 0) dq",
			postgresSQL: "SELECT COUNT(*) AS failed_rows FROM (SELECT * FROM orders WHERE amount < 0) dq",
			mssqlSQL:    "SELECT COUNT(*) AS failed_rows FROM (SELECT * FROM orders WHERE amount < 0) dq",
		},
	}
}

// compileTestDialects is the engine matrix every golden case runs through. MSSQL
// regex is excluded because the support table refuses that combination.
func compileTestDialects(golden compileGolden) []struct {
	name  string
	value storepb.Engine
	want  string
} {
	return []struct {
		name  string
		value storepb.Engine
		want  string
	}{
		{name: "mysql", value: storepb.Engine_MYSQL, want: golden.mysqlSQL},
		{name: "starrocks", value: storepb.Engine_STARROCKS, want: golden.mysqlSQL},
		{name: "postgres", value: storepb.Engine_POSTGRES, want: golden.postgresSQL},
		{name: "mssql", value: storepb.Engine_MSSQL, want: golden.mssqlSQL},
	}
}

// TestCompileRuleGoldens pins the complete SQL, arguments, result columns and
// query kind of all 13 rule types on all four dialect families.
func TestCompileRuleGoldens(t *testing.T) {
	t.Parallel()

	for _, golden := range compileRuleGoldens() {
		for _, dialectCase := range compileTestDialects(golden) {
			t.Run(golden.name+"/"+dialectCase.name, func(t *testing.T) {
				t.Parallel()
				dialect, err := DialectFor(dialectCase.value)
				require.NoError(t, err)
				query, err := Compile(CompileInput{
					Dialect: dialect,
					Target:  compileTestTarget(dialectCase.value),
					Type:    golden.ruleType,
					Config:  golden.config,
					Filter:  golden.filter,
				})
				require.NoError(t, err)
				require.Equal(t, dialectCase.want, query.SQL)
				require.Equal(t, golden.wantArgs, query.Args)
				require.Equal(t, golden.wantColumns, query.Columns)
				require.Equal(t, golden.wantKind, query.Kind)
			})
		}
	}
}

// TestCompileRegexGoldens keeps MSSQL out of the matrix: it is the one engine
// without a regular-expression operator, and the support table rejects the rule
// before it can be compiled.
func TestCompileRegexGoldens(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{
		Regex: &storepb.RegexConfig{Column: "email", Pattern: "^[^@]+@[^@]+$"},
	}}
	want := map[storepb.Engine]string{
		storepb.Engine_MYSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `email` IS NULL OR `email` NOT REGEXP ? " +
			"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
		storepb.Engine_STARROCKS: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `email` IS NULL OR `email` NOT REGEXP ? " +
			"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
		storepb.Engine_POSTGRES: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN "email" IS NULL OR "email" !~ $1 ` +
			`THEN 1 ELSE 0 END) AS failed_rows FROM "public"."orders"`,
	}
	for engine, wantSQL := range want {
		t.Run(engine.String(), func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(engine)
			require.NoError(t, err)
			query, err := Compile(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
				Config:  config,
			})
			require.NoError(t, err)
			require.Equal(t, wantSQL, query.SQL)
			require.Equal(t, []any{"^[^@]+@[^@]+$"}, query.Args)
			require.Equal(t, []string{"total_rows", "failed_rows"}, query.Columns)
		})
	}
}

// TestCompileFilterVariants pins how the rule filter reaches the SQL: every
// clause is ANDed, values are bound, and IS NULL adds no argument.
func TestCompileFilterVariants(t *testing.T) {
	t.Parallel()

	rowCount := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
		RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(1)},
	}}
	tests := []struct {
		name     string
		filter   Filter
		wantArgs []any
		mysql    string
		postgres string
		mssql    string
	}{
		{
			name: "eq",
			filter: Filter{Clauses: []FilterClause{
				{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
			}},
			wantArgs: []any{"paid"},
			mysql:    "SELECT COUNT(*) AS total_rows FROM `shop`.`orders` WHERE `status` = ?",
			postgres: `SELECT COUNT(*) AS total_rows FROM "public"."orders" WHERE "status" = $1`,
			mssql:    "SELECT COUNT(*) AS total_rows FROM [public].[orders] WHERE [status] = @p1",
		},
		{
			name: "in",
			filter: Filter{Clauses: []FilterClause{
				{Column: "status", Op: FilterIN, Values: []string{"paid", "open"}},
			}},
			wantArgs: []any{"paid", "open"},
			mysql:    "SELECT COUNT(*) AS total_rows FROM `shop`.`orders` WHERE `status` IN (?, ?)",
			postgres: `SELECT COUNT(*) AS total_rows FROM "public"."orders" WHERE "status" IN ($1, $2)`,
			mssql:    "SELECT COUNT(*) AS total_rows FROM [public].[orders] WHERE [status] IN (@p1, @p2)",
		},
		{
			name: "is null",
			filter: Filter{Clauses: []FilterClause{
				{Column: "email", Op: FilterISNull},
			}},
			// CompileFilter allocates its argument slice, so a clause without values
			// yields an empty (non-nil) list.
			wantArgs: []any{},
			mysql:    "SELECT COUNT(*) AS total_rows FROM `shop`.`orders` WHERE `email` IS NULL",
			postgres: `SELECT COUNT(*) AS total_rows FROM "public"."orders" WHERE "email" IS NULL`,
			mssql:    "SELECT COUNT(*) AS total_rows FROM [public].[orders] WHERE [email] IS NULL",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, dialectCase := range []struct {
				name   string
				engine storepb.Engine
				want   string
			}{
				{name: "mysql", engine: storepb.Engine_MYSQL, want: test.mysql},
				{name: "postgres", engine: storepb.Engine_POSTGRES, want: test.postgres},
				{name: "mssql", engine: storepb.Engine_MSSQL, want: test.mssql},
			} {
				dialect, err := DialectFor(dialectCase.engine)
				require.NoError(t, err)
				query, err := Compile(CompileInput{
					Dialect: dialect,
					Target:  compileTestTarget(dialectCase.engine),
					Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
					Config:  rowCount,
					Filter:  test.filter,
				})
				require.NoError(t, err)
				require.Equal(t, dialectCase.want, query.SQL, dialectCase.name)
				require.Equal(t, test.wantArgs, query.Args, dialectCase.name)
			}
		})
	}
}

// TestCompilePlaceholderNumbering pins the argument order when a rule both binds
// filter values and config values. Numbered dialects ($n / @pN) keep the filter's
// arguments first because the filter numbers its own placeholders from 1; a
// positional dialect (?) binds in text order, so the config values that appear in
// the SELECT list come first.
func TestCompilePlaceholderNumbering(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
		AcceptedValues: &storepb.AcceptedValuesConfig{Column: "status", Values: []string{"paid", "open"}},
	}}
	filter := Filter{Clauses: []FilterClause{
		{Column: "email", Op: FilterEQ, Values: []string{"a@b.c"}},
	}}
	tests := []struct {
		engine   storepb.Engine
		wantSQL  string
		wantArgs []any
	}{
		{
			engine:  storepb.Engine_MYSQL,
			wantSQL: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `status` IS NULL OR `status` NOT IN (?, ?) THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders` WHERE `email` = ?",
			// The ?s are bound in text order: the NOT IN list comes before the WHERE.
			wantArgs: []any{"paid", "open", "a@b.c"},
		},
		{
			engine:   storepb.Engine_POSTGRES,
			wantSQL:  `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN "status" IS NULL OR "status" NOT IN ($2, $3) THEN 1 ELSE 0 END) AS failed_rows FROM "public"."orders" WHERE "email" = $1`,
			wantArgs: []any{"a@b.c", "paid", "open"},
		},
		{
			engine:   storepb.Engine_MSSQL,
			wantSQL:  "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN [status] IS NULL OR [status] NOT IN (@p2, @p3) THEN 1 ELSE 0 END) AS failed_rows FROM [public].[orders] WHERE [email] = @p1",
			wantArgs: []any{"a@b.c", "paid", "open"},
		},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			query, err := Compile(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(test.engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
				Config:  config,
				Filter:  filter,
			})
			require.NoError(t, err)
			require.Equal(t, test.wantSQL, query.SQL)
			require.Equal(t, test.wantArgs, query.Args)
		})
	}
}

// TestCompileIdentifierEscaping pins that a quote character in a table, schema,
// database or column name is doubled for the dialect instead of ending the
// identifier early.
func TestCompileIdentifierEscaping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		engine storepb.Engine
		target ResolvedTarget
		want   string
	}{
		{
			name:   "mysql",
			engine: storepb.Engine_MYSQL,
			target: ResolvedTarget{
				Target:   Target{GUID: compileTestGUID},
				Engine:   storepb.Engine_MYSQL,
				Database: "da`ta",
				Table:    "or`der",
				Columns:  []ColumnInfo{{Name: "em`ail", Type: "varchar(1)", Kind: ColumnKindString}},
			},
			want: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `em``ail` IS NULL THEN 1 ELSE 0 END) " +
				"AS failed_rows FROM `da``ta`.`or``der`",
		},
		{
			name:   "postgres",
			engine: storepb.Engine_POSTGRES,
			target: ResolvedTarget{
				Target:  Target{GUID: compileTestGUID},
				Engine:  storepb.Engine_POSTGRES,
				Schema:  `pub"lic`,
				Table:   `or"der`,
				Columns: []ColumnInfo{{Name: `em"ail`, Type: "text", Kind: ColumnKindString}},
			},
			want: `SELECT COUNT(*) AS total_rows, SUM(CASE WHEN "em""ail" IS NULL THEN 1 ELSE 0 END) ` +
				`AS failed_rows FROM "pub""lic"."or""der"`,
		},
		{
			name:   "mssql",
			engine: storepb.Engine_MSSQL,
			target: ResolvedTarget{
				Target:  Target{GUID: compileTestGUID},
				Engine:  storepb.Engine_MSSQL,
				Schema:  "pub]lic",
				Table:   "or]der",
				Columns: []ColumnInfo{{Name: "em]ail", Type: "nvarchar(1)", Kind: ColumnKindString}},
			},
			want: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN [em]]ail] IS NULL THEN 1 ELSE 0 END) " +
				"AS failed_rows FROM [pub]]lic].[or]]der]",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			query, err := Compile(CompileInput{
				Dialect: dialect,
				Target:  test.target,
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
				Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
					NullPercent: &storepb.NullPercentConfig{Column: test.target.Columns[0].Name, MaxNullCount: proto.Int64(0)},
				}},
			})
			require.NoError(t, err)
			require.Equal(t, test.want, query.SQL)
		})
	}
}

// TestCompileMSSQLRegexRejected: T-SQL has no regular-expression operator, and
// the compiler must say so rather than render a wrong query.
func TestCompileMSSQLRegexRejected(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MSSQL)
	require.NoError(t, err)
	_, err = Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MSSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{
			Regex: &storepb.RegexConfig{Column: "email", Pattern: "^a$"},
		}},
	})
	require.Error(t, err)
	require.Equal(t, common.Invalid, common.ErrorCode(err))
	require.Contains(t, err.Error(), "regular-expression")
}

// TestCompileCrossColumnTolerance pins the tolerance arithmetic per operator and
// its single bound argument.
func TestCompileCrossColumnTolerance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *storepb.QualityRuleConfig
		mysql  string
	}{
		{
			name: "eq",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{
				CrossColumn: &storepb.CrossColumnConfig{
					LeftColumn:  "qty",
					RightColumn: "amount",
					Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
					Tolerance:   proto.Float64(0.5),
				},
			}},
			mysql: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN ABS(`qty` - `amount`) > ? " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
		},
		{
			name: "lte",
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{
				CrossColumn: &storepb.CrossColumnConfig{
					LeftColumn:  "qty",
					RightColumn: "amount",
					Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_LTE,
					Tolerance:   proto.Float64(1),
				},
			}},
			mysql: "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN `qty` > `amount` + ? " +
				"THEN 1 ELSE 0 END) AS failed_rows FROM `shop`.`orders`",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(storepb.Engine_MYSQL)
			require.NoError(t, err)
			query, err := Compile(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(storepb.Engine_MYSQL),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
				Config:  test.config,
			})
			require.NoError(t, err)
			require.Equal(t, test.mysql, query.SQL)
			require.Equal(t, []any{test.config.GetCrossColumn().GetTolerance()}, query.Args)
		})
	}
}

// TestCompileCustomSQLWrapping pins the exact wrapper: user SQL is trimmed and
// counted, never rewritten or filtered.
func TestCompileCustomSQLWrapping(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_POSTGRES),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{
			CustomSql: &storepb.CustomSQLConfig{Sql: "  SELECT * FROM orders WHERE amount < 0;  "},
		}},
		Filter: Filter{Clauses: []FilterClause{
			{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
		}},
	})
	require.NoError(t, err)
	// The user query owns its predicate, so the rule's filter is not applied.
	require.Equal(t, "SELECT COUNT(*) AS failed_rows FROM (SELECT * FROM orders WHERE amount < 0) dq", query.SQL)
	require.Empty(t, query.Args)
	require.Equal(t, []string{"failed_rows"}, query.Columns)
	// The wrapper already aggregates; the executor must not count rows again.
	require.Equal(t, QueryAggregate, query.Kind)
}

// TestCompileValidationRejectsBadConfigs pins the save-time rejections that need
// the resolved target or the config's own arithmetic.
func TestCompileValidationRejectsBadConfigs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ruleType    storepb.QualityRuleType
		config      *storepb.QualityRuleConfig
		wantMessage string
	}{
		{
			name:     "null percent missing column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{MaxNullPct: proto.Float64(1)},
			}},
			wantMessage: "a column is required",
		},
		{
			name:     "null percent unknown column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{Column: "nope", MaxNullPct: proto.Float64(1)},
			}},
			wantMessage: "does not exist",
		},
		{
			name:     "null percent both threshold forms",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullCount: proto.Int64(1), MaxNullPct: proto.Float64(1)},
			}},
			wantMessage: "not both",
		},
		{
			name:     "null percent no threshold",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{Column: "email"},
			}},
			wantMessage: "needs max_null_count or max_null_pct",
		},
		{
			name:     "null percent warn above fail",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_NullPercent{
				NullPercent: &storepb.NullPercentConfig{Column: "email", MaxNullCount: proto.Int64(1), WarnNullCount: proto.Int64(2)},
			}},
			wantMessage: "exceeds max_null_count",
		},
		{
			name:     "unique missing column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
				Unique: &storepb.UniqueConfig{},
			}},
			wantMessage: "a column is required",
		},
		{
			name:     "unique combination no columns",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{
				UniqueCombination: &storepb.UniqueCombinationConfig{},
			}},
			wantMessage: "at least one column",
		},
		{
			name:     "unique combination too many columns",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{
				UniqueCombination: &storepb.UniqueCombinationConfig{
					Columns: []string{"id", "email", "amount", "created_at", "status", "qty", "price", "updated_at", "name", "id", "email"},
				},
			}},
			wantMessage: "at most",
		},
		{
			name:     "accepted values empty",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
				AcceptedValues: &storepb.AcceptedValuesConfig{Column: "status"},
			}},
			wantMessage: "at least one value",
		},
		{
			name:     "accepted value not numeric",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
				AcceptedValues: &storepb.AcceptedValuesConfig{Column: "id", Values: []string{"abc"}},
			}},
			wantMessage: "not a number",
		},
		{
			name:     "accepted values warn above fail",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_AcceptedValues{
				AcceptedValues: &storepb.AcceptedValuesConfig{
					Column: "status", Values: []string{"paid"}, FailThreshold: proto.Float64(1), WarnThreshold: proto.Float64(2),
				},
			}},
			wantMessage: "exceeds failure threshold",
		},
		{
			name:     "range on a string column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{
				Range: &storepb.RangeConfig{Column: "email", Min: proto.String("a")},
			}},
			wantMessage: "numeric or temporal",
		},
		{
			name:     "range without bounds",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{
				Range: &storepb.RangeConfig{Column: "amount"},
			}},
			wantMessage: "needs min or max",
		},
		{
			name:     "range min above max",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Range{
				Range: &storepb.RangeConfig{Column: "amount", Min: proto.String("100"), Max: proto.String("0")},
			}},
			wantMessage: "exceeds max",
		},
		{
			name:     "regex on a numeric column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{
				Regex: &storepb.RegexConfig{Column: "id", Pattern: "^a$"},
			}},
			wantMessage: "needs a text type",
		},
		{
			name:     "regex without pattern",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REGEX,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Regex{
				Regex: &storepb.RegexConfig{Column: "email"},
			}},
			wantMessage: "needs a pattern",
		},
		{
			name:     "string length on a numeric column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StringLength{
				StringLength: &storepb.StringLengthConfig{Column: "id", MinLen: proto.Int64(1)},
			}},
			wantMessage: "needs a text type",
		},
		{
			name:     "string length min above max",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_StringLength{
				StringLength: &storepb.StringLengthConfig{Column: "email", MinLen: proto.Int64(9), MaxLen: proto.Int64(1)},
			}},
			wantMessage: "exceeds max_len",
		},
		{
			name:     "freshness on a string column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{
				Freshness: &storepb.FreshnessConfig{TimestampColumn: "email", MaxLagSeconds: 60},
			}},
			wantMessage: "needs a temporal column",
		},
		{
			name:     "freshness warn above fail",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Freshness{
				Freshness: &storepb.FreshnessConfig{TimestampColumn: "created_at", MaxLagSeconds: 60, WarnLagSeconds: proto.Int32(120)},
			}},
			wantMessage: "exceeds max_lag_seconds",
		},
		{
			name:     "cross column without operator",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{
				CrossColumn: &storepb.CrossColumnConfig{LeftColumn: "qty", RightColumn: "amount"},
			}},
			wantMessage: "comparison operator",
		},
		{
			name:     "cross column mixed kinds",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{
				CrossColumn: &storepb.CrossColumnConfig{
					LeftColumn:  "qty",
					RightColumn: "email",
					Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
				},
			}},
			wantMessage: "needs a numeric or temporal column",
		},
		{
			name:     "cross column tolerance on temporal",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CROSS_COLUMN,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CrossColumn{
				CrossColumn: &storepb.CrossColumnConfig{
					LeftColumn:  "created_at",
					RightColumn: "updated_at",
					Op:          storepb.QualityComparisonOperator_QUALITY_COMPARISON_OPERATOR_EQ,
					Tolerance:   proto.Float64(1),
				},
			}},
			wantMessage: "needs numeric columns",
		},
		{
			name:     "referential integrity without parent column",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{
				ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{ChildColumn: "status", ParentTable: "customer_status"},
			}},
			wantMessage: "needs a parent column",
		},
		{
			name:     "referential integrity parent in another schema",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{
				ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
					ChildColumn:  "status",
					ParentTable:  "other.customer_status",
					ParentColumn: "code",
				},
			}},
			wantMessage: "must be in the rule's schema",
		},
		{
			name:     "custom sql empty",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{
				CustomSql: &storepb.CustomSQLConfig{Sql: "   "},
			}},
			wantMessage: "needs SQL",
		},
		{
			name:     "custom sql multiple statements",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{
				CustomSql: &storepb.CustomSQLConfig{Sql: "SELECT 1; SELECT 2"},
			}},
			wantMessage: "exactly one statement",
		},
		{
			name:     "custom sql not a select",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_CustomSql{
				CustomSql: &storepb.CustomSQLConfig{Sql: "DELETE FROM orders"},
			}},
			wantMessage: "single SELECT or WITH",
		},
		{
			name:     "row count without bounds",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
				RowCount: &storepb.RowCountConfig{},
			}},
			wantMessage: "needs at least one",
		},
		{
			name:     "row count exact with min",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
				RowCount: &storepb.RowCountConfig{ExactRows: proto.Int64(1), MinRows: proto.Int64(1)},
			}},
			wantMessage: "cannot be combined",
		},
		{
			name:     "row count min above max",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCount{
				RowCount: &storepb.RowCountConfig{MinRows: proto.Int64(9), MaxRows: proto.Int64(1)},
			}},
			wantMessage: "exceeds max_rows",
		},
		{
			name:     "row count change without bounds",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCountChange{
				RowCountChange: &storepb.RowCountChangeConfig{},
			}},
			wantMessage: "needs at least one",
		},
		{
			name:     "row count change above 1000 percent",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_RowCountChange{
				RowCountChange: &storepb.RowCountChangeConfig{MaxIncreasePct: proto.Float64(1001)},
			}},
			wantMessage: "outside the accepted",
		},
		{
			name:     "config arm does not match the rule type",
			ruleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
			config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
				Unique: &storepb.UniqueConfig{Column: "email"},
			}},
			wantMessage: "not for the rule's",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(storepb.Engine_POSTGRES)
			require.NoError(t, err)
			_, err = Compile(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(storepb.Engine_POSTGRES),
				Type:    test.ruleType,
				Config:  test.config,
			})
			require.Error(t, err)
			require.Equal(t, common.Invalid, common.ErrorCode(err), "error code")
			require.Contains(t, err.Error(), test.wantMessage)
		})
	}
}

// TestCompileAllColumnUniqueUsesEveryColumn pins that all_columns expands to the
// target's full column list in registry order.
func TestCompileAllColumnUniqueUsesEveryColumn(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_POSTGRES)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_POSTGRES),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE_COMBINATION,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_UniqueCombination{
			UniqueCombination: &storepb.UniqueCombinationConfig{AllColumns: true},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, `SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, (SELECT COUNT(*) FROM "public"."orders") AS total_rows FROM (SELECT "id", "email", "amount", "created_at", "status", "qty", "price", "updated_at", "name", COUNT(*) AS cnt FROM "public"."orders" WHERE "id" IS NOT NULL AND "email" IS NOT NULL AND "amount" IS NOT NULL AND "created_at" IS NOT NULL AND "status" IS NOT NULL AND "qty" IS NOT NULL AND "price" IS NOT NULL AND "updated_at" IS NOT NULL AND "name" IS NOT NULL GROUP BY "id", "email", "amount", "created_at", "status", "qty", "price", "updated_at", "name" HAVING COUNT(*) > 1) dq`, query.SQL)
}

// TestCompileCountNullsKeepsNullKeys pins the count_nulls opt-in: only then does
// the grouped input keep NULL keys.
func TestCompileCountNullsKeepsNullKeys(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
			Unique: &storepb.UniqueConfig{Column: "email", CountNulls: true},
		}},
	})
	require.NoError(t, err)
	require.NotContains(t, query.SQL, "IS NOT NULL")
	require.Equal(t, "SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, "+
		"(SELECT COUNT(*) FROM `shop`.`orders`) AS total_rows FROM "+
		"(SELECT `email`, COUNT(*) AS cnt FROM `shop`.`orders` GROUP BY `email` HAVING COUNT(*) > 1) dq", query.SQL)
}

// TestCompileUniqueFilterPlaceholderOrder pins how a filtered unique rule binds
// its arguments: the filter predicate appears once in the total_rows scalar
// subquery and once inside the grouped input, so a positional dialect (?) binds
// the values once per occurrence while numbered dialects reuse $n / @pN.
func TestCompileUniqueFilterPlaceholderOrder(t *testing.T) {
	t.Parallel()

	config := &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_Unique{
		Unique: &storepb.UniqueConfig{Column: "email"},
	}}
	filter := Filter{Clauses: []FilterClause{
		{Column: "status", Op: FilterEQ, Values: []string{"paid"}},
		{Column: "qty", Op: FilterGTE, Values: []string{"3"}},
	}}
	tests := []struct {
		engine   storepb.Engine
		wantSQL  string
		wantArgs []any
	}{
		{
			engine: storepb.Engine_MYSQL,
			wantSQL: "SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, " +
				"(SELECT COUNT(*) FROM `shop`.`orders` WHERE `status` = ? AND `qty` >= ?) AS total_rows FROM " +
				"(SELECT `email`, COUNT(*) AS cnt FROM `shop`.`orders` " +
				"WHERE `status` = ? AND `qty` >= ? AND `email` IS NOT NULL " +
				"GROUP BY `email` HAVING COUNT(*) > 1) dq",
			// The filter's two ?s appear twice in the text, so their values bind twice.
			wantArgs: []any{"paid", int64(3), "paid", int64(3)},
		},
		{
			engine: storepb.Engine_POSTGRES,
			wantSQL: `SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, ` +
				`(SELECT COUNT(*) FROM "public"."orders" WHERE "status" = $1 AND "qty" >= $2) AS total_rows FROM ` +
				`(SELECT "email", COUNT(*) AS cnt FROM "public"."orders" ` +
				`WHERE "status" = $1 AND "qty" >= $2 AND "email" IS NOT NULL ` +
				`GROUP BY "email" HAVING COUNT(*) > 1) dq`,
			wantArgs: []any{"paid", int64(3)},
		},
		{
			engine: storepb.Engine_MSSQL,
			wantSQL: "SELECT COALESCE(SUM(cnt - 1), 0) AS failed_rows, COUNT(*) AS duplicate_groups, " +
				"(SELECT COUNT(*) FROM [public].[orders] WHERE [status] = @p1 AND [qty] >= @p2) AS total_rows FROM " +
				"(SELECT [email], COUNT(*) AS cnt FROM [public].[orders] " +
				"WHERE [status] = @p1 AND [qty] >= @p2 AND [email] IS NOT NULL " +
				"GROUP BY [email] HAVING COUNT(*) > 1) dq",
			wantArgs: []any{"paid", int64(3)},
		},
	}
	for _, test := range tests {
		t.Run(test.engine.String(), func(t *testing.T) {
			t.Parallel()
			dialect, err := DialectFor(test.engine)
			require.NoError(t, err)
			query, err := Compile(CompileInput{
				Dialect: dialect,
				Target:  compileTestTarget(test.engine),
				Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
				Config:  config,
				Filter:  filter,
			})
			require.NoError(t, err)
			require.Equal(t, test.wantSQL, query.SQL)
			require.Equal(t, test.wantArgs, query.Args)
			require.Equal(t, []string{"failed_rows", "duplicate_groups", "total_rows"}, query.Columns)
		})
	}
}

// TestCompileReferentialIntegrityAllowNull pins that allow_null drops NULL child
// values from the orphan count while keeping them in total_rows.
func TestCompileReferentialIntegrityAllowNull(t *testing.T) {
	t.Parallel()

	dialect, err := DialectFor(storepb.Engine_MYSQL)
	require.NoError(t, err)
	query, err := Compile(CompileInput{
		Dialect: dialect,
		Target:  compileTestTarget(storepb.Engine_MYSQL),
		Type:    storepb.QualityRuleType_QUALITY_RULE_TYPE_REFERENTIAL_INTEGRITY,
		Config: &storepb.QualityRuleConfig{Config: &storepb.QualityRuleConfig_ReferentialIntegrity{
			ReferentialIntegrity: &storepb.ReferentialIntegrityConfig{
				ChildColumn:  "status",
				ParentTable:  "shop.customer_status",
				ParentColumn: "code",
				AllowNull:    true,
			},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "SELECT COUNT(*) AS total_rows, SUM(CASE WHEN c.`status` IS NOT NULL AND p.`code` IS NULL "+
		"THEN 1 ELSE 0 END) AS failed_rows FROM (SELECT * FROM `shop`.`orders`) c "+
		"LEFT JOIN `shop`.`customer_status` p ON c.`status` = p.`code`", query.SQL)
}

// TestRuleTypeCatalogIsComplete is the guard against a new QualityRuleType reaching
// the API without the per-type tables this package maintains by hand. Every enum
// value except UNSPECIFIED must have a compiler and be supported on a dialect this
// build accepts, and only schema_drift may be a metadata rule.
func TestRuleTypeCatalogIsComplete(t *testing.T) {
	t.Parallel()

	seen := 0
	for value, name := range storepb.QualityRuleType_name {
		ruleType := storepb.QualityRuleType(value)
		if ruleType == storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED {
			require.Equal(t, "QUALITY_RULE_TYPE_UNSPECIFIED", name)
			continue
		}
		seen++
		require.Contains(t, compilers, ruleType, "rule type %s has no compiler", name)
		ok, reason := Supported(storepb.Engine_POSTGRES, ruleType)
		require.True(t, ok, "%s: %s", name, reason)
		require.Empty(t, reason, "a supported rule type must not carry a refusal reason")
	}

	// The enum has 18 real values, and the compiler map covers exactly those: a
	// stale compiler for a removed value would mean a rule this build cannot
	// describe.
	require.Equal(t, 18, seen)
	require.Len(t, compilers, seen)
	_, hasUnspecifiedCompiler := compilers[storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED]
	require.False(t, hasUnspecifiedCompiler)

	// The object-type matrix: a table accepts every rule type, while a view only
	// accepts the table-level checks. Asserting the exact sets, not a spot check,
	// is what keeps RuleTypeForTarget from silently widening.
	viewAllowed := map[storepb.QualityRuleType]bool{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT:        true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE: true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS:        true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL:       true,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT:     true,
	}
	for ruleType := range compilers {
		require.True(t, RuleTypeForTarget(storepb.MetaType_TABLE, ruleType), "%s must watch a table", ruleType)
		require.True(t, RuleTypeForTarget(storepb.MetaType_EXTERNAL_TABLE, ruleType), "%s must watch an external table", ruleType)
		for _, viewType := range []storepb.MetaType{storepb.MetaType_VIEW, storepb.MetaType_MATERIALIZED_VIEW} {
			require.Equal(t, viewAllowed[ruleType], RuleTypeForTarget(viewType, ruleType),
				"%s on %s", ruleType, viewType)
		}
	}

	metadataRules := 0
	for ruleType := range compilers {
		if IsMetadataRule(ruleType) {
			metadataRules++
		}
	}
	require.Equal(t, 1, metadataRules, "only schema_drift reads the registry instead of the target")
	require.True(t, IsMetadataRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT))
}
