package schemasync

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/component/state"
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

	guid, err := convertMetadataToGUID("inst1", storepb.MetaType_DATABASE, databaseMeta)
	require.NoError(t, err)
	require.Equal(t, buildGUID("inst1", "db1"), guid)

	prefix := buildGUID("inst1", "db1", "public")
	guid, err = convertMetadataToGUID(prefix, storepb.MetaType_TABLE, tableMeta)
	require.NoError(t, err)
	require.Equal(t, buildGUID(prefix, "users"), guid)

	children := getChildMetadataResources(guid, storepb.MetaType_TABLE, tableMeta)
	require.Len(t, children, 2)
	require.Equal(t, []string{buildGUID(prefix, "users", "id"), buildGUID(prefix, "users", "name")}, []string{children[0].GUID, children[1].GUID})
	require.Equal(t, "id", children[0].Metadata.GetColumnMetadata().Name)
	require.Equal(t, "name", children[1].Metadata.GetColumnMetadata().Name)

	_, err = convertMetadataToGUID("inst1", storepb.MetaType_COLUMN, &storepb.StoredMetadata{})
	require.Error(t, err)
}

func TestBatchMetaCreateStoreMetaResourceTable(t *testing.T) {
	t.Parallel()

	b := &batchMetaCreate{}
	prefix := buildGUID("inst1", "db1", "public")
	err := b.StoreMetaResource(context.Background(), prefix, storepb.MetaType_TABLE, buildTableMeta("users", "id", "name"))
	require.NoError(t, err)
	require.Len(t, b.guidList, 3)

	got := map[string]storepb.MetaType{}
	gotMeta := map[string]*storepb.StoredMetadata{}
	for _, item := range b.guidList {
		got[item.GUID] = item.ObjectType
		gotMeta[item.GUID] = item.Metadata
	}

	require.Equal(t, storepb.MetaType_TABLE, got[buildGUID(prefix, "users")])
	require.Equal(t, storepb.MetaType_COLUMN, got[buildGUID(prefix, "users", "id")])
	require.Equal(t, storepb.MetaType_COLUMN, got[buildGUID(prefix, "users", "name")])
	require.Equal(t, "id", gotMeta[buildGUID(prefix, "users", "id")].GetColumnMetadata().Name)
	require.Equal(t, "name", gotMeta[buildGUID(prefix, "users", "name")].GetColumnMetadata().Name)
}

func buildTableMetaWithStats(name string, rowCount, dataSize, indexSize, dataFree int64, columns ...string) *storepb.StoredMetadata {
	metaCols := make([]*storepb.ColumnMetadata, 0, len(columns))
	for _, col := range columns {
		metaCols = append(metaCols, &storepb.ColumnMetadata{Name: col})
	}
	return &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{
			TableMetadata: &storepb.TableMetadata{
				Name:      name,
				Columns:   metaCols,
				RowCount:  rowCount,
				DataSize:  dataSize,
				IndexSize: indexSize,
				DataFree:  dataFree,
			},
		},
	}
}

func TestBatchMetaCreateDiff(t *testing.T) {
	t.Parallel()

	unchangedMeta := buildTableMeta("orders", "id")
	changedBeforeMeta := buildTableMeta("users", "id")
	changedAfterMeta := buildTableMeta("users", "id", "name")
	newMeta := buildTableMeta("products", "id")
	statsChangedMeta := buildTableMetaWithStats("stats_only", 100, 200, 300, 400, "col1")

	_, unchangedHash, err := store.CalcStoreMetaHash(unchangedMeta)
	require.NoError(t, err)
	_, changedBeforeHash, err := store.CalcStoreMetaHash(changedBeforeMeta)
	require.NoError(t, err)
	// Stats-changed table: existing entry has the same schema but different stats.
	statsExistingHash, err := store.CalcMetaHash(buildTableMetaWithStats("stats_only", 999, 888, 777, 666, "col1"))
	require.NoError(t, err)

	batch := &batchMetaCreate{
		exist: []*store.MetaRegistryResource{
			{GUID: buildGUID("inst", "db", "public", "orders"), ObjectType: storepb.MetaType_TABLE, MetaHash: unchangedHash},
			{GUID: buildGUID("inst", "db", "public", "users"), ObjectType: storepb.MetaType_TABLE, MetaHash: changedBeforeHash},
			{GUID: buildGUID("inst", "db", "public", "legacy"), ObjectType: storepb.MetaType_TABLE, MetaHash: []byte("legacy-hash")},
			{GUID: buildGUID("inst", "db", "public", "__manual_sql__/summary"), ObjectType: storepb.MetaType_MANUAL_SQL, MetaHash: []byte("manual-hash")},
			{GUID: buildGUID("inst", "db", "public", "stats_only"), ObjectType: storepb.MetaType_TABLE, MetaHash: statsExistingHash},
		},
		guidList: []*store.CreateMetaRegistryResourceMessage{
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "public", "orders"), ObjectType: storepb.MetaType_TABLE, Metadata: unchangedMeta}},
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "public", "users"), ObjectType: storepb.MetaType_TABLE, Metadata: changedAfterMeta}},
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "public", "products"), ObjectType: storepb.MetaType_TABLE, Metadata: newMeta}},
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "public", "stats_only"), ObjectType: storepb.MetaType_TABLE, Metadata: statsChangedMeta}},
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
	_, statsOnlyFound := updateKeys[store.MetaGUIDKey{GUID: buildGUID("inst", "db", "public", "stats_only"), ObjectType: storepb.MetaType_TABLE}]

	require.True(t, changedFound, "table with added column should be an update")
	require.True(t, newFound, "new table should be an update")
	require.False(t, unchangedFound, "identical table should not be an update")
	require.False(t, statsOnlyFound, "table with only stats change should not be an update")
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

