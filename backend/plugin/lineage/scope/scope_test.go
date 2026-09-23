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
	require.Empty(t, scope.Tables())
	require.Empty(t, scope.CTEs())
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

// Two relations can carry the same name when they come from different
// qualifiers. Both stay addressable, and a qualified reference picks the one its
// qualifier names instead of whichever was registered last.
func TestScope_SameNameDifferentQualifier(t *testing.T) {
	scope := NewScope(nil)
	db1 := &TableRef{Schema: "db1", Table: "t", Alias: "t"}
	db2 := &TableRef{Schema: "db2", Table: "t", Alias: "t"}
	scope.AddTable(db1)
	scope.AddTable(db2)

	require.Len(t, scope.Tables(), 2, "both relations have to stay in scope")

	resolved, err := scope.ResolveColumn(ColumnRef{Schema: "db1", Table: "t", Column: "a"})
	require.NoError(t, err)
	require.Equal(t, "db1", resolved.Schema)
	require.Equal(t, "t", resolved.Table)
	require.Equal(t, "a", resolved.Column)

	resolved, err = scope.ResolveColumn(ColumnRef{Schema: "db2", Table: "t", Column: "b"})
	require.NoError(t, err)
	require.Equal(t, "db2", resolved.Schema)

	// A qualifier no relation carries must not silently bind to one of them.
	_, err = scope.ResolveColumn(ColumnRef{Schema: "other", Table: "t", Column: "a"})
	require.ErrorContains(t, err, "table not found")
}

// Registering the same address twice stays idempotent, so a repeated
// registration replaces the earlier reference instead of duplicating it.
func TestScope_AddTable_SameAddressReplaces(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(&TableRef{Schema: "db", Table: "t", Alias: "t"})
	replacement := &TableRef{Schema: "db", Table: "t", Alias: "t"}
	scope.AddTable(replacement)

	require.Len(t, scope.Tables(), 1)
	found, ok := scope.FindRelation(RelationKey{Qualifier: "db", Name: "t"})
	require.True(t, ok)
	require.Same(t, replacement, found)
}

// A reference may qualify a relation that its FROM clause left unqualified: the
// analyzer does not know which database that clause resolves in, so any
// qualifier is accepted for it.
func TestScope_QualifiedReferenceMatchesUnqualifiedRelation(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(&TableRef{Table: "users", Alias: "users"})

	resolved, err := scope.ResolveColumn(ColumnRef{Schema: "mydb", Table: "users", Column: "id"})
	require.NoError(t, err)
	require.Equal(t, "id", resolved.Column)
}

// ResolveColumnRef hands back the relation, so a caller does not have to look the
// name up again and guess.
func TestScope_ResolveColumnRef_ReturnsRelation(t *testing.T) {
	scope := NewScope(nil)
	table := &TableRef{Schema: "db1", Table: "t", Alias: "t"}
	scope.AddTable(table)

	resolved, ref, err := scope.ResolveColumnRef(ColumnRef{Schema: "db1", Table: "t", Column: "a"})
	require.NoError(t, err)
	require.Same(t, table, ref)
	require.Equal(t, "a", resolved.Column)
}

