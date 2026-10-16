package algorithm

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/scope"
)

// queryLocalRelation builds the relation a wildcard expands over: its column lookup
// answers with names, which is what makes the expansion possible at all.
func queryLocalRelation(names []string) *scope.TableRef {
	relation := &scope.TableRef{Table: "d", Alias: "d", IsSubquery: true}
	relation.SetColumnLookup(func() []string { return names })
	return relation
}

// TestExpandQueryLocalWildcardRecordsOneOutputColumnPerExposedColumn pins the shape
// the rule rests on: the output column names the exposed name, and its source is the
// relation's scope key plus that name, so the trace that answers it reads the column
// the relation itself describes.
func TestExpandQueryLocalWildcardRecordsOneOutputColumnPerExposedColumn(t *testing.T) {
	t.Parallel()

	sp := scope.NewScope(nil)
	require.True(t, ExpandQueryLocalWildcard(sp, queryLocalRelation([]string{"k", "name"}), nil))

	columns := sp.GetOutputColumns()
	require.Len(t, columns, 2)
	require.Equal(t, []string{"k", "name"}, []string{columns[0].Alias, columns[1].Alias})
	for i, want := range []string{"k", "name"} {
		require.Len(t, columns[i].Sources, 1)
		require.Equal(t, scope.ColumnRef{Table: "d", Column: want}, columns[i].Sources[0].Ref)
	}
}

// TestExpandQueryLocalWildcardLeavesWhatItCannotDescribe pins every refusal: a base
// table is the catalog path's to expand, a query-local relation whose columns are
// unknown keeps the single wildcard output column, and one whose exposed names repeat
// keeps it too — the trace addresses a query-local relation's lineage by target name,
// so two columns exposing one name cannot be told apart and would swap their sources.
func TestExpandQueryLocalWildcardLeavesWhatItCannotDescribe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		relation *scope.TableRef
	}{
		{
			name:     "a base table",
			relation: &scope.TableRef{Table: "t", Alias: "t"},
		},
		{
			name:     "a query-local relation with no described columns",
			relation: &scope.TableRef{Table: "d", Alias: "d", IsSubquery: true},
		},
		{
			name: "a query-local relation whose lookup answers no names",
			relation: func() *scope.TableRef {
				relation := &scope.TableRef{Table: "d", Alias: "d", IsSubquery: true}
				relation.SetColumnLookup(func() []string { return nil })
				return relation
			}(),
		},
		{
			// PostgreSQL 16 returns two columns for this relation, so the statement is
			// real SQL; the names cannot carry the positions, which is what the whole
			// wildcard reading is kept for.
			name:     "a query-local relation exposing one name twice",
			relation: queryLocalRelation([]string{"x", "x"}),
		},
		{
			name:     "a query-local relation whose exposed names differ only by case",
			relation: queryLocalRelation([]string{"K", "k"}),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			sp := scope.NewScope(nil)
			require.False(t, ExpandQueryLocalWildcard(sp, tc.relation, nil))
			require.Empty(t, sp.GetOutputColumns())
		})
	}
}

// TestExpandQueryLocalWildcardLeavesOutTheExcludedColumns pins `* EXCEPT (…)` against a
// query-local relation, matched the way the engines match the identifiers in that list.
func TestExpandQueryLocalWildcardLeavesOutTheExcludedColumns(t *testing.T) {
	t.Parallel()

	sp := scope.NewScope(nil)
	require.True(t, ExpandQueryLocalWildcard(sp, queryLocalRelation([]string{"k", "Name", "x"}), []string{"name"}))

	columns := sp.GetOutputColumns()
	require.Len(t, columns, 2)
	require.Equal(t, []string{"k", "x"}, []string{columns[0].Alias, columns[1].Alias})

	// Excluding every column still reports the expansion: the relation's exposure was
	// known, it simply contributes no output column. The engines reject such a
	// statement (Doris "All slots in * EXCEPT clause are excepted", StarRocks "EXCLUDE
	// clause removes all columns from query scope"), so this is the reading of a shape
	// no executable input reaches rather than a gap left unreported.
	sp = scope.NewScope(nil)
	require.True(t, ExpandQueryLocalWildcard(sp, queryLocalRelation([]string{"k"}), []string{"K"}))
	require.Empty(t, sp.GetOutputColumns())
}

// TestQueryLocalColumnRefKeysTheRelationTheStatementNamed pins the reference an
// expansion contributes: the relation's alias when it has one, its own name otherwise,
// and the column the caller asked for.
func TestQueryLocalColumnRefKeysTheRelationTheStatementNamed(t *testing.T) {
	t.Parallel()

	require.Equal(t, scope.ColumnRef{Table: "d", Column: "k"},
		scope.QueryLocalColumnRef(&scope.TableRef{Table: "cte", Alias: "d"}, "k"))
	require.Equal(t, scope.ColumnRef{Table: "d", Column: "k"},
		scope.QueryLocalColumnRef(&scope.TableRef{Table: "d"}, "k"))
}
