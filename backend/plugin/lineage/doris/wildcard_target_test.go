package doris

import (
	"testing"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/testutil"
)

// wildcardCatalog registers a source and two targets for the wildcard cases: `dst`
// shares the source's column names and `dst2` renames the second one, so a target list
// that is filled from the wrong position, or filled entirely from its first entry, is
// visible in the edge set.
func wildcardCatalog() catalog.Provide {
	cat := testutil.NewMemoryCatalogProvide()
	add := func(name string, columns ...string) {
		meta := &catalog.TableMeta{ID: model.ObjectIdentifier{Name: name}}
		for _, column := range columns {
			meta.Columns = append(meta.Columns, catalog.ColumnMeta{Name: column})
		}
		cat.AddTable(meta)
	}
	add("src", "k", "name")
	add("dst", "k", "name")
	add("dst2", "k", "user")
	return cat
}

// TestAWildcardOverAQueryLocalRelationFillsTheTargetListPositionally pins the declared
// target list being filled from the wildcard's own position. `SELECT *` over a
// query-local relation is a single output column that expands into one edge per column
// of that relation, so the target list cannot be read per output column: the relation's
// own columns take the list's entries in order, starting where the wildcard sits.
//
// The base-table control shows where the rule came from — there `*` expands per catalog
// column — and the last two cases keep the readings that already held: with no declared
// list each edge is named after its own column, and a renamed target takes the entry at
// its position.
func TestAWildcardOverAQueryLocalRelationFillsTheTargetListPositionally(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "a derived table",
			sql:  "INSERT INTO dst (k, name) SELECT * FROM (SELECT k, name FROM src) d",
			want: []string{"src.k -> dst.k", "src.name -> dst.name"},
		},
		{
			name: "a base table control",
			sql:  "INSERT INTO dst (k, name) SELECT * FROM src",
			want: []string{"src.k -> dst.k", "src.name -> dst.name"},
		},
		{
			name: "a renamed target",
			sql:  "INSERT INTO dst2 (k, user) SELECT * FROM (SELECT k, name FROM src) d",
			want: []string{"src.k -> dst2.k", "src.name -> dst2.user"},
		},
		{
			name: "a table wildcard",
			sql:  "INSERT INTO dst (k, name) SELECT d.* FROM (SELECT k, name FROM src) d",
			want: []string{"src.k -> dst.k", "src.name -> dst.name"},
		},
		{
			name: "no declared target list",
			sql:  "INSERT INTO dst SELECT * FROM (SELECT k, name FROM src) d",
			want: []string{"src.k -> dst.k", "src.name -> dst.name"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			requireEdgesWithCatalog(t, tc.sql, wildcardCatalog(), tc.want)
		})
	}
}
