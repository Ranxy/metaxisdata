package doris

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// variantCatalog registers the relations these tests read. The types matter: only
// a VARIANT column takes the dot form, which is what the engine does with the same
// statements (checked against a live Doris 4.1.4).
func variantCatalog() catalog.Provide {
	cat := testutil.NewMemoryCatalogProvide()
	add := func(name string, columns ...catalog.ColumnMeta) {
		cat.AddTable(&catalog.TableMeta{ID: model.ObjectIdentifier{Name: name}, Columns: columns})
	}
	add("ev",
		catalog.ColumnMeta{Name: "id", Type: "bigint(20)"},
		catalog.ColumnMeta{Name: "v", Type: "variant"})
	add("ev2",
		catalog.ColumnMeta{Name: "k", Type: "int"},
		catalog.ColumnMeta{Name: "name", Type: "string"})
	add("ev3",
		catalog.ColumnMeta{Name: "k", Type: "int"},
		catalog.ColumnMeta{Name: "user", Type: "string"})
	add("a", catalog.ColumnMeta{Name: "v", Type: "variant"})
	add("b", catalog.ColumnMeta{Name: "v", Type: "variant"})
	return cat
}

// TestVARIANTSubcolumnPathLineage pins the shapes a VARIANT subcolumn path takes.
// Doris reads a dotted reference into one column, so that column stays the whole
// source of the output column and the path never becomes an endpoint.
func TestVARIANTSubcolumnPathLineage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "a dotted path reads the VARIANT column it indexes",
			sql:  "SELECT v.user.name AS n FROM ev",
			want: []string{"ev.v -> __result__.n"},
		},
		{
			name: "the output column keeps the path the statement wrote",
			sql:  "SELECT v.user.name FROM ev",
			want: []string{"ev.v -> __result__.v.user.name"},
		},
		{
			name: "a relation may qualify the path's base column",
			sql:  "SELECT ev.v.user.name AS n FROM ev",
			want: []string{"ev.v -> __result__.n"},
		},
		{
			name: "a path in a predicate is an influence on the rows",
			sql:  "SELECT id FROM ev WHERE v.user.name = 'alice'",
			want: []string{"ev.id -> __result__.id", "ev.v -> __result__."},
		},
		{
			name: "a path in a generator argument reads the same column",
			sql:  "SELECT * FROM ev LATERAL VIEW explode(v.tags) u AS tag",
			want: []string{"ev.id -> __result__.id", "ev.v -> __result__.v", "ev.v -> __result__.tag"},
		},
		{
			// The relation reading keeps priority, as the engine gives it: this
			// reads ev2's column `name`, not a path on ev's VARIANT column `v`.
			name: "a relation prefix beats a VARIANT path of the same name",
			sql:  "SELECT v.name AS n FROM ev, ev2 v",
			want: []string{"ev2.name -> __result__.n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requireEdgesWithCatalog(t, tc.sql, variantCatalog(), tc.want)
		})
	}
}

// TestAVARIANTReadNeedsCatalogEvidence pins the gate. A dotted reference is read
// as a path only when the catalog says its base column is a VARIANT column:
// otherwise it keeps the unresolved gap it had before, so a scalar column's
// invented "field", a name no relation owns, a relation whose column of that name
// is another type, and two VARIANT columns of one name all stay unguessed.
func TestAVARIANTReadNeedsCatalogEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		cat  catalog.Provide
	}{
		{name: "no catalog at all", sql: "SELECT v.user.name AS n FROM ev"},
		{name: "the base column is not VARIANT", sql: "SELECT id.name AS n FROM ev", cat: variantCatalog()},
		{name: "no relation owns the name", sql: "SELECT x.name AS n FROM ev", cat: variantCatalog()},
		{
			name: "a relation prefix owns the column as another type",
			sql:  "SELECT v.user.name AS n FROM ev, ev3 v",
			cat:  variantCatalog(),
		},
		{name: "two relations expose the VARIANT column", sql: "SELECT v.name AS n FROM a, b", cat: variantCatalog()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			relations, err := analyzeSQL(tc.sql, tc.cat)
			var unsupported *lineage.UnsupportedStatementError
			require.ErrorAs(t, err, &unsupported)
			require.Empty(t, relations, "a reference without evidence is a gap, not an edge")
		})
	}
}

// TestIsVariantType pins the one type the dot form is read into. Doris reports it
// lowercase, and JSON is not it: the same engine rejects `j.a` on a JSON column.
func TestIsVariantType(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		dataType string
		want     bool
	}{
		{dataType: "variant", want: true},
		{dataType: "VARIANT", want: true},
		{dataType: " Variant ", want: true},
		{dataType: "variant<properties('variant_max_subcolumns_count'='2048')>", want: true},
		{dataType: "json", want: false},
		{dataType: "bigint(20)", want: false},
		{dataType: "variantx", want: false},
		{dataType: "", want: false},
	} {
		require.Equal(t, tc.want, isVariantType(tc.dataType), "type=%q", tc.dataType)
	}
}
