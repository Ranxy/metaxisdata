package dataquality

import (
	"strings"

	dorisast "github.com/bytebase/omni/doris/ast"
	dorisparser "github.com/bytebase/omni/doris/parser"
	omnimssql "github.com/bytebase/omni/mssql"
	mssqlast "github.com/bytebase/omni/mssql/ast"
	mysqlast "github.com/bytebase/omni/mysql/ast"
	mysqlparser "github.com/bytebase/omni/mysql/parser"
	omnipg "github.com/bytebase/omni/pg"
	pgast "github.com/bytebase/omni/pg/ast"
	starrocksast "github.com/bytebase/omni/starrocks/ast"
	starrocksparser "github.com/bytebase/omni/starrocks/parser"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// maxCustomSQLBytes bounds a custom SQL definition. It mirrors the compiler's
// limit (8 KiB, the design's storage limit) so a statement accepted here is not
// then refused for its length.
const maxCustomSQLBytes = compileMaxCustomSQLLength

// ValidateCustomSQL is the save/dry-run check for a custom-SQL rule (design §6.7):
// the text is non-empty, within the size limit, parses with the target engine's
// own omni parser, is exactly one statement, and that statement is a read-only
// query (SELECT, WITH ... SELECT, or the dialect's equivalent query node).
//
// Rejected by construction rather than by text matching: multi-statement scripts,
// EXPLAIN, DDL, DML, procedure calls, SELECT ... INTO and row-locking SELECTs. The
// statement is never rewritten here; the executor wraps it in COUNT(*) and the
// read-only transaction decides the rest.
func ValidateCustomSQL(engine storepb.Engine, sql string) error {
	statement := strings.TrimSpace(sql)
	if statement == "" {
		return invalidConfig("custom SQL is required")
	}
	if len(statement) > maxCustomSQLBytes {
		return invalidConfig("custom SQL must be at most %d bytes, got %d", maxCustomSQLBytes, len(statement))
	}

	switch engine {
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB, storepb.Engine_OCEANBASE:
		return validateMySQLCustomSQL(statement)
	case storepb.Engine_POSTGRES:
		return validatePostgresCustomSQL(statement)
	case storepb.Engine_MSSQL:
		return validateMSSQLCustomSQL(statement)
	case storepb.Engine_STARROCKS:
		return validateStarRocksCustomSQL(statement)
	case storepb.Engine_DORIS:
		return validateDorisCustomSQL(statement)
	case storepb.Engine_ENGINE_UNSPECIFIED:
		return invalidConfig("the target instance has no engine, so its custom SQL cannot be parsed")
	default:
		return invalidConfig("engine %s has no custom SQL parser", engine)
	}
}

// validateMySQLCustomSQL parses with omni's MySQL parser, which also covers TiDB,
// MariaDB, OceanBase and (by their own registrations) StarRocks/Doris.
func validateMySQLCustomSQL(statement string) (err error) {
	// omni's lexers panic on a few malformed inputs (an unterminated executable
	// comment is the known one, see .agents/docs/omni-upstream-defects.md); a save
	// must fail cleanly rather than take the request goroutine down.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = invalidConfig("the MySQL SQL parser failed on this statement: %v", recovered)
		}
	}()

	list, parseErr := mysqlparser.Parse(statement)
	if parseErr != nil {
		return invalidConfig("custom SQL does not parse as MySQL SQL: %v", parseErr)
	}
	count := 0
	if list != nil {
		count = list.Len()
	}
	if count != 1 {
		return invalidConfig("custom SQL must be exactly one statement, got %d", count)
	}
	selectStmt, ok := list.Items[0].(*mysqlast.SelectStmt)
	if !ok {
		return invalidConfig("custom SQL must be a single SELECT or WITH ... SELECT, got %s", mysqlStatementName(list.Items[0]))
	}
	if selectStmt.Into != nil {
		return invalidConfig("custom SQL must not write its result: SELECT ... INTO is not allowed")
	}
	if selectStmt.ForUpdate != nil {
		return invalidConfig("custom SQL must be read-only: SELECT ... FOR UPDATE/SHARE is not allowed")
	}
	return nil
}

// validatePostgresCustomSQL parses with omni's PostgreSQL parser. PostgreSQL
// represents a WITH query and a set operation as a SelectStmt, so one node covers
// every accepted shape; TABLE/VALUES also arrive as SelectStmt and are accepted as
// read-only queries.
func validatePostgresCustomSQL(statement string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = invalidConfig("the PostgreSQL SQL parser failed on this statement: %v", recovered)
		}
	}()

	statements, parseErr := omnipg.Parse(statement)
	if parseErr != nil {
		return invalidConfig("custom SQL does not parse as PostgreSQL SQL: %v", parseErr)
	}
	count := 0
	var only pgast.Node
	for _, statement := range statements {
		if statement.Empty() {
			continue
		}
		only = statement.AST
		count++
	}
	if count != 1 {
		return invalidConfig("custom SQL must be exactly one statement, got %d", count)
	}
	switch stmt := only.(type) {
	case *pgast.SelectStmt:
		if stmt.IntoClause != nil {
			return invalidConfig("custom SQL must not write its result: SELECT ... INTO is not allowed")
		}
		if stmt.LockingClause != nil {
			return invalidConfig("custom SQL must be read-only: locking SELECTs are not allowed")
		}
		return nil
	case *pgast.CreateTableAsStmt:
		if stmt.IsSelectInto {
			return invalidConfig("custom SQL must not write its result: SELECT ... INTO is not allowed")
		}
		return invalidConfig("custom SQL must be a single SELECT or WITH ... SELECT, got CREATE TABLE AS")
	default:
		return invalidConfig("custom SQL must be a single SELECT or WITH ... SELECT")
	}
}

