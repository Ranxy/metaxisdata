package schemasync

import (
	"context"
	"log/slog"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// reportInstanceSyncFailure records an instance sync failure the periodic scan
// met. Connection-limit saturation is recoverable contention — another caller is
// using the instance's slots — rather than a broken instance: the next scan
// retries it because LastSyncTime did not advance, and an administrator cannot
// fix another request's concurrency, so it is not escalated.
func (s *Syncer) reportInstanceSyncFailure(ctx context.Context, instance *store.InstanceMessage, err error) {
	if errors.Is(err, errInstanceConnectionsExhausted) {
		slog.Debug("Instance sync skipped: the connection limit is saturated",
			slog.String("instance", instance.ResourceID))
		return
	}
	slog.Warn("Failed to sync instance",
		slog.String("instance", instance.ResourceID),
		log.WithError(err))
	// Nobody asked for this sync, so the failure would otherwise leave no trace
	// anyone reads until the next scheduled attempt.
	s.notifyBackgroundInstanceFailure(ctx, instance, err)
}

// SyncAllDatabases queues every database of an instance for the operation that
// asked for it, and seals the operation: a list it could not read leaves the
// operation reporting the instance step alone instead of waiting for databases
// that were never queued.
func (s *Syncer) SyncAllDatabases(ctx context.Context, operation *SyncOperation, instance *store.InstanceMessage) {
	find := &store.FindDatabaseMessage{}
	if instance != nil {
		find.InstanceID = &instance.ResourceID
	}
	databases, err := s.store.ListDatabases(ctx, find)
	if err != nil {
		// A full sync that could not read the database list must not report
		// success: seal the operation with the error so the user gets an ERROR
		// message instead of an INFO message naming zero databases.
		slog.Error("Failed to list databases to sync", log.WithError(err))
		s.FailOperation(ctx, operation, errors.Wrap(err, "failed to list the databases to sync"))
		return
	}
	s.EnqueueDatabases(ctx, operation, databases)
}
func (s *Syncer) QueueLineageAnalysis(metaGUID string, metaType storepb.MetaType) {
	if s == nil || s.lineageQueue == nil || metaGUID == "" {
		return
	}
	s.lineageQueue.QueueAnalysis(metaGUID, metaType)
}

// errInstanceConnectionsExhausted signals that the per-instance connection
// limiter is saturated; the caller should retry the sync later.
var errInstanceConnectionsExhausted = state.ErrInstanceConnectionLimit

// instanceEngine returns the source engine an instance's metadata names, or the
// unspecified engine when the instance or its metadata is missing.
func instanceEngine(instance *store.InstanceMessage) storepb.Engine {
	if instance == nil {
		return storepb.Engine_ENGINE_UNSPECIFIED
	}
	return instance.Metadata.GetEngine()
}

// schemaSyncFailureReason maps a failed sync's error to the failure-reason label.
// Only classifications the sync paths can prove are used: a driver error carries
// no transient/permanent type, so anything unclassified is "other".
func schemaSyncFailureReason(err error) string {
	switch {
	case errors.Is(err, state.ErrInstanceConnectionLimit):
		return metrics.FailureReasonConnectionLimit
	case errors.Is(err, context.Canceled):
		return metrics.FailureReasonCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return metrics.FailureReasonTimeout
	case errors.Is(err, ErrDatabaseHidden):
		return metrics.FailureReasonHidden
	case common.ErrorCode(err) == common.NotFound:
		return metrics.FailureReasonNotFound
	default:
		return metrics.FailureReasonOther
	}
}

// countMetaResourcesByType tallies a committed sync's changed rows by object type,
// so each type is one counter increment instead of one per row.
func countMetaResourcesByType(resources []*store.CreateMetaRegistryResourceMessage) map[storepb.MetaType]int {
	counts := make(map[storepb.MetaType]int, len(resources))
	for _, resource := range resources {
		counts[resource.ObjectType]++
	}
	return counts
}

// acquireInstanceConnection reserves one of the instance's outstanding
// connection slots and returns the release func. Every path that opens a driver
// goes through it, so instance-level and API-triggered syncs are throttled by
// the same limit as the periodic checker.
func (s *Syncer) acquireInstanceConnection(instance *store.InstanceMessage) (func(), error) {
	maximumConnections := int(instance.Metadata.GetMaximumConnections())
	if maximumConnections <= 0 {
		maximumConnections = common.DefaultInstanceMaximumConnections
	}
	release, ok := s.stateCfg.TryAcquireInstanceConnection(state.InstanceConnectionRequest{
		InstanceID:         instance.ResourceID,
		MaximumConnections: maximumConnections,
		Priority:           state.ConnectionPriorityBackground,
	})
	if !ok {
		// The checker keeps the database queued and retries on a later tick, so it
		// must not hold a slot waiting. A queued data-quality run owns the next free
		// slot; see .agents/docs/instance-connections.md.
		return nil, errors.Wrapf(state.ErrInstanceConnectionLimit, "instance %q already has all of its outstanding connections", instance.ResourceID)
	}
	return release, nil
}

// GetInstanceMeta gets the instance metadata.
func (s *Syncer) GetInstanceMeta(ctx context.Context, instance *store.InstanceMessage) (*db.InstanceMetadata, error) {
	release, err := s.acquireInstanceConnection(instance)
	if err != nil {
		return nil, err
	}
	defer release()

	driver, err := s.dbFactory.GetAdminDatabaseDriver(ctx, instance, nil /* database */, db.ConnectionContext{})
	if err != nil {
		return nil, err
	}
	defer driver.Close(ctx)

	deadlineCtx, cancelFunc := context.WithDeadline(ctx, time.Now().Add(syncTimeout))
	defer cancelFunc()
	driverStarted := time.Now()
	instanceMeta, err := driver.SyncInstance(deadlineCtx)
	s.metrics.ObserveDriverCall(instanceEngine(instance), metrics.DriverCallSyncInstance, time.Since(driverStarted))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to sync instance: %s", instance.ResourceID)
	}

	if instanceMeta.Metadata == nil {
		instanceMeta.Metadata = &storepb.Instance{}
	}

	instanceMeta.Metadata.LastSyncTime = timestamppb.Now()

	return instanceMeta, nil
}

