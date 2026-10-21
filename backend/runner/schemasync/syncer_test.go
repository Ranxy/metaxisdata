package schemasync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/metrics"
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
		storedStatistics: map[store.MetaGUIDKey]store.MetaRegistryStatistics{
			// The stored row still carries the old statistics; the hash ignores them,
			// which is exactly why the diff has to compare them separately.
			{GUID: buildGUID("inst", "db", "public", "stats_only"), ObjectType: storepb.MetaType_TABLE}: {
				RowCount: 999, DataSize: 888, IndexSize: 777, DataFree: 666,
			},
		},
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
	// The stats-only change is written to the metadata column alone: no history
	// row and no lineage re-analysis, but the row the API reads is refreshed.
	require.Len(t, batch.statsUpdates, 1)
	require.Equal(t, buildGUID("inst", "db", "public", "stats_only"), batch.statsUpdates[0].GUID)
	require.Len(t, deletes, 1)
	require.Equal(t, buildGUID("inst", "db", "public", "legacy"), deletes[0].GUID)
}

// The write transaction must not compute anything: prepare() computes the diff
// and populates the update/delete sets, and write() only persists them. Moving
// the diff back inside the transaction would put it back across the snapshot's
// network round trips.
func TestBatchMetaCreatePreparePopulatesDiff(t *testing.T) {
	t.Parallel()

	unchangedMeta := buildTableMeta("orders", "id")
	_, unchangedHash, err := store.CalcStoreMetaHash(unchangedMeta)
	require.NoError(t, err)

	batch := &batchMetaCreate{
		exist: []*store.MetaRegistryResource{
			{GUID: buildGUID("inst", "db", "public", "orders"), ObjectType: storepb.MetaType_TABLE, MetaHash: unchangedHash},
			{GUID: buildGUID("inst", "db", "public", "legacy"), ObjectType: storepb.MetaType_TABLE, MetaHash: []byte("legacy-hash")},
		},
		guidList: []*store.CreateMetaRegistryResourceMessage{
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "public", "orders"), ObjectType: storepb.MetaType_TABLE, Metadata: unchangedMeta}},
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "public", "products"), ObjectType: storepb.MetaType_TABLE, Metadata: buildTableMeta("products", "id")}},
		},
	}

	require.NoError(t, batch.prepare())
	require.Len(t, batch.updates, 1)
	require.Equal(t, buildGUID("inst", "db", "public", "products"), batch.updates[0].GUID)
	require.Len(t, batch.deletes, 1)
	require.Equal(t, buildGUID("inst", "db", "public", "legacy"), batch.deletes[0].GUID)
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

	stateCfg, err := state.New(nil)
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

// A soft-deleted row is history, and only the instance enumeration revives it, so
// the queue never grows one for the checker to open.
func TestEnqueueDatabaseSkipsDeletedDatabases(t *testing.T) {
	t.Parallel()

	s := &Syncer{}

	s.enqueueDatabase(nil)
	s.enqueueDatabase(&store.DatabaseMessage{Deleted: true})
	require.Equal(t, 0, countDatabaseSyncMapItems(&s.databaseSyncMap))

	s.enqueueDatabase(&store.DatabaseMessage{InstanceID: "i1", DatabaseName: "d1"})
	require.Equal(t, 1, countDatabaseSyncMapItems(&s.databaseSyncMap))
}

// An operation waits for the databases it queued: they are registered before they
// are queued, and the ones that can never be synced are not registered at all.
func TestEnqueueDatabasesRegistersWhatTheOperationWaitsFor(t *testing.T) {
	t.Parallel()

	s := &Syncer{}
	instance := &store.InstanceMessage{ResourceID: "inst-1"}
	databases := []*store.DatabaseMessage{
		{InstanceID: "inst-1", DatabaseName: "d1"},
		nil,
		{InstanceID: "inst-1", DatabaseName: "gone", Deleted: true},
		{InstanceID: "inst-1", DatabaseName: "d2"},
	}

	operation := s.StartOperation(storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, 7, instance)
	s.RecordInstanceResult(context.Background(), operation, nil)
	s.EnqueueDatabases(context.Background(), operation, databases)

	require.Equal(t, 2, countDatabaseSyncMapItems(&s.databaseSyncMap))
	operation.mu.Lock()
	require.Len(t, operation.pending, 2)
	require.True(t, operation.sealed)
	operation.mu.Unlock()

	// Both databases report, and the operation finishes into exactly one message.
	s.completeDatabase(context.Background(), databases[0], nil)
	s.completeDatabase(context.Background(), databases[3], nil)

	require.Empty(t, s.operations)
	require.Equal(t, 2, operation.succeeded)
}

