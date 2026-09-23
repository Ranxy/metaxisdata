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
	// Tables available in this scope (key: alias or table name)
	tables map[string]*TableRef
	// CTEs available in this scope
	ctes map[string]*CTEDefinition
	// Output columns for this scope (for SELECT statements)
	outputColumns []OutputColumn
}

// NewScope creates a new scope with an optional parent.
func NewScope(parent *Scope) *Scope {
	return &Scope{
		parent:        parent,
		tables:        make(map[string]*TableRef),
		ctes:          make(map[string]*CTEDefinition),
		outputColumns: make([]OutputColumn, 0),
	}
}

// AddTable adds a table reference to the current scope.
func (s *Scope) AddTable(ref *TableRef) {
	key := ref.Alias
	if key == "" {
		key = ref.Table
	}
	s.tables[key] = ref
}

// AddCTE adds a CTE definition to the current scope.
func (s *Scope) AddCTE(cte *CTEDefinition) {
	s.ctes[cte.Name] = cte
}

// AddOutputColumn adds an output column to the current scope.
func (s *Scope) AddOutputColumn(col OutputColumn) {
	s.outputColumns = append(s.outputColumns, col)
}

// FindTable looks up a table by name or alias in the current scope and parent scopes.
func (s *Scope) FindTable(name string) (*TableRef, bool) {
	// Check current scope
	if ref, ok := s.tables[name]; ok {
		return ref, true
	}
	// Check parent scope
	if s.parent != nil {
		return s.parent.FindTable(name)
	}
	return nil, false
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

// GetTables returns all tables in the current scope (not including parent scopes).
func (s *Scope) GetTables() map[string]*TableRef {
	return s.tables
}

// GetCTEs returns all CTEs in the current scope (not including parent scopes).
func (s *Scope) GetCTEs() map[string]*CTEDefinition {
	return s.ctes
}

// ResolveColumn resolves a column reference to its source table.
// It handles both qualified (table.column) and unqualified (column) references.
func (s *Scope) ResolveColumn(colRef ColumnRef) (*ColumnRef, error) {
	// A reference already resolved against the scope it came from is returned
	// unchanged: a later lookup happens in a sibling scope that deliberately
	// does not contain the table it resolved to.
	if colRef.Resolved {
		return &colRef, nil
	}
	// If fully qualified, just verify it exists
	if colRef.Table != "" {
		if ref, ok := s.FindTable(colRef.Table); ok {
			// For CTEs and subqueries, use the alias as the key for lookups
			// For regular tables, use the actual table name
			tableName := ref.Table
			if ref.IsCTE || ref.IsSubquery {
				tableName = colRef.Table // Use the alias/key for lookups
			}
			return &ColumnRef{
				Schema: ref.Schema,
				Table:  tableName,
				Column: colRef.Column,
			}, nil
		}
		// Check if it's a CTE (CTEs are also added to tables now, so this is redundant)
		if cte, ok := s.FindCTE(colRef.Table); ok {
			return &ColumnRef{
				Schema: "",
				Table:  cte.Name,
				Column: colRef.Column,
			}, nil
		}
		return nil, errors.Errorf("table not found: %s", colRef.Table)
	}

	// Unqualified column - search tables in scope.
	//
	// The walk is sorted by key rather than ranging the map directly: a map walk
	// picks an arbitrary relation when the column name is ambiguous across FROM
	// relations, which would make the resolved source (and therefore the emitted
	// lineage edge) nondeterministic for NATURAL JOIN and similar shapes.
	tableKeys := make([]string, 0, len(s.tables))
	for key := range s.tables {
		tableKeys = append(tableKeys, key)
	}
	slices.Sort(tableKeys)

	// With several relations in scope, catalog metadata decides: the relation
	// that actually owns the column wins instead of the alphabetically first one.
	// The rule is skipped whenever any relation lacks metadata, so the ordering
	// rule below stays the deterministic fallback for the metadata-less case.
	if len(tableKeys) > 1 {
		if resolved, decided, err := s.resolveByColumnMetadata(tableKeys, colRef); decided {
			return resolved, err
		}
	}

	// Fall back to the first relation in key order, which also covers a single
	// relation and every scope whose metadata is unavailable.
	if len(tableKeys) > 0 {
		ref := s.tables[tableKeys[0]]
		return &ColumnRef{
			Schema: ref.Schema,
			Table:  ref.Table,
			Column: colRef.Column,
		}, nil
	}

	// Also check CTEs (also sorted for determinism).
	cteKeys := make([]string, 0, len(s.ctes))
	for key := range s.ctes {
		cteKeys = append(cteKeys, key)
	}
	slices.Sort(cteKeys)
	for _, key := range cteKeys {
		cte := s.ctes[key]
		return &ColumnRef{
			Schema: "",
			Table:  cte.Name,
			Column: colRef.Column,
		}, nil
	}

	// Try parent scope
	if s.parent != nil {
		return s.parent.ResolveColumn(colRef)
	}
	return nil, errors.Errorf("column not found: %s", colRef.Column)
}

// resolveByColumnMetadata picks the relation that owns an unqualified column
// from catalog metadata. decided is false when metadata is incomplete for at
// least one relation, in which case the caller falls back to its ordering rule.
// When every relation has metadata and none owns the column, the reference is
// unresolvable: an error is returned so the caller drops the edge instead of
// attributing it to an arbitrary table.
func (s *Scope) resolveByColumnMetadata(keys []string, colRef ColumnRef) (*ColumnRef, bool, error) {
	allKnown := true
	for _, key := range keys {
		ref := s.tables[key]
		if ref.IsSubquery || ref.IsCTE {
			// A temporary relation's columns are described by its lineage, which
			// the caller resolves separately; do not second-guess it here.
			return nil, false, nil
		}
		names := ref.ColumnNames()
		if names == nil {
			allKnown = false
			continue
		}
		for _, name := range names {
			if strings.EqualFold(name, colRef.Column) {
				return &ColumnRef{Schema: ref.Schema, Table: ref.Table, Column: colRef.Column}, true, nil
			}
		}
	}
	if allKnown {
		return nil, true, errors.Errorf("column %q not found in any table in scope", colRef.Column)
	}
	return nil, false, nil
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
