package doris

import (
	"strings"

	dorisparser "github.com/bytebase/omni/doris/parser"
)

// stripIntoOutfile removes the `INTO OUTFILE '…'` result sink from a statement omni's
// Doris grammar rejected at INTO, so the query in front of it can be analyzed.
//
// The engine accepts the clause: on 4.1.4 the statement fails later, on the export
// policy (`Exporting results to local disk is not allowed`), not on the syntax. The
// sink carries no column lineage, so the projection's own edges are the whole lineage
// of the statement — which is what the StarRocks analyzer reports for the same
// statement, since its AST has IntoOutfileClause and its traversal ignores it, with no
// diagnostic. Analyzing the stripped query is therefore what the sibling dialect
// already does, not a new reading of the statement.
//
// It reports false unless the statement really is a query followed by that clause: at
// depth 0, `INTO` immediately followed by `OUTFILE` and a string literal. `INSERT INTO`
// and `COPY INTO` name a relation after INTO and are left alone, as is an `INTO` inside
// a subquery. What remains goes back to the parser, and a prefix that still does not
// parse keeps the statement's original failure.
func stripIntoOutfile(sql string) (string, bool) {
	tokens, lexErrs := dorisparser.Tokenize(sql)
	if len(lexErrs) > 0 || len(tokens) == 0 {
		return "", false
	}
	into, ok := dorisparser.KeywordToken("INTO")
	if !ok {
		return "", false
	}
	outfile, ok := dorisparser.KeywordToken("OUTFILE")
	if !ok {
		return "", false
	}
	depth := 0
	for i, t := range tokens {
		if t.Kind == int('(') {
			depth++
			continue
		}
		if t.Kind == int(')') {
			depth--
			continue
		}
		if depth != 0 || t.Kind != into || i == 0 {
			continue
		}
		if i+2 >= len(tokens) || tokens[i+1].Kind != outfile ||
			dorisparser.TokenName(tokens[i+2].Kind) != "STRING" {
			return "", false
		}
		return strings.TrimSpace(sql[:t.Loc.Start]), true
	}
	return "", false
}
