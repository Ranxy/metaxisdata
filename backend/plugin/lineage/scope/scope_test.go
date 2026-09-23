package scope

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewScope(t *testing.T) {
	scope := NewScope(nil)
	require.NotNil(t, scope)
	require.Nil(t, scope.Parent())
	require.Empty(t, scope.GetOutputColumns())
	require.Empty(t, scope.GetTables())
	require.Empty(t, scope.GetCTEs())
}

func TestScope_AddTable(t *testing.T) {
	scope := NewScope(nil)

	table := &TableRef{
		Schema: "mydb",
		Table:  "users",
		Alias:  "u",
	}

	scope.AddTable(table)

	// Should be findable by alias
	found, ok := scope.FindTable("u")
	require.True(t, ok)
	require.Equal(t, table, found)

	// Should not be findable by table name when alias is set
	_, ok = scope.FindTable("users")
	require.False(t, ok)
}

func TestScope_AddTable_NoAlias(t *testing.T) {
	scope := NewScope(nil)

	table := &TableRef{
		Schema: "mydb",
		Table:  "users",
		Alias:  "", // No alias
	}

	scope.AddTable(table)

	// Should be findable by table name
	found, ok := scope.FindTable("users")
	require.True(t, ok)
	require.Equal(t, table, found)
}

func TestScope_AddCTE(t *testing.T) {
	scope := NewScope(nil)

	cte := &CTEDefinition{
		Name:    "my_cte",
		Columns: []string{"id", "name"},
	}

	scope.AddCTE(cte)

	found, ok := scope.FindCTE("my_cte")
	require.True(t, ok)
	require.Equal(t, cte, found)
}

func TestScope_AddOutputColumn(t *testing.T) {
	scope := NewScope(nil)

	col := OutputColumn{
		Alias: "user_id",
		SourceColumns: []ColumnRef{
			{Schema: "mydb", Table: "users", Column: "id"},
		},
		IsDerived: false,
	}

	scope.AddOutputColumn(col)

	outputs := scope.GetOutputColumns()
	require.Len(t, outputs, 1)
	require.Equal(t, col, outputs[0])
}

func TestScope_FindTable_InParentScope(t *testing.T) {
	parent := NewScope(nil)
	child := NewScope(parent)

	table := &TableRef{
		Schema: "mydb",
		Table:  "users",
		Alias:  "",
	}

	parent.AddTable(table)

	// Child should be able to find table in parent
	found, ok := child.FindTable("users")
	require.True(t, ok)
	require.Equal(t, table, found)
}

func TestScope_FindCTE_InParentScope(t *testing.T) {
	parent := NewScope(nil)
	child := NewScope(parent)

	cte := &CTEDefinition{
		Name:    "parent_cte",
		Columns: []string{"id"},
	}

	parent.AddCTE(cte)

	// Child should be able to find CTE in parent
	found, ok := child.FindCTE("parent_cte")
	require.True(t, ok)
	require.Equal(t, cte, found)
}

func TestScope_ResolveColumn_Qualified(t *testing.T) {
	scope := NewScope(nil)

	table := &TableRef{
		Schema: "mydb",
		Table:  "users",
		Alias:  "u",
	}
	scope.AddTable(table)

	// Resolve qualified column reference
	colRef := ColumnRef{
		Table:  "u",
		Column: "id",
	}

	resolved, err := scope.ResolveColumn(colRef)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Equal(t, "mydb", resolved.Schema)
	require.Equal(t, "users", resolved.Table) // Should use alias for CTEs/subqueries check
	require.Equal(t, "id", resolved.Column)
}

func TestScope_ResolveColumn_Unqualified(t *testing.T) {
	scope := NewScope(nil)

	table := &TableRef{
		Schema: "mydb",
		Table:  "users",
		Alias:  "",
	}
	scope.AddTable(table)

	// Resolve unqualified column reference
	colRef := ColumnRef{
		Column: "id",
	}

	resolved, err := scope.ResolveColumn(colRef)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Equal(t, "mydb", resolved.Schema)
	require.Equal(t, "users", resolved.Table)
	require.Equal(t, "id", resolved.Column)
}

func TestScope_ResolveColumn_Unqualified_MultipleTablesAmbiguous(t *testing.T) {
	scope := NewScope(nil)

	table1 := &TableRef{
		Schema: "mydb",
		Table:  "users",
		Alias:  "",
	}
	table2 := &TableRef{
		Schema: "mydb",
		Table:  "orders",
		Alias:  "",
	}

	scope.AddTable(table1)
	scope.AddTable(table2)

	// Resolve unqualified column reference - should return first match
	colRef := ColumnRef{
		Column: "id",
	}

	resolved, err := scope.ResolveColumn(colRef)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	// Should match one of the tables (implementation returns first match)
	require.Contains(t, []string{"users", "orders"}, resolved.Table)
}

// Catalog metadata disambiguates an unqualified column across several relations:
// the relation that owns the column wins instead of the first one in key order.
func TestScope_ResolveColumn_MetadataDisambiguates(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(tableWithColumns("a", []string{"id", "x"}))
	scope.AddTable(tableWithColumns("b", []string{"id", "y"}))

	resolved, err := scope.ResolveColumn(ColumnRef{Column: "y"})
	require.NoError(t, err)
	require.Equal(t, "b", resolved.Table)
	require.Equal(t, "y", resolved.Column)
}

// A column no relation owns is unresolvable: it must not be attributed to an
// arbitrary relation.
func TestScope_ResolveColumn_MetadataRejectsUnknownColumn(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(tableWithColumns("a", []string{"id"}))
	scope.AddTable(tableWithColumns("b", []string{"id"}))

	resolved, err := scope.ResolveColumn(ColumnRef{Column: "nope"})
	require.Error(t, err)
	require.Nil(t, resolved)
}

