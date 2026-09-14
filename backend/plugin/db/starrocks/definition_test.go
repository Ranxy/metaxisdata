package starrocks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func TestShowCreateQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		objectType storepb.MetaType
		want       string
		wantOK     bool
	}{
		{
			name:       "table",
			objectType: storepb.MetaType_TABLE,
			want:       "SHOW CREATE TABLE `db1`.`t1`",
			wantOK:     true,
		},
		{
			name:       "view",
			objectType: storepb.MetaType_VIEW,
			want:       "SHOW CREATE VIEW `db1`.`t1`",
			wantOK:     true,
		},
		{
			name:       "materialized view",
			objectType: storepb.MetaType_MATERIALIZED_VIEW,
			want:       "SHOW CREATE MATERIALIZED VIEW `db1`.`t1`",
			wantOK:     true,
		},
		{
			// Identifiers cannot be bound as parameters, so an embedded
			// backtick must be escaped by doubling.
			name:       "escapes backticks",
			objectType: storepb.MetaType_TABLE,
			want:       "SHOW CREATE TABLE `db1`.`we``ird`",
			wantOK:     true,
		},
		{
			name:       "sequence has no definition",
			objectType: storepb.MetaType_SEQUENCE,
			want:       "",
			wantOK:     false,
		},
		{
			name:       "function has no definition",
			objectType: storepb.MetaType_FUNCTION,
			want:       "",
			wantOK:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			name := "t1"
			if tc.name == "escapes backticks" {
				name = "we`ird"
			}
			got, ok := showCreateQuery("db1", tc.objectType, name)
			require.Equal(t, tc.wantOK, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

// Unsupported object types must be answered without touching the connection: the
// driver is built with a nil *sql.DB here, so a query would panic instead of
// failing the assertion.
func TestGetObjectDefinitionSkipsUnsupportedTypes(t *testing.T) {
	t.Parallel()

	d := &Driver{}
	for _, objectType := range []storepb.MetaType{
		storepb.MetaType_UNSPECIFIED,
		storepb.MetaType_DATABASE,
		storepb.MetaType_SCHEMA,
		storepb.MetaType_COLUMN,
		storepb.MetaType_SEQUENCE,
		storepb.MetaType_FUNCTION,
		storepb.MetaType_PROCEDURE,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_MANUAL_SQL,
	} {
		definition, ok, err := d.GetObjectDefinition(context.Background(), objectType, "t1")
		require.NoError(t, err, objectType.String())
		require.False(t, ok, objectType.String())
		require.Empty(t, definition, objectType.String())
	}
}

// SHOW CREATE returns four columns for a view on MySQL/StarRocks and two on
// Doris, so the DDL column has to be found by name.
func TestCreateColumnIndex(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, createColumnIndex([]string{"Table", "Create Table"}))
	require.Equal(t, 1, createColumnIndex([]string{"View", "Create View", "character_set_client", "collation_connection"}))
	require.Equal(t, 1, createColumnIndex([]string{"Name", "Create Materialized View"}))
	require.Equal(t, 0, createColumnIndex([]string{"Create Table", "Table"}))
	require.Equal(t, -1, createColumnIndex([]string{"Table", "Table"}))
}
