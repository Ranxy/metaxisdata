package starrocks

import (
	"strings"

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
