package doris

import (
	"strings"

	dorisparser "github.com/bytebase/omni/doris/parser"
)

// rewriteWithBeforeDML recovers the statements Doris runs whose WITH clause stands before
// INSERT, UPDATE or DELETE — a place omni's grammar rejects it.
//
// The engine takes all three: on 4.1.4 `WITH c AS (SELECT 1 AS a) UPDATE t SET b = 9 WHERE
// a = 1` really updates the row, the INSERT really inserts and the DELETE really deletes,
// each read back from the table. For INSERT there is a place omni reads, because the query
// an INSERT writes carries its own WITH: `INSERT INTO t (a) WITH c AS (…) SELECT a FROM c`
// parses and is analyzed with the CTE intact. So the clause moves in front of that query's
// SELECT or VALUES. UPDATE and DELETE have no such place, and their clause goes only when
// the statement reads none of its names — an unused CTE writes nothing — while a name it
// does read keeps the statement's hard failure.
//
// It reports false for anything it cannot do completely, so the caller keeps the parse
// error the statement came with.
func rewriteWithBeforeDML(sql string) (string, bool) {
	tokens, lexErrs := dorisparser.Tokenize(sql)
	if len(lexErrs) > 0 || len(tokens) == 0 {
		return "", false
	}
	kinds := make(map[string]int, 12)
	for _, name := range []string{
		"WITH", "RECURSIVE", "INSERT", "UPDATE", "DELETE", "SELECT", "VALUES", "PARTITION", "LABEL",
	} {
		kind, ok := dorisparser.KeywordToken(name)
		if !ok {
			return "", false
		}
		kinds[name] = kind
	}
	if tokens[0].Kind != kinds["WITH"] {
		return "", false
	}

	// The clause ends where the statement it introduces begins.
	statement, depth := -1, 0
	for i := 1; i < len(tokens); i++ {
		t := tokens[i]
		if t.Kind == int('(') {
			depth++
			continue
		}
		if t.Kind == int(')') {
			depth--
			continue
		}
		if depth != 0 || i == len(tokens)-1 {
			continue
		}
		if t.Kind == kinds["INSERT"] || t.Kind == kinds["UPDATE"] || t.Kind == kinds["DELETE"] {
			statement = i
			break
		}
	}
	if statement < 0 {
		return "", false
	}
	clause := strings.TrimSpace(sql[:tokens[statement].Loc.Start])
	if clause == "" {
		return "", false
	}
	rest := sql[tokens[statement].Loc.Start:]

	if tokens[statement].Kind == kinds["INSERT"] {
		point, source, ok := insertSource(rest, kinds)
		if !ok {
			return "", false
		}
		if source == kinds["SELECT"] {
			return rest[:point] + clause + " " + rest[point:], true
		}
		// A VALUES source can never name the clause, so it is never moved — only dropped,
		// and the same rule as an UPDATE or a DELETE decides whether that is safe: a row
		// whose subquery reads the CTE keeps the statement's hard failure.
	}

	names, ok := cteNames(tokens[:statement], kinds)
	if !ok || readsAnyName(tokens[statement:], names) {
		return "", false
	}
	return rest, true
}

// insertSource is where an INSERT's source begins and which kind it is. A WITH clause can
// stand in front of a SELECT, which is where omni reads it, and a PARTITION clause before
// that changes nothing about the placement (the engine takes `INSERT INTO t PARTITION (p)
// (a) WITH c AS (…) SELECT …`, and omni parses it). It cannot stand in front of VALUES:
// omni's grammar takes a WITH only before a SELECT, which is why the caller treats the two
// kinds differently.
//
// A LABEL clause makes this report nothing. `INSERT INTO t WITH LABEL l WITH c AS (…) SELECT
// …` runs on the engine but omni rejects it, so there is no placement to use and the
// statement keeps its parse error.
func insertSource(rest string, kinds map[string]int) (int, int, bool) {
	tokens, lexErrs := dorisparser.Tokenize(rest)
	if len(lexErrs) > 0 || len(tokens) == 0 {
		return 0, 0, false
	}
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
		if depth != 0 {
			continue
		}
		if t.Kind == kinds["WITH"] || t.Kind == kinds["LABEL"] {
			return 0, 0, false
		}
		if t.Kind == kinds["SELECT"] || t.Kind == kinds["VALUES"] {
			return t.Loc.Start, t.Kind, true
		}
	}
	return 0, 0, false
}

// cteNames reads the names a WITH clause defines, which are the names a statement can read
// from it. The clause has already parsed as a clause — the statement behind it is what
// failed — so only the shape of the list has to be walked: a name, an optional column list
// and `AS ( … )`, repeated after commas at the clause's own level.
func cteNames(tokens []dorisparser.Token, kinds map[string]int) ([]string, bool) {
	var names []string
	depth, expectName := 0, false
	for _, t := range tokens {
		if t.Kind == int('(') {
			depth++
			continue
		}
		if t.Kind == int(')') {
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if t.Kind == kinds["WITH"] || t.Kind == int(',') {
			expectName = true
			continue
		}
		// `WITH RECURSIVE name AS ( … )` names its CTE after the keyword, and every other
		// token between a name and the next comma belongs to that name's own definition.
		if !expectName || t.Kind == kinds["RECURSIVE"] {
			continue
		}
		if !isIdentifierToken(t) {
			return nil, false
		}
		names = append(names, t.Str)
		expectName = false
	}
	if len(names) == 0 {
		return nil, false
	}
	return names, true
}

// readsAnyName reports whether the statement reads any of these names, and every token
// counts whatever kind it has. A CTE may be named with a word that lexes as a keyword —
// `WITH `type` AS (…) UPDATE t SET b = (SELECT k FROM type)` runs on the engine, and the
// bare reference lexes as that keyword — so a check that only looked at identifier tokens
// would drop a clause the statement reads, turning an honest hard failure into an edge from
// a table the statement never names. A name that merely appears in a string literal counts
// too, which costs only a recovery the statement did not need.
func readsAnyName(tokens []dorisparser.Token, names []string) bool {
	for _, t := range tokens {
		for _, name := range names {
			if t.Str == name {
				return true
			}
		}
	}
	return false
}
