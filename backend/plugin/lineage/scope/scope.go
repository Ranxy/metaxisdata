package scope

import (
	"slices"
	"strings"

	"github.com/pkg/errors"
)

// Scope represents a lexical scope in SQL, tracking available tables and columns.
// Scopes are nested (e.g., subquery inside a main query).
type Scope struct {
	parent *Scope
	// Relations available in this scope, in registration order. They are a slice
	// rather than a map keyed by name because two relations can carry the same
	// name when they come from different qualifiers (db1.t and db2.t), and both
	// have to stay addressable: a map keyed by name silently keeps only one.
	relations []*TableRef
	// CTEs available in this scope
	ctes map[string]*CTEDefinition
	// Output columns for this scope (for SELECT statements)
	outputColumns []OutputColumn
}

// NewScope creates a new scope with an optional parent.
func NewScope(parent *Scope) *Scope {
	return &Scope{
		parent:        parent,
		relations:     make([]*TableRef, 0),
		ctes:          make(map[string]*CTEDefinition),
		outputColumns: make([]OutputColumn, 0),
	}
}

// AddTable adds a table reference to the current scope. Registering the same
// address twice replaces the earlier reference, which keeps a repeated
// registration idempotent; two same-named relations from different databases or
// schemas have different keys and coexist.
func (s *Scope) AddTable(ref *TableRef) {
	key := ref.Key()
	for i, existing := range s.relations {
		if existing.Key() == key {
			s.relations[i] = ref
			return
		}
	}
	s.relations = append(s.relations, ref)
}

// AddCTE adds a CTE definition to the current scope.
func (s *Scope) AddCTE(cte *CTEDefinition) {
	s.ctes[cte.Name] = cte
}

// AddOutputColumn adds an output column to the current scope.
func (s *Scope) AddOutputColumn(col OutputColumn) {
	s.outputColumns = append(s.outputColumns, col)
}

// FindRelation returns the relation a reference with this key addresses,
// searching parent scopes when the current one has none. An exactly matching key
// wins; otherwise a relation that accepts the reference (see TableRef.addresses)
// is returned.
func (s *Scope) FindRelation(key RelationKey) (*TableRef, bool) {
	for _, ref := range s.relations {
		if ref.Key() == key {
			return ref, true
		}
	}
	for _, ref := range s.relations {
		if ref.addresses(key) {
			return ref, true
		}
	}
	if s.parent != nil {
		return s.parent.FindRelation(key)
	}
	return nil, false
}

// FindTable returns the relation addressed by name alone, which is how a
// reference that names no qualifier addresses it.
func (s *Scope) FindTable(name string) (*TableRef, bool) {
	return s.FindRelation(RelationKey{Name: name})
}

// FindCTE looks up a CTE by name in the current scope and parent scopes.
func (s *Scope) FindCTE(name string) (*CTEDefinition, bool) {
	// Check current scope
	if cte, ok := s.ctes[name]; ok {
		return cte, true
	}
	// Check parent scope
	if s.parent != nil {
		return s.parent.FindCTE(name)
	}
	return nil, false
}

// Tables returns the scope's relations in registration order, without the parent
// scope's. Every relation is returned, so a wildcard expands against both db1.t
// and db2.t.
func (s *Scope) Tables() []*TableRef {
	return slices.Clone(s.relations)
}

