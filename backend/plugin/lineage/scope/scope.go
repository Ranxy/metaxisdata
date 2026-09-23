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
// name up again and guessing. It reports the first of ResolveColumnRefs'
// resolutions, which is the only one for every reference except an unqualified
// name that several relations in scope own. A CTE that is in scope only as a
// definition is presented as the relation it describes. The relation is nil when
// this scope cannot identify it, which happens for a reference that was already
// resolved against a scope this one does not share.
func (s *Scope) ResolveColumnRef(colRef ColumnRef) (*ColumnRef, *TableRef, error) {
	resolved, err := s.ResolveColumnRefs(colRef)
	if err != nil {
		return nil, nil, err
	}
	return &resolved[0].Ref, resolved[0].Relation, nil
}

// ResolveColumnRefs resolves a column reference to every relation in scope that
// owns it, in the scope's name order. A qualified reference and one that was
// already resolved elsewhere have exactly one resolution; an unqualified name
// has one per owning relation, which is how a coalesced USING or NATURAL JOIN
// column reports both sides.
func (s *Scope) ResolveColumnRefs(colRef ColumnRef) ([]ResolvedColumn, error) {
	// A reference already resolved against the scope it came from is returned
	// unchanged: a later lookup happens in a sibling scope that deliberately
	// does not contain the table it resolved to. The relation is reported when
	// this scope still knows it, because a query-local relation's lineage has to
	// be traced even then.
	if colRef.Resolved {
		if ref, ok := s.FindRelation(RelationKey{Qualifier: colRef.Schema, Name: colRef.Table}); ok {
			return []ResolvedColumn{{Ref: colRef, Relation: ref}}, nil
		}
		return []ResolvedColumn{{Ref: colRef}}, nil
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
			return []ResolvedColumn{{
				Ref: ColumnRef{
					Schema: ref.Schema,
					Table:  tableName,
					Column: colRef.Column,
				},
				Relation: ref,
			}}, nil
		}
		// A CTE that is not registered as a relation still describes its columns.
		if cte, ok := s.FindCTE(colRef.Table); ok {
			return []ResolvedColumn{{
				Ref:      ColumnRef{Table: cte.Name, Column: colRef.Column},
				Relation: cteTableRef(cte),
			}}, nil
		}
		return nil, errors.Errorf("table not found: %s", colRef.Table)
	}

	return s.resolveUnqualified(colRef)
}

// resolution is what one scope can say about an unqualified column.
type resolution struct {
	// columns holds one entry per relation in scope that owns the name, in the
	// scope's name order. Several entries mean more than one relation exposes it.
	columns []ResolvedColumn
	// undecidable reports that a relation in scope has unknown columns, so a
	// scope with no owner cannot be ruled out as the one that provides the name.
	undecidable bool
}

// resolveUnqualified resolves a column that names no qualifier. The search walks
// outward from this scope, because an unqualified name belongs to the innermost
// scope that provides it: a correlated reference inside a subquery has to reach
// the enclosing query when the subquery's own relations do not own the name.
//
// Catalog metadata decides whether a scope provides the name. It is consulted
// only for base tables and trusted in both directions: a relation that owns the
// column becomes an answer, and a scope whose relations are all known and none
// of which owns the column is skipped in favour of the enclosing one. When a
// relation's columns are unknown the scope cannot be ruled out, so the innermost
// relation by name answers instead of the search reaching outward — the
// deterministic rule a metadata-less scope has always had.
//
// Only a scope that is skipped as definitively absent lets the search continue.
// An ambiguous name does not: an enclosing scope cannot resolve a name this one
// already found, and neither does an undecidable one, whose relations are the
// only plausible owners.
func (s *Scope) resolveUnqualified(colRef ColumnRef) ([]ResolvedColumn, error) {
	// fallback is the innermost scope holding exactly one relation. A name
	// nothing owns still has to be attributed somewhere, and a single candidate
	// is not a guess. It is dropped once a scope offers several candidates, so
	// the rule keeps its "only when there is no choice" meaning.
	var (
		fallback       *TableRef
		hasAlternative bool
	)
	for cur := s; cur != nil; cur = cur.parent {
		candidates := cur.sortedRelations()
		if fallback == nil && !hasAlternative && len(candidates) == 1 {
			fallback = candidates[0]
		}
		if len(candidates) > 1 {
			hasAlternative = true
		}

		res := cur.resolveInScope(colRef)
		if len(res.columns) > 0 {
			return res.columns, nil
		}
		if res.undecidable && len(candidates) > 0 {
			return columnsOf(candidates[:1], colRef.Column), nil
		}
		// An empty scope still describes the CTEs declared in it, which are the
		// only relations an unqualified name can address there.
		if len(candidates) == 0 {
			if ctes := cur.sortedCTEs(); len(ctes) > 0 {
				cte := ctes[0]
				return []ResolvedColumn{{
					Ref:      ColumnRef{Table: cte.Name, Column: colRef.Column},
					Relation: cteTableRef(cte),
				}}, nil
			}
		}
	}
	if fallback != nil {
		return columnsOf([]*TableRef{fallback}, colRef.Column), nil
	}
	return nil, errors.Errorf("column not found: %s", colRef.Column)
}

// resolveInScope classifies an unqualified column against this scope's own
// relations. Every relation is inspected even when one of them cannot be
// described, so a relation the catalog confirms owns the name is preferred over
// a temporary relation that might also own it.
func (s *Scope) resolveInScope(colRef ColumnRef) resolution {
	var owners []*TableRef
	undecidable := false
	for _, ref := range s.sortedRelations() {
		if ref.IsSubquery || ref.IsCTE {
			undecidable = true
			continue
		}
		names := ref.ColumnNames()
		if names == nil {
			undecidable = true
			continue
		}
		for _, name := range names {
			if strings.EqualFold(name, colRef.Column) {
				owners = append(owners, ref)
				break
			}
		}
	}
	return resolution{columns: columnsOf(owners, colRef.Column), undecidable: undecidable}
}

// columnsOf describes the relations that own a column, the shape every
// resolution of a base table takes.
func columnsOf(refs []*TableRef, column string) []ResolvedColumn {
	if len(refs) == 0 {
		return nil
	}
	out := make([]ResolvedColumn, 0, len(refs))
	for _, ref := range refs {
		out = append(out, ResolvedColumn{
			Ref:      ColumnRef{Schema: ref.Schema, Table: ref.Table, Column: column},
			Relation: ref,
		})
	}
	return out
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
