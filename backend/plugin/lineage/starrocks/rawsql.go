package starrocks

import (
	"strings"

	"github.com/pkg/errors"

	nodes "github.com/bytebase/omni/starrocks/ast"
	starrocksparser "github.com/bytebase/omni/starrocks/parser"
)

// source is one nesting level's SQL text together with the tokens needed to
// rebuild expression text with inter-token whitespace removed.
//
// omni exposes a few query bodies (derived tables, CTAS, expression subqueries)
// only as raw text, so analyzing one will push a new source onto the analyzer's
// stack instead of remapping offsets into the outer statement.
type source struct {
	text   string
	tokens []starrocksparser.Token
}

// newSource tokenizes text for expression reconstruction. Lex errors are
// ignored here because the parser has already validated the statement before
// any analysis runs.
func newSource(text string) source {
	tokens, _ := starrocksparser.Tokenize(text)
	return source{text: text, tokens: tokens}
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
	file, errs := starrocksparser.Parse(raw)
	if len(errs) > 0 {
		return nil, errors.Wrap(&errs[0], "failed to parse raw query")
	}
	if file == nil || len(file.Stmts) != 1 {
		return nil, errors.New("expected exactly 1 statement in raw query")
	}
	return file.Stmts[0], nil
}

// exprText reconstructs an expression's source text with inter-token whitespace
// removed, matching the MySQL analyzer's normalization. Slicing by token keeps
// whitespace inside string literals intact. loc is interpreted against the
// current source.
func (a *Analyzer) exprText(loc nodes.Loc) string {
	src := a.currentSource()
	if src == nil || !loc.IsValid() || loc.Start >= loc.End || loc.End > len(src.text) {
		return ""
	}
	var b strings.Builder
	for i := range src.tokens {
		t := src.tokens[i]
		if t.Loc.Start >= loc.End {
			break
		}
		if t.Loc.Start >= loc.Start && t.Loc.End <= loc.End {
			_, _ = b.WriteString(src.text[t.Loc.Start:t.Loc.End])
		}
	}
	if b.Len() == 0 {
		return src.text[loc.Start:loc.End]
	}
	return b.String()
}

// exprTextOf returns the reconstructed source text for a node.
func (a *Analyzer) exprTextOf(n nodes.Node) string {
	return a.exprText(nodes.NodeLoc(n))
}
