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
// with a syntax error (finding F2 in .agents/docs/lineage-semantics.md). Those
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
	// Only a statement that opens with CREATE or ALTER has the header this
	// fallback understands. A statement that merely contains the word VIEW — in a
	// SHOW form, say — must keep its own parse error rather than being reported as
	// a broken view query.
	if !opensViewDDL(tokens) {
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
	// always follows the body introducer, so the first depth-0 AS after VIEW is
	// the right one.
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

	// The target name is the dotted run of identifiers right after VIEW, past the
	// `[IF NOT EXISTS]` prologue, optionally followed by a parenthesized column
	// list.
	parts, i := viewNameParts(tokens, viewIndex+1, asIndex)
	if len(parts) == 0 {
		return nil, false
	}
	var columns []string
	if i < asIndex && tokens[i].Kind == int('(') {
		// A column list delimits the name, so what follows it before AS
		// (`SECURITY NONE`, `DISTRIBUTED BY HASH(id)`) cannot be part of it.
		columns = columnListTokens(tokens[i:asIndex])
	} else {
		// With no column list, an identifier between the name and AS belongs to a
		// clause this fallback does not model — a refresh mode that is two tokens
		// rather than one keyword, or `DEFINER = 'u'`. Reading it as another name
		// part is how the target became `mv.ASYNC`, so the fallback refuses and the
		// caller keeps the original parse error.
		for ; i < asIndex; i++ {
			if isIdentifierToken(tokens[i]) {
				return nil, false
			}
		}
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

// opensViewDDL reports whether the statement opens with CREATE or ALTER, the two
// forms whose header a view definition can start with.
func opensViewDDL(tokens []starrocksparser.Token) bool {
	create, createOK := starrocksparser.KeywordToken("CREATE")
	alter, alterOK := starrocksparser.KeywordToken("ALTER")
	if !createOK || !alterOK {
		return false
	}
	return tokens[0].Kind == create || tokens[0].Kind == alter
}

// viewNameParts collects the dotted name that identifies the object, starting at
// index from and stopping before limit. `[IF NOT EXISTS]` is skipped: it is
// keywords, and none of them can be a name. Only a dot may separate two segments,
// so `db1.v` is one name while `v DEFINER` is a name followed by a clause this
// fallback does not model. It returns the index the caller resumes from.
func viewNameParts(tokens []starrocksparser.Token, from, limit int) ([]string, int) {
	i := from
	for i < limit && isViewPrologueKeyword(tokens[i]) {
		i++
	}
	if i >= limit || !isIdentifierToken(tokens[i]) {
		return nil, i
	}
	parts := []string{tokens[i].Str}
	i++
	for i+1 < limit && tokens[i].Kind == int('.') && isIdentifierToken(tokens[i+1]) {
		parts = append(parts, tokens[i+1].Str)
		i += 2
	}
	return parts, i
}

// isViewPrologueKeyword reports whether t is one of the keywords `IF NOT EXISTS`,
// which may sit between VIEW and the name.
func isViewPrologueKeyword(t starrocksparser.Token) bool {
	for _, name := range []string{"IF", "NOT", "EXISTS"} {
		if kind, ok := starrocksparser.KeywordToken(name); ok && t.Kind == kind {
			return true
		}
	}
	return false
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