// A deactivated instance reports a zero sync interval, which means "never sync".
// The scheduling predicate must reject it even though lastSyncTime.Add(0) is
// never after now, otherwise every tick re-queues all of its databases.
func TestShouldSyncNowRejectsNeverSyncInterval(t *testing.T) {
	t.Parallel()

	now := time.Now()
	require.False(t, shouldSyncNow(defaultSyncInterval, time.Unix(0, 0), now))
	require.False(t, shouldSyncNow(defaultSyncInterval, now.Add(-time.Hour), now))
}

func TestShouldSyncNowRespectsInterval(t *testing.T) {
	t.Parallel()

	now := time.Now()
	const interval = 15 * time.Minute

	require.True(t, shouldSyncNow(interval, time.Unix(0, 0), now), "never synced resource is due")
	require.False(t, shouldSyncNow(interval, now.Add(-time.Minute), now), "recently synced resource is not due")
	require.True(t, shouldSyncNow(interval, now.Add(-interval), now), "resource synced exactly one interval ago is due")
	require.True(t, shouldSyncNow(interval, now.Add(-2*interval), now), "stale resource is due")
}

func TestGetOrDefaultLastSyncTime(t *testing.T) {
	t.Parallel()

	valid := timestamppb.New(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC))
	require.True(t, getOrDefaultLastSyncTime(valid).Equal(valid.AsTime()))

	invalid := &timestamppb.Timestamp{Seconds: 1, Nanos: 1_000_000_000}
	require.True(t, getOrDefaultLastSyncTime(invalid).Equal(time.Unix(0, 0)))

	require.True(t, getOrDefaultLastSyncTime(nil).Equal(time.Unix(0, 0)))
}

// Every driver-opening path goes through the per-instance limiter, so an
// instance at its connection limit is refused and the caller retries later
// instead of opening another pool.
func TestAcquireInstanceConnectionEnforcesTheLimit(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	syncer := &Syncer{stateCfg: stateCfg}
	instance := &store.InstanceMessage{
		ResourceID: "inst-1",
		Metadata:   &storepb.Instance{MaximumConnections: 2},
	}

	releaseFirst, err := syncer.acquireInstanceConnection(instance)
	require.NoError(t, err)
	releaseSecond, err := syncer.acquireInstanceConnection(instance)
	require.NoError(t, err)

	_, err = syncer.acquireInstanceConnection(instance)
	require.ErrorIs(t, err, errInstanceConnectionsExhausted)

	releaseFirst()
	releaseThird, err := syncer.acquireInstanceConnection(instance)
	require.NoError(t, err)

	releaseSecond()
	releaseThird()
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

// SyncInstance used to return the whole snapshot, so a caller was told it had
// synced databases that the sync_databases allowlist had skipped.
func TestFilterSyncedDatabases(t *testing.T) {
	t.Parallel()

	databases := []*storepb.DatabaseSchemaMetadata{{Name: "app"}, {Name: "staging"}, {Name: "archive"}}

	require.Equal(t, databases, filterSyncedDatabases(databases, nil), "an empty allowlist syncs everything")

	filtered := filterSyncedDatabases(databases, []string{"app", "archive"})
	require.Len(t, filtered, 2)
	require.Equal(t, "app", filtered[0].Name)
	require.Equal(t, "archive", filtered[1].Name)

	require.Empty(t, filterSyncedDatabases(databases, []string{"not-present"}))
}
