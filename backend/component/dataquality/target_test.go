package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// primaryKeyColumns feeds the failure sample's row identity, so it must keep the
// key order, use the registry's own spelling, and refuse an index key that is
// not a plain column.
func TestPrimaryKeyColumns(t *testing.T) {
	t.Parallel()

	columns := []ColumnInfo{{Name: "TeamID"}, {Name: "agent_id"}, {Name: "email"}}
	table := func(indexes ...*storepb.IndexMetadata) *storepb.StoredMetadata {
		return &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{
			TableMetadata: &storepb.TableMetadata{Indexes: indexes},
		}}
	}

	require.Equal(t,
		[]string{"TeamID", "agent_id"},
		primaryKeyColumns(table(&storepb.IndexMetadata{
			Name:        "team_agent_pkey",
			Primary:     true,
			Expressions: []string{"teamid", "agent_id"},
		}), storepb.MetaType_TABLE, columns))

	// A unique index that is not the primary key does not identify a row.
	require.Nil(t, primaryKeyColumns(table(&storepb.IndexMetadata{
		Name:        "email_key",
		Unique:      true,
		Expressions: []string{"email"},
	}), storepb.MetaType_TABLE, columns))

	// A functional key names no column to select; the sample falls back to the
	// checked columns rather than guessing what the expression means.
	require.Nil(t, primaryKeyColumns(table(&storepb.IndexMetadata{
		Name:        "lower_email_pkey",
		Primary:     true,
		Expressions: []string{"lower(email)"},
	}), storepb.MetaType_TABLE, columns))

	// Relations the sync records no index for have no key to add.
	require.Nil(t, primaryKeyColumns(&storepb.StoredMetadata{}, storepb.MetaType_TABLE, columns))
	require.Nil(t, primaryKeyColumns(&storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_ExternalTableMetadata{
			ExternalTableMetadata: &storepb.ExternalTableMetadata{},
		},
	}, storepb.MetaType_EXTERNAL_TABLE, columns))
}
