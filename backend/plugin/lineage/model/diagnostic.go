package model

import (
	"fmt"
	"strings"
)

// DiagnosticCategory classifies what an analysis could not represent. The
// categories call for different action: a shape the analyzer does not model is a
// coverage gap in the analyzer, while a reference that does not resolve is
// usually the SQL naming something it cannot see.
type DiagnosticCategory int

const (
	// DiagnosticNotModelled is a statement or clause shape the analyzer does not
	// model yet.
	DiagnosticNotModelled DiagnosticCategory = iota + 1
	// DiagnosticUnresolved is a reference that resolved to nothing.
	DiagnosticUnresolved
	// DiagnosticAmbiguous is a reference a single-valued position could not
	// choose between, which is an unqualified name several relations in scope own.
	DiagnosticAmbiguous
	// DiagnosticCatalogUnavailable is a metadata lookup that failed. The analysis
	// carried on with the relation's columns unknown, so the note is what keeps a
	// degraded result distinguishable from a complete one.
	DiagnosticCatalogUnavailable
)

// Diagnostic is one thing an analysis could not represent, kept as data rather
// than as a sentence so a caller can act on the category and point at the
// reference. String renders it the way an error message carries it, which is what
// keeps a stored version message and a caller's report the same text.
type Diagnostic struct {
	Category DiagnosticCategory
	// Subject names what the diagnostic is about, the way the analyzer names it:
	// the statement or clause shape it does not model ("MERGE", "WITH before
	// INSERT"), or the clause a reference was written in ("a CTE body").
	Subject string
	// Reference is the identifier a reference diagnostic could not resolve, the
	// way the SQL writes it ("t.c", "t", "c"). It is empty for a diagnostic about
	// a shape rather than a reference.
	Reference string
	// Detail carries the cause for a diagnostic that needs one, such as the
	// parser defect that makes a clause unanalyzable or the catalog error behind
	// a degraded lookup.
	Detail string
}

// String renders the diagnostic as the sentence an error message carries.
func (d Diagnostic) String() string {
	switch d.Category {
	case DiagnosticNotModelled:
		if d.Detail != "" {
			return "not modelled: " + d.Subject + ": " + d.Detail
		}
		return "not modelled: " + d.Subject
	case DiagnosticUnresolved:
		return "unresolved reference in " + d.Subject + ": " + d.Reference
	case DiagnosticAmbiguous:
		return "ambiguous reference in " + d.Subject + ": " + d.Reference
	case DiagnosticCatalogUnavailable:
		return "catalog lookup failed for " + d.Reference + ": " + d.Detail
	default:
		return "unclassified diagnostic"
	}
}

// FormatDiagnostics renders notes as the "; "-joined text an error message
// carries. omitted is how many the collector's limit left out: naming the count
// says the result is partial without letting a statement full of unresolved
// references grow the text without bound.
func FormatDiagnostics(notes []Diagnostic, omitted int) string {
	parts := make([]string, 0, len(notes)+1)
	for _, note := range notes {
		parts = append(parts, note.String())
	}
	if omitted > 0 {
		parts = append(parts, fmt.Sprintf("%d more not listed", omitted))
	}
	return strings.Join(parts, "; ")
}
