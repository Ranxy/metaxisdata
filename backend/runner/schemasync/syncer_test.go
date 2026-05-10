package schemasync

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/store"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func buildTableMeta(name string, columns ...string) *storepb.StoredMetadata {
	metaCols := make([]*storepb.ColumnMetadata, 0, len(columns))
	for _, col := range columns {
		metaCols = append(metaCols, &storepb.ColumnMetadata{Name: col})
	}
	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{
			TableMetadata: &storepb.TableMetadata{
				Name:    name,
				Columns: metaCols,
			},
		},
	}
}

func TestConvertMetadataToGUID(t *testing.T) {
	t.Parallel()

	databaseMeta := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_DatabaseSchemaMetadata{DatabaseSchemaMetadata: &storepb.DatabaseSchemaMetadata{Name: "db1"}},
	}
	tableMeta := buildTableMeta("users", "id", "name")

	guid, children, err := convertMetadataToGUID("inst1", storepb.MetaType_DATABASE, databaseMeta)
	require.NoError(t, err)
	require.Equal(t, buildGUID("inst1", "db1"), guid)
	require.Empty(t, children)

	prefix := buildGUID("inst1", "db1", "public")
	guid, children, err = convertMetadataToGUID(prefix, storepb.MetaType_TABLE, tableMeta)
	require.NoError(t, err)
	require.Equal(t, buildGUID(prefix, "users"), guid)
	require.Equal(t, []string{buildGUID(prefix, "users", "id"), buildGUID(prefix, "users", "name")}, children)

	_, _, err = convertMetadataToGUID("inst1", storepb.MetaType_COLUMN, &storepb.StoredMetadata{})
	require.Error(t, err)
}

func TestBatchMetaCreateStoreMetaResourceV2Table(t *testing.T) {
	t.Parallel()

	b := &batchMetaCreate{}
	prefix := buildGUID("inst1", "db1", "public")
	err := b.StoreMetaResourceV2(context.Background(), prefix, storepb.MetaType_TABLE, buildTableMeta("users", "id", "name"))
	require.NoError(t, err)
	require.Len(t, b.guidList, 3)

	got := map[string]storepb.MetaType{}
	for _, item := range b.guidList {
		got[item.GUID] = item.ObjectType
	}

	require.Equal(t, storepb.MetaType_TABLE, got[buildGUID(prefix, "users")])
	require.Equal(t, storepb.MetaType_COLUMN, got[buildGUID(prefix, "users", "id")])
	require.Equal(t, storepb.MetaType_COLUMN, got[buildGUID(prefix, "users", "name")])
}

func TestBatchMetaCreateDiff(t *testing.T) {
	t.Parallel()

	unchangedMeta := buildTableMeta("orders", "id")
	changedBeforeMeta := buildTableMeta("users", "id")
	changedAfterMeta := buildTableMeta("users", "id", "name")
	newMeta := buildTableMeta("products", "id")

	_, unchangedHash, err := store.CalcStoreMetaHash(unchangedMeta)
	require.NoError(t, err)
	_, changedBeforeHash, err := store.CalcStoreMetaHash(changedBeforeMeta)
	require.NoError(t, err)

	batch := &batchMetaCreate{
		exist: []*store.MetaRegistryResource{
			{GUID: buildGUID("inst", "db", "public", "orders"), ObjectType: storepb.MetaType_TABLE, MetaHash: unchangedHash},
			{GUID: buildGUID("inst", "db", "public", "users"), ObjectType: storepb.MetaType_TABLE, MetaHash: changedBeforeHash},
			{GUID: buildGUID("inst", "db", "public", "legacy"), ObjectType: storepb.MetaType_TABLE, MetaHash: []byte("legacy-hash")},
			{GUID: buildGUID("inst", "db", "public", "__manual_sql__/summary"), ObjectType: storepb.MetaType_MANUAL_SQL, MetaHash: []byte("manual-hash")},
		},
		guidList: []*store.CreateMetaRegistryResourceMessage{
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "public", "orders"), ObjectType: storepb.MetaType_TABLE, Metadata: unchangedMeta}},
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "public", "users"), ObjectType: storepb.MetaType_TABLE, Metadata: changedAfterMeta}},
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "public", "products"), ObjectType: storepb.MetaType_TABLE, Metadata: newMeta}},
		},
	}

	updates, deletes, err := batch.diff()
	require.NoError(t, err)

	updateKeys := make(map[store.MetaGUIDKey]struct{})
	for _, item := range updates {
		updateKeys[item.GUIDKey()] = struct{}{}
		require.NotEmpty(t, item.MetaHash)
		require.NotEmpty(t, item.MetadataBytes)
	}

	_, changedFound := updateKeys[store.MetaGUIDKey{GUID: buildGUID("inst", "db", "public", "users"), ObjectType: storepb.MetaType_TABLE}]
	_, newFound := updateKeys[store.MetaGUIDKey{GUID: buildGUID("inst", "db", "public", "products"), ObjectType: storepb.MetaType_TABLE}]
	_, unchangedFound := updateKeys[store.MetaGUIDKey{GUID: buildGUID("inst", "db", "public", "orders"), ObjectType: storepb.MetaType_TABLE}]

	require.True(t, changedFound)
	require.True(t, newFound)
	require.False(t, unchangedFound)
	require.Len(t, deletes, 1)
	require.Equal(t, buildGUID("inst", "db", "public", "legacy"), deletes[0].GUID)
}