// Incomplete metadata keeps the deterministic ordering rule instead of guessing.
func TestScope_ResolveColumn_PartialMetadataFallsBack(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(tableWithColumns("a", []string{"id"}))
	scope.AddTable(&TableRef{Table: "b", Alias: "b"})

	resolved, err := scope.ResolveColumn(ColumnRef{Column: "z"})
	require.NoError(t, err)
	require.Equal(t, "a", resolved.Table)
}

// A temporary relation has no catalog columns, so its presence keeps the
// ordering rule; the deterministic key order then picks the first relation.
func TestScope_ResolveColumn_TemporaryRelationFallsBack(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(tableWithColumns("a", []string{"id"}))
	scope.AddTable(&TableRef{Table: "c", Alias: "c", IsCTE: true})

	resolved, err := scope.ResolveColumn(ColumnRef{Column: "id"})
	require.NoError(t, err)
	require.Equal(t, "a", resolved.Table)
}

// A single relation keeps the ordering rule: there is nothing to disambiguate,
// and stale metadata must not drop a column that does exist.
func TestScope_ResolveColumn_SingleTableKeepsFallback(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(tableWithColumns("a", []string{"id"}))

	resolved, err := scope.ResolveColumn(ColumnRef{Column: "not_in_metadata"})
	require.NoError(t, err)
	require.Equal(t, "a", resolved.Table)
}

func tableWithColumns(name string, columns []string) *TableRef {
	ref := &TableRef{Table: name, Alias: name}
	ref.SetColumnLookup(func() []string { return columns })
	return ref
}

func TestScope_ResolveColumn_NotFound(t *testing.T) {
	scope := NewScope(nil)

	table := &TableRef{
		Schema: "mydb",
		Table:  "users",
		Alias:  "u",
	}
	scope.AddTable(table)

	// Try to resolve column from non-existent table
	colRef := ColumnRef{
		Table:  "orders",
		Column: "id",
	}

	resolved, err := scope.ResolveColumn(colRef)
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Contains(t, err.Error(), "table not found")
}

// A reference that was already resolved in the scope it came from must pass
// through unchanged, even though this scope has no matching table. This is what
// lets a merged set-operation arm or a flattened expression subquery keep the
// table it resolved to in a sibling scope.
func TestScope_ResolveColumn_ResolvedPassThrough(t *testing.T) {
	scope := NewScope(nil)

	colRef := ColumnRef{Schema: "db", Table: "t", Column: "c", Resolved: true}

	resolved, err := scope.ResolveColumn(colRef)
	require.NoError(t, err)
	require.Equal(t, colRef, *resolved)
}

func TestScope_ResolveColumn_InParentScope(t *testing.T) {
	parent := NewScope(nil)
	child := NewScope(parent)

	table := &TableRef{
		Schema: "mydb",
		Table:  "users",
		Alias:  "",
	}
	parent.AddTable(table)

	// Child should be able to resolve column from parent scope
	colRef := ColumnRef{
		Column: "id",
	}

	resolved, err := child.ResolveColumn(colRef)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Equal(t, "users", resolved.Table)
}

func TestScope_ResolveColumn_CTE(t *testing.T) {
	scope := NewScope(nil)

	cte := &CTEDefinition{
		Name:    "my_cte",
		Columns: []string{"id", "name"},
	}
	scope.AddCTE(cte)

	// Resolve column from CTE
	colRef := ColumnRef{
		Table:  "my_cte",
		Column: "id",
	}

	resolved, err := scope.ResolveColumn(colRef)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Equal(t, "", resolved.Schema)
	require.Equal(t, "my_cte", resolved.Table)
	require.Equal(t, "id", resolved.Column)
}

func TestScope_ResolveColumn_Subquery(t *testing.T) {
	scope := NewScope(nil)

	subquery := &TableRef{
		Schema:     "",
		Table:      "sq",
		Alias:      "sq",
		IsSubquery: true,
	}
	scope.AddTable(subquery)

	// Resolve column from subquery
	colRef := ColumnRef{
		Table:  "sq",
		Column: "total",
	}

	resolved, err := scope.ResolveColumn(colRef)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	require.Equal(t, "sq", resolved.Table)
	require.Equal(t, "total", resolved.Column)
}

func TestScope_NestedScopes(t *testing.T) {
	// Create a hierarchy: root -> child1 -> child2
	root := NewScope(nil)
	child1 := NewScope(root)
	child2 := NewScope(child1)

	// Add tables at different levels
	rootTable := &TableRef{Schema: "db", Table: "root_table", Alias: ""}
	child1Table := &TableRef{Schema: "db", Table: "child1_table", Alias: ""}
	child2Table := &TableRef{Schema: "db", Table: "child2_table", Alias: ""}

	root.AddTable(rootTable)
	child1.AddTable(child1Table)
	child2.AddTable(child2Table)

	// child2 should find its own table
	found, ok := child2.FindTable("child2_table")
	require.True(t, ok)
	require.Equal(t, child2Table, found)

	// child2 should find child1's table
	found, ok = child2.FindTable("child1_table")
	require.True(t, ok)
	require.Equal(t, child1Table, found)

	// child2 should find root's table
	found, ok = child2.FindTable("root_table")
	require.True(t, ok)
	require.Equal(t, rootTable, found)

	// root should not find child1's table
	_, ok = root.FindTable("child1_table")
	require.False(t, ok)

	// root should not find child2's table
	_, ok = root.FindTable("child2_table")
	require.False(t, ok)
}
