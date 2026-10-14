package mssql

import (
	"strings"

	nodes "github.com/bytebase/omni/mssql/ast"
	mssqlparser "github.com/bytebase/omni/mssql/parser"
)

// A WITH clause in front of a data-modification statement is dropped by the T-SQL parser:
// it reads the CTE list, finds no SELECT to attach it to, and leaves an empty statement
// followed by the DML statement, whose text span is empty as a result. The definitions are
// unreachable from the tree, and no edge from the statement could be trusted without them.
//
// They are recovered from the source text instead. This is the technique StarRocks uses for
// a view body its grammar rejects (`starrocks/viewbody.go`): take the piece the parser could
// not carry, and parse that piece on its own. Nothing is filed against the upstream module;
// see .agents/docs/omni-upstream-defects.md section 10.1.

// recoverWithClause rebuilds the CTE list of a statement the parser dropped it from.
//
// The CTE list is everything before the statement's first top-level data-modification
// keyword. Parsing that prefix with a plain `SELECT 1` after it yields the WithClause node,
// and because the prefix is a prefix of the original text, the offsets the CTE bodies carry
// still point at the statement being analyzed.
func recoverWithClause(sql string) (*nodes.WithClause, bool) {
	boundary, ok := withClauseBoundary(sql)
	if !ok {
		return nil, false
	}
	list, err := mssqlparser.Parse(sql[:boundary] + " SELECT 1")
	if err != nil || list == nil {
		return nil, false
	}
	for _, item := range list.Items {
		selectStmt, isSelect := item.(*nodes.SelectStmt)
		if !isSelect || selectStmt == nil || selectStmt.WithClause == nil {
			continue
		}
		return selectStmt.WithClause, true
	}
	return nil, false
}

// withClauseBoundary returns the offset of the data-modification keyword that ends the CTE
// list.
//
// Depth matters: a CTE body is a query and is wrapped in parentheses, so a keyword inside
// one belongs to it. A bracketed identifier is one token and cannot be mistaken for the
// keyword it spells.
func withClauseBoundary(sql string) (int, bool) {
	depth := 0
	for _, token := range mssqlparser.Tokenize(sql) {
		switch {
		case token.Type == '(':
			depth++
		case token.Type == ')':
			if depth > 0 {
				depth--
			}
		case depth > 0:
			// Inside a CTE body or an argument list.
		default:
			switch strings.ToUpper(sql[token.Loc:trimEnd(sql, token.End)]) {
			case "INSERT", "UPDATE", "DELETE", "MERGE":
				return token.Loc, true
			}
		}
	}
	return 0, false
}

// trimEnd drops trailing whitespace a lexer may have swallowed with the token, so the raw
// slice is the keyword itself.
func trimEnd(sql string, end int) int {
	for end > 0 && end <= len(sql) {
		switch sql[end-1] {
		case ' ', '\t', '\r', '\n':
			end--
		default:
			return end
		}
	}
	return end
}

// attachWithClause gives a data-modification statement the CTE list it was parsed without.
// It reports false for a statement that already carries one, which is a shape this recovery
// is not for.
func attachWithClause(stmt nodes.Node, withClause *nodes.WithClause) bool {
	if withClause == nil {
		return false
	}
	switch s := stmt.(type) {
	case *nodes.InsertStmt:
		if s.WithClause == nil {
			s.WithClause = withClause
			return true
		}
	case *nodes.UpdateStmt:
		if s.WithClause == nil {
			s.WithClause = withClause
			return true
		}
	case *nodes.DeleteStmt:
		if s.WithClause == nil {
			s.WithClause = withClause
			return true
		}
	case *nodes.MergeStmt:
		if s.WithClause == nil {
			s.WithClause = withClause
			return true
		}
	default:
	}
	return false
}