func countDatabaseSyncMapItems(m *sync.Map) int {
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// One sync per database at a time: a second caller joins the running one instead
// of starting a sync that would diff the same digest and interleave its writes.
func TestDatabaseSyncGateSerializesOneDatabase(t *testing.T) {
	t.Parallel()

	gate := &databaseSyncGate{}
	owner, ok := gate.acquire("inst-1;db1")
	require.True(t, ok, "the first caller owns the database's sync")

	joiner, ok := gate.acquire("inst-1;db1")
	require.False(t, ok, "the second caller must not own the same database's sync")
	require.Same(t, owner, joiner)

	other, ok := gate.acquire("inst-1;db2")
	require.True(t, ok, "another database is unaffected")

	gate.finish("inst-1;db1", owner, errors.New("sync failed"))
	select {
	case <-joiner.done:
	default:
		t.Fatal("finish must release the waiters")
	}
	require.EqualError(t, joiner.err, "sync failed")

	fresh, ok := gate.acquire("inst-1;db1")
	require.True(t, ok, "a finished sync must not be reused")
	require.NotSame(t, owner, fresh)

	gate.finish("inst-1;db1", fresh, nil)
	gate.finish("inst-1;db2", other, nil)
}

// A nil database is nothing to sync; the store being nil proves the check runs
// before anything dereferences the message.
func TestSyncDatabaseSchemaIgnoresNilDatabase(t *testing.T) {
	t.Parallel()

	syncer := &Syncer{}
	require.NoError(t, syncer.SyncDatabaseSchema(context.Background(), nil))
}

// A joiner reuses the running sync's outcome, including its error, without
// touching the store itself.
func TestSyncDatabaseSchemaJoinerReusesTheOwnerResult(t *testing.T) {
	t.Parallel()

	syncer := &Syncer{}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "db1"}
	call, owner := syncer.databaseSyncGate.acquire(database.String())
	require.True(t, owner)

	go func() {
		time.Sleep(10 * time.Millisecond)
		syncer.databaseSyncGate.finish(database.String(), call, errors.New("owner failed"))
	}()

	require.EqualError(t, syncer.SyncDatabaseSchema(context.Background(), database), "owner failed")
}

// A caller that is waiting on a running sync returns its own cancellation
// instead of staying pinned to the owner.
func TestSyncDatabaseSchemaJoinerReturnsItsOwnCancellation(t *testing.T) {
	t.Parallel()

	syncer := &Syncer{}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "db1"}
	call, owner := syncer.databaseSyncGate.acquire(database.String())
	require.True(t, owner)
	defer syncer.databaseSyncGate.finish(database.String(), call, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, syncer.SyncDatabaseSchema(ctx, database), context.Canceled)
}

// The backoff ladder of a failed database sync: fast early retries that grow to
// the instance scan's cadence.
func TestDatabaseSyncRetryBackoff(t *testing.T) {
	t.Parallel()

	require.Equal(t, time.Minute, databaseSyncRetryBackoff(1))
	require.Equal(t, 5*time.Minute, databaseSyncRetryBackoff(2))
	require.Equal(t, instanceSyncInterval, databaseSyncRetryBackoff(maxDatabaseSyncRetries))
}

