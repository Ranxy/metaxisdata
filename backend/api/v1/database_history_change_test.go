package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The timeline summary is user-facing copy: the section counts are joined in the
// fixed section order and each label is pluralized, including the irregular
// "index"/"property" forms and the multi-word labels, which pluralize only their
// last word.
func TestBuildMetadataHistorySummaryPluralizesSectionLabels(t *testing.T) {
	t.Parallel()

	sections := []v1pb.MetadataHistorySection{
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_INDEX,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_FOREIGN_KEY,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_CHECK_CONSTRAINT,
		v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF,
	}
	changes := func(count int, operation v1pb.MetadataHistoryOperation) []*v1pb.MetadataHistoryChangeItem {
		items := make([]*v1pb.MetadataHistoryChangeItem, 0, count)
		for range count {
			items = append(items, &v1pb.MetadataHistoryChangeItem{Operation: operation})
		}
		return items
	}
	group := func(section v1pb.MetadataHistorySection, items []*v1pb.MetadataHistoryChangeItem) *v1pb.MetadataHistoryChangeGroup {
		return &v1pb.MetadataHistoryChangeGroup{Section: section, Changes: items}
	}
	created, updated := v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED

	require.Equal(t,
		"+2 columns, +2 indexes, +2 foreign keys, +2 check constraints, ~2 properties",
		buildMetadataHistorySummary(updated, []*v1pb.MetadataHistoryChangeGroup{
			group(sections[0], changes(2, created)),
			group(sections[1], changes(2, created)),
			group(sections[2], changes(2, created)),
			group(sections[3], changes(2, created)),
			group(sections[4], changes(2, updated)),
		}))

	require.Equal(t,
		"+1 column, +1 index, +1 foreign key, +1 check constraint, +1 property",
		buildMetadataHistorySummary(updated, []*v1pb.MetadataHistoryChangeGroup{
			group(sections[0], changes(1, created)),
			group(sections[1], changes(1, created)),
			group(sections[2], changes(1, created)),
			group(sections[3], changes(1, created)),
			group(sections[4], changes(1, created)),
		}))
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
