// Package sqlsanitize turns one application SQL statement into the two forms the
// application-SQL ingest path keeps: a literal-free text that is safe to store, and a
// canonical form whose hash is the statement's deduplication key.
//
// The rules it applies are N1-N3 of .agents/docs/application-sql-ingestion.md ("Decisions"):
//
//	N1  every literal becomes `?`, and a PostgreSQL `$n` is rewritten to the same `?`
//	    so that one statement has one stored form however the application wrote it.
//	    An application may already have replaced its literals, which is why the
//	    rewrite has to be idempotent
//	N2  comments are removed; all other spacing is preserved byte for byte
//	N3  identifiers and keywords are copied verbatim, quotes and case included
//
// N4-N6 — collapsing IN lists, VALUES row groups, and constant-only expressions — are
// deliberately not applied yet. They change what counts as one statement, so they
// belong with a corpus of real application SQL that can measure over-merging; the
// design's section 13.7 records that corpus as still to come. Applying any of them
// bumps Version and requires the one-off re-fingerprint pass the design describes.
//
// Classification reads the token's source text rather than omni's token-type
// constants, which are unexported in every dialect package. See token.go.
package sqlsanitize

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

const (
	// Version identifies the normalization rules applied by this package. It is
	// stored beside every fingerprint because a change to the rules silently splits
	// one statement into several rows until the re-fingerprint pass has run.
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

// ErrUnlexable reports a statement that could not be tokenized: empty input, or a
// dialect whose lexer stopped on a malformed token. Such a statement is never
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
	spans := tokenSpans(engine, sql)
	if len(spans) == 0 {
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

// canonicalForm re-joins the tokens with single spaces, so that whitespace cannot
// split one statement into two fingerprints.
func canonicalForm(engine storepb.Engine, text string) string {
	spans := tokenSpans(engine, text)
	parts := make([]string, 0, len(spans))
	for _, s := range spans {
		parts = append(parts, text[s.loc:s.end])
	}
	return strings.Join(parts, " ")
}
