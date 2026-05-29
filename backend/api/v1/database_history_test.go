package v1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestBuildMetadataHistoryEventContexts(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.May, 29, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(2 * time.Hour)
	t2 := t1.Add(2 * time.Hour)

	events := buildMetadataHistoryEventContexts([]*store.MetaRegistryHistory{
		{GUID: "inst;db;public;users", ObjectType: storepb.MetaType_TABLE, ValidFrom: t0, ValidTo: &t1},
		{GUID: "inst;db;public;users", ObjectType: storepb.MetaType_TABLE, ValidFrom: t1, ValidTo: &t2},
	})

	require.Len(t, events, 3)
	require.Equal(t, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, events[0].operation)
	require.Equal(t, t0, events[0].eventTime)
	require.Equal(t, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, events[1].operation)
	require.Equal(t, t1, events[1].eventTime)
	require.Equal(t, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, events[2].operation)
	require.Equal(t, t2, events[2].eventTime)
}

func TestBuildMetadataHistoryEventResultForTable(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.May, 29, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	before := &store.MetaRegistryHistory{
		GUID:       "inst;db;public;users",
		ObjectType: storepb.MetaType_TABLE,
		ValidFrom:  t0,
		ValidTo:    &t1,
		Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
			Name:    "users",
			Comment: "before",
			Columns: []*storepb.ColumnMetadata{{Name: "id", Type: "INT", Position: 1}, {Name: "age", Type: "INT", Position: 2}},
			Indexes: []*storepb.IndexMetadata{{Name: "idx_users_age", Expressions: []string{"age"}, Type: "BTREE"}},
		}}},
	}
	after := &store.MetaRegistryHistory{
		GUID:       "inst;db;public;users",
		ObjectType: storepb.MetaType_TABLE,
		ValidFrom:  t1,
		Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
			Name:    "users",
			Comment: "after",
			Columns: []*storepb.ColumnMetadata{{Name: "id", Type: "BIGINT", Position: 1}, {Name: "email", Type: "TEXT", Position: 2}},
			Indexes: []*storepb.IndexMetadata{{Name: "idx_users_email", Expressions: []string{"email"}, Type: "BTREE"}},
		}}},
	}

	result, err := buildMetadataHistoryEventResult("inst;db;public;users", v1pb.MetaType_TABLE, metadataHistoryEventContext{
		eventTime: t1,
		validFrom: t1,
		operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED,
		before:    before,
		after:     after,
	})
	require.NoError(t, err)
	require.NotNil(t, result.Entry)
	require.Equal(t, "+1 column ~1 column -1 column, +1 index -1 index, ~1 property", result.Entry.GetSummary())
	require.Len(t, result.ChangeGroups, 3)
	require.Equal(t, v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_SELF, result.ChangeGroups[0].Section)
	require.Equal(t, v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN, result.ChangeGroups[1].Section)
	require.Equal(t, v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_INDEX, result.ChangeGroups[2].Section)
	require.Len(t, result.ChangeGroups[1].Changes, 3)
	require.Len(t, result.ChangeGroups[2].Changes, 2)
	var columnCounts *v1pb.MetadataHistorySectionChangeCount
	for _, item := range result.Entry.GetSectionChanges() {
		if item.GetSection() == v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_COLUMN {
			columnCounts = item
			break
		}
	}
	require.NotNil(t, columnCounts)
	require.Equal(t, int32(1), columnCounts.GetAdded())
	require.Equal(t, int32(1), columnCounts.GetUpdated())
	require.Equal(t, int32(1), columnCounts.GetRemoved())
}

func TestBuildMetadataHistoryEventResultForManualSQL(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, time.May, 29, 12, 0, 0, 0, time.UTC)

	result, err := buildMetadataHistoryEventResult("inst;db;public;__manual_sql__/active_users", v1pb.MetaType_MANUAL_SQL, metadataHistoryEventContext{
		eventTime: t0,
		validFrom: t0,
		operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED,
		after: &store.MetaRegistryHistory{
			GUID:       "inst;db;public;__manual_sql__/active_users",
			ObjectType: storepb.MetaType_MANUAL_SQL,
			ValidFrom:  t0,
			Metadata: &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ManualSqlMetadata{ManualSqlMetadata: &storepb.ManualSQLMetadata{
				ManualSqlId: "active_users",
				Name:        "active_users",
				Title:       "Active Users",
				SqlText:     "SELECT id FROM users",
				Tags:        []string{"daily", "report"},
				Attributes:  map[string]string{"owner": "analytics"},
			}}},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "created, +2 tags, +1 attribute", result.Entry.GetSummary())
	require.Len(t, result.ChangeGroups, 2)
	require.Equal(t, v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TAG, result.ChangeGroups[0].Section)
	require.Equal(t, v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_ATTRIBUTE, result.ChangeGroups[1].Section)
}
