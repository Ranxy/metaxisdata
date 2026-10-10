package spike

import (
	"errors"
	"strings"

	mysqlnodes "github.com/bytebase/omni/mysql/ast"
	mysqlparser "github.com/bytebase/omni/mysql/parser"
	omnipg "github.com/bytebase/omni/pg"
	pgast "github.com/bytebase/omni/pg/ast"
	srast "github.com/bytebase/omni/starrocks/ast"
	srparser "github.com/bytebase/omni/starrocks/parser"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// errNoTarget means the statement is not a DML write this probe can attribute.
var errNoTarget = errors.New("no DML target found")

// DMLTargets answers the one question the zero-relation branch needs: which table
// does this statement write? It exists because `INSERT INTO t (a,b) VALUES (...)`
// yields no column relation in any dialect, so nothing else names the target.
//
// It is deliberately a per-dialect read of the DML node rather than a text scan:
// qualified and quoted names, INSERT IGNORE, REPLACE, multi-table UPDATE and
// DELETE ... USING all have to keep working.
func DMLTargets(engine storepb.Engine, sql string) ([]string, error) {
	switch engine {
	case storepb.Engine_POSTGRES:
		return pgTargets(sql)
	case storepb.Engine_STARROCKS:
		return starrocksTargets(sql)
	default:
		return mysqlTargets(sql)
	}
}

func mysqlTargets(sql string) ([]string, error) {
	list, err := mysqlparser.Parse(sql)
	if err != nil {
		return nil, err
	}
	if list.Len() != 1 {
		return nil, errNoTarget
	}
	switch stmt := list.Items[0].(type) {
	case *mysqlnodes.InsertStmt:
		return []string{mysqlName(stmt.Table)}, nil
	case *mysqlnodes.UpdateStmt:
		return mysqlNames(stmt.Tables)
	case *mysqlnodes.DeleteStmt:
		return mysqlNames(stmt.Tables)
	default:
		return nil, errNoTarget
	}
}

// mysqlNames keeps only the plain table references: a multi-table UPDATE's table
// list can hold joins and subqueries, which are sources rather than write targets.
func mysqlNames(exprs []mysqlnodes.TableExpr) ([]string, error) {
	out := make([]string, 0, len(exprs))
	for _, e := range exprs {
		if ref, ok := e.(*mysqlnodes.TableRef); ok && ref.Name != "" {
			out = append(out, mysqlName(ref))
		}
	}
	if len(out) == 0 {
		return nil, errNoTarget
	}
	return out, nil
}

func mysqlName(ref *mysqlnodes.TableRef) string {
	if ref == nil {
		return ""
	}
	if ref.Schema != "" {
		return ref.Schema + "." + ref.Name
	}
	return ref.Name
}

func pgTargets(sql string) ([]string, error) {
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
			return []string{pgName(s.Relation)}, nil
		case *pgast.UpdateStmt:
			return []string{pgName(s.Relation)}, nil
		case *pgast.DeleteStmt:
			return []string{pgName(s.Relation)}, nil
		}
	}
	return nil, errNoTarget
}

func pgName(ref *pgast.RangeVar) string {
	if ref == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	for _, p := range []string{ref.Catalogname, ref.Schemaname, ref.Relname} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ".")
}

func starrocksTargets(sql string) ([]string, error) {
	file, errs := srparser.Parse(sql)
	if len(errs) > 0 {
		return nil, &errs[0]
	}
	if file == nil {
		return nil, errNoTarget
	}
	for _, node := range file.Stmts {
		switch s := node.(type) {
		case *srast.InsertStmt:
			return []string{joinParts(s.Target)}, nil
		case *srast.UpdateStmt:
			return []string{joinParts(s.Target)}, nil
		case *srast.DeleteStmt:
			return []string{joinParts(s.Target)}, nil
		}
	}
	return nil, errNoTarget
}

func joinParts(name *srast.ObjectName) string {
	if name == nil {
		return ""
	}
	return strings.Join(name.Parts, ".")
}
