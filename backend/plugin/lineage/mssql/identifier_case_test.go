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

// TestClauseKeywordAliasGuard pins both halves of the alias workaround. The parser consumes a
// clause keyword as a relation alias in two shapes, and the dialect drops the alias when the
// token after it continues the clause — but NATURAL and USING are ODBC and future reserved
// words rather than T-SQL reserved ones, so a statement may also have written one as a genuine
// alias, and then it has to be kept.
func TestClauseKeywordAliasGuard(t *testing.T) {
	t.Parallel()

	// The clause forms: the relation keeps its own name, so a reference to it resolves.
	for _, tc := range []struct{ name, sql string }{
		{"natural join", "SELECT t1.a FROM t1 NATURAL JOIN t2"},
		{"merge using", "MERGE INTO tgt USING src ON tgt.id = src.id WHEN MATCHED THEN UPDATE SET tgt.a = src.a"},
		{"merge using after a table hint", "MERGE INTO tgt WITH (READPAST) USING src ON tgt.id = src.id WHEN MATCHED THEN UPDATE SET tgt.a = src.a"},
		{"merge using after a comment", "MERGE INTO tgt /* c */ USING src ON tgt.id = src.id WHEN MATCHED THEN UPDATE SET tgt.a = src.a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			relations, err := analyzeSQL(tc.sql, nil)
			require.NoError(t, err, tc.sql)
			require.NotEmpty(t, relations, tc.sql)
		})
	}

	// The alias forms: the statement wrote the keyword as the relation's own alias.
	for _, sql := range []string{
		"SELECT NATURAL.a FROM t1 NATURAL",
		"SELECT USING.a FROM t1 USING",
		"SELECT NATURAL.a FROM t1 NATURAL, t2",
		"MERGE INTO [tgt] AS [USING] USING src ON 1 = 1 WHEN MATCHED THEN UPDATE SET [USING].a = 1",
	} {
		relations, err := analyzeSQL(sql, nil)
		require.NoError(t, err, sql)
		require.NotEmpty(t, relations, sql)
	}
}
