package mssql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// catalogOf registers one table under the name and columns given.
func catalogOf(name string, columns ...string) *testutil.MemoryCatalogProvide {
	provider := testutil.NewMemoryCatalogProvide()
	metas := make([]catalog.ColumnMeta, 0, len(columns))
	for _, column := range columns {
		metas = append(metas, catalog.ColumnMeta{Name: column})
	}
	provider.AddTable(&catalog.TableMeta{ID: model.ObjectIdentifier{Name: name}, Columns: metas})
	return provider
}

// TestSourceColumnTakesTheRegisteredSpelling pins the identifier rule of this dialect: SQL
// Server compares identifiers case-insensitively, so `SELECT id FROM Users` reads the column
// the registry knows as `ID`. Recording the statement's spelling named a field that object
// does not have, which left the edge's two ends unattached to one another on the graph.
func TestSourceColumnTakesTheRegisteredSpelling(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT id, name FROM Users", catalogOf("Users", "ID", "Name"))
	require.NoError(t, err)
	require.Len(t, relations, 2)
	require.Equal(t, "Users", relations[0].Source.Table.Name)
	require.Equal(t, "ID", relations[0].Source.Name)
	require.Equal(t, "Name", relations[1].Source.Name)
	// The result's own column keeps the statement's spelling: it is not a registry object.
	require.Equal(t, "id", relations[0].Target.Name)
}

// TestAnExactSpellingWinsOverAFoldedOne pins the one case where the rule must not fold: a
// case-sensitive collation can hold `id` and `ID` as two columns, and then the statement
// named one of them exactly.
func TestAnExactSpellingWinsOverAFoldedOne(t *testing.T) {
	t.Parallel()

	cat := catalogOf("t", "id", "ID")
	relations, err := analyzeSQL("SELECT ID, id FROM t", cat)
	require.NoError(t, err)
	require.Len(t, relations, 2)
	require.Equal(t, "ID", relations[0].Source.Name)
	require.Equal(t, "id", relations[1].Source.Name)
}

// TestPredicateColumnTakesTheRegisteredSpelling pins the same rule on the row-set influence
// a predicate leaves: an influence is emitted by the shared machinery, which resolves the
// reference itself, so the spelling is settled before it is handed over.
func TestPredicateColumnTakesTheRegisteredSpelling(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT a FROM Users WHERE NAME = 'x'", catalogOf("Users", "A", "Name"))
	require.NoError(t, err)
	for _, relation := range relations {
		if relation.Target.Name != "" {
			continue
		}
		require.Equal(t, "Name", relation.Source.Name, "the influence kept the statement's spelling")
	}
}

// TestTableNameCaseIsNotFolded pins the half of the rule this dialect cannot carry. A
// relation's name is looked up in the registry by its meta GUID, which is case-sensitive, so
// a table written in another case is a table the registry does not know — the edge names what
// the statement wrote, and nothing canonicalizes it. Resolving it needs a case-insensitive
// registry lookup, which is a store change rather than a dialect one; see
// .agents/docs/mssql-lineage.md.
func TestTableNameCaseIsNotFolded(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT id FROM users", catalogOf("Users", "ID", "Name"))
	require.NoError(t, err)
	require.Len(t, relations, 1)
	require.Equal(t, "users", relations[0].Source.Table.Name)
	require.Equal(t, "id", relations[0].Source.Name)
}