// A CTE that exists only as a definition is still presented as the relation a
// reference resolved to, so the caller handles it like any other temporary
// relation.
func TestScope_ResolveColumnRef_CTEDefinition(t *testing.T) {
	scope := NewScope(nil)
	scope.AddCTE(&CTEDefinition{Name: "c", Columns: []string{"id"}})

	resolved, ref, err := scope.ResolveColumnRef(ColumnRef{Table: "c", Column: "id"})
	require.NoError(t, err)
	require.NotNil(t, ref)
	require.True(t, ref.IsCTE)
	require.Equal(t, "c", ref.Table)
	require.Equal(t, "id", resolved.Column)
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

// A relation whose columns are unknown is the only one that can still own a name
// no described relation owns, so it answers before one the catalog confirms does
// not — including a temporary relation, which is why an unqualified name over a
// derived table reaches that table's own lineage.
func TestScope_ResolveColumn_UndescribedRelationAnswers(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(tableWithColumns("a", []string{"id"}))
	scope.AddTable(&TableRef{Table: "b", Alias: "b"})

	resolved, err := scope.ResolveColumn(ColumnRef{Column: "z"})
	require.NoError(t, err)
	require.Equal(t, "b", resolved.Table)

	derived := NewScope(nil)
	derived.AddTable(tableWithColumns("a", []string{"id"}))
	derived.AddTable(&TableRef{Table: "d", Alias: "d", IsSubquery: true})

	resolved, err = derived.ResolveColumn(ColumnRef{Column: "z"})
	require.NoError(t, err)
	require.Equal(t, "d", resolved.Table)
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

// Several relations owning the name is not a guess to make: every one of them is
// a real source, which is what a coalesced USING or NATURAL JOIN column looks
// like.
func TestScope_ResolveColumnRefs_ReturnsEveryOwner(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(tableWithColumns("a", []string{"id", "x"}))
	scope.AddTable(tableWithColumns("b", []string{"id", "y"}))

	resolved, err := scope.ResolveColumnRefs(ColumnRef{Column: "id"})
	require.NoError(t, err)
	require.Len(t, resolved, 2)
	require.Equal(t, "a", resolved[0].Ref.Table)
	require.Equal(t, "b", resolved[1].Ref.Table)
	require.Same(t, scope.Tables()[0], resolved[0].Relation)
	require.Same(t, scope.Tables()[1], resolved[1].Relation)

	// The singular accessor reports the first of them.
	first, err := scope.ResolveColumn(ColumnRef{Column: "id"})
	require.NoError(t, err)
	require.Equal(t, "a", first.Table)
}

// A scope whose relations are all known and none of which owns the name does not
// provide it, so the search continues in the enclosing scope: this is what makes
// a correlated reference to an outer column resolve instead of being dropped or
// attributed to an unrelated inner table.
func TestScope_ResolveColumn_CorrelatedOuterReference(t *testing.T) {
	outer := NewScope(nil)
	outer.AddTable(tableWithColumns("orders", []string{"id", "amount"}))
	inner := NewScope(outer)
	inner.AddTable(tableWithColumns("customers", []string{"id", "name"}))

	resolved, err := inner.ResolveColumn(ColumnRef{Column: "amount"})
	require.NoError(t, err)
	require.Equal(t, "orders", resolved.Table)
	require.Equal(t, "amount", resolved.Column)

	// A name the enclosing scope does not own either is not invented: the single
	// inner relation keeps it, because a lone candidate is not a guess.
	resolved, err = inner.ResolveColumn(ColumnRef{Column: "nosuch"})
	require.NoError(t, err)
	require.Equal(t, "customers", resolved.Table)
}

// The search reaches outward only when the current scope is known not to provide
// the name. An ambiguous or an undecidable scope answers locally even when an
// enclosing scope owns the name.
func TestScope_ResolveColumn_AmbiguousDoesNotReachOutward(t *testing.T) {
	outer := NewScope(nil)
	outer.AddTable(tableWithColumns("orders", []string{"id", "amount"}))
	inner := NewScope(outer)
	inner.AddTable(tableWithColumns("customers", []string{"id", "amount"}))
	inner.AddTable(tableWithColumns("regions", []string{"id", "amount"}))

	// Ambiguous: the enclosing scope's `amount` is not the answer.
	resolved, err := inner.ResolveColumn(ColumnRef{Column: "amount"})
	require.NoError(t, err)
	require.Equal(t, "customers", resolved.Table)

	// Undecidable: a relation whose columns are unknown may own the name, so it
	// answers here rather than the enclosing scope.
	undecidable := NewScope(outer)
	undecidable.AddTable(tableWithColumns("customers", []string{"id"}))
	undecidable.AddTable(&TableRef{Table: "zarchive", Alias: "zarchive"})
	resolved, err = undecidable.ResolveColumn(ColumnRef{Column: "amount"})
	require.NoError(t, err)
	require.Equal(t, "zarchive", resolved.Table)
}

// A relation the catalog confirms owns the name is preferred over a temporary
// relation that might also own it: the confirmed owner is a fact, the temporary
// relation's columns are not described here at all.
func TestScope_ResolveColumn_ConfirmedOwnerBeatsTemporary(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(&TableRef{Table: "aaa", Alias: "aaa", IsCTE: true})
	scope.AddTable(tableWithColumns("zzz", []string{"id"}))

	resolved, err := scope.ResolveColumn(ColumnRef{Column: "id"})
	require.NoError(t, err)
	require.Equal(t, "zzz", resolved.Table)
}

// A qualified reference has exactly one resolution however many relations share
// the name.
func TestScope_ResolveColumnRefs_QualifiedIsSingle(t *testing.T) {
	scope := NewScope(nil)
	scope.AddTable(&TableRef{Schema: "db1", Table: "t", Alias: "t"})
	scope.AddTable(&TableRef{Schema: "db2", Table: "t", Alias: "t"})

	resolved, err := scope.ResolveColumnRefs(ColumnRef{Schema: "db2", Table: "t", Column: "id"})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	require.Equal(t, "db2", resolved[0].Ref.Schema)
	require.Equal(t, "db2", resolved[0].Relation.Schema)
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
