package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestPluralize(t *testing.T) {
	t.Parallel()

	require.Equal(t, "column", pluralize("column", 1))
	require.Equal(t, "columns", pluralize("column", 2))
	require.Equal(t, "indexes", pluralize("index", 2))
	require.Equal(t, "properties", pluralize("property", 3))
	require.Equal(t, "foreign keys", pluralize("foreign key", 2))
	require.Equal(t, "check constraints", pluralize("check constraint", 0))
	require.Equal(t, "changes", pluralize("change", 2))
}

// A foreign table keeps its columns inside its own metadata, so its history has
// to report a column group as well as the external server it points at.
func TestExternalTableHistoryChangeGroups(t *testing.T) {
	t.Parallel()

	history := func(server string, columns ...*storepb.ColumnMetadata) *store.MetaRegistryHistory {
		return &store.MetaRegistryHistory{Metadata: &storepb.StoredMetadata{
			Type: &storepb.StoredMetadata_ExternalTableMetadata{ExternalTableMetadata: &storepb.ExternalTableMetadata{
				Name:                 "foreign_users",
				ExternalServerName:   server,
				ExternalDatabaseName: "remote",
				Columns:              columns,
			}},
		}}
	}
	before := history("old_server", &storepb.ColumnMetadata{Name: "user_id", Type: "integer"})
	after := history("new_server",
		&storepb.ColumnMetadata{Name: "user_id", Type: "integer"},
		&storepb.ColumnMetadata{Name: "user_name", Type: "text"},
	)

	groups := buildMetadataHistoryChangeGroups(
		v1pb.MetaType_EXTERNAL_TABLE, before, after,
		v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED,
	)
	require.Len(t, groups, 2)

	require.Equal(t, v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF, groups[0].Section)
	require.Len(t, groups[0].Changes, 1)
	require.Equal(t, []*v1pb.MetadataFieldChange{{
		Field:       "external_server_name",
		DisplayName: "external server",
		Before:      "old_server",
		After:       "new_server",
	}}, groups[0].Changes[0].FieldChanges)

	require.Equal(t, v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN, groups[1].Section)
	require.Len(t, groups[1].Changes, 1)
	require.Equal(t, "user_name", groups[1].Changes[0].Key)
}
