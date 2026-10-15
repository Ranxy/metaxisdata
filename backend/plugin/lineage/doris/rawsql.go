package doris

import (
	"strings"

	"github.com/pkg/errors"

	nodes "github.com/bytebase/omni/doris/ast"
	dorisparser "github.com/bytebase/omni/doris/parser"
)

// source is one nesting level's SQL text together with the tokens needed to
// rebuild expression text with inter-token whitespace removed.
//
// omni exposes a few query bodies (derived tables, CTAS, expression subqueries)
// only as raw text, so analyzing one will push a new source onto the analyzer's
// stack instead of remapping offsets into the outer statement.
type source struct {
	text   string
	tokens []dorisparser.Token
	// original, originalTokens and edits are set when the text was rewritten by
	// replacement. An expression is then named the way the statement wrote it, which is
	// what the engine does: it names a column after the text the query holds, so a
	// rewritten spelling would put a name in the registry that the engine never reports.
	original       string
	originalTokens []dorisparser.Token
	edits          []textEdit
}

// textEdit is one replaced region of a rewritten source: the rewritten text
// [rewrittenStart, rewrittenEnd) stands for the original text [originalStart,
// originalEnd). Everything outside the regions is identical, so an offset outside them
// maps by the length difference accumulated before it.
type textEdit struct {
	rewrittenStart int
	rewrittenEnd   int
	originalStart  int
	originalEnd    int
}

// originalOffset maps an offset in a rewritten source back to the text it stands for. It
// reports false when the offset falls strictly inside a replaced region, where no
// boundary of the original answers to it; the caller then keeps the rewritten text.
func originalOffset(edits []textEdit, offset int) (int, bool) {
	shift := 0
	for _, e := range edits {
		if offset <= e.rewrittenStart {
			return offset - shift, true
		}
		if offset < e.rewrittenEnd {
			return 0, false
		}
		shift += (e.rewrittenEnd - e.rewrittenStart) - (e.originalEnd - e.originalStart)
	}
	return offset - shift, true
}

// newSource tokenizes text for expression reconstruction. Lex errors are
// ignored here because the parser has already validated the statement before
// any analysis runs.
func newSource(text string) source {
	tokens, _ := dorisparser.Tokenize(text)
	return source{text: text, tokens: tokens}
}

// newRewrittenSource is newSource for text a rewrite produced, keeping the text it was
// written from so an expression is named as the statement wrote it.
func newRewrittenSource(text, original string, edits []textEdit) source {
	src := newSource(text)
	src.original = original
	if len(edits) > 0 {
		src.originalTokens, _ = dorisparser.Tokenize(original)
		src.edits = edits
	}
	return src
}

// currentSource returns the source being analyzed, or nil when the stack is
// empty.
func (a *Analyzer) currentSource() *source {
	if len(a.sources) == 0 {
		return nil
	}
	return &a.sources[len(a.sources)-1]
}

// pushSource makes src the current source.
func (a *Analyzer) pushSource(src source) {
	a.sources = append(a.sources, src)
}

// popSource restores the previous source. The top-level source is never
// removed.
func (a *Analyzer) popSource() {
	if len(a.sources) <= 1 {
		return
	}
	a.sources = a.sources[:len(a.sources)-1]
}

// parseRawQuery parses a query body omni only exposes as text (derived tables,
// CTAS, expression subqueries). The returned node's Loc values are relative to
// raw, so the caller must have pushed a matching source.
func parseRawQuery(raw string) (nodes.Node, error) {
	file, err := dorisparser.Parse(raw)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse raw query")
	}
	if file == nil || len(file.Stmts) != 1 {
		return nil, errors.New("expected exactly 1 statement in raw query")
	}
	return file.Stmts[0], nil
}

// parseQueryBody parses a query body and reports the source its expressions have to be
// read from, which is not raw when a rewrite applied.
func parseQueryBody(raw string) (nodes.Node, source, error) {
	if text, original, edits, ok := rewriteQuery(raw); ok {
		if query, err := parseRawQuery(text); err == nil {
			return query, newRewrittenSource(text, original, edits), nil
		}
	}
	query, err := parseRawQuery(raw)
	return query, newSource(raw), err
}

// rewriteQuery applies each rewrite for a shape omni's grammar rejects while the engine
// runs it, and reports the text to parse, the text that was written, and the edits that
// map one onto the other — empty when a rewrite only moved or dropped whole regions,
// which changes no expression's own text. The rewrites preserve what the statement means
// — a derived table read once becomes a CTE read once, a result sink carries no lineage —
// so applying one to a statement that failed for some other reason only helps when the
// rewritten text parses at all.
func rewriteQuery(sql string) (string, string, []textEdit, bool) {
	original, edits := sql, []textEdit(nil)
	for _, rewrite := range []func(string) (string, bool){rewriteLateralOverDerivedTable, stripIntoOutfile} {
		if next, ok := rewrite(sql); ok {
			sql = next
		}
	}
	// Rewriting a spelling into another one is the case that has to keep the original
	// text: it is the only rewrite that changes what an expression says.
	if text, from, rewritten, ok := rewriteMixedVariantPaths(sql); ok {
		original, edits, sql = from, rewritten, text
	}
	if sql == original {
		return "", "", nil, false
	}
	file, err := dorisparser.Parse(sql)
	if err != nil || file == nil || len(file.Stmts) != 1 {
		return "", "", nil, false
	}
	return sql, original, edits, true
}

// exprText reconstructs an expression's source text with inter-token whitespace
// removed, matching the MySQL analyzer's normalization. Slicing by token keeps
// whitespace inside string literals intact. loc is interpreted against the
// current source, and against the text the statement wrote when that source is a
// rewritten one: the engine names a column after the text the query holds, so a
// rewritten spelling must not become a name of its own.
func (a *Analyzer) exprText(loc nodes.Loc) string {
	src := a.currentSource()
	if src == nil {
		return ""
	}
	text, tokens := src.text, src.tokens
	if len(src.edits) > 0 && loc.IsValid() {
		start, startOK := originalOffset(src.edits, loc.Start)
		end, endOK := originalOffset(src.edits, loc.End)
		if startOK && endOK && start <= end && end <= len(src.original) {
			text, tokens, loc = src.original, src.originalTokens, nodes.Loc{Start: start, End: end}
		}
	}
	if !loc.IsValid() || loc.Start >= loc.End || loc.End > len(text) {
		return ""
	}
	var b strings.Builder
	for i := range tokens {
		t := tokens[i]
		if t.Loc.Start >= loc.End {
			break
		}
		if t.Loc.Start >= loc.Start && t.Loc.End <= loc.End {
			_, _ = b.WriteString(text[t.Loc.Start:t.Loc.End])
		}
	}
	if b.Len() == 0 {
		return text[loc.Start:loc.End]
	}
	return b.String()
}

// exprTextOf returns the reconstructed source text for a node.
func (a *Analyzer) exprTextOf(n nodes.Node) string {
	return a.exprText(nodes.NodeLoc(n))
}
