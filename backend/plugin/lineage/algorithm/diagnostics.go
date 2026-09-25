package algorithm

import (
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// diagnosticLimit bounds the notes one analysis reports, so an input full of
// unresolved references cannot produce unbounded output; the count left out is
// reported instead.
const diagnosticLimit = 10

// Diagnostics collects what an analysis could not represent. A caller records the
// notes beside the edges it did find — the runner stores them on the lineage
// version — which is what makes a partial result distinguishable from a complete
// one: the edges alone cannot say whether an edge is missing because the SQL never
// had it or because the analyzer dropped it.
//
// A note is a model.Diagnostic rather than a sentence, so a caller can act on the
// category and point at the reference instead of re-reading its own message.
type Diagnostics struct {
	seen    map[model.Diagnostic]struct{}
	notes   []model.Diagnostic
	omitted int
}

// NewDiagnostics creates an empty note store.
func NewDiagnostics() *Diagnostics {
	return &Diagnostics{seen: make(map[model.Diagnostic]struct{})}
}

// NotModelled reports a statement or clause shape the analyzer does not model.
// detail carries the cause when the shape needs one — the parser defect behind a
// skipped clause — and is empty for a shape that speaks for itself.
func (d *Diagnostics) NotModelled(subject, detail string) {
	d.add(model.Diagnostic{Category: model.DiagnosticNotModelled, Subject: subject, Detail: detail})
}

// Unresolved reports a reference that resolved to nothing, together with the
// clause it was written in.
func (d *Diagnostics) Unresolved(clause string, ref scope.ColumnRef) {
	d.add(model.Diagnostic{
		Category:  model.DiagnosticUnresolved,
		Subject:   clause,
		Reference: refName(ref.Table, ref.Column),
	})
}

// Ambiguous reports a reference a single-valued position could not choose between,
// which is an unqualified name several relations in scope own.
func (d *Diagnostics) Ambiguous(clause string, ref scope.ColumnRef) {
	d.add(model.Diagnostic{
		Category:  model.DiagnosticAmbiguous,
		Subject:   clause,
		Reference: refName(ref.Table, ref.Column),
	})
}

// UnresolvedQualifier reports a wildcard whose qualifier names no relation in
// scope. There is no column to name, so the note names the qualifier.
func (d *Diagnostics) UnresolvedQualifier(clause, qualifier string) {
	d.add(model.Diagnostic{
		Category:  model.DiagnosticUnresolved,
		Subject:   clause,
		Reference: qualifier,
	})
}

// CatalogUnavailable reports a metadata lookup that failed. The analysis carries
// on with the relation's columns unknown, which is what makes a wildcard fall back
// to a bulk edge, so the note is what keeps a catalog outage from reading as a
// complete analysis: the edges of a degraded result and of a good one look alike.
func (d *Diagnostics) CatalogUnavailable(relation string, err error) {
	d.add(model.Diagnostic{
		Category:  model.DiagnosticCatalogUnavailable,
		Reference: relation,
		Detail:    err.Error(),
	})
}

// refName names a reference the way the SQL writes it, leaving out the qualifier
// when the reference has none.
func refName(table, column string) string {
	switch {
	case table == "":
		return column
	case column == "":
		return table
	default:
		return table + "." + column
	}
}

// add records one note, keeping the first occurrence of each. A nil store accepts
// notes silently, so a caller that only wants the edges can pass none.
func (d *Diagnostics) add(note model.Diagnostic) {
	if d == nil {
		return
	}
	if d.seen == nil {
		d.seen = make(map[model.Diagnostic]struct{})
	}
	if _, ok := d.seen[note]; ok {
		return
	}
	d.seen[note] = struct{}{}
	if len(d.notes) >= diagnosticLimit {
		d.omitted++
		return
	}
	d.notes = append(d.notes, note)
}

// AppendTo adds the notes to a caller's own failure reasons, so a statement that
// fails whole does not hide what the analysis learned before it failed.
func (d *Diagnostics) AppendTo(reasons []string) []string {
	if d == nil || len(d.notes) == 0 {
		return reasons
	}
	return append(reasons, model.FormatDiagnostics(d.notes, d.omitted))
}

// Notes returns the notes in the order they were first reported, and how many the
// limit left out. It returns nil when nothing was dropped, which is the signal a
// caller reads as "this analysis is complete".
func (d *Diagnostics) Notes() ([]model.Diagnostic, int) {
	if d == nil || len(d.notes) == 0 {
		return nil, 0
	}
	notes := make([]model.Diagnostic, len(d.notes))
	copy(notes, d.notes)
	return notes, d.omitted
}
