package algorithm

import (
	"fmt"
	"slices"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// diagnosticLimit bounds the notes one analysis reports, so an input full of
// unresolved references cannot produce an unbounded message; the count left out
// is summarized instead.
const diagnosticLimit = 10

// Diagnostics collects what an analysis could not represent. A caller records the
// notes beside the edges it did find — the runner stores them on the lineage
// version — which is what makes a partial result distinguishable from a complete
// one: the edges alone cannot say whether an edge is missing because the SQL never
// had it or because the analyzer dropped it.
//
// Notes are classified by cause because the two causes call for different action:
// a shape this analyzer does not model yet is a coverage gap in this package,
// while a reference that does not resolve is usually the SQL naming something the
// analyzer cannot see — a relation outside the clause's scope, or one the registry
// does not have.
type Diagnostics struct {
	seen    map[string]struct{}
	notes   []string
	omitted int
}

// NewDiagnostics creates an empty note store.
func NewDiagnostics() *Diagnostics {
	return &Diagnostics{seen: make(map[string]struct{})}
}

// NotModelled reports a statement or clause shape the analyzer does not model.
func (d *Diagnostics) NotModelled(what string) {
	d.add("not modelled: " + what)
}

// Unresolved reports a reference that resolved to nothing, together with the
// clause it was written in.
func (d *Diagnostics) Unresolved(clause string, ref scope.ColumnRef) {
	d.add("unresolved reference in " + clause + ": " + refName(ref.Table, ref.Column))
}

// Ambiguous reports a reference a single-valued position could not choose between,
// which is an unqualified name several relations in scope own.
func (d *Diagnostics) Ambiguous(clause string, ref scope.ColumnRef) {
	d.add("ambiguous reference in " + clause + ": " + refName(ref.Table, ref.Column))
}

// UnresolvedQualifier reports a wildcard whose qualifier names no relation in
// scope. There is no column to name, so the note names the qualifier.
func (d *Diagnostics) UnresolvedQualifier(clause, qualifier string) {
	d.add("unresolved reference in " + clause + ": " + qualifier)
}

// CatalogUnavailable reports a metadata lookup that failed. The analysis carries
// on with the relation's columns unknown, which is what makes a wildcard fall back
// to a bulk edge, so the note is what keeps a catalog outage from reading as a
// complete analysis: the edges of a degraded result and of a good one look alike.
func (d *Diagnostics) CatalogUnavailable(relation string, err error) {
	d.add("catalog lookup failed for " + relation + ": " + err.Error())
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
func (d *Diagnostics) add(note string) {
	if d == nil {
		return
	}
	if d.seen == nil {
		d.seen = make(map[string]struct{})
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
	return append(reasons, d.Messages()...)
}

// Messages returns the notes in the order they were first reported, followed by
// how many the limit left out. It returns nil when nothing was dropped, which is
// the signal a caller reads as "this analysis is complete".
func (d *Diagnostics) Messages() []string {
	if d == nil || len(d.notes) == 0 {
		return nil
	}
	out := slices.Clone(d.notes)
	if d.omitted > 0 {
		out = append(out, fmt.Sprintf("%d more not listed", d.omitted))
	}
	return out
}
