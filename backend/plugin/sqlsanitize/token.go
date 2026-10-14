package sqlsanitize

import (
	"regexp"
	"strings"
	"unicode/utf8"

	omnimssql "github.com/bytebase/omni/mssql/parser"
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

// trimTrailingSpace moves a token's end back over whitespace the lexer swallowed.
//
// PostgreSQL's lexer hands a string constant the whitespace that follows it: in
// `status = 'new' AND total > 10` the constant comes back as one token spanning `'new' `. A
// span taken at face value would replace that space along with the literal and turn `= ? AND`
// into `= ?AND`. No token's own text ever ends in whitespace, so trimming it is safe.
func trimTrailingSpace(sql string, s span) span {
	for s.end > s.loc {
		switch sql[s.end-1] {
		case ' ', '\t', '\r', '\n':
			s.end--
		default:
			return s
		}
	}
	return s
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
	// Token type, so the loops are written out.
	switch engine {
	case storepb.Engine_POSTGRES:
		l := omnipg.NewLexer(sql)
		for i := 0; i <= len(sql); i++ {
			t := l.NextToken()
			if t.Type == 0 || t.End <= t.Loc {
				break
			}
			spans = append(spans, trimTrailingSpace(sql, span{t.Loc, t.End}))
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
				spans = append(spans, trimTrailingSpace(sql, span{t.Loc.Start, t.Loc.End}))
			}
		}
	case storepb.Engine_MSSQL:
		// The T-SQL lexer, walked one token at a time rather than through `Tokenize`: that
		// helper collects a slice first, which is one allocation per token more than this
		// needs. Its lexer is what keeps this dialect's literals and identifiers whole — it
		// is the only one that reads `N'…'`, `[…]`, `"…"`, `#tmp`, `0x1f`, `$12.50` and
		// `@@ROWCOUNT` as one token each. Falling through to the MySQL family's lexer split
		// several of them, so `N'abc'` was stored as `N?`.
		//
		// It is also the only lexer here that says what it could not finish: an unterminated
		// string, quoted identifier, bracketed name or block comment sets Err and hands back a
		// token that swallows the rest of the input, so an empty tail is no evidence that a
		// rule looked at every byte. A statement it abandoned is refused, the way StarRocks
		// refuses its own lex errors.
		l := omnimssql.NewLexer(sql)
		for i := 0; i <= len(sql); i++ {
			t := l.NextToken()
			if l.Err != nil {
				return nil
			}
			if t.Type == 0 || t.End <= t.Loc {
				break
			}
			spans = append(spans, trimTrailingSpace(sql, span{t.Loc, t.End}))
		}
	default:
		l := omnimysql.NewLexer(sql)
		for i := 0; i <= len(sql); i++ {
			t := l.NextToken()
			if t.Type == 0 || t.End <= t.Loc {
				break
			}
			spans = append(spans, trimTrailingSpace(sql, span{t.Loc, t.End}))
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
//	                 and in T-SQL, whose connections default to QUOTED_IDENTIFIER ON
//	[...]            quoted identifier: T-SQL hands it back whole, brackets included
//	`...`            quoted identifier (MySQL family, StarRocks)
//	#...             a temporary table's name in T-SQL, a line comment in the MySQL family
//	123 / 1.5 / .5   numeric literal, including the leading-dot spelling
//	$12.50 £10 €7     T-SQL money literals: a currency symbol and an optionally signed number
//	$action           T-SQL pseudo-column, not a literal
//	0x1f              T-SQL binary literal
//	x'..' b'..'      prefixed literals: one lexer token that starts with a letter
//	$$..$$ $tag$..$  PostgreSQL dollar-quoted string
//	$1               PostgreSQL placeholder, not a literal
//	?                the wire placeholder itself, left alone
func isLiteral(engine storepb.Engine, raw string) bool {
	if raw == "" || raw == Placeholder {
		return false
	}
	// T-SQL spells money as a currency symbol followed by digits, and the symbol is not a
	// digit, so the rules below would read it as an identifier and store the literal.
	if engine == storepb.Engine_MSSQL && tSQLMoneyLiteral(raw) {
		return true
	}
	switch raw[0] {
	case '\'':
		return true
	case '"':
		return engine != storepb.Engine_POSTGRES && engine != storepb.Engine_MSSQL
	case '[':
		// A bracketed identifier, which is the only way T-SQL spells a name that holds a
		// space or a reserved word. Its text can contain a quote — `[it's]` — so the
		// letter-led rule below would read it as a literal.
		return false
	case '#':
		return false
	case '`':
		return false
	case '$':
		// A `$`-led token that is not money is a T-SQL pseudo-column (`$action`,
		// `$identity`), and in PostgreSQL it is a numbered placeholder.
		return engine != storepb.Engine_MSSQL && !dollarParam.MatchString(raw)
	case '@':
		// @var and @@system_var are variables, not literals.
		return false
	case '.':
		// `.5` is a number; the tokenizer keeps the dot, so the digit after it decides.
		return len(raw) > 1 && raw[1] >= '0' && raw[1] <= '9'
	}
	if raw[0] >= '0' && raw[0] <= '9' {
		return true
	}
	// x'AB', b'01', n'..', e'..', u&'..', _utf8'..': one token, letter-led, quoting.
	return strings.ContainsRune(raw, '\'')
}

// tSQLMoneyLiteral reports whether a token is a T-SQL money literal: a currency symbol whose
// number is optionally signed, as the T-SQL lexer's own money amount is — `$-4.78`, `$+2`,
// `$ 4`, `$.5`. `$` is the one ASCII symbol it accepts; the rest are not ASCII (`£`, `¢`,
// `￥`, `＄`, `￡` …), and rather than enumerate upstream's table this reads the shape: a first
// rune that is not a letter or a digit, followed by a number. That keeps `$action` (a
// pseudo-column), `£abc` (an identifier) and an identifier merely written in another script
// (`名前`) out of the rule, because none of them has a number after its first rune.
func tSQLMoneyLiteral(raw string) bool {
	if raw == "" {
		return false
	}
	symbol, size := utf8.DecodeRuneInString(raw)
	if symbol != '$' && symbol < utf8.RuneSelf {
		// An ASCII token that is not `$`-led begins with a letter, a digit or punctuation, and
		// none of those is a currency symbol.
		return false
	}
	rest := strings.TrimLeft(raw[size:], " \t")
	rest = strings.TrimLeft(rest, "+-")
	if rest == "" {
		return false
	}
	return (rest[0] >= '0' && rest[0] <= '9') || rest[0] == '.'
}

// stripComments removes the comments a gap between two tokens can hold and leaves the
// whitespace around them alone. A block comment becomes one space so that `a/*c*/b`
// does not fuse into `ab`.
//
// `#` is excluded for T-SQL, where it is the first character of a temporary table's name:
// reading it as a line comment deleted everything after `FROM #tmp` from the stored text
// and from the fingerprint. PostgreSQL excludes it for its own reason, `#` being an
// operator character there.
func stripComments(engine storepb.Engine, gap string) string {
	var b strings.Builder
	b.Grow(len(gap))
	for i := 0; i < len(gap); {
		switch {
		case strings.HasPrefix(gap[i:], "--"),
			engine != storepb.Engine_POSTGRES && engine != storepb.Engine_MSSQL && strings.HasPrefix(gap[i:], "#"):
			nl := strings.IndexByte(gap[i:], '\n')
			if nl < 0 {
				return b.String()
			}
			i += nl
		case strings.HasPrefix(gap[i:], "/*"):
			consumed := blockCommentEnd(gap[i:])
			if consumed < 0 {
				return b.String()
			}
			i += consumed
			_ = b.WriteByte(' ')
		default:
			_ = b.WriteByte(gap[i])
			i++
		}
	}
	return b.String()
}

// blockCommentEnd returns the length of the block comment starting at s, or -1 when it is
// never closed. PostgreSQL and T-SQL nest block comments, so stopping at the first `*/` would
// leave the rest of the comment in the stored text, and the same mistake makes the
// closed-comment check in Sanitize reject a valid statement. The MySQL family and StarRocks
// do not nest: their lexers ended the comment at the first `*/`, so a gap between tokens never
// holds a nested one, and a `*/` left in a gap is not part of a comment at all.
func blockCommentEnd(s string) int {
	depth := 0
	for i := 0; i+1 < len(s); {
		switch {
		case s[i] == '/' && s[i+1] == '*':
			depth++
			i += 2
		case s[i] == '*' && s[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return -1
}
