package catalog

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestOutputColumns(t *testing.T) {
	t.Parallel()

	column := func(name string) *storepb.ColumnMetadata {
		return &storepb.ColumnMetadata{Name: name, Type: "text", Nullable: true}
	}

	tests := []struct {
		name  string
		meta  *storepb.StoredMetadata
		want  []ColumnMeta
		known bool
	}{
		{
			name: "table",
			meta: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
				Name:    "t",
				Columns: []*storepb.ColumnMetadata{column("a"), column("b")},
			}}},
			want:  []ColumnMeta{{Name: "a", Type: "text", Nullable: true}, {Name: "b", Type: "text", Nullable: true}},
			known: true,
		},
		{
			name: "view",
			meta: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{
				Name:    "v",
				Columns: []*storepb.ColumnMetadata{column("a")},
			}}},
			want:  []ColumnMeta{{Name: "a", Type: "text", Nullable: true}},
			known: true,
		},
		{
			// A materialized view exposes its own output columns, not its
			// dependency (source) columns.
			name: "materialized view",
			meta: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{
				Name:              "mv",
				Columns:           []*storepb.ColumnMetadata{column("total")},
				DependencyColumns: []*storepb.DependencyColumn{{Schema: "public", Table: "orders", Column: "amount"}},
			}}},
			want:  []ColumnMeta{{Name: "total", Type: "text", Nullable: true}},
			known: true,
		},
		{
			// An instance whose materialized views were synced before the
			// output-column list existed must keep the wildcard fallback rather
			// than expanding to nothing.
			name: "materialized view without columns is unknown",
			meta: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{
				Name:              "mv",
				DependencyColumns: []*storepb.DependencyColumn{{Schema: "public", Table: "orders", Column: "amount"}},
			}}},
			want:  nil,
			known: false,
		},
		{
			name: "object type without a column list",
			meta: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_DatabaseSchemaMetadata{DatabaseSchemaMetadata: &storepb.DatabaseSchemaMetadata{
				Name: "db",
			}}},
			want:  nil,
			known: false,
		},
		{
			name:  "missing metadata",
			meta:  nil,
			want:  nil,
			known: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			columns, known := outputColumns(test.meta)
			require.Equal(t, test.known, known)
			require.Equal(t, test.want, columns)
		})
	}
}
