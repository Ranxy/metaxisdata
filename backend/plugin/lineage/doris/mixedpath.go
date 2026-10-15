package doris

import (
	"strings"

	dorisparser "github.com/bytebase/omni/doris/parser"
)

// rewriteMixedVariantPaths rewrites a VARIANT subcolumn path that mixes the bracket and
// dotted spellings — `v['user'].name` — into the all-bracket form of the same path, which
// omni parses.
//
// The engine runs both spellings and answers the same value: on 4.1.4 both
// `SELECT v['user'].name FROM ev` and `SELECT v['user']['name'] FROM ev` return `alice`,
// and both name the column after the text the statement wrote (`v['user'].name` and
// `v['user']['name']`). omni stops at the dot that follows a subscript, so the mixed form
// used to cost the statement its whole lineage.
//
// Only a dot that follows a *string* subscript is rewritten. `a[1].b` reads a field of an
// array element, which the bracket form does not mean, so it is left to fail rather than
// rewritten into something else; a key that is not a plain identifier is left alone for
// the same reason, since the bracket form keeps it verbatim.
//
// It reports the rewritten text, the text that was written, and the edits mapping one onto
// the other, so an expression keeps the name the statement gave it.
func rewriteMixedVariantPaths(sql string) (string, string, []textEdit, bool) {
	tokens, lexErrs := dorisparser.Tokenize(sql)
	if len(lexErrs) > 0 || len(tokens) == 0 {
		return "", "", nil, false
	}
	type replacement struct {
		start, end int
		text       string
	}
	var (
		replacements []replacement
		// openBrackets says, per open bracket, whether it holds a string literal: a path
		// is read into keys, and only those can be spelled with a dot.
		openBrackets []bool
		afterKey     bool
	)
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch {
		case t.Kind == int('['):
			openBrackets = append(openBrackets, i+2 < len(tokens) &&
				dorisparser.TokenName(tokens[i+1].Kind) == "STRING" && tokens[i+2].Kind == int(']'))
			afterKey = false
		case t.Kind == int(']'):
			afterKey = false
			if len(openBrackets) > 0 {
				afterKey = openBrackets[len(openBrackets)-1]
				openBrackets = openBrackets[:len(openBrackets)-1]
			}
		case t.Kind == int('.') && afterKey:
			if i+1 >= len(tokens) || !isPathSegmentToken(tokens[i+1]) {
				afterKey = false
				continue
			}
			segment := tokens[i+1]
			replacements = append(replacements, replacement{
				start: t.Loc.Start,
				end:   segment.Loc.End,
				text:  "['" + strings.ReplaceAll(segment.Str, "'", "''") + "']",
			})
			afterKey = true
			i++
		default:
			afterKey = false
		}
	}
	if len(replacements) == 0 {
		return "", "", nil, false
	}

	var (
		b     strings.Builder
		edits = make([]textEdit, 0, len(replacements))
		pos   = 0
	)
	for _, r := range replacements {
		_, _ = b.WriteString(sql[pos:r.start])
		start := b.Len()
		_, _ = b.WriteString(r.text)
		edits = append(edits, textEdit{
			rewrittenStart: start,
			rewrittenEnd:   b.Len(),
			originalStart:  r.start,
			originalEnd:    r.end,
		})
		pos = r.end
	}
	_, _ = b.WriteString(sql[pos:])
	return b.String(), sql, edits, true
}

// isPathSegmentToken reports whether t can name a segment of a subcolumn path. omni
// lexes the segment after a dot as NAME — its own kind, distinct from the IDENT that
// starts a path — and a segment may also be quoted.
func isPathSegmentToken(t dorisparser.Token) bool {
	switch dorisparser.TokenName(t.Kind) {
	case "IDENT", "QUOTED_IDENT", "NAME":
		return true
	default:
		return false
	}
}