// validateMSSQLCustomSQL parses with omni's T-SQL parser. GO batch separators are
// not statements; a script that is one SELECT plus GO is still one statement.
func validateMSSQLCustomSQL(statement string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = invalidConfig("the MSSQL SQL parser failed on this statement: %v", recovered)
		}
	}()

	statements, parseErr := omnimssql.Parse(statement)
	if parseErr != nil {
		return invalidConfig("custom SQL does not parse as MSSQL SQL: %v", parseErr)
	}
	count := 0
	var only mssqlast.Node
	for _, parsed := range statements {
		if parsed.Empty() {
			continue
		}
		if _, isBatchSeparator := parsed.AST.(*mssqlast.GoStmt); isBatchSeparator {
			continue
		}
		only = parsed.AST
		count++
	}
	if count != 1 {
		return invalidConfig("custom SQL must be exactly one statement, got %d", count)
	}
	selectStmt, ok := only.(*mssqlast.SelectStmt)
	if !ok {
		return invalidConfig("custom SQL must be a single SELECT or WITH ... SELECT, got %s", mssqlStatementName(only))
	}
	if selectStmt.IntoTable != nil {
		return invalidConfig("custom SQL must not write its result: SELECT ... INTO is not allowed")
	}
	return nil
}

// validateStarRocksCustomSQL parses with omni's StarRocks parser. This AST models
// a set operation and a parenthesized query as their own nodes instead of folding
// them into SelectStmt, so every query shape is walked to its SELECT leaves.
func validateStarRocksCustomSQL(statement string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = invalidConfig("the StarRocks SQL parser failed on this statement: %v", recovered)
		}
	}()

	file, parseErr := starrocksparser.Parse(statement)
	if parseErr != nil {
		return invalidConfig("custom SQL does not parse as StarRocks SQL: %v", parseErr)
	}
	if file == nil || len(file.Stmts) != 1 {
		count := 0
		if file != nil {
			count = len(file.Stmts)
		}
		return invalidConfig("custom SQL must be exactly one statement, got %d", count)
	}
	return validateStarRocksQuery(file.Stmts[0])
}

// validateStarRocksQuery walks the StarRocks query node to its SELECT leaves,
// rejecting an INTO OUTFILE result sink anywhere in the tree.
func validateStarRocksQuery(node starrocksast.Node) error {
	switch stmt := node.(type) {
	case *starrocksast.SelectStmt:
		if stmt.Into != nil {
			return invalidConfig("custom SQL must not write its result: INTO OUTFILE is not allowed")
		}
		return nil
	case *starrocksast.SetOpStmt:
		if err := validateStarRocksQuery(stmt.Left); err != nil {
			return err
		}
		return validateStarRocksQuery(stmt.Right)
	case *starrocksast.ParenSelect:
		return validateStarRocksQuery(stmt.Sel)
	default:
		return invalidConfig("custom SQL must be a single SELECT or WITH ... SELECT")
	}
}

// validateDorisCustomSQL parses with omni's Doris parser. Doris folds a
// parenthesized query into SelectStmt and models only set operations separately.
func validateDorisCustomSQL(statement string) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = invalidConfig("the Doris SQL parser failed on this statement: %v", recovered)
		}
	}()

	file, parseErr := dorisparser.Parse(statement)
	if parseErr != nil {
		return invalidConfig("custom SQL does not parse as Doris SQL: %v", parseErr)
	}
	if file == nil || len(file.Stmts) != 1 {
		count := 0
		if file != nil {
			count = len(file.Stmts)
		}
		return invalidConfig("custom SQL must be exactly one statement, got %d", count)
	}
	return validateDorisQuery(file.Stmts[0])
}

// validateDorisQuery walks the Doris query node to its SELECT leaves.
func validateDorisQuery(node dorisast.Node) error {
	switch stmt := node.(type) {
	case *dorisast.SelectStmt:
		return nil
	case *dorisast.SetOpStmt:
		if err := validateDorisQuery(stmt.Left); err != nil {
			return err
		}
		return validateDorisQuery(stmt.Right)
	default:
		return invalidConfig("custom SQL must be a single SELECT or WITH ... SELECT")
	}
}

// mysqlStatementName names a rejected statement's node for the error message, so a
// caller can tell a DDL from an EXPLAIN without reading the SQL.
func mysqlStatementName(node mysqlast.Node) string {
	switch node.(type) {
	case *mysqlast.ExplainStmt:
		return "EXPLAIN"
	case *mysqlast.InsertStmt:
		return "INSERT"
	case *mysqlast.UpdateStmt:
		return "UPDATE"
	case *mysqlast.DeleteStmt:
		return "DELETE"
	case *mysqlast.CreateTableStmt, *mysqlast.CreateViewStmt, *mysqlast.DropTableStmt:
		return "DDL"
	case *mysqlast.TableStmt:
		return "TABLE"
	case *mysqlast.CallStmt:
		return "CALL"
	default:
		return "a non-SELECT statement"
	}
}

// mssqlStatementName names a rejected T-SQL statement's node for the error message.
func mssqlStatementName(node mssqlast.Node) string {
	switch node.(type) {
	case *mssqlast.InsertStmt:
		return "INSERT"
	case *mssqlast.UpdateStmt:
		return "UPDATE"
	case *mssqlast.DeleteStmt:
		return "DELETE"
	case *mssqlast.MergeStmt:
		return "MERGE"
	case *mssqlast.CreateTableStmt, *mssqlast.CreateViewStmt, *mssqlast.AlterTableStmt:
		return "DDL"
	case *mssqlast.ExecStmt:
		return "EXEC"
	default:
		return "a non-SELECT statement"
	}
}
