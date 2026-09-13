package v1

import (
	"slices"
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
	}, false)

	require.Len(t, events, 3)
	require.Equal(t, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_CREATED, events[0].operation)
	require.Equal(t, t0, events[0].eventTime)
	require.Equal(t, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, events[1].operation)
	require.Equal(t, t1, events[1].eventTime)
	require.Equal(t, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, events[2].operation)
	require.Equal(t, t2, events[2].eventTime)

	// The oldest row of a page probe is context only: it tells the row after it
	// that it replaced it, and contributes no entry of its own.
	probed := buildMetadataHistoryEventContexts([]*store.MetaRegistryHistory{
		{GUID: "inst;db;public;users", ObjectType: storepb.MetaType_TABLE, ValidFrom: t0, ValidTo: &t1},
		{GUID: "inst;db;public;users", ObjectType: storepb.MetaType_TABLE, ValidFrom: t1, ValidTo: &t2},
	}, true)

	require.Len(t, probed, 2)
	require.Equal(t, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED, probed[0].operation)
	require.Equal(t, t1, probed[0].eventTime)
	require.Equal(t, t0, probed[0].before.ValidFrom, "the dropped row still supplies the replaced state")
	require.Equal(t, v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_DELETED, probed[1].operation)
	require.Equal(t, t2, probed[1].eventTime)
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

	result := buildMetadataHistoryEventResult("inst;db;public;users", v1pb.MetaType_TABLE, metadataHistoryEventContext{
		eventTime: t1,
		validFrom: t1,
		operation: v1pb.MetadataHistoryOperation_METADATA_HISTORY_OPERATION_UPDATED,
		before:    before,
		after:     after,
	})
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

	result := buildMetadataHistoryEventResult("inst;db;public;__manual_sql__/active_users", v1pb.MetaType_MANUAL_SQL, metadataHistoryEventContext{
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
	require.Equal(t, "created, +2 tags, +1 attribute", result.Entry.GetSummary())
	require.Len(t, result.ChangeGroups, 2)
	require.Equal(t, v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_TAG, result.ChangeGroups[0].Section)
	require.Equal(t, v1pb.MetadataHistorySection_METADATA_HISTORY_SECTION_ATTRIBUTE, result.ChangeGroups[1].Section)
}

// Generated columns, MSSQL identity sequences and the per-column index
// attributes are real changes; the comparators used to ignore them, so an
// update that only touched one of them showed as "no changes".
func TestCompareColumnFieldsCoversGenerationAndIdentity(t *testing.T) {
	t.Parallel()

	before := &v1pb.ColumnMetadata{
		Name:       "total",
		Generation: &v1pb.GenerationMetadata{Type: v1pb.GenerationMetadata_TYPE_STORED, Expression: "a + b"},
	}
	after := &v1pb.ColumnMetadata{
		Name:              "total",
		Generation:        &v1pb.GenerationMetadata{Type: v1pb.GenerationMetadata_TYPE_STORED, Expression: "a + c"},
		IdentitySeed:      5,
		IdentityIncrement: 2,
	}

	fields := map[string]*v1pb.MetadataFieldChange{}
	for _, change := range compareColumnFields(before, after) {
		fields[change.GetField()] = change
	}
	require.Contains(t, fields, "generation_expression")
	require.Contains(t, fields, "identity_seed")
	require.Contains(t, fields, "identity_increment")
	require.NotContains(t, fields, "type")
}

func TestDiffIndexGroupCoversPerColumnAttributes(t *testing.T) {
	t.Parallel()

	before := &v1pb.IndexMetadata{Name: "idx", KeyLength: []int64{-1}, Descending: []bool{false}}
	after := &v1pb.IndexMetadata{Name: "idx", KeyLength: []int64{10}, Descending: []bool{true}, OpclassNames: []string{"text_pattern_ops"}}

	group := diffIndexGroupFromList([]*v1pb.IndexMetadata{before}, []*v1pb.IndexMetadata{after})
	require.NotNil(t, group)
	require.Len(t, group.Changes, 1)

	fields := map[string]bool{}
	for _, change := range group.Changes[0].GetFieldChanges() {
		fields[change.GetField()] = true
	}
	require.True(t, fields["key_length"])
	require.True(t, fields["descending"])
	require.True(t, fields["opclass_names"])
}

func TestDiffForeignKeyGroupCoversMatchType(t *testing.T) {
	t.Parallel()

	before := &v1pb.TableMetadata{
		Name:        "orders",
		ForeignKeys: []*v1pb.ForeignKeyMetadata{{Name: "fk", MatchType: "SIMPLE"}},
	}
	after := &v1pb.TableMetadata{
		Name:        "orders",
		ForeignKeys: []*v1pb.ForeignKeyMetadata{{Name: "fk", MatchType: "FULL"}},
	}

	group := diffForeignKeyGroup(before, after)
	require.NotNil(t, group)
	require.Len(t, group.Changes, 1)
	require.Len(t, group.Changes[0].GetFieldChanges(), 1)
	require.Equal(t, "match_type", group.Changes[0].GetFieldChanges()[0].GetField())
}

// Paging must not depend on how much history exists: a page derived from the
// bounded probe has to match the page the whole history would produce, walk
// every entry exactly once, and end with an empty token.
func TestMetadataHistoryPageMatchesFullHistory(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.May, 29, 10, 0, 0, 0, time.UTC)
	history := buildDenseMetadataHistory(base, 40)

	full, _, err := metadataHistoryPage(history, &pageOffset{limit: len(history)*2 + 10})
	require.NoError(t, err)

	for _, size := range []int{1, 3, 7} {
		limit := size
		var collected []metadataHistoryEventContext
		token := ""
		for page := 0; ; page++ {
			require.Less(t, page, len(full)+1, "paging must terminate")
			offset := &pageOffset{limit: limit, offset: 0}
			if token != "" {
				parsed, err := parseLimitAndOffset(&pageSize{token: token, limit: limit, maximum: 1000})
				require.NoError(t, err)
				offset = parsed
			}

			probeSize := metadataHistoryProbeSize(offset)
			start := max(0, len(history)-probeSize)
			probe := slices.Clone(history[start:])
			slices.Reverse(probe)

			events, nextToken, err := metadataHistoryPage(probe, offset)
			require.NoError(t, err)
			collected = append(collected, events...)
			if nextToken == "" {
				break
			}
			token = nextToken
		}

		require.Len(t, collected, len(full), "page size %d must visit every event once", size)
		for i := range collected {
			require.Equal(t, full[i].eventTime, collected[i].eventTime, "page size %d entry %d", size, i)
			require.Equal(t, full[i].operation, collected[i].operation, "page size %d entry %d", size, i)
		}
	}
}

// buildDenseMetadataHistory returns ascending history rows covering creates,
// updates and deletions, one per hour.
func buildDenseMetadataHistory(base time.Time, count int) []*store.MetaRegistryHistory {
	const guid = "inst;db;public;users"
	rows := make([]*store.MetaRegistryHistory, 0, count)
	for i := range count {
		from := base.Add(time.Duration(i) * time.Hour)
		row := &store.MetaRegistryHistory{GUID: guid, ObjectType: storepb.MetaType_TABLE, ValidFrom: from}
		if i == count-1 {
			// The open row: its valid_to stays nil.
			rows = append(rows, row)
			continue
		}
		if i%4 == 3 {
			// A gap: the row closes with no successor, which is a deletion.
			to := from.Add(30 * time.Minute)
			row.ValidTo = &to
			rows = append(rows, row)
			continue
		}
		to := base.Add(time.Duration(i+1) * time.Hour)
		row.ValidTo = &to
		rows = append(rows, row)
	}
	return rows
}
