package starrocks

import (
	"strings"

	starrocksparser "github.com/bytebase/omni/starrocks/parser"
)

// viewDDL is a CREATE/ALTER VIEW or CREATE MATERIALIZED VIEW statement whose
// query body omni's grammar cannot parse in place, split into the pieces the
// analyzer needs.
//
// omni's parseCreateView feeds the body to parseSelectStmt, which accepts only
// a plain SELECT: a WITH clause, a set operation or a parenthesized body fails
// with a syntax error (finding F2 in plan/starrocks_lineage_plan.md). Those
// bodies are extracted here and parsed as a top-level query, where all three
// forms are supported.
type viewDDL struct {
	schema  string
	name    string
	columns []string
	body    string
}

// extractViewDDL splits a view-like statement that omni rejected into its
// target and query body. It reports false for anything that is not a
// CREATE/ALTER VIEW or CREATE MATERIALIZED VIEW statement.
func extractViewDDL(sql string) (*viewDDL, bool) {
	tokens, lexErrs := starrocksparser.Tokenize(sql)
	if len(lexErrs) > 0 || len(tokens) == 0 {
		return nil, false
	}
	asKind, ok := starrocksparser.KeywordToken("AS")
	if !ok {
		return nil, false
	}
	viewKind, ok := starrocksparser.KeywordToken("VIEW")
	if !ok {
		return nil, false
	}

	// Find VIEW and the top-level AS that introduces the body. A CTE's own AS
	// always follows the body introducer, and `REFRESH ASYNC` is a single
	// keyword token, so the first depth-0 AS after VIEW is the right one.
	viewIndex, asIndex := -1, -1
	depth := 0
	for i, t := range tokens {
		if t.Kind == int('(') {
			depth++
		} else if t.Kind == int(')') {
			depth--
		}
		if depth != 0 {
			continue
		}
		if viewIndex < 0 {
			if t.Kind == viewKind {
				viewIndex = i
			}
			continue
		}
		if t.Kind == asKind {
			asIndex = i
			break
		}
	}
	if viewIndex < 0 || asIndex <= viewIndex+1 {
		return nil, false
	}

	// The name and the optional column list sit between VIEW and AS. Catalog
	// qualifiers, IF NOT EXISTS, COMMENT and SECURITY NONE are keywords or
	// punctuation, so the identifier scan skips them.
	var parts, columns []string
	for i := viewIndex + 1; i < asIndex; i++ {
		t := tokens[i]
		if t.Kind == int('(') {
			columns = columnListTokens(tokens[i:asIndex])
			break
		}
		if isIdentifierToken(t) {
			parts = append(parts, t.Str)
		}
	}
	if len(parts) == 0 {
		return nil, false
	}

	body := strings.TrimSpace(sql[tokens[asIndex].Loc.End:])
	if body == "" {
		return nil, false
	}

	ddl := &viewDDL{name: parts[len(parts)-1], columns: columns, body: body}
	if len(parts) > 1 {
		ddl.schema = parts[len(parts)-2]
	}
	return ddl, true
}

// columnListTokens collects the top-level column names of a view column list.
func columnListTokens(tokens []starrocksparser.Token) []string {
	out := make([]string, 0)
	depth := 0
	for _, t := range tokens {
		if t.Kind == int('(') {
			depth++
			continue
		}
		if t.Kind == int(')') {
			depth--
			if depth == 0 {
				return out
			}
			continue
		}
		if depth == 1 && isIdentifierToken(t) {
			out = append(out, t.Str)
		}
	}
	return out
}

// isIdentifierToken reports whether t is a bare or backtick-quoted identifier.
// omni exports no identifier TokenKind, so the token's name is the only public
// discriminator.
func isIdentifierToken(t starrocksparser.Token) bool {
	switch starrocksparser.TokenName(t.Kind) {
	case "IDENT", "QUOTED_IDENT":
		return true
	default:
		return false
	}
}