// A failed database stays queued with a growing delay instead of waiting for the
// next instance scan, a fresh enqueue supersedes that delay, and the last
// attempt hands the database back to the scan.
func TestScheduleRetryBacksOffAndGivesUp(t *testing.T) {
	t.Parallel()

	syncer := &Syncer{}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "db1"}
	key := database.String()

	// The checker drains the entry before syncing it.
	syncer.databaseSyncMap.Delete(key)
	syncer.scheduleRetry(database)
	require.Equal(t, 1, mustDatabaseRetry(t, syncer, key).attempts)
	require.Equal(t, 1, countDatabaseSyncMapItems(&syncer.databaseSyncMap), "a failed database must stay queued")

	syncer.scheduleRetry(database)
	require.Equal(t, 2, mustDatabaseRetry(t, syncer, key).attempts)

	// A fresh enqueue supersedes the pending backoff.
	syncer.enqueueDatabase(database)
	_, ok := syncer.databaseSyncRetryMap.Load(key)
	require.False(t, ok)

	// The retry cap leaves the database to the next instance scan instead of
	// backing off forever: the checker drains the entry before each attempt and
	// the last failure does not re-queue it.
	for range maxDatabaseSyncRetries + 1 {
		syncer.databaseSyncMap.Delete(key)
		syncer.scheduleRetry(database)
	}
	_, ok = syncer.databaseSyncRetryMap.Load(key)
	require.False(t, ok)
	require.Equal(t, 0, countDatabaseSyncMapItems(&syncer.databaseSyncMap))
}

// The checker must skip a queued database until its backoff elapses.
func TestRetryDueHonorsBackoff(t *testing.T) {
	t.Parallel()

	syncer := &Syncer{}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "db1"}
	key := database.String()
	require.True(t, syncer.retryDue(key, time.Now()), "a database that never failed is due")

	syncer.scheduleRetry(database)
	require.False(t, syncer.retryDue(key, time.Now()), "a failed database must not be retried before its backoff")
	require.True(t, syncer.retryDue(key, time.Now().Add(databaseSyncRetryBackoff(1)+time.Second)))

	syncer.enqueueDatabase(database)
	require.True(t, syncer.retryDue(key, time.Now()), "a fresh enqueue clears the backoff")
}

func mustDatabaseRetry(t *testing.T, syncer *Syncer, key string) databaseRetry {
	t.Helper()
	v, ok := syncer.databaseSyncRetryMap.Load(key)
	require.True(t, ok)
	entry, ok := v.(databaseRetry)
	require.True(t, ok)
	return entry
}

// The API and the periodic scan can enumerate the same instance at once; only one
// may write the instance row and the database rows, and the other reuses the
// result instead of racing it.
func TestInstanceSyncGateSerializesOneInstance(t *testing.T) {
	t.Parallel()

	gate := &instanceSyncGate{}
	owner, ok := gate.acquire("inst-1")
	require.True(t, ok, "the first caller owns the instance's sync")

	joiner, ok := gate.acquire("inst-1")
	require.False(t, ok, "the second caller must not own the same instance's sync")
	require.Same(t, owner, joiner)

	other, ok := gate.acquire("inst-2")
	require.True(t, ok, "another instance is unaffected")

	instance := &store.InstanceMessage{ResourceID: "inst-1"}
	gate.finish("inst-1", owner, instance, nil, nil, errors.New("sync failed"))
	select {
	case <-joiner.done:
	default:
		t.Fatal("finish must release the waiters")
	}
	require.Same(t, instance, joiner.updatedInstance)
	require.EqualError(t, joiner.err, "sync failed")

	fresh, ok := gate.acquire("inst-1")
	require.True(t, ok, "a finished sync must not be reused")
	require.NotSame(t, owner, fresh)

	gate.finish("inst-1", fresh, nil, nil, nil, nil)
	gate.finish("inst-2", other, nil, nil, nil, nil)
}

// The statistics refresh runs on its own slower clock so a row count does not
// rewrite every table's metadata on each sync. A database this process has not
// refreshed yet is due, which is what makes a restart refresh the estimates.
func TestStatisticsRefreshCadence(t *testing.T) {
	t.Parallel()

	s := &Syncer{}
	require.True(t, s.statisticsRefreshDue("inst;db"))
	require.True(t, s.statisticsRefreshDue("inst;other"))

	s.recordStatisticsRefresh("inst;db")
	require.False(t, s.statisticsRefreshDue("inst;db"))
	require.True(t, s.statisticsRefreshDue("inst;other"))
}

