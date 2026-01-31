package scope

import (
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

	// Unqualified column - search tables in scope
	// Return the first match (optimization: avoid collecting all matches)
	for _, ref := range s.tables {
		// For now, assume all columns are available from all tables
		// In a full implementation, we'd check ref.Columns
		return &ColumnRef{
			Schema: ref.Schema,
			Table:  ref.Table,
			Column: colRef.Column,
		}, nil
	}

	// Also check CTEs
	for _, cte := range s.ctes {
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
