// Package dmltarget answers one question about a DML statement: which table does it
// write?
//
// It exists for a specific gap in the lineage analyzers. Lineage is a column-level
// relation, so a statement whose written values come from no column at all yields no
// relation in any dialect — `INSERT INTO t (a, b) VALUES (?, ?)`, or a `DELETE` that
// references no column. Nothing in the analysis then names the table being written, so
// the statement can neither be classified as a write nor drawn with an edge to its
// target. See .agents/docs/application-sql-ingestion.md, "Classification and the table-level
// edge".
//
// The caller is expected to ask only because the analysis came back empty. That
// population is narrow: an UPDATE answers with `t.* -> t.a` even for a constant
// assignment, every DDL statement has a real target, a SELECT writes nothing, and a
// multi-table DELETE or UPDATE over a join references columns. None of those need this
// package. Where it can read a table it still answers — the caller simply never asks —
// and where it cannot it returns ErrNoTarget rather than a guessed name.
package dmltarget

import (
	"errors"

	mysqlnodes "github.com/bytebase/omni/mysql/ast"
	mysqlparser "github.com/bytebase/omni/mysql/parser"
	omnipg "github.com/bytebase/omni/pg"
	pgast "github.com/bytebase/omni/pg/ast"
	srast "github.com/bytebase/omni/starrocks/ast"
	srparser "github.com/bytebase/omni/starrocks/parser"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
)

// ErrNoTarget reports a statement this package cannot attribute a write to.
var ErrNoTarget = errors.New("no DML write target")

// Targets returns the tables a statement writes, named as the statement named them.
// InstanceID is always empty: a statement only ever names an object down to
// database/schema level, and catalog.AnalysisContext.Complete fills in the rest — the
// same rule the analyzers and AnalyzeSQL follow.
//
// A name the parser could not read is never invented: an empty target returns
// ErrNoTarget rather than a zero identifier.
func Targets(engine storepb.Engine, sql string) ([]model.ObjectIdentifier, error) {
	switch engine {
	case storepb.Engine_POSTGRES:
		return postgresTargets(sql)
	case storepb.Engine_STARROCKS:
		return starrocksTargets(sql)
	default:
		return mysqlTargets(sql)
	}
}

func mysqlTargets(sql string) ([]model.ObjectIdentifier, error) {
	list, err := mysqlparser.Parse(sql)
	if err != nil {
		return nil, err
	}
	if list.Len() != 1 {
		return nil, ErrNoTarget
	}
	switch stmt := list.Items[0].(type) {
	case *mysqlnodes.InsertStmt:
		return one(mysqlIdentifier(stmt.Table))
	case *mysqlnodes.UpdateStmt:
		return many(mysqlIdentifiers(stmt.Tables))
	case *mysqlnodes.DeleteStmt:
		return mysqlDeleteTargets(stmt)
	default:
		return nil, ErrNoTarget
	}
}

// mysqlDeleteTargets reads a DELETE's target table.
//
// The `Using` clause splits the two forms, and only one of them can be read here. In
// `DELETE FROM t` the table list holds the table. In `DELETE o FROM orders o JOIN users
// u` it holds the DELETE list, which MySQL lets the statement write as aliases — `o` is
// not a table, and resolving it would mean walking the join tree in `Using`. So a
// DELETE with a USING clause refuses rather than naming an alias as a table.
//
// Every such statement is outside this package's population anyway: it references
// columns, so the analyzer already named its target.
func mysqlDeleteTargets(stmt *mysqlnodes.DeleteStmt) ([]model.ObjectIdentifier, error) {
	if len(stmt.Using) > 0 {
		return nil, ErrNoTarget
	}
	return many(mysqlIdentifiers(stmt.Tables))
}

// mysqlIdentifiers keeps only the plain table references: a multi-table UPDATE's table
// list can hold joins and derived tables, which are sources rather than write targets.
func mysqlIdentifiers(exprs []mysqlnodes.TableExpr) []model.ObjectIdentifier {
	out := make([]model.ObjectIdentifier, 0, len(exprs))
	for _, e := range exprs {
		if ref, ok := e.(*mysqlnodes.TableRef); ok && ref.Name != "" {
			out = append(out, mysqlIdentifier(ref))
		}
	}
	return out
}

// mysqlIdentifier maps a qualifier to the database, which is what it means in the MySQL
// family (lineage-semantics.md, "Cross-dialect naming and edge rules").
func mysqlIdentifier(ref *mysqlnodes.TableRef) model.ObjectIdentifier {
	if ref == nil {
		return model.ObjectIdentifier{}
	}
	return model.ObjectIdentifier{Database: ref.Schema, Name: ref.Name}
}

func postgresTargets(sql string) ([]model.ObjectIdentifier, error) {
	stmts, err := omnipg.Parse(sql)
	if err != nil {
		return nil, err
	}
	for _, stmt := range stmts {
		if stmt.Empty() {
			continue
		}
		switch s := stmt.AST.(type) {
		case *pgast.InsertStmt:
			return one(postgresIdentifier(s.Relation))
		case *pgast.UpdateStmt:
			return one(postgresIdentifier(s.Relation))
		case *pgast.DeleteStmt:
			return one(postgresIdentifier(s.Relation))
		}
	}
	return nil, ErrNoTarget
}

// postgresIdentifier keeps the catalog and schema apart: PostgreSQL's qualifier is a
// schema, and the registry has no catalog dimension, so a three-part name keeps the
// catalog in Database only because RangeVar offers no better slot for it.
func postgresIdentifier(ref *pgast.RangeVar) model.ObjectIdentifier {
	if ref == nil {
		return model.ObjectIdentifier{}
	}
	return model.ObjectIdentifier{
		Database: ref.Catalogname,
		Schema:   ref.Schemaname,
		Name:     ref.Relname,
	}
}

func starrocksTargets(sql string) ([]model.ObjectIdentifier, error) {
	file, errs := srparser.Parse(sql)
	if len(errs) > 0 {
		return nil, &errs[0]
	}
	if file == nil {
		return nil, ErrNoTarget
	}
	for _, node := range file.Stmts {
		switch s := node.(type) {
		case *srast.InsertStmt:
			return one(starrocksIdentifier(s.Target))
		case *srast.UpdateStmt:
			return one(starrocksIdentifier(s.Target))
		case *srast.DeleteStmt:
			return one(starrocksIdentifier(s.Target))
		}
	}
	return nil, ErrNoTarget
}

// starrocksIdentifier takes the last two parts of a name, which is the rule the
// StarRocks analyzer already applies (backend/plugin/lineage/starrocks/expr.go:42): the
// qualifier is a database, and a three-part name's catalog is dropped because the
// registry has no catalog dimension.
func starrocksIdentifier(name *srast.ObjectName) model.ObjectIdentifier {
	if name == nil || len(name.Parts) == 0 {
		return model.ObjectIdentifier{}
	}
	parts := name.Parts
	if len(parts) == 1 {
		return model.ObjectIdentifier{Name: parts[0]}
	}
	return model.ObjectIdentifier{Database: parts[len(parts)-2], Name: parts[len(parts)-1]}
}

func one(id model.ObjectIdentifier) ([]model.ObjectIdentifier, error) {
	if id.Name == "" {
		return nil, ErrNoTarget
	}
	return []model.ObjectIdentifier{id}, nil
}

func many(ids []model.ObjectIdentifier) ([]model.ObjectIdentifier, error) {
	if len(ids) == 0 {
		return nil, ErrNoTarget
	}
	return ids, nil
}
