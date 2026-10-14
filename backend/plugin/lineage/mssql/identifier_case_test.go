package mssql

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
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

// TestARelationIsRecordedUnderTheNameTheRegistryKnows pins the rule the catalog lookup could not
// carry before it could fold: SQL Server binds an identifier by the database collation, so
// `users` is the relation the registry holds as `Users`, and the edge has to name that object.
// Recording the statement's spelling addressed a meta GUID the registry does not have, and the
// graph drew a floating node beside the object instead of the object.
func TestARelationIsRecordedUnderTheNameTheRegistryKnows(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT id FROM users", catalogOf("Users", "ID", "Name"))
	require.NoError(t, err)
	require.Len(t, relations, 1)
	require.Equal(t, "Users", relations[0].Source.Table.Name)
	require.Equal(t, "ID", relations[0].Source.Name)

	// A data-modification target is a stored relation too, and it is settled at the same
	// place, so the edge leaving the statement names the object the statement wrote.
	relations, err = analyzeSQL("INSERT INTO logs (msg) SELECT name FROM t", catalogOf("Logs", "msg"))
	require.NoError(t, err)
	require.NotEmpty(t, relations)
	for _, relation := range relations {
		require.Equal(t, "Logs", relation.Target.Table.Name, "the target kept the statement's spelling")
	}
}

// TestANameTheRegistryDoesNotKnowStaysAsWritten pins the other side of the rule: nothing is
// invented. A relation no spelling of which the registry holds keeps the name the statement
// wrote, and the schema the statement wrote with it.
func TestANameTheRegistryDoesNotKnowStaysAsWritten(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT id FROM other.users", catalogOf("Users", "ID"))
	require.NoError(t, err)
	require.Len(t, relations, 1)
	require.Equal(t, "users", relations[0].Source.Table.Name)
	require.Equal(t, "other", relations[0].Source.Table.Schema)
	require.Equal(t, "id", relations[0].Source.Name)
}

// TestAnAmbiguousFoldedNameIsReportedAndNotChosen pins the one answer the lookup must not give.
// A case-sensitive collation can hold `Users` and `USERS` as two objects, and then a third
// spelling names neither of them; picking one would be an edge into the wrong object, so the
// analysis reports the gap and keeps the name as written.
func TestAnAmbiguousFoldedNameIsReportedAndNotChosen(t *testing.T) {
	t.Parallel()

	cat := testutil.NewMemoryCatalogProvide()
	for _, name := range []string{"Users", "USERS"} {
		cat.AddTable(&catalog.TableMeta{
			ID:      model.ObjectIdentifier{Name: name},
			Columns: []catalog.ColumnMeta{{Name: "ID"}},
		})
	}

	relations, err := analyzeSQL("SELECT id FROM users", cat)
	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Contains(t, unsupported.Error(), "catalog lookup failed for users")
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

// TestIdentifierCaseIsFolded pins the third half of the case story: a T-SQL identifier is bound
// by the database's collation, which is case-insensitive by default, so a qualifier written in
// another case than the relation, its alias or a CTE was written in still names it. The scope
// asks each dialect for its rule (`scope.SetFoldNames`), and this dialect is the one that asks
// for folding.
func TestIdentifierCaseIsFolded(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, sql, wantTable string }{
		{"table name as a qualifier", "SELECT T.a FROM t", "t"},
		{"schema qualified name", "SELECT dbo.T.a FROM dbo.t", "t"},
		{"relation alias", "SELECT X.a FROM t AS x", "t"},
		{"alias written in another case", "SELECT x.a FROM T AS X", "T"},
		{"cte named as a qualifier", "WITH c AS (SELECT a FROM t) SELECT C.a FROM c", "t"},
		{"cte written in another case", "WITH C AS (SELECT a FROM t) SELECT c.a FROM C", "t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			relations, err := analyzeSQL(tc.sql, nil)
			require.NoError(t, err, tc.sql)
			require.Len(t, relations, 1, tc.sql)
			require.Equal(t, tc.wantTable, relations[0].Source.Table.Name)
			require.Equal(t, "a", relations[0].Source.Name)
		})
	}
}