func TestGetOrDefaultSyncInterval(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		instance *store.InstanceMessage
		want     time.Duration
	}{
		{
			name:     "nil metadata",
			instance: &store.InstanceMessage{},
			want:     defaultSyncInterval,
		},
		{
			name: "not activated",
			instance: &store.InstanceMessage{Metadata: &storepb.Instance{
				Activation:   false,
				SyncInterval: durationpb.New(10 * time.Minute),
			}},
			want: defaultSyncInterval,
		},
		{
			name: "activated but zero interval",
			instance: &store.InstanceMessage{Metadata: &storepb.Instance{
				Activation:   true,
				SyncInterval: durationpb.New(0),
			}},
			want: defaultSyncInterval,
		},
		{
			name: "activated with positive interval",
			instance: &store.InstanceMessage{Metadata: &storepb.Instance{
				Activation:   true,
				SyncInterval: durationpb.New(30 * time.Minute),
			}},
			want: 30 * time.Minute,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, getOrDefaultSyncInterval(tc.instance))
		})
	}
}

func TestGetOrDefaultLastSyncTime(t *testing.T) {
	t.Parallel()

	valid := timestamppb.New(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))
	require.True(t, getOrDefaultLastSyncTime(valid).Equal(valid.AsTime()))

	invalid := &timestamppb.Timestamp{Seconds: 1, Nanos: 1_000_000_000}
	require.True(t, getOrDefaultLastSyncTime(invalid).Equal(time.Unix(0, 0)))

	require.True(t, getOrDefaultLastSyncTime(nil).Equal(time.Unix(0, 0)))
}

func TestSyncDatabaseAsync(t *testing.T) {
	t.Parallel()

	s := &Syncer{}

	s.SyncDatabaseAsync(nil)
	require.Equal(t, 0, countDatabaseSyncMapItems(&s.databaseSyncMap))

	s.SyncDatabaseAsync(&store.DatabaseMessage{Deleted: true})
	require.Equal(t, 0, countDatabaseSyncMapItems(&s.databaseSyncMap))

	s.SyncDatabaseAsync(&store.DatabaseMessage{InstanceID: "i1", DatabaseName: "d1"})
	require.Equal(t, 1, countDatabaseSyncMapItems(&s.databaseSyncMap))

	s.SyncDatabasesAsync([]*store.DatabaseMessage{
		{InstanceID: "i1", DatabaseName: "d2"},
		nil,
		{InstanceID: "i1", DatabaseName: "d3", Deleted: true},
	})
	require.Equal(t, 2, countDatabaseSyncMapItems(&s.databaseSyncMap))
}

func countDatabaseSyncMapItems(m *sync.Map) int {
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}
