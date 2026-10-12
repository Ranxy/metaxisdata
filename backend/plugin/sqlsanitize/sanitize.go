// Package sqlsanitize turns one application SQL statement into the two forms the
// application-SQL ingest path keeps: a literal-free text that is safe to store, and a
// canonical form whose hash is the statement's deduplication key.
//
// The rules it applies are:
//
//	N1  every literal becomes `?`, and a PostgreSQL `$n` is rewritten to the same `?`
//	    so that one statement has one stored form however the application wrote it.
//	    An application may already have replaced its literals, which is why the
//	    rewrite has to be idempotent
//	N2  comments are removed; all other spacing is preserved byte for byte
//	N3  identifiers and keywords are copied verbatim, quotes and case included
//	N4  an IN list of placeholders is one placeholder
//	N5  a VALUES list of repeated placeholder groups is one group
//
// N1-N3 shape both forms; N4 and N5 shape the fingerprint alone, so the stored text keeps
// what the application wrote. Nothing else folds: an operator and a placeholder count are
// part of a statement's identity. See fold.go.
// Any later change to these rules must bump Version, so that a deployment can tell
// which rows were fingerprinted under which rules.
//
// Classification reads the token's source text rather than omni's token-type
// constants, which are unexported in every dialect package. See token.go.
package sqlsanitize

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

const (
	// Version identifies the rules applied by this package, and is stored beside every
	// fingerprint. It is 1 because this is the first rule set to run anywhere: the
	// foldings in fold.go were added while the feature was still unreleased, so they are
	// part of version 1 rather than a second version with rows to migrate.
	//
	// Its whole purpose is the next change. Rewriting a rule splits one statement into
	// rows fingerprinted under the old one, and a deployment that already holds rows
	// needs to know that happened; Version is how it finds out.
	Version = 1

	// Placeholder is the wire and stored form of every literal. Applications send
	// it, and it is the only form that is ever persisted.
	Placeholder = "?"

	// neutralLiteral is what each placeholder becomes before analysis. A string
	// literal is the only substitute that parses in every position: MySQL's
	// LOAD DATA and JSON_TABLE PATH, PostgreSQL's COPY source and INTERVAL, and
	// StarRocks' COPY INTO all demand a string and reject NULL, 0 and $n there.
	// '?' rather than '' keeps a derived column name readable — `price * '?'`
	// rather than `price * ''`.
	neutralLiteral = "'?'"
)

// ErrUnlexable reports a statement that could not be tokenized: empty input, text that is not
// UTF-8 at all, or a dialect whose lexer stopped on a malformed token. Such a statement is never
// stored.
var ErrUnlexable = errors.New("statement could not be lexed")

// Statement is one statement in the two forms the ingest path keeps.
type Statement struct {
	// Text is what gets stored: every literal replaced by Placeholder, comments
	// removed, and all other spacing left as the application wrote it. Spacing is
	// preserved because the analyzer names an unaliased expression column after its
	// source text, and that name is stored in column_lineage.
	Text string

	// Canonical is Text with its tokens re-joined by single spaces. It exists only to
	// be hashed: whitespace must not split one statement into two.
	Canonical string

	// Fingerprint is the statement's deduplication key, the sha256 of Canonical.
	Fingerprint string
}

// Sanitize rewrites one statement. The caller splits a multi-statement script first:
// this package is defined for exactly one statement, like the analyzers it feeds.
func Sanitize(engine storepb.Engine, sql string) (Statement, error) {
	// The stored text goes into `TEXT` columns of a UTF-8 database, and a byte that is not UTF-8
	// has no valid form to store. Both dialect families copy an identifier verbatim, so an
	// invalid byte inside one would be carried all the way to the write and fail there, as a
	// store error the application would be told to retry forever.
	if !utf8.ValidString(sql) {
		return Statement{}, ErrUnlexable
	}
	spans := tokenSpans(engine, sql)
	if len(spans) == 0 {
		return Statement{}, ErrUnlexable
	}
	// The MySQL-family and PostgreSQL lexers stop on a malformed token rather than
	// reporting one, and only the bytes inside a token are ever inspected. Whatever follows
	// the last token is copied verbatim, so accepting a statement such a lexer abandoned
	// would store bytes no rule looked at — an unterminated string in PostgreSQL is not a
	// token at all, and `SELECT 'a` would store the literal. A statement that cannot be
	// tokenized to its end is a statement that cannot be sanitized. StarRocks reports its
	// own lex errors, so it is already refused above.
	if tail := sql[spans[len(spans)-1].end:]; strings.TrimSpace(stripComments(engine, tail)) != "" {
		return Statement{}, ErrUnlexable
	}
	text := strings.TrimSpace(rewriteLiterals(engine, sql, spans))
	if text == "" {
		return Statement{}, ErrUnlexable
	}
	canonical := canonicalForm(engine, text)
	sum := sha256.Sum256([]byte(canonical))
	return Statement{
		Text:        text,
		Canonical:   canonical,
		Fingerprint: hex.EncodeToString(sum[:]),
	}, nil
}

// Analyzable returns the text an analyzer can parse. Every placeholder becomes a
// string literal, because the MySQL family's grammar has no production that accepts a
// placeholder at all. The rewrite walks tokens, so a `?` inside a quoted identifier is
// never touched.
func Analyzable(engine storepb.Engine, text string) string {
	spans := tokenSpans(engine, text)
	if len(spans) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	cursor := 0
	for _, s := range spans {
		if s.loc > cursor {
			_, _ = b.WriteString(text[cursor:s.loc])
		}
		raw := text[s.loc:s.end]
		if raw == Placeholder {
			_, _ = b.WriteString(neutralLiteral)
		} else {
			_, _ = b.WriteString(raw)
		}
		cursor = s.end
	}
	if cursor < len(text) {
		_, _ = b.WriteString(text[cursor:])
	}
	return b.String()
}

// rewriteLiterals splices the source text: a literal span becomes Placeholder, and the
// gaps between tokens — where comments and whitespace live — lose their comments and
// keep everything else.
func rewriteLiterals(engine storepb.Engine, sql string, spans []span) string {
	var b strings.Builder
	b.Grow(len(sql))
	cursor := 0
	for _, s := range spans {
		if s.loc > cursor {
			_, _ = b.WriteString(stripComments(engine, sql[cursor:s.loc]))
		}
		raw := sql[s.loc:s.end]
		if isLiteral(engine, raw) || isPlaceholder(raw) {
			_, _ = b.WriteString(Placeholder)
		} else {
			_, _ = b.WriteString(raw)
		}
		cursor = s.end
	}
	if cursor < len(sql) {
		_, _ = b.WriteString(stripComments(engine, sql[cursor:]))
	}
	return b.String()
}

// canonicalForm turns the stored text into the fingerprint's input: its tokens joined by single
// spaces, with the two list shapes folded away. Whitespace alone must not split one statement into
// two, and neither must the length of an IN list or the size of a batch insert. Nothing else is
// normalized — a differently written constant or a different operator is a different statement.
// See fold.go.
func canonicalForm(engine storepb.Engine, text string) string {
	spans := tokenSpans(engine, text)
	parts := make([]string, 0, len(spans))
	for _, s := range spans {
		parts = append(parts, text[s.loc:s.end])
	}
	parts = foldInLists(parts)
	parts = foldValueGroups(parts)
	return strings.Join(parts, " ")
}
