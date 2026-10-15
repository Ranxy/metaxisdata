package doris

import (
	"strings"

	dorisparser "github.com/bytebase/omni/doris/parser"
)

// rewriteLateralOverDerivedTable rewrites a query whose LATERAL VIEW sits on a derived
// table into the CTE form of the same query, because omni's Doris grammar rejects the
// first and parses the second.
//
// `FROM (SELECT arr FROM t) d LATERAL VIEW explode(d.arr) u AS elem` is a statement the
// engine runs (checked on 4.1.4: it returns the same rows, under the same column names,
// as the rewritten form below) while omni stops at LATERAL, which used to cost the whole
// statement its lineage. `WITH d AS (SELECT arr FROM t) SELECT * FROM d LATERAL VIEW
// explode(d.arr) u AS elem` is the same query — a derived table and a CTE that is read
// exactly once are the same relation — and it parses.
//
// The rewrite is only attempted when it cannot change what a name means. The name is
// hoisted to the front of the statement, where a CTE outranks a base table of the same
// name, so every other use of that name must be a qualifier (`d.arr`), which the rewrite
// preserves; a statement that also holds a relation named `d` is refused and keeps its
// hard failure. Only one such derived table is handled, and only at the statement's own
// level: a derived table inside a CTE body or a subquery would have to be hoisted past a
// WITH list it is not visible from.
func rewriteLateralOverDerivedTable(sql string) (string, bool) {
	tokens, lexErrs := dorisparser.Tokenize(sql)
	if len(lexErrs) > 0 || len(tokens) == 0 {
		return "", false
	}
	selectKind, ok := dorisparser.KeywordToken("SELECT")
	if !ok {
		return "", false
	}
	withKind, ok := dorisparser.KeywordToken("WITH")
	if !ok {
		return "", false
	}
	asKind, ok := dorisparser.KeywordToken("AS")
	if !ok {
		return "", false
	}
	// Only a query has a FROM clause to hoist from; a DDL header is split off before
	// this runs, and an INSERT ... SELECT that fails for another reason is left alone.
	if tokens[0].Kind != selectKind && tokens[0].Kind != withKind {
		return "", false
	}
	lateral, ok := dorisparser.KeywordToken("LATERAL")
	if !ok {
		return "", false
	}

	// The derived table is the parenthesized group at the statement's own level whose
	// close is followed by its alias and then LATERAL.
	var (
		openAtEnd  int
		closeAtEnd int
		alias      int
		found      bool
	)
	depth, openAt := 0, -1
	for i, t := range tokens {
		if t.Kind == int('(') {
			if depth == 0 {
				openAt = i
			}
			depth++
			continue
		}
		if t.Kind != int(')') {
			continue
		}
		depth--
		if depth != 0 || openAt < 0 {
			continue
		}
		switch {
		case i+2 < len(tokens) && isIdentifierToken(tokens[i+1]) && tokens[i+2].Kind == lateral:
			alias = i + 1
		case i+3 < len(tokens) && tokens[i+1].Kind == asKind &&
			isIdentifierToken(tokens[i+2]) && tokens[i+3].Kind == lateral:
			// `( … ) AS d LATERAL VIEW …`, which the engine accepts too.
			alias = i + 2
		default:
			openAt = -1
			continue
		}
		if found {
			// Two of them: the second would have to be hoisted first, and the first would
			// then be read inside it. Refuse rather than guess an order.
			return "", false
		}
		openAtEnd, closeAtEnd, found = openAt, i, true
	}
	if !found {
		return "", false
	}

	// Every other use of the name has to be a qualifier. A bare one would be another
	// relation — a FROM item, a join operand — that the hoisted CTE would shadow.
	for i, t := range tokens {
		if i == alias || !isIdentifierToken(t) || t.Str != tokens[alias].Str {
			continue
		}
		if i+1 >= len(tokens) || tokens[i+1].Kind != int('.') {
			return "", false
		}
	}

	body := strings.TrimSpace(sql[tokens[openAtEnd].Loc.End:tokens[closeAtEnd].Loc.Start])
	if body == "" {
		return "", false
	}
	// `(body) alias` becomes `alias`, which leaves the LATERAL VIEW that follows it
	// reading the same relation under the same name.
	rest := sql[:tokens[openAtEnd].Loc.Start] + tokens[alias].Str + sql[tokens[alias].Loc.End:]

	if tokens[0].Kind == withKind {
		// Append to the statement's own CTE list: the derived table is in the main
		// query, so its body is visible from there and the new CTE is too. The comma
		// separates it from the CTE the list already ends with. A derived table that
		// sits before the list's end came from somewhere this rewrite did not look, so
		// the statement is left alone rather than rewritten into something else.
		insert := mainQueryStart(tokens, selectKind)
		if insert < 0 || insert > tokens[openAtEnd].Loc.Start {
			return "", false
		}
		return rest[:insert] + ", " + tokens[alias].Str + " AS (" + body + ") " + rest[insert:], true
	}
	return "WITH " + tokens[alias].Str + " AS (" + body + ") " + rest, true
}

// mainQueryStart is the source offset of the query a WITH clause introduces, i.e. the
// first SELECT at the statement's own level.
func mainQueryStart(tokens []dorisparser.Token, selectKind int) int {
	depth := 0
	for _, t := range tokens {
		if t.Kind == int('(') {
			depth++
			continue
		}
		if t.Kind == int(')') {
			depth--
			continue
		}
		if depth == 0 && t.Kind == selectKind {
			return t.Loc.Start
		}
	}
	return -1
}
