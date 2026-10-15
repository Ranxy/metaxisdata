package doris

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// TestAQualifiedColumnTheCatalogDoesNotHaveIsAGap pins that a qualified reference
// to a described relation is answered from the catalog, the way the engine answers
// it: Doris reports `Unknown column 'nope' in 'ev2'` for the same statements. Every
// clause that reads a column has to report the gap under its own name rather than
// emit an edge to a column the registry does not have.
func TestAQualifiedColumnTheCatalogDoesNotHaveIsAGap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want string
		// edges are the relations the statement still produces: the gap drops one
		// reference, never the whole analysis.
		edges []string
	}{
		{
			name: "an output column",
			sql:  "SELECT ev2.nope AS n FROM ev2",
			want: "unresolved reference in an output column: ev2.nope",
		},
		{
			name: "an output target",
			sql:  "CREATE TABLE t3 AS SELECT ev2.nope AS n FROM ev2",
			want: "unresolved reference in an output target: ev2.nope",
		},
		{
			name: "an inserted column",
			sql:  "INSERT INTO ev2 (name) SELECT ev2.nope FROM ev2",
			want: "unresolved reference in an output target: ev2.nope",
		},
		{
			name: "an assignment",
			sql:  "UPDATE ev2 SET name = ev2.nope",
			want: "unresolved reference in an assignment: ev2.nope",
		},
		{
			name: "a DELETE condition",
			sql:  "DELETE FROM ev2 WHERE ev2.nope = 1",
			want: "unresolved reference in a DELETE condition: ev2.nope",
		},
		{
			name:  "a predicate",
			sql:   "SELECT id FROM ev2 WHERE ev2.nope = 1",
			want:  "unresolved reference in a predicate: ev2.nope",
			edges: []string{"ev2.id -> __result__.id"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			relations, err := analyzeSQL(tc.sql, variantCatalog())

			var unsupported *lineage.UnsupportedStatementError
			require.ErrorAs(t, err, &unsupported)
			require.Contains(t, unsupported.Error(), tc.want)
			got := make([]string, 0, len(relations))
			for _, relation := range relations {
				got = append(got, edgeText(relation))
			}
			want := tc.edges
			if want == nil {
				want = []string{}
			}
			require.ElementsMatch(t, want, got, "sql=%q", tc.sql)
		})
	}
}

// TestAnUnexplainableVARIANTBaseIsAGap pins the reading the catalog refuses and no
// path explains: the alias `v` addresses ev8, which has no `name`, and the base
// column `v` belongs to both relations, so neither reading is available. The
// engine reports `v.name is ambiguous: v#1, v#3` — a gap, not an edge to ev8.name.
func TestAnUnexplainableVARIANTBaseIsAGap(t *testing.T) {
	t.Parallel()

	relations, err := analyzeSQL("SELECT v.name AS n FROM ev7, ev8 v", variantCatalog())

	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Contains(t, unsupported.Error(), "unresolved reference in an output column: v.name")
	require.Empty(t, relations)
}

// TestAQualifiedReferenceToAQueryLocalRelationIsVerified pins that the rule covers
// the relations the statement declares for itself: a CTE, a derived table and a
// table function expose a column list whenever the analyzer can name one, and a
// reference to a column outside it is a gap rather than an edge to a column the
// relation does not offer. The table function's single exposed column is the one
// named by its argument, so `u.number` resolves and `u.nope` does not.
func TestAQualifiedReferenceToAQueryLocalRelationIsVerified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want []string
	}{{
		name: "a CTE column outside its output",
		sql:  "WITH c AS (SELECT name FROM ev2) SELECT c.nope AS n FROM c",
	}, {
		name: "a CTE column it does output",
		sql:  "WITH c AS (SELECT name FROM ev2) SELECT c.name AS n FROM c",
		want: []string{"ev2.name -> __result__.n"},
	}, {
		name: "a derived table column outside its output",
		sql:  "SELECT d.nope AS n FROM (SELECT name FROM ev2) d",
	}, {
		name: "a table function column it does not expose",
		sql:  "SELECT u.nope AS n FROM numbers(\"number\" = \"2\") u",
	}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			relations, err := analyzeSQL(tc.sql, variantCatalog())
			got := make([]string, 0, len(relations))
			for _, relation := range relations {
				got = append(got, edgeText(relation))
			}
			if tc.want == nil {
				var unsupported *lineage.UnsupportedStatementError
				require.ErrorAs(t, err, &unsupported)
				require.Contains(t, unsupported.Error(), "unresolved reference in an output column")
			} else {
				require.NoError(t, err)
			}
			require.ElementsMatch(t, tc.want, got, "sql=%q", tc.sql)
		})
	}
}

// TestAQualifiedReferenceWithoutCatalogEvidenceKeepsItsReading pins what the rule
// must not touch. A relation the catalog cannot describe, one it describes with no
// columns at all, and one no catalog is available for are all unknown rather than
// empty, so the reading the analyzer always had stays exactly as it was: a partial
// catalog never turns an edge into a gap on its own.
func TestAQualifiedReferenceWithoutCatalogEvidenceKeepsItsReading(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		cat  catalog.Provide
		want []string
	}{
		{
			name: "no catalog at all",
			sql:  "SELECT ev2.nope AS n FROM ev2",
			want: []string{"ev2.nope -> __result__.n"},
		},
		{
			name: "a relation the catalog does not know",
			sql:  "SELECT ev9.nope AS n FROM ev9",
			cat:  variantCatalog(),
			want: []string{"ev9.nope -> __result__.n"},
		},
		{
			name: "a relation whose column list is empty",
			sql:  "SELECT evE.nope AS n FROM evE",
			cat:  emptyColumnCatalog(),
			want: []string{"evE.nope -> __result__.n"},
		},
		{
			name: "a column name in another case is the column",
			sql:  "SELECT ev2.NAME AS n FROM ev2",
			cat:  variantCatalog(),
			want: []string{"ev2.NAME -> __result__.n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requireEdgesWithCatalog(t, tc.sql, tc.cat, tc.want)
		})
	}
}

// emptyColumnCatalog registers one relation the registry holds without a column
// list: a table whose metadata has not been synced yet. The lookup answers with no
// names, which is unknown rather than empty.
func emptyColumnCatalog() catalog.Provide {
	cat := testutil.NewMemoryCatalogProvide()
	cat.AddTable(&catalog.TableMeta{ID: model.ObjectIdentifier{Name: "evE"}})
	return cat
}

// TestASchemaQualifiedReferenceIsVerifiedAgainstThatDatabase pins the shape a
// production registry answers: the relation an unqualified FROM clause registers
// carries the statement's database, and the column the statement wrote is verified
// against that relation's own column list rather than against the name alone.
func TestASchemaQualifiedReferenceIsVerifiedAgainstThatDatabase(t *testing.T) {
	t.Parallel()

	cat := testutil.NewMemoryCatalogProvide()
	cat.AddTable(&catalog.TableMeta{
		ID:      model.ObjectIdentifier{Database: "db1", Name: "ev2"},
		Columns: []catalog.ColumnMeta{{Name: "name", Type: "string"}},
	})

	requireEdgesWithCatalog(t, "SELECT db1.ev2.name AS n FROM db1.ev2", cat, []string{"db1.ev2.name -> __result__.n"})

	relations, err := analyzeSQL("SELECT db1.ev2.nope AS n FROM db1.ev2", cat)

	var unsupported *lineage.UnsupportedStatementError
	require.ErrorAs(t, err, &unsupported)
	require.Contains(t, unsupported.Error(), "unresolved reference in an output column: ev2.nope")
	require.Empty(t, relations)
}