// CTEs returns the scope's CTE definitions in name order, without the parent
// scope's.
func (s *Scope) CTEs() []*CTEDefinition {
	keys := make([]string, 0, len(s.ctes))
	for key := range s.ctes {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	out := make([]*CTEDefinition, 0, len(keys))
	for _, key := range keys {
		out = append(out, s.ctes[key])
	}
	return out
}

// ResolveColumn resolves a column reference to its source table.
// It handles both qualified (table.column) and unqualified (column) references.
func (s *Scope) ResolveColumn(colRef ColumnRef) (*ColumnRef, error) {
	resolved, _, err := s.ResolveColumnRef(colRef)
	return resolved, err
}

// ResolveColumnRef is ResolveColumn plus the relation the reference resolved to,
// so a caller can tell a base table from a query-local one without looking the
// name up again and guessing. A CTE that is in scope only as a definition is
// presented as the relation it describes. The relation is nil when this scope
// cannot identify it, which happens for a reference that was already resolved
// against a scope this one does not share.
func (s *Scope) ResolveColumnRef(colRef ColumnRef) (*ColumnRef, *TableRef, error) {
	// A reference already resolved against the scope it came from is returned
	// unchanged: a later lookup happens in a sibling scope that deliberately
	// does not contain the table it resolved to. The relation is reported when
	// this scope still knows it, because a query-local relation's lineage has to
	// be traced even then.
	if colRef.Resolved {
		if ref, ok := s.FindRelation(RelationKey{Qualifier: colRef.Schema, Name: colRef.Table}); ok {
			return &colRef, ref, nil
		}
		return &colRef, nil, nil
	}

	// If qualified, the qualifier has to agree with the relation it addresses;
	// the resolved name stays the real table name so the emitted edge names the
	// table, not the alias the query happened to use.
	if colRef.Table != "" {
		if ref, ok := s.FindRelation(RelationKey{Qualifier: colRef.Schema, Name: colRef.Table}); ok {
			tableName := ref.Table
			if ref.IsCTE || ref.IsSubquery {
				tableName = ref.addressName()
			}
			return &ColumnRef{
				Schema: ref.Schema,
				Table:  tableName,
				Column: colRef.Column,
			}, ref, nil
		}
		// A CTE that is not registered as a relation still describes its columns.
		if cte, ok := s.FindCTE(colRef.Table); ok {
			return &ColumnRef{
				Schema: "",
				Table:  cte.Name,
				Column: colRef.Column,
			}, cteTableRef(cte), nil
		}
		return nil, nil, errors.Errorf("table not found: %s", colRef.Table)
	}

	// Unqualified column - search relations in scope.
	//
	// The candidates are ordered by name rather than read in registration order:
	// an unqualified name that several relations could satisfy would otherwise
	// pick a relation by how the FROM clause happened to be written, which makes
	// the emitted edge nondeterministic for NATURAL JOIN and similar shapes.
	candidates := s.sortedRelations()

	// With several relations in scope, catalog metadata decides: the relation
	// that actually owns the column wins instead of the first one by name. The
	// rule is skipped whenever any relation lacks metadata, so the ordering rule
	// below stays the deterministic fallback for the metadata-less case.
	if len(candidates) > 1 {
		if resolved, ref, decided, err := resolveByColumnMetadata(candidates, colRef); decided {
			return resolved, ref, err
		}
	}

	// Fall back to the first relation by name, which also covers a single
	// relation and every scope whose metadata is unavailable.
	if len(candidates) > 0 {
		ref := candidates[0]
		return &ColumnRef{
			Schema: ref.Schema,
			Table:  ref.Table,
			Column: colRef.Column,
		}, ref, nil
	}

	// Also check CTEs (also ordered for determinism).
	if ctes := s.sortedCTEs(); len(ctes) > 0 {
		cte := ctes[0]
		return &ColumnRef{
			Schema: "",
			Table:  cte.Name,
			Column: colRef.Column,
		}, cteTableRef(cte), nil
	}

	// Try parent scope
	if s.parent != nil {
		return s.parent.ResolveColumnRef(colRef)
	}
	return nil, nil, errors.Errorf("column not found: %s", colRef.Column)
}

// sortedRelations orders the scope's relations by the name a reference uses and
// then by qualifier. The name order keeps the alphabetical rule this scope had
// when it was keyed by name; the qualifier is the tie-break that makes the
// choice deterministic now that two relations can share a name.
func (s *Scope) sortedRelations() []*TableRef {
	sorted := slices.Clone(s.relations)
	slices.SortStableFunc(sorted, func(a, b *TableRef) int {
		if cmp := strings.Compare(a.addressName(), b.addressName()); cmp != 0 {
			return cmp
		}
		return strings.Compare(a.Schema, b.Schema)
	})
	return sorted
}

func (s *Scope) sortedCTEs() []*CTEDefinition {
	keys := make([]string, 0, len(s.ctes))
	for key := range s.ctes {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	out := make([]*CTEDefinition, 0, len(keys))
	for _, key := range keys {
		out = append(out, s.ctes[key])
	}
	return out
}

// cteTableRef presents a CTE definition as the relation a reference resolved to,
// so a caller handles a CTE and a base table the same way.
func cteTableRef(cte *CTEDefinition) *TableRef {
	return &TableRef{
		Table:   cte.Name,
		Alias:   cte.Name,
		IsCTE:   true,
		Lineage: cte.Lineage,
	}
}

// resolveByColumnMetadata picks the relation that owns an unqualified column
// from catalog metadata. decided is false when metadata is incomplete for at
// least one relation, in which case the caller falls back to its ordering rule.
// When every relation has metadata and none owns the column, the reference is
// unresolvable: an error is returned so the caller drops the edge instead of
// attributing it to an arbitrary table.
func resolveByColumnMetadata(refs []*TableRef, colRef ColumnRef) (*ColumnRef, *TableRef, bool, error) {
	allKnown := true
	for _, ref := range refs {
		if ref.IsSubquery || ref.IsCTE {
			// A temporary relation's columns are described by its lineage, which
			// the caller resolves separately; do not second-guess it here.
			return nil, nil, false, nil
		}
		names := ref.ColumnNames()
		if names == nil {
			allKnown = false
			continue
		}
		for _, name := range names {
			if strings.EqualFold(name, colRef.Column) {
				return &ColumnRef{Schema: ref.Schema, Table: ref.Table, Column: colRef.Column}, ref, true, nil
			}
		}
	}
	if allKnown {
		return nil, nil, true, errors.Errorf("column %q not found in any table in scope", colRef.Column)
	}
	return nil, nil, false, nil
}

// GetOutputColumns returns the output columns of this scope.
func (s *Scope) GetOutputColumns() []OutputColumn {
	return s.outputColumns
}

// Parent returns the parent scope.
func (s *Scope) Parent() *Scope {
	return s.parent
}

// SetOutputColumn updates an output column at a specific index.
func (s *Scope) SetOutputColumn(index int, col OutputColumn) {
	if index >= 0 && index < len(s.outputColumns) {
		s.outputColumns[index] = col
	}
}