// The DDL refresh clock and rotation cursor live in the process: a restart makes
// every database due for a full refresh, a committed full refresh advances the
// cursor, and a change-only plan leaves both alone.
func TestDefinitionRefreshCadence(t *testing.T) {
	t.Parallel()

	s := &Syncer{}
	full, cursor := s.definitionRefreshDue("inst;db")
	require.True(t, full)
	require.Zero(t, cursor)

	s.recordDefinitionRefresh("inst;db", definitionFetchPlan{full: true, nextCursor: 7})
	full, cursor = s.definitionRefreshDue("inst;db")
	require.False(t, full)
	require.Equal(t, 7, cursor)

	s.recordDefinitionRefresh("inst;db", definitionFetchPlan{full: false, nextCursor: 99})
	_, cursor = s.definitionRefreshDue("inst;db")
	require.Equal(t, 7, cursor)
}

// A row that is already hidden is history: syncing it would write LastSyncTime to
// a database the target no longer has, and only the instance enumeration may
// revive it.
func TestSyncDatabaseSchemaReturnsHiddenForADeletedRow(t *testing.T) {
	t.Parallel()

	syncer := &Syncer{}
	err := syncer.SyncDatabaseSchema(context.Background(), &store.DatabaseMessage{
		InstanceID:   "inst-1",
		DatabaseName: "gone",
		Deleted:      true,
	})
	require.ErrorIs(t, err, ErrDatabaseHidden)
}

// A database hidden by an instance enumeration is dequeued and an operation
// waiting for its outcome hears it instead of waiting for a result that will never
// come — without escalating to the administrators. This is the sequence
// SyncInstance runs on its soft-delete path.
func TestHiddenDatabaseIsDequeuedAndReported(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	instance := &store.InstanceMessage{ResourceID: "inst-1", Metadata: &storepb.Instance{Title: "prod"}}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "app"}

	operation := syncer.StartOperation(storepb.SyncTrigger_SYNC_TRIGGER_MANUAL, 7, instance)
	syncer.RecordInstanceResult(context.Background(), operation, nil)
	syncer.EnqueueDatabases(context.Background(), operation, []*store.DatabaseMessage{database})
	require.Equal(t, 1, countDatabaseSyncMapItems(&syncer.databaseSyncMap))

	syncer.forgetQueuedDatabase(database.String())
	syncer.completeDatabase(context.Background(), database, fmt.Errorf("%w: database %q", ErrDatabaseHidden, database.DatabaseName))

	require.Equal(t, 0, countDatabaseSyncMapItems(&syncer.databaseSyncMap), "a hidden database must not stay queued")
	require.Empty(t, notifier.admin, "a hidden database is not an administrator's problem")
	require.Len(t, notifier.sent, 1)
	detail := notifier.sent[0].GetSchemaSync()
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, notifier.sent[0].GetSeverity())
	require.Equal(t, int32(1), detail.GetFailedCount())
	require.Len(t, detail.GetDatabases(), 1)
	require.Equal(t, storepb.SyncDatabaseState_SYNC_DATABASE_STATE_FAILED, detail.GetDatabases()[0].GetState())
	require.Contains(t, detail.GetDatabases()[0].GetError(), "hidden")
}

// Forgetting a queued database clears both the queue entry and its pending
// backoff, so a hidden row cannot be retried or re-queued by the checker.
func TestForgetQueuedDatabaseClearsTheQueueAndBackoff(t *testing.T) {
	t.Parallel()

	syncer := &Syncer{}
	database := &store.DatabaseMessage{InstanceID: "inst-1", DatabaseName: "app"}
	key := database.String()

	syncer.enqueueDatabase(database)
	syncer.scheduleRetry(database)
	require.Equal(t, 1, countDatabaseSyncMapItems(&syncer.databaseSyncMap))
	_, retryPending := syncer.databaseSyncRetryMap.Load(key)
	require.True(t, retryPending)

	syncer.forgetQueuedDatabase(key)

	require.Equal(t, 0, countDatabaseSyncMapItems(&syncer.databaseSyncMap))
	_, retryPending = syncer.databaseSyncRetryMap.Load(key)
	require.False(t, retryPending)
}

