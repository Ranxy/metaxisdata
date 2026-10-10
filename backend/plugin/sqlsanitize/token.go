package sqlsanitize

import (
	"regexp"
	"strings"

	omnimysql "github.com/bytebase/omni/mysql/parser"
	omnipg "github.com/bytebase/omni/pg/parser"
	omnisr "github.com/bytebase/omni/starrocks/parser"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// span is one token's byte range in the source text. Comments never appear as tokens —
// every upstream lexer skips them — so the gaps between spans are where comments and
// whitespace live. That is what lets the two dialect families share one sanitizer
// without either of them teaching us its comment syntax.
type span struct {
	loc, end int
}

// tokenSpans returns every token's byte range for one dialect.
//
// Locations are all this reads. Reading the token *type* would mean depending on
// tokSCONST / tokICONST and their siblings, which are unexported in every dialect
// package: hardcoding their numbers would fail silently if upstream ever renumbered
// them. The source slice is stable regardless of how a lexer decodes Str.
func tokenSpans(engine storepb.Engine, sql string) []span {
	spans := make([]span, 0, 64)
	// A token consumes at least one byte, so the loop bound cannot cut off a real
	// token; it only stops a lexer that fails to make progress. Each lexer has its own
	// Token type, so the three loops are written out.
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
		// StarRocks exposes a one-shot tokenizer and is the only dialect that reports
		// its own lex errors.
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

// dollarParam matches PostgreSQL's numbered placeholder, `$1`.
var dollarParam = regexp.MustCompile(`^\$\d+$`)

// isPlaceholder reports a token that already is a placeholder in some dialect's own
// syntax. PostgreSQL numbers its placeholders where the wire contract uses `?`, and
// an application built on a PostgreSQL driver will naturally send `$1`; both have to
// land on one stored form or the same statement deduplicates twice.
func isPlaceholder(raw string) bool {
	return raw == Placeholder || dollarParam.MatchString(raw)
}

// isLiteral decides from the token's raw source text alone. The rules it encodes:
//
//	'...'            string literal everywhere
//	"..."            string in the MySQL family and StarRocks, quoted identifier in PostgreSQL
//	`...`            quoted identifier (MySQL family, StarRocks)
//	123 / 1.5        numeric literal
//	x'..' b'..'      prefixed literals: one lexer token that starts with a letter
//	$$..$$ $tag$..$  PostgreSQL dollar-quoted string
//	$1               PostgreSQL placeholder, not a literal
//	?                the wire placeholder itself, left alone
func isLiteral(engine storepb.Engine, raw string) bool {
	if raw == "" || raw == Placeholder {
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
		return !dollarParam.MatchString(raw)
	case '@':
		// @var and @@system_var are variables, not literals.
		return false
	}
	if raw[0] >= '0' && raw[0] <= '9' {
		return true
	}
	// x'AB', b'01', n'..', e'..', u&'..', _utf8'..': one token, letter-led, quoting.
	return strings.ContainsRune(raw, '\'')
}

// stripComments removes the comments a gap between two tokens can hold and leaves the
// whitespace around them alone. A block comment becomes one space so that `a/*c*/b`
// does not fuse into `ab`.
func stripComments(engine storepb.Engine, gap string) string {
	var b strings.Builder
	b.Grow(len(gap))
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
