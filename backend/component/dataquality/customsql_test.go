package dataquality

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// requireCustomSQLInvalid asserts the statement is refused as a bad request, not
// as a server fault: every parse or shape failure is InvalidArgument.
func requireCustomSQLInvalid(t *testing.T, engine storepb.Engine, sql, reason string) {
	t.Helper()
	err := ValidateCustomSQL(engine, sql)
	require.Error(t, err, "engine %s should reject %q (%s)", engine, sql, reason)
	require.Equal(t, common.Invalid, common.ErrorCode(err), "engine %s on %q: %v", engine, sql, err)
}

func requireCustomSQLValid(t *testing.T, engine storepb.Engine, sql string) {
	t.Helper()
	require.NoError(t, ValidateCustomSQL(engine, sql), "engine %s should accept %q", engine, sql)
}

// The read-only query shapes every engine must accept: a plain SELECT, a WITH
// query, a UNION (the dialects model it differently but all accept it), trailing
// semicolons and whitespace.
func TestValidateCustomSQLAcceptsReadOnlyQueries(t *testing.T) {
	t.Parallel()

	engines := []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_TIDB,
		storepb.Engine_MARIADB,
		storepb.Engine_OCEANBASE,
		storepb.Engine_STARROCKS,
		storepb.Engine_DORIS,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
	}
	queries := []string{
		"SELECT 1",
		"SELECT id, name FROM orders WHERE id > 100",
		"WITH t AS (SELECT 1 AS x) SELECT x FROM t",
		"SELECT 1 UNION SELECT 2",
		"SELECT 1;",
		"  SELECT 1  ",
	}
	for _, engine := range engines {
		for index, query := range queries {
			engine, query := engine, query
			t.Run(fmt.Sprintf("%s/query%d", engine, index), func(t *testing.T) {
				t.Parallel()
				requireCustomSQLValid(t, engine, query)
			})
		}
	}

	// StarRocks/Doris model a parenthesized query as its own node; the other
	// query shapes above already cover the folded representation.
	for _, engine := range []storepb.Engine{storepb.Engine_STARROCKS, storepb.Engine_DORIS, storepb.Engine_MYSQL, storepb.Engine_POSTGRES} {
		requireCustomSQLValid(t, engine, "(SELECT 1)")
	}

	// A GO batch separator is not a statement; one SELECT plus GO is still one
	// statement on T-SQL.
	requireCustomSQLValid(t, storepb.Engine_MSSQL, "SELECT 1\nGO")
}

// Every engine refuses the statement shapes the design lists: empty input, a
// script, DDL, DML and an oversized statement.
func TestValidateCustomSQLRejectsNonQueriesOnEveryEngine(t *testing.T) {
	t.Parallel()

	engines := []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_TIDB,
		storepb.Engine_MARIADB,
		storepb.Engine_OCEANBASE,
		storepb.Engine_STARROCKS,
		storepb.Engine_DORIS,
		storepb.Engine_POSTGRES,
		storepb.Engine_MSSQL,
	}
	rejected := []struct {
		sql    string
		reason string
	}{
		{sql: "", reason: "empty"},
		{sql: "   ", reason: "blank"},
		{sql: "-- only a comment", reason: "no statement"},
		{sql: "SELECT 1; SELECT 2", reason: "multi-statement"},
		{sql: "INSERT INTO orders VALUES (1)", reason: "DML"},
		{sql: "UPDATE orders SET id = 1", reason: "DML"},
		{sql: "DELETE FROM orders", reason: "DML"},
		{sql: "CREATE TABLE t (id int)", reason: "DDL"},
		{sql: "DROP TABLE orders", reason: "DDL"},
		{sql: "SELECT 'unterminated", reason: "syntax error"},
	}
	for _, engine := range engines {
		for index, rejectedCase := range rejected {
			engine, rejectedCase := engine, rejectedCase
			t.Run(fmt.Sprintf("%s/rejected%d", engine, index), func(t *testing.T) {
				t.Parallel()
				requireCustomSQLInvalid(t, engine, rejectedCase.sql, rejectedCase.reason)
			})
		}
	}

	// The size limit is checked before any parser runs, so one engine is enough.
	oversized := "SELECT '" + strings.Repeat("x", maxCustomSQLBytes) + "'"
	requireCustomSQLInvalid(t, storepb.Engine_MYSQL, oversized, "above the 8 KiB limit")
}

// Dialect-specific shapes: SELECT INTO, row locking and EXPLAIN are refused where
// the parser can express them, and INTO OUTFILE is refused on StarRocks.
func TestValidateCustomSQLRejectsDialectSpecificWrites(t *testing.T) {
	t.Parallel()

	cases := []struct {
		engine storepb.Engine
		sql    string
		reason string
	}{
		{storepb.Engine_MYSQL, "SELECT * INTO newtab FROM orders", "SELECT INTO"},
		{storepb.Engine_MYSQL, "SELECT id INTO @x FROM orders", "SELECT INTO variable"},
		{storepb.Engine_MYSQL, "SELECT * FROM orders FOR UPDATE", "locking SELECT"},
		{storepb.Engine_TIDB, "SELECT * FROM orders FOR UPDATE", "locking SELECT"},
		{storepb.Engine_MYSQL, "EXPLAIN SELECT 1", "EXPLAIN"},
		{storepb.Engine_MYSQL, "TABLE orders", "TABLE statement"},
		{storepb.Engine_MYSQL, "CALL refresh_orders()", "procedure call"},
		{storepb.Engine_POSTGRES, "SELECT * INTO newtab FROM orders", "SELECT INTO"},
		{storepb.Engine_POSTGRES, "SELECT * FROM orders FOR UPDATE", "locking SELECT"},
		{storepb.Engine_POSTGRES, "EXPLAIN SELECT 1", "EXPLAIN"},
		{storepb.Engine_MSSQL, "SELECT * INTO newtab FROM orders", "SELECT INTO"},
		{storepb.Engine_MSSQL, "EXPLAIN SELECT 1", "EXPLAIN"},
		{storepb.Engine_STARROCKS, "EXPLAIN SELECT 1", "EXPLAIN"},
		{storepb.Engine_STARROCKS, "SELECT * FROM orders INTO OUTFILE '/tmp/out'", "INTO OUTFILE"},
		{storepb.Engine_DORIS, "EXPLAIN SELECT 1", "EXPLAIN"},
	}
	for index, testCase := range cases {
		testCase := testCase
		t.Run(fmt.Sprintf("dialect%d", index), func(t *testing.T) {
			t.Parallel()
			requireCustomSQLInvalid(t, testCase.engine, testCase.sql, testCase.reason)
		})
	}
}

// An engine this build has no parser for is refused explicitly rather than parsed
// with the wrong dialect.
func TestValidateCustomSQLRejectsUnsupportedEngines(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_ENGINE_UNSPECIFIED, storepb.Engine(99)} {
		requireCustomSQLInvalid(t, engine, "SELECT 1", "no parser for the engine")
	}
}

// A custom SQL that the compiler will then wrap must not be rewritten here: the
// same text is accepted with a trailing semicolon and with its own filter in the
// SQL (which is how custom SQL expresses filtering; the rule-level filter does not
// apply to it).
func TestValidateCustomSQLKeepsTheStatementAsWritten(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{storepb.Engine_MYSQL, storepb.Engine_POSTGRES, storepb.Engine_MSSQL} {
		requireCustomSQLValid(t, engine, "SELECT id FROM orders WHERE created_at < CURRENT_TIMESTAMP;")
	}
	requireCustomSQLValid(t, storepb.Engine_MYSQL, "SELECT id FROM orders WHERE name = ';'")
}