// Instance-level connection-limit saturation is recoverable contention: the next
// scan retries it, so it must not be reported as a broken instance to the
// administrators. Any other failure still is.
func TestConnectionLimitSaturationDoesNotNotifyTheAdministrators(t *testing.T) {
	t.Parallel()

	notifier := &fakeNotifier{}
	syncer := &Syncer{notifier: notifier}
	instance := &store.InstanceMessage{ResourceID: "inst-1", Metadata: &storepb.Instance{Title: "prod"}}

	syncer.reportInstanceSyncFailure(context.Background(), instance, state.ErrInstanceConnectionLimit)
	require.Empty(t, notifier.admin)
	require.Empty(t, notifier.sent)

	syncer.reportInstanceSyncFailure(context.Background(), instance, errors.New("connection refused"))
	require.Len(t, notifier.admin, 1)
	require.Equal(t, "connection refused", notifier.admin[0].GetSchemaSync().GetInstanceError())
}

// A database whose instance failed to sync is not queued: the instance failure
// has already been reported, and each of its databases would retry the same
// broken connection and raise a failure notice of its own. A due database of a
// healthy instance is still queued.
func TestDatabasesToEnqueueSkipsFailedInstances(t *testing.T) {
	t.Parallel()

	now := time.Now()
	due := func(instanceID, databaseName string) *store.DatabaseMessage {
		return &store.DatabaseMessage{
			InstanceID:   instanceID,
			DatabaseName: databaseName,
			Metadata:     &storepb.DatabaseMetadata{LastSyncTime: timestamppb.New(now.Add(-time.Hour))},
		}
	}
	instances := map[string]*store.InstanceMessage{
		"healthy": {ResourceID: "healthy", Metadata: &storepb.Instance{
			Activation:   true,
			SyncInterval: durationpb.New(15 * time.Minute),
		}},
		"broken": {ResourceID: "broken", Metadata: &storepb.Instance{
			Activation:   true,
			SyncInterval: durationpb.New(15 * time.Minute),
		}},
	}
	databases := []*store.DatabaseMessage{
		due("healthy", "app"),
		due("broken", "app"),
		due("unknown", "app"),
		due("healthy", "recent"),
	}
	databases[3].Metadata.LastSyncTime = timestamppb.New(now.Add(-time.Minute))

	got := databasesToEnqueue(databases, instances, map[string]struct{}{"broken": {}}, now)
	require.Len(t, got, 1, "only the due database of the healthy instance is queued")
	require.Equal(t, "healthy", got[0].InstanceID)
	require.Equal(t, "app", got[0].DatabaseName)
}

func TestQueueLineageAnalysisFeedsTheQueue(t *testing.T) {
	t.Parallel()

	queue := &fakeLineageQueue{}
	syncer := &Syncer{lineageQueue: queue}
	syncer.QueueLineageAnalysis("guid-1", storepb.MetaType_VIEW)
	require.Equal(t, []string{"guid-1"}, queue.guids)

	// An empty GUID and a syncer without a queue are no-ops, not panics.
	syncer.QueueLineageAnalysis("", storepb.MetaType_VIEW)
	(&Syncer{}).QueueLineageAnalysis("guid-2", storepb.MetaType_VIEW)
	require.Equal(t, []string{"guid-1"}, queue.guids)
}

type fakeLineageQueue struct {
	guids []string
}

func (q *fakeLineageQueue) QueueAnalysis(metaGUID string, _ storepb.MetaType) {
	q.guids = append(q.guids, metaGUID)
}

// A snapshot can name the same (GUID, object type) twice; the batch upsert would
// fail the whole database's sync with "cannot affect row a second time", so the
// diff keeps the first entry and logs the collision.
func TestBatchMetaCreateDiffDeduplicatesCollidingGUIDs(t *testing.T) {
	t.Parallel()

	first := buildTableMeta("users", "id")
	second := buildTableMeta("users", "id", "name")
	guid := buildGUID("inst", "db", "public", "users")
	batch := &batchMetaCreate{
		guidList: []*store.CreateMetaRegistryResourceMessage{
			{MetaRegistryResource: store.MetaRegistryResource{GUID: guid, ObjectType: storepb.MetaType_TABLE, Metadata: first}},
			{MetaRegistryResource: store.MetaRegistryResource{GUID: guid, ObjectType: storepb.MetaType_TABLE, Metadata: second}},
		},
	}

	updates, deletes, err := batch.diff()
	require.NoError(t, err)
	require.Len(t, updates, 1)
	require.Empty(t, deletes)
	require.Equal(t, guid, updates[0].GUID)
	require.Len(t, updates[0].Metadata.GetTableMetadata().Columns, 1,
		"the first entry is kept deterministically")
}

