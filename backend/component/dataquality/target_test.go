package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// primaryKeyColumns feeds the failure sample's row identity, so it must keep the
// key order, use the registry's own spelling, understand the way each engine
// prints an index key, and refuse a key that is not a plain column.
func TestPrimaryKeyColumns(t *testing.T) {
	t.Parallel()

	columns := []ColumnInfo{
		{Name: "id"},
		{Name: "ID"},
		{Name: "TeamID"},
		{Name: "agent_id"},
		{Name: "email"},
	}
	table := func(indexes ...*storepb.IndexMetadata) *storepb.StoredMetadata {
		return &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{
			TableMetadata: &storepb.TableMetadata{Indexes: indexes},
		}}
	}
	primary := func(expressions ...string) *storepb.IndexMetadata {
		return &storepb.IndexMetadata{Name: "pkey", Primary: true, Expressions: expressions}
	}

	require.Equal(t,
		[]string{"id", "agent_id"},
		primaryKeyColumns(table(primary("id", "agent_id")), storepb.MetaType_TABLE, columns))

	// PostgreSQL prints a mixed-case or reserved word the way it must be written,
	// so the key arrives quoted; SQL Server may bracket it.
	require.Equal(t,
		[]string{"TeamID"},
		primaryKeyColumns(table(primary(`"TeamID"`)), storepb.MetaType_TABLE, columns))
	require.Equal(t,
		[]string{"ID"},
		primaryKeyColumns(table(primary("[ID]")), storepb.MetaType_TABLE, columns))
	require.Equal(t,
		[]string{"agent_id"},
		primaryKeyColumns(table(primary("agent_id DESC")), storepb.MetaType_TABLE, columns))

	// A unique index that is not the primary key does not identify a row.
	require.Nil(t, primaryKeyColumns(table(&storepb.IndexMetadata{
		Name:        "email_key",
		Unique:      true,
		Expressions: []string{"email"},
	}), storepb.MetaType_TABLE, columns))

	// A functional key, or a key carrying a collation clause, names no single
	// column to select: the sample falls back to the checked columns.
	require.Nil(t, primaryKeyColumns(table(primary("lower(email)")), storepb.MetaType_TABLE, columns))
	require.Nil(t, primaryKeyColumns(table(primary(`email COLLATE "C"`)), storepb.MetaType_TABLE, columns))

	// Relations the sync records no index for have no key to add.
	require.Nil(t, primaryKeyColumns(&storepb.StoredMetadata{}, storepb.MetaType_TABLE, columns))
	require.Nil(t, primaryKeyColumns(&storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ExternalTableMetadata{
			ExternalTableMetadata: &storepb.ExternalTableMetadata{},
		},
	}, storepb.MetaType_EXTERNAL_TABLE, columns))
}
