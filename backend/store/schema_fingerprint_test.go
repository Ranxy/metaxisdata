package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func fingerprintColumn(name, columnType string, nullable bool) *storepb.ColumnMetadata {
	return &storepb.ColumnMetadata{Name: name, Type: columnType, Nullable: nullable}
}

func fingerprintTable(columns []*storepb.ColumnMetadata, indexes []*storepb.IndexMetadata) *storepb.StoredMetadata {
	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
			Name:    "orders",
			Columns: columns,
			Indexes: indexes,
		}},
	}
}

func fingerprintView(definition string, columns []*storepb.ColumnMetadata) *storepb.StoredMetadata {
	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: &storepb.ViewMetadata{
			Name:       "order_summary",
			Definition: definition,
			Columns:    columns,
		}},
	}
}

func mustSchemaFingerprint(t *testing.T, meta *storepb.StoredMetadata) []byte {
	t.Helper()
	fingerprint, ok := CalcSchemaFingerprint(meta)
	require.True(t, ok, "the metadata should carry a shape")
	require.Len(t, fingerprint, 32)
	return fingerprint
}

// Two tables with the same columns, order, nullability and primary key are the
// same shape to a check, whatever their comments, defaults, engines, collations,
// row counts or secondary indexes say. A comment edit must never read as
// "the structure changed".
func TestSchemaFingerprintIgnoresEverythingAFieldSemanticsDoesNotDependOn(t *testing.T) {
	t.Parallel()

	base := fingerprintTable(
		[]*storepb.ColumnMetadata{
			fingerprintColumn("id", "bigint", false),
			fingerprintColumn("name", "varchar(64)", true),
		},
		[]*storepb.IndexMetadata{{Name: "PRIMARY", Primary: true, Expressions: []string{"id"}}},
	)
	changed := fingerprintTable(
		[]*storepb.ColumnMetadata{
			{Name: "id", Type: "bigint", Nullable: false, Comment: "the identifier", Default: "0", CharacterSet: "utf8mb4", Collation: "utf8mb4_bin"},
			{Name: "name", Type: "varchar(64)", Nullable: true, Comment: "display name", Default: "'x'"},
		},
		[]*storepb.IndexMetadata{
			{Name: "idx_name", Expressions: []string{"name"}, Unique: true, Comment: "lookup"},
			{Name: "PRIMARY", Primary: true, Expressions: []string{"id"}, Type: "BTREE", Unique: true, Visible: true},
		},
	)
	changed.GetTableMetadata().Engine = "InnoDB"
	changed.GetTableMetadata().Collation = "utf8mb4_general_ci"
	changed.GetTableMetadata().Charset = "utf8mb4"
	changed.GetTableMetadata().RowCount = 4242
	changed.GetTableMetadata().DataSize = 65536

	require.Equal(t, mustSchemaFingerprint(t, base), mustSchemaFingerprint(t, changed))
}

func TestSchemaFingerprintTracksColumnStructure(t *testing.T) {
	t.Parallel()

	base := fingerprintTable(
		[]*storepb.ColumnMetadata{
			fingerprintColumn("id", "bigint", false),
			fingerprintColumn("name", "varchar(64)", true),
		},
		nil,
	)
	baseFingerprint := mustSchemaFingerprint(t, base)

	cases := map[string]*storepb.StoredMetadata{
		"column added": fingerprintTable(
			[]*storepb.ColumnMetadata{
				fingerprintColumn("id", "bigint", false),
				fingerprintColumn("name", "varchar(64)", true),
				fingerprintColumn("note", "text", true),
			}, nil),
		"column removed": fingerprintTable(
			[]*storepb.ColumnMetadata{fingerprintColumn("id", "bigint", false)}, nil),
		"column renamed": fingerprintTable(
			[]*storepb.ColumnMetadata{
				fingerprintColumn("id", "bigint", false),
				fingerprintColumn("label", "varchar(64)", true),
			}, nil),
		"column type changed": fingerprintTable(
			[]*storepb.ColumnMetadata{
				fingerprintColumn("id", "bigint", false),
				fingerprintColumn("name", "varchar(128)", true),
			}, nil),
		"nullability changed": fingerprintTable(
			[]*storepb.ColumnMetadata{
				fingerprintColumn("id", "bigint", false),
				fingerprintColumn("name", "varchar(64)", false),
			}, nil),
		"column order changed": fingerprintTable(
			[]*storepb.ColumnMetadata{
				fingerprintColumn("name", "varchar(64)", true),
				fingerprintColumn("id", "bigint", false),
			}, nil),
	}

	for name, meta := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.NotEqual(t, baseFingerprint, mustSchemaFingerprint(t, meta))
		})
	}
}