// A catalog value containing a NUL byte (Doris reports the default of an empty
// BITMAP/HLL column as a raw 0x00) must not reach the jsonb write. The batch diff
// builds the bytes that BatchCreateMetaRegistryResourceAt inserts, so this is the
// last point before the store call that used to fail with SQLSTATE 22P05.
func TestBatchMetaCreateDiffSanitizesNULMetadata(t *testing.T) {
	t.Parallel()

	meta := &storepb.StoredMetadata{
		Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: &storepb.TableMetadata{
			Name: "dbt",
			Columns: []*storepb.ColumnMetadata{
				{Name: "s", Position: 1, Type: "varchar(50)"},
				{Name: "bm", Position: 2, Type: "bitmap", Default: "\x00"},
			},
		}},
	}
	batch := &batchMetaCreate{
		guidList: []*store.CreateMetaRegistryResourceMessage{
			{MetaRegistryResource: store.MetaRegistryResource{GUID: buildGUID("inst", "db", "rev", "dbt"), ObjectType: storepb.MetaType_TABLE, Metadata: meta}},
		},
	}

	updates, deletes, err := batch.diff()
	require.NoError(t, err)
	require.Empty(t, deletes)
	require.Len(t, updates, 1)
	require.NotContains(t, string(updates[0].MetadataBytes), "\x00")
	require.NotContains(t, string(updates[0].MetadataBytes), `\u0000`)

	persisted := &storepb.StoredMetadata{}
	require.NoError(t, common.ProtojsonUnmarshaler.Unmarshal(updates[0].MetadataBytes, persisted))
	require.Equal(t, "\uFFFD", persisted.GetTableMetadata().Columns[1].Default)

	// The caller's snapshot is unchanged: only the persisted bytes are sanitized.
	require.Equal(t, "\x00", meta.GetTableMetadata().Columns[1].Default)
}

// The failure reason is a label on the counter, so it may only name what the sync
// paths can prove. A driver error has no transient/permanent classification and
// must collapse to "other".
func TestSchemaSyncFailureReason(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "connection limit", err: fmt.Errorf("all slots taken: %w", errInstanceConnectionsExhausted), want: metrics.FailureReasonConnectionLimit},
		{name: "canceled", err: fmt.Errorf("sync stopped: %w", context.Canceled), want: metrics.FailureReasonCanceled},
		{name: "deadline", err: fmt.Errorf("sync stopped: %w", context.DeadlineExceeded), want: metrics.FailureReasonTimeout},
		{name: "hidden", err: fmt.Errorf("database %q: %w", "db1", ErrDatabaseHidden), want: metrics.FailureReasonHidden},
		{name: "not found", err: fmt.Errorf("driver: %w", common.Errorf(common.NotFound, "database is gone")), want: metrics.FailureReasonNotFound},
		{name: "driver error", err: errors.New("connection refused"), want: metrics.FailureReasonOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, schemaSyncFailureReason(tt.err))
		})
	}

	require.Equal(t, metrics.FailureReasonOther, schemaSyncFailureReason(common.Errorf(common.Internal, "internal")))
}

// The committed-write counter is per object type: the diff merges created and
// updated rows, so one map entry per type is what gets reported.
func TestCountMetaResourcesByType(t *testing.T) {
	t.Parallel()

	resource := func(metaType storepb.MetaType) *store.CreateMetaRegistryResourceMessage {
		return &store.CreateMetaRegistryResourceMessage{
			MetaRegistryResource: store.MetaRegistryResource{ObjectType: metaType},
		}
	}

	counts := countMetaResourcesByType([]*store.CreateMetaRegistryResourceMessage{
		resource(storepb.MetaType_TABLE),
		resource(storepb.MetaType_TABLE),
		resource(storepb.MetaType_COLUMN),
		resource(storepb.MetaType_VIEW),
	})
	require.Equal(t, map[storepb.MetaType]int{
		storepb.MetaType_TABLE:  2,
		storepb.MetaType_COLUMN: 1,
		storepb.MetaType_VIEW:   1,
	}, counts)
	require.Empty(t, countMetaResourcesByType(nil))
}
