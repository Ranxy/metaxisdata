// Package spike holds throwaway probes for the application-SQL ingestion design.
// Delete the whole directory once the findings are recorded in .agents/docs.
package spike

import (
	"errors"
	"strings"

	omnimysql "github.com/bytebase/omni/mysql/parser"
	omnipg "github.com/bytebase/omni/pg/parser"
	omnisr "github.com/bytebase/omni/starrocks/parser"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// placeholder is the wire and stored form of every literal.
const placeholder = "?"

// neutralLiteral is what a placeholder becomes before analysis. A string literal
// is the only substitute that parses in every position the corpus exercised:
// MySQL's LOAD DATA and JSON_TABLE PATH, PostgreSQL's COPY source and INTERVAL,
// and StarRocks' COPY INTO all demand a string and reject NULL, 0 and $n there.
// Any string works, so this picks '?' to keep derived column names readable —
// `price * '?'` rather than `price * ”`.
const neutralLiteral = "'?'"

// errUnlexable is what a statement omni's lexer cannot walk produces.
var errUnlexable = errors.New("statement could not be lexed")

// span is one token's byte range in the source text. Comments never appear as
// tokens — both upstream lexers skip them — so the gaps between spans are where
// comments and whitespace live.
type span struct {
	loc, end int
}

// Sanitized is one statement in the two forms the design stores and hashes.
type Sanitized struct {
	// Text is what gets stored: every literal replaced by `?`, comments removed,
	// and all other spacing left byte-for-byte as the application wrote it. It is
	// what a human reads and what the fingerprint is computed from.
	Text string
	// Canonical is Text with tokens re-joined by single spaces: the fingerprint's
	// input, so that whitespace cannot split one statement into two.
	Canonical string
}

// lexSpans returns every token's byte range. It deliberately reads nothing but
// the location, so it never depends on omni's unexported token-type constants,
// which is what a type-driven classifier would have had to hardcode.
func lexSpans(engine storepb.Engine, sql string) []span {
	spans := make([]span, 0, 64)
	// A token consumes at least one byte, so the loop bound cannot cut off a real
	// token; it only stops a lexer that fails to make progress. Each lexer has its
	// own Token type, so the three loops are written out.
	switch engine {
	case storepb.Engine_POSTGRES:
		l := omnipg.NewLexer(sql)
		for i := 0; i <= len(sql); i++ {
			t := l.NextToken()
			if t.Type == 0 || t.End <= t.Loc {
				break
			}
			spans = append(spans, span{t.Loc, t.End})
		}
	case storepb.Engine_STARROCKS:
		tokens, errs := omnisr.Tokenize(sql)
		if len(errs) > 0 {
			return nil
		}
		for _, t := range tokens {
			if t.Loc.End > t.Loc.Start {
				spans = append(spans, span{t.Loc.Start, t.Loc.End})
			}
		}
	default:
		l := omnimysql.NewLexer(sql)
		for i := 0; i <= len(sql); i++ {
			t := l.NextToken()
			if t.Type == 0 || t.End <= t.Loc {
				break
			}
			spans = append(spans, span{t.Loc, t.End})
		}
	}
	return spans
}

// isLiteral decides from the token's raw source text alone, which is what makes a
// type-free classifier possible. The per-dialect rules it encodes:
//
//	'...'            string literal everywhere
//	"..."            string in the MySQL family and StarRocks, quoted identifier in PostgreSQL
//	`...`            quoted identifier (MySQL family, StarRocks)
//	123 / 1.5        numeric literal
//	x'..' b'..'      prefixed literals: one lexer token that starts with a letter
//	$$..$$ $tag$..$  PostgreSQL dollar-quoted string
//	$1               PostgreSQL placeholder, not a literal
func isLiteral(engine storepb.Engine, raw string) bool {
	if raw == "" {
		return false
	}
	switch raw[0] {
	case '\'':
		return true
	case '"':
		return engine != storepb.Engine_POSTGRES
	case '`':
		return false
	case '$':
		return isDollarLiteral(raw)
	}
	if raw[0] >= '0' && raw[0] <= '9' {
		return true
	}
	// x'AB', b'01', n'..', e'..', u&'..', _utf8'..': one token, letter-led, quoting.
	return strings.ContainsRune(raw, '\'')
}

// isDollarLiteral separates PostgreSQL's two dollar uses: $n is a placeholder,
// $$..$$ / $tag$..$tag$ is a string.
func isDollarLiteral(raw string) bool {
	if len(raw) < 2 {
		return false
	}
	c := raw[1]
	return c < '0' || c > '9'
}

// Sanitize rewrites every literal into `?` and drops comments. It is the only
// place a statement's text is transformed, and its output is the only form that
// is ever stored.
func Sanitize(engine storepb.Engine, sql string) (Sanitized, error) {
	spans := lexSpans(engine, sql)
	if len(spans) == 0 {
		return Sanitized{}, errUnlexable
	}
	var b strings.Builder
	cursor := 0
	for _, s := range spans {
		if s.loc > cursor {
			_, _ = b.WriteString(stripComments(engine, sql[cursor:s.loc]))
		}
		raw := sql[s.loc:s.end]
		if isLiteral(engine, raw) {
			_, _ = b.WriteString(placeholder)
		} else {
			_, _ = b.WriteString(raw)
		}
		cursor = s.end
	}
	if cursor < len(sql) {
		_, _ = b.WriteString(stripComments(engine, sql[cursor:]))
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return Sanitized{}, errUnlexable
	}
	return Sanitized{Text: text, Canonical: canonicalForm(engine, text)}, nil
}

// Analyzable turns the stored text into what the dialect's analyzer can parse:
// every `?` token becomes a string literal. The rewrite walks tokens, so a `?`
// inside a quoted identifier is never touched.
func Analyzable(engine storepb.Engine, stored string) string {
	spans := lexSpans(engine, stored)
	if len(spans) == 0 {
		return stored
	}
	return replacePlaceholderTokens(stored, spans)
}

func replacePlaceholderTokens(stored string, spans []span) string {
	var b strings.Builder
	cursor := 0
	for _, s := range spans {
		if s.loc > cursor {
			_, _ = b.WriteString(stored[cursor:s.loc])
		}
		raw := stored[s.loc:s.end]
		if raw == placeholder {
			_, _ = b.WriteString(neutralLiteral)
		} else {
			_, _ = b.WriteString(raw)
		}
		cursor = s.end
	}
	if cursor < len(stored) {
		_, _ = b.WriteString(stored[cursor:])
	}
	return b.String()
}

// canonicalForm re-joins the tokens with single spaces, so that whitespace cannot
// split one statement into two fingerprints.
func canonicalForm(engine storepb.Engine, stored string) string {
	spans := lexSpans(engine, stored)
	parts := make([]string, 0, len(spans))
	for _, s := range spans {
		parts = append(parts, stored[s.loc:s.end])
	}
	return strings.Join(parts, " ")
}

// stripComments removes the comments a gap between two tokens can hold, leaving
// the whitespace around them alone. A block comment becomes one space so that
// `a/*c*/b` does not fuse into `ab`.
func stripComments(engine storepb.Engine, gap string) string {
	var b strings.Builder
	for i := 0; i < len(gap); {
		switch {
		case strings.HasPrefix(gap[i:], "--"),
			engine != storepb.Engine_POSTGRES && strings.HasPrefix(gap[i:], "#"):
			nl := strings.IndexByte(gap[i:], '\n')
			if nl < 0 {
				return b.String()
			}
			i += nl
		case strings.HasPrefix(gap[i:], "/*"):
			end := strings.Index(gap[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += 2 + end + 2
			_ = b.WriteByte(' ')
		default:
			_ = b.WriteByte(gap[i])
			i++
		}
	}
	return b.String()
}
