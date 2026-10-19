package dataquality

import (
	"fmt"
	"strings"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Dialect is everything the compiler needs to know about the target engine's
// SQL surface. Keeping it an interface is what makes one rule definition compile
// to each engine's own syntax without a per-type switch on the engine.
type Dialect interface {
	// Name is the dialect's identifier, for error messages.
	Name() string
	// QuoteIdentifier quotes one identifier, escaping its quote character.
	QuoteIdentifier(identifier string) string
	// Placeholder returns the placeholder for the n-th (1-based) argument.
	Placeholder(n int) string
	// QuoteTable renders the table a target names, qualified as the engine
	// expects.
	QuoteTable(target ResolvedTarget) (string, error)
	// SupportsRegex reports whether the engine has a regular-expression operator
	// the compiler can emit. MSSQL does not.
	SupportsRegex() bool
	// LengthExpr returns the expression for a string's character length.
	LengthExpr(column string) string
	// LimitClause renders a row limit for a SELECT.
	LimitClause(n int) string
	// NormalizeBool renders a boolean literal for the engine.
	NormalizeBool(value bool) string
}

// DialectFor returns the dialect for an engine, and an error for an engine this
// build cannot compile for.
func DialectFor(engine storepb.Engine) (Dialect, error) {
	switch engine {
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB,
		storepb.Engine_OCEANBASE, storepb.Engine_STARROCKS, storepb.Engine_DORIS:
		return mysqlDialect{engine: engine}, nil
	case storepb.Engine_POSTGRES:
		return postgresDialect{}, nil
	case storepb.Engine_MSSQL:
		return mssqlDialect{}, nil
	case storepb.Engine_ENGINE_UNSPECIFIED:
		return nil, errors.New("the target instance has no engine")
	default:
		return nil, errors.Errorf("no data quality dialect for engine %s", engine)
	}
}

// mysqlDialect covers the MySQL wire family (MySQL, TiDB, MariaDB, OceanBase) and
// StarRocks/Doris, which share backtick quoting, `?` placeholders, LIMIT and
// REGEXP.
type mysqlDialect struct {
	engine storepb.Engine
}

func (d mysqlDialect) Name() string { return d.engine.String() }

func (mysqlDialect) QuoteIdentifier(identifier string) string {
	return "`" + strings.ReplaceAll(identifier, "`", "``") + "`"
}

func (mysqlDialect) Placeholder(_ int) string { return "?" }

// QuoteTable renders `database`.`table`. The MySQL family has no schema segment,
// so a non-empty schema is the caller's error rather than something to silently
// drop: it means the target was resolved for the wrong engine.
func (d mysqlDialect) QuoteTable(target ResolvedTarget) (string, error) {
	if target.Schema != "" {
		return "", errors.Errorf("engine %s has no schema segment for target %q", d.Name(), target.GUID)
	}
	if target.Database == "" || target.Table == "" {
		return "", errors.New("the target database and table names are required")
	}
	return d.QuoteIdentifier(target.Database) + "." + d.QuoteIdentifier(target.Table), nil
}

func (mysqlDialect) SupportsRegex() bool { return true }

func (mysqlDialect) LengthExpr(column string) string {
	return "CHAR_LENGTH(" + column + ")"
}

func (mysqlDialect) LimitClause(n int) string { return fmt.Sprintf("LIMIT %d", n) }

func (mysqlDialect) NormalizeBool(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}

// postgresDialect is PostgreSQL, with double-quoted identifiers, $n placeholders,
// `~` for regular expressions and LIMIT.
type postgresDialect struct{}

func (postgresDialect) Name() string { return "POSTGRES" }

func (postgresDialect) QuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func (postgresDialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }

// QuoteTable renders "schema"."table". The connection already selects the
// database, so a database segment is not part of the SQL.
func (d postgresDialect) QuoteTable(target ResolvedTarget) (string, error) {
	if target.Schema == "" || target.Table == "" {
		return "", errors.New("the target schema and table names are required")
	}
	return d.QuoteIdentifier(target.Schema) + "." + d.QuoteIdentifier(target.Table), nil
}

func (postgresDialect) SupportsRegex() bool { return true }

func (postgresDialect) LengthExpr(column string) string { return "CHAR_LENGTH(" + column + ")" }

func (postgresDialect) LimitClause(n int) string { return fmt.Sprintf("LIMIT %d", n) }

func (postgresDialect) NormalizeBool(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}

// mssqlDialect is T-SQL: [bracketed] identifiers, @pN placeholders and TOP/OFFSET
// FETCH for limits. It has no regular-expression operator, so regex rules are
// rejected at validation time.
type mssqlDialect struct{}

func (mssqlDialect) Name() string { return "MSSQL" }

func (mssqlDialect) QuoteIdentifier(identifier string) string {
	return "[" + strings.ReplaceAll(identifier, "]", "]]") + "]"
}

func (mssqlDialect) Placeholder(n int) string { return fmt.Sprintf("@p%d", n) }

// QuoteTable renders [schema].[table]. The connection selects the database.
func (d mssqlDialect) QuoteTable(target ResolvedTarget) (string, error) {
	if target.Schema == "" || target.Table == "" {
		return "", errors.New("the target schema and table names are required")
	}
	return d.QuoteIdentifier(target.Schema) + "." + d.QuoteIdentifier(target.Table), nil
}

func (mssqlDialect) SupportsRegex() bool { return false }

func (mssqlDialect) LengthExpr(column string) string { return "LEN(" + column + ")" }

// LimitClause is only valid in a query that already has an ORDER BY, so the
// compiler emits ORDER BY (SELECT NULL) before it where it needs a bounded
// result. The clause itself stays a suffix.
func (mssqlDialect) LimitClause(n int) string {
	return fmt.Sprintf("OFFSET 0 ROWS FETCH NEXT %d ROWS ONLY", n)
}

func (mssqlDialect) NormalizeBool(value bool) string {
	// T-SQL has no boolean literal in expressions; 1/0 is what a predicate yields.
	if value {
		return "1"
	}
	return "0"
}