// SyncInstance syncs the schema for all databases in an instance.
//
// The API and the periodic scan can ask for the same instance at once, so the
// enumeration is serialized per instance exactly as a database sync is: a caller
// that arrives while one is running waits for it and reuses its result. Two
// concurrent enumerations would otherwise create the same database rows and write
// the same deleted flags against each other's reads.
func (s *Syncer) SyncInstance(ctx context.Context, instance *store.InstanceMessage) (updatedInstance *store.InstanceMessage, databases []*storepb.DatabaseSchemaMetadata, newDatabases []*store.DatabaseMessage, err error) {
	if instance == nil {
		return nil, nil, nil, errors.New("instance is required")
	}

	key := instance.ResourceID
	call, owner := s.instanceSyncGate.acquire(key)
	if !owner {
		select {
		case <-call.done:
			return call.updatedInstance, call.databases, call.newDatabases, call.err
		case <-ctx.Done():
			return nil, nil, nil, ctx.Err()
		}
	}
	// The outcome and duration belong to the owner: a waiter reuses the owner's
	// result and is not a second sync. The clock starts after the gate, so the
	// time a caller spent waiting for another sync is not reported as sync time.
	engine := instanceEngine(instance)
	started := time.Now()
	defer func() {
		if r := recover(); r != nil {
			// Waiters must be released, and a panic is not a result to reuse.
			s.instanceSyncGate.finish(key, call, nil, nil, nil, errors.Errorf("instance schema sync panicked: %v", r))
			s.metrics.ObserveInstanceSync(engine, false, time.Since(started))
			s.metrics.CountSchemaSyncFailure(metrics.SyncStageInstance, metrics.FailureReasonOther)
			panic(r)
		}
		s.instanceSyncGate.finish(key, call, updatedInstance, databases, newDatabases, err)
		s.metrics.ObserveInstanceSync(engine, err == nil, time.Since(started))
		if err != nil {
			s.metrics.CountSchemaSyncFailure(metrics.SyncStageInstance, schemaSyncFailureReason(err))
		}
	}()

	instanceMeta, err := s.GetInstanceMeta(ctx, instance)
	if err != nil {
		return nil, nil, nil, err
	}

	storedDatabases, err := s.store.ListDatabases(ctx, &store.FindDatabaseMessage{InstanceID: &instance.ResourceID})
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, "failed to sync database for instance: %s. Failed to find database list", instance.ResourceID)
	}

	// Index the stored databases once: the loop below used a linear scan per
	// snapshot entry, which is quadratic for an instance with many databases.
	storedByName := make(map[string]*store.DatabaseMessage, len(storedDatabases))
	for _, database := range storedDatabases {
		storedByName[database.DatabaseName] = database
	}

	for _, databaseMetadata := range instanceMeta.Databases {
		if _, ok := storedByName[databaseMetadata.Name]; ok {
			continue
		}
		newDatabase, err := s.store.CreateDatabaseDefault(ctx, &store.DatabaseMessage{
			InstanceID:   instance.ResourceID,
			DatabaseName: databaseMetadata.Name,
		})
		if err != nil {
			return nil, nil, nil, errors.Wrapf(err, "failed to create instance %q database %q in sync runner", instance.ResourceID, databaseMetadata.Name)
		}
		if newDatabase != nil {
			newDatabases = append(newDatabases, newDatabase)
		}
	}

	// Databases that vanished from the instance snapshot are soft-deleted. The
	// snapshot is privilege-filtered on some engines (MySQL's information_schema
	// only lists what the connecting user may see) and an incomplete
	// instanceMeta.Databases would stop their sync, so log what disappears.
	snapshotNames := make(map[string]struct{}, len(instanceMeta.Databases))
	for _, databaseMetadata := range instanceMeta.Databases {
		snapshotNames[databaseMetadata.Name] = struct{}{}
	}
	var missingDatabases []*store.DatabaseMessage
	for _, database := range storedDatabases {
		if _, ok := snapshotNames[database.DatabaseName]; !ok {
			missingDatabases = append(missingDatabases, database)
		}
	}
	if len(missingDatabases) > 0 {
		names := make([]string, 0, len(missingDatabases))
		for _, database := range missingDatabases {
			names = append(names, database.DatabaseName)
		}
		slog.Warn("Soft-deleting databases missing from the synced instance snapshot",
			slog.String("instance", instance.ResourceID),
			slog.Int("count", len(names)),
			slog.Any("databases", names))
		// One statement for the whole set: hiding the databases one transaction at
		// a time left the instance partially hidden when one of them failed.
		if err := s.store.BatchSoftDeleteDatabases(ctx, instance.ResourceID, names); err != nil {
			return nil, nil, nil, errors.Wrapf(err, "failed to hide %d databases for instance %q", len(names), instance.ResourceID)
		}
		// A hidden database must not stay queued: the checker would keep syncing
		// it and write LastSyncTime to a row that is meant to be history. An
		// operation already waiting for it hears the hidden outcome instead of
		// waiting for a result that will never come.
		for _, database := range missingDatabases {
			s.forgetQueuedDatabase(database.String())
			s.completeDatabase(ctx, database, errors.Wrapf(ErrDatabaseHidden, "database %q", database.DatabaseName))
		}
	}

	// The sync only owns the fields it observed: last_sync_time, the engine
	// version and the case-sensitivity flag. Writing a whole metadata clone built
	// before the (potentially long) driver round trip would silently revert any
	// concurrent API change to the title, data sources or activation; the patch
	// reads the row under lock and merges. It is written only after the database
	// enumeration succeeded: writing it earlier marked an instance as synced even
	// when its databases could not be created or hidden, skipping it for a whole
	// interval.
	updatedInstance, err = s.store.UpdateInstance(ctx, &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		MetadataUpdates: []func(*storepb.Instance){
			func(metadata *storepb.Instance) {
				metadata.LastSyncTime = instanceMeta.Metadata.LastSyncTime
				metadata.MysqlLowerCaseTableNames = instanceMeta.Metadata.MysqlLowerCaseTableNames
				metadata.Version = instanceMeta.Version
			},
		},
	})
	if err != nil {
		return nil, nil, nil, err
	}

	return updatedInstance, instanceMeta.Databases, newDatabases, nil
}