func TestSchemaFingerprintTracksThePrimaryKey(t *testing.T) {
	t.Parallel()

	columns := []*storepb.ColumnMetadata{
		fingerprintColumn("id", "bigint", false),
		fingerprintColumn("tenant", "bigint", false),
	}
	withoutPrimary := fingerprintTable(columns, nil)
	singleKey := fingerprintTable(columns, []*storepb.IndexMetadata{{Name: "PRIMARY", Primary: true, Expressions: []string{"id"}}})
	compositeKey := fingerprintTable(columns, []*storepb.IndexMetadata{{Name: "PRIMARY", Primary: true, Expressions: []string{"id", "tenant"}}})
	renamedPrimary := fingerprintTable(columns, []*storepb.IndexMetadata{{Name: "orders_pkey", Primary: true, Expressions: []string{"id"}, Comment: "moved"}})

	withoutPrimaryFingerprint := mustSchemaFingerprint(t, withoutPrimary)
	require.NotEqual(t, withoutPrimaryFingerprint, mustSchemaFingerprint(t, singleKey))
	require.NotEqual(t, mustSchemaFingerprint(t, singleKey), mustSchemaFingerprint(t, compositeKey))
	// The constraint's name and comment are not the key.
	require.Equal(t, mustSchemaFingerprint(t, singleKey), mustSchemaFingerprint(t, renamedPrimary))
}

// A view's SQL body is its structure: its columns can stay the same while it
// starts reading a different query, and a check on the view has to know.
func TestSchemaFingerprintTracksAViewDefinition(t *testing.T) {
	t.Parallel()

	columns := []*storepb.ColumnMetadata{fingerprintColumn("id", "bigint", false)}
	first := fingerprintView("SELECT id FROM orders", columns)
	second := fingerprintView("SELECT id FROM orders WHERE deleted = 0", columns)
	commented := fingerprintView("SELECT id FROM orders", columns)
	commented.GetViewMetadata().Comment = "live orders"
	commented.GetViewMetadata().DependencyColumns = []*storepb.DependencyColumn{{Column: "id"}}

	firstFingerprint := mustSchemaFingerprint(t, first)
	require.NotEqual(t, firstFingerprint, mustSchemaFingerprint(t, second))
	// Comments and dependency lists do not change what the view returns.
	require.Equal(t, firstFingerprint, mustSchemaFingerprint(t, commented))
}

func TestSchemaFingerprintCoversMaterializedViewsAndExternalTables(t *testing.T) {
	t.Parallel()

	materialized := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{
			Name:       "mv_orders",
			Definition: "SELECT id FROM orders",
			Columns:    []*storepb.ColumnMetadata{fingerprintColumn("id", "bigint", false)},
		}},
	}
	redefined := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: &storepb.MaterializedViewMetadata{
			Name:       "mv_orders",
			Definition: "SELECT id FROM orders WHERE id > 0",
			Columns:    []*storepb.ColumnMetadata{fingerprintColumn("id", "bigint", false)},
		}},
	}
	require.NotEqual(t, mustSchemaFingerprint(t, materialized), mustSchemaFingerprint(t, redefined))

	external := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ExternalTableMetadata{ExternalTableMetadata: &storepb.ExternalTableMetadata{
			Name:    "ext_orders",
			Columns: []*storepb.ColumnMetadata{fingerprintColumn("id", "bigint", false)},
		}},
	}
	externalChanged := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ExternalTableMetadata{ExternalTableMetadata: &storepb.ExternalTableMetadata{
			Name:    "ext_orders",
			Columns: []*storepb.ColumnMetadata{fingerprintColumn("id", "int", false)},
		}},
	}
	require.NotEqual(t, mustSchemaFingerprint(t, external), mustSchemaFingerprint(t, externalChanged))
}

func TestSchemaFingerprintIsNotDefinedForTypesWithoutAShape(t *testing.T) {
	t.Parallel()

	cases := map[string]*storepb.StoredMetadata{
		"nil":         nil,
		"column":      {Type: &storepb.StoredMetadata_ColumnMetadata{ColumnMetadata: fingerprintColumn("id", "bigint", false)}},
		"database":    {Type: &storepb.StoredMetadata_DatabaseSchemaMetadata{DatabaseSchemaMetadata: &storepb.DatabaseSchemaMetadata{Name: "db"}}},
		"schema":      {Type: &storepb.StoredMetadata_SchemaMetadata{SchemaMetadata: &storepb.SchemaMetadata{Name: "public"}}},
		"sequence":    {Type: &storepb.StoredMetadata_SequenceMetadata{SequenceMetadata: &storepb.SequenceMetadata{Name: "seq"}}},
		"function":    {Type: &storepb.StoredMetadata_FunctionMetadata{FunctionMetadata: &storepb.FunctionMetadata{Name: "f"}}},
		"procedure":   {Type: &storepb.StoredMetadata_ProcedureMetadata{ProcedureMetadata: &storepb.ProcedureMetadata{Name: "p"}}},
		"unspecified": {},
	}
	for name, meta := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ok := CalcSchemaFingerprint(meta)
			require.False(t, ok)
		})
	}
}

func TestSchemaFingerprintIsDeterministic(t *testing.T) {
	t.Parallel()

	meta := fingerprintTable(
		[]*storepb.ColumnMetadata{
			fingerprintColumn("id", "bigint", false),
			fingerprintColumn("tenant", "bigint", false),
		},
		[]*storepb.IndexMetadata{{Name: "PRIMARY", Primary: true, Expressions: []string{"id", "tenant"}}},
	)
	require.Equal(t, mustSchemaFingerprint(t, meta), mustSchemaFingerprint(t, meta))
}
