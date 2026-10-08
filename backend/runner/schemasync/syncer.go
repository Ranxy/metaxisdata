// Package schemasync is a runner that synchronize database schemas.
package schemasync

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/runner/lineageanalyzer"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

const (
	instanceSyncInterval        = 15 * time.Minute
	databaseSyncCheckerInterval = 10 * time.Second
	syncTimeout                 = 15 * time.Minute
	// defaultSyncInterval means never sync.
	defaultSyncInterval = 0 * time.Second
	MaximumOutstanding  = 100
	// maxDatabaseSyncRetries bounds one database's backoff retries before it is
	// left to the next instance-level scan.
	maxDatabaseSyncRetries = 3
)

// NewSyncer creates a schema syncer. notifier reports the outcome of a sync
// operation to the user who asked for it, and a background failure to the
// workspace administrators; a nil notifier leaves the runner silent, which is
// what its own tests want.
func NewSyncer(stores *store.Store, dbFactory *dbfactory.DBFactory, stateCfg *state.State, lineageAnalyzer *lineageanalyzer.Analyzer, lineageRevalidator LineageRevalidator, notifier Notifier) *Syncer {
	return &Syncer{
		store:              stores,
		dbFactory:          dbFactory,
		stateCfg:           stateCfg,
		lineageAnalyzer:    lineageAnalyzer,
		lineageRevalidator: lineageRevalidator,
		notifier:           notifier,
	}
}

// LineageRevalidator is signalled when a schema sync has added or changed
// metadata. Ingested lineage that named a relation the registry did not have is
// re-checked then, as soon as the relation appears, instead of at the next
// maintenance pass.
type LineageRevalidator interface {
	Trigger()
}

// Syncer is the schema syncer.
type Syncer struct {
	store              *store.Store
	dbFactory          *dbfactory.DBFactory
	stateCfg           *state.State
	lineageAnalyzer    *lineageanalyzer.Analyzer
	lineageRevalidator LineageRevalidator
	databaseSyncMap    sync.Map // map[string]*store.DatabaseMessage
	// databaseSyncRetryMap holds the backoff of a database whose sync failed:
	// the database stays queued and the checker skips it until nextAt, so a
	// transient failure does not wait for the next instance-level scan.
	databaseSyncRetryMap sync.Map // map[string]databaseRetry
	// databaseSyncGate serializes the schema sync of one database. The queue
	// above is drained on dequeue, so it is not an in-flight guard by itself,
	// and the API path calls SyncDatabaseSchema directly.
	databaseSyncGate databaseSyncGate
	// notifier writes the messages a sync operation produces. See operation.go.
	notifier Notifier
	// operations are the user-visible sync operations still waiting for their
	// databases. The slice is short — one entry per API-triggered sync in flight —
	// and is guarded by operationMu because the API registers them while the
	// checker reports their databases.
	operationMu sync.Mutex
	operations  []*SyncOperation
}

// databaseSyncGate makes one schema sync per database run at a time inside the
// process. Two concurrent syncs would each diff the digest they read before
// either writes, so the later commit can delete a row the other just created or
// deadlock against it. A caller that arrives while a sync is running waits for
// it and reuses its outcome instead of starting a second sync.
type databaseSyncGate struct {
	mu       sync.Mutex
	inFlight map[string]*databaseSyncCall
}

// databaseSyncCall is one database's running sync: done is closed when the
// owner published err.
type databaseSyncCall struct {
	done chan struct{}
	err  error
}

// acquire returns the call that owns the sync for key, or the call to wait on
// when another caller already owns it.
func (g *databaseSyncGate) acquire(key string) (call *databaseSyncCall, owner bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inFlight == nil {
		g.inFlight = map[string]*databaseSyncCall{}
	}
	if existing, ok := g.inFlight[key]; ok {
		return existing, false
	}
	call = &databaseSyncCall{done: make(chan struct{})}
	g.inFlight[key] = call
	return call, true
}

// owns reports whether some caller currently holds this database's sync. A
// database being synced has already been dequeued, so it is invisible to the queue
// and only this knows it is still in progress.
func (g *databaseSyncGate) owns(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	_, ok := g.inFlight[key]
	return ok
}

// finish publishes the owner's result and releases the waiters. The entry is
// removed before done is closed, so a caller arriving after this point starts a
// fresh sync instead of reusing a finished one.
func (g *databaseSyncGate) finish(key string, call *databaseSyncCall, err error) {
	g.mu.Lock()
	call.err = err
	delete(g.inFlight, key)
	g.mu.Unlock()
	close(call.done)
}

// databaseRetry is the backoff state of a failed database schema sync.
type databaseRetry struct {
	attempts int
	nextAt   time.Time
}

// databaseSyncRetryBackoff returns the delay before the next attempt. The last
// step equals instanceSyncInterval: past it the 15-minute instance scan is the
// retry.
func databaseSyncRetryBackoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	default:
		return instanceSyncInterval
	}
}

// enqueueDatabase queues a database for the checker. A fresh request (an API full
// sync or the periodic scan) supersedes any pending backoff. A deleted row is never
// queued: it is kept as history, and only the instance enumeration revives it.
func (s *Syncer) enqueueDatabase(database *store.DatabaseMessage, _ *SyncOperation) {
	if database == nil || database.Deleted {
		return
	}
	key := database.String()
	s.databaseSyncRetryMap.Delete(key)
	s.databaseSyncMap.Store(key, database)
}

// scheduleRetry re-queues a failed database with a bounded backoff; the checker
// skips it until nextAt. After the last attempt the database is left to the next
// instance-level scan instead of backing off forever. It reports whether the
// retries are exhausted, which is the point at which a background failure is
// worth telling the administrators about.
func (s *Syncer) scheduleRetry(database *store.DatabaseMessage) bool {
	key := database.String()
	attempts := 1
	if v, ok := s.databaseSyncRetryMap.Load(key); ok {
		if entry, ok := v.(databaseRetry); ok {
			attempts = entry.attempts + 1
		}
	}
	if attempts > maxDatabaseSyncRetries {
		s.databaseSyncRetryMap.Delete(key)
		slog.Warn("Database schema sync gave up after repeated failures; waiting for the next instance scan",
			slog.String("instance", database.InstanceID),
			slog.String("database", database.DatabaseName))
		return true
	}
	s.databaseSyncRetryMap.Store(key, databaseRetry{attempts: attempts, nextAt: time.Now().Add(databaseSyncRetryBackoff(attempts))})
	s.databaseSyncMap.Store(key, database)
	return false
}

// retryDue reports whether a queued database may be synced at now. A database
// whose sync failed stays queued but is skipped until its backoff elapses.
func (s *Syncer) retryDue(key any, now time.Time) bool {
	v, ok := s.databaseSyncRetryMap.Load(key)
	if !ok {
		return true
	}
	entry, ok := v.(databaseRetry)
	if !ok {
		return true
	}
	return !now.Before(entry.nextAt)
}

// Run will run the schema syncer once.
func (s *Syncer) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	sp := pool.New()
	sp.Go(func() {
		s.trySyncAll(ctx)
		slog.Debug(fmt.Sprintf("Schema syncer started and will run every %v", instanceSyncInterval))
		ticker := time.NewTicker(instanceSyncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.trySyncAll(ctx)
			case <-ctx.Done(): // if cancel() execute
				return
			}
		}
	})

	sp.Go(func() {
		ticker := time.NewTicker(databaseSyncCheckerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.expireOperations(ctx, time.Now())
				instances, err := s.store.ListInstances(ctx, &store.FindInstanceMessage{})
				if err != nil {
					// A transient store error must not stop the checker for the
					// lifetime of the process; skip this tick and retry.
					slog.Error("Failed to list instance", log.WithError(err))
					continue
				}
				instanceMap := make(map[string]*store.InstanceMessage)
				for _, instance := range instances {
					instanceMap[instance.ResourceID] = instance
				}
				now := time.Now()
				dbwp := pool.New().WithMaxGoroutines(MaximumOutstanding)
				s.databaseSyncMap.Range(func(key, value any) bool {
					database, ok := value.(*store.DatabaseMessage)
					if !ok {
						return true
					}

					if _, ok := instanceMap[database.InstanceID]; !ok {
						slog.Debug("Instance not found",
							slog.String("instance", database.InstanceID))
						// The instance is gone (or not visible any more); drop the
						// entry instead of retrying and logging it every tick
						// forever. An operation waiting for this database is told:
						// no further attempt will ever report for it.
						s.databaseSyncRetryMap.Delete(key)
						s.databaseSyncMap.Delete(key)
						s.completeDatabase(ctx, database, errors.Errorf("instance %q is no longer there", database.InstanceID))
						return true
					}

					// A failed database stays queued but is skipped until its
					// backoff elapses, so a tick does not retry it immediately.
					if !s.retryDue(key, now) {
						return true
					}

					s.databaseSyncMap.Delete(key)
					dbwp.Go(func() {
						defer func() {
							// A panic inside one database sync must not propagate
							// out of Wait() and take the checker goroutine (or the
							// process) down with it.
							if r := recover(); r != nil {
								err, ok := r.(error)
								if !ok {
									err = errors.Errorf("%v", r)
								}
								slog.Error("Database schema sync PANIC RECOVER",
									slog.String("instance", database.InstanceID),
									slog.String("database", database.DatabaseName),
									log.WithError(err))
								s.completeDatabase(ctx, database, err)
								s.scheduleRetry(database)
							}
						}()
						slog.Debug("Sync database schema", slog.String("instance", database.InstanceID), slog.String("database", database.DatabaseName))
						if err := s.SyncDatabaseSchema(ctx, database); err != nil {
							if errors.Is(err, errInstanceConnectionsExhausted) {
								// The per-instance limiter is saturated; keep the
								// database queued and retry on a later tick. This is
								// contention, not a failed sync, so it does not
								// advance the backoff.
								s.databaseSyncMap.Store(database.String(), database)
								return
							}
							slog.Warn("Failed to sync database schema",
								slog.String("instance", database.InstanceID),
								slog.String("databaseName", database.DatabaseName),
								log.WithError(err))
							// The operation reports this first result; the retries below
							// are the checker's own recovery, and only their exhaustion is
							// worth interrupting an administrator for.
							s.completeDatabase(ctx, database, err)
							if s.scheduleRetry(database) {
								s.notifyBackgroundDatabaseFailure(ctx, instanceMap[database.InstanceID], database, err)
							}
							return
						}
						s.databaseSyncRetryMap.Delete(database.String())
						s.completeDatabase(ctx, database, nil)
					})
					return true
				})
				dbwp.Wait()
			case <-ctx.Done(): // if cancel() execute
				return
			}
		}
	})
	sp.Wait()
}

func (s *Syncer) trySyncAll(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			err, ok := r.(error)
			if !ok {
				err = errors.Errorf("%v", r)
			}
			slog.Error("Instance syncer PANIC RECOVER", log.WithError(err), log.Stack("panic-stack"))
		}
	}()

	wp := pool.New().WithMaxGoroutines(MaximumOutstanding)
	instances, err := s.store.ListInstances(ctx, &store.FindInstanceMessage{})
	if err != nil {
		slog.Error("Failed to retrieve instances", log.WithError(err))
		return
	}
	now := time.Now()
	for _, instance := range instances {
		if !shouldSyncNow(getOrDefaultSyncInterval(instance), getOrDefaultLastSyncTime(instance.Metadata.LastSyncTime), now) {
			continue
		}

		wp.Go(func() {
			slog.Debug("Sync instance schema", slog.String("instance", instance.ResourceID))
			if _, _, _, err := s.SyncInstance(ctx, instance); err != nil {
				slog.Warn("Failed to sync instance",
					slog.String("instance", instance.ResourceID),
					log.WithError(err))
				// Nobody asked for this sync, so the failure would otherwise leave
				// no trace anyone reads until the next scheduled attempt.
				s.notifyBackgroundInstanceFailure(ctx, instance, err)
			}
		})
	}
	wp.Wait()

	instancesMap := map[string]*store.InstanceMessage{}
	for _, instance := range instances {
		instancesMap[instance.ResourceID] = instance
	}

	databases, err := s.store.ListDatabases(ctx, &store.FindDatabaseMessage{})
	if err != nil {
		slog.Error("Failed to retrieve databases", log.WithError(err))
		return
	}
	for _, database := range databases {
		database := database
		instance, ok := instancesMap[database.InstanceID]
		if !ok {
			continue
		}
		// The database inherits the sync interval from the instance.
		if !shouldSyncNow(getOrDefaultSyncInterval(instance), getOrDefaultLastSyncTime(database.Metadata.LastSyncTime), now) {
			continue
		}

		s.enqueueDatabase(database, nil)
	}
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
		slog.Debug("Failed to find databases to sync",
			slog.String("error", err.Error()))
		databases = nil
	}
	s.EnqueueDatabases(ctx, operation, databases)
}

func (s *Syncer) QueueLineageAnalysis(metaGUID string, metaType storepb.MetaType) {
	if s == nil || s.lineageAnalyzer == nil || metaGUID == "" {
		return
	}
	s.lineageAnalyzer.QueueAnalysis(metaGUID, metaType)
}

// errInstanceConnectionsExhausted signals that the per-instance connection
// limiter is saturated; the caller should retry the sync later.
var errInstanceConnectionsExhausted = state.ErrInstanceConnectionLimit

// acquireInstanceConnection reserves one of the instance's outstanding
// connection slots and returns the release func. Every path that opens a driver
// goes through it, so instance-level and API-triggered syncs are throttled by
// the same limit as the periodic checker.
func (s *Syncer) acquireInstanceConnection(instance *store.InstanceMessage) (func(), error) {
	maximumConnections := int(instance.Metadata.GetMaximumConnections())
	if maximumConnections <= 0 {
		maximumConnections = common.DefaultInstanceMaximumConnections
	}
	return s.stateCfg.AcquireInstanceConnection(instance.ResourceID, maximumConnections)
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
	instanceMeta, err := driver.SyncInstance(deadlineCtx)
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
func (s *Syncer) SyncInstance(ctx context.Context, instance *store.InstanceMessage) (*store.InstanceMessage, []*storepb.DatabaseSchemaMetadata, []*store.DatabaseMessage, error) {
	instanceMeta, err := s.GetInstanceMeta(ctx, instance)
	if err != nil {
		return nil, nil, nil, err
	}
	metadata, ok := proto.Clone(instance.Metadata).(*storepb.Instance)
	if !ok {
		return nil, nil, nil, errors.Errorf("failed to convert instance metadata type")
	}
	metadata.LastSyncTime = instanceMeta.Metadata.LastSyncTime
	metadata.MysqlLowerCaseTableNames = instanceMeta.Metadata.MysqlLowerCaseTableNames

	updateInstance := &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		Metadata:   metadata,
	}
	if instanceMeta.Version != instance.Metadata.GetVersion() {
		metadata.Version = instanceMeta.Version
	}
	updatedInstance, err := s.store.UpdateInstance(ctx, updateInstance)
	if err != nil {
		return nil, nil, nil, err
	}

	databases, err := s.store.ListDatabases(ctx, &store.FindDatabaseMessage{InstanceID: &instance.ResourceID})
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, "failed to sync database for instance: %s. Failed to find database list", instance.ResourceID)
	}
	var newDatabases []*store.DatabaseMessage

	// Index the stored databases once: the loop below used a linear scan per
	// snapshot entry, which is quadratic for an instance with many databases.
	storedByName := make(map[string]*store.DatabaseMessage, len(databases))
	for _, database := range databases {
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
	var missingDatabases []string
	for _, database := range databases {
		if _, ok := snapshotNames[database.DatabaseName]; !ok {
			missingDatabases = append(missingDatabases, database.DatabaseName)
		}
	}
	if len(missingDatabases) > 0 {
		slog.Warn("Soft-deleting databases missing from the synced instance snapshot",
			slog.String("instance", instance.ResourceID),
			slog.Int("count", len(missingDatabases)),
			slog.Any("databases", missingDatabases))
	}
	for _, databaseName := range missingDatabases {
		d := true
		if _, err := s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{
			InstanceID:   instance.ResourceID,
			DatabaseName: databaseName,
			Deleted:      &d,
		}); err != nil {
			return nil, nil, nil, errors.Errorf("failed to update database %q for instance %q", databaseName, instance.ResourceID)
		}
	}

	return updatedInstance, instanceMeta.Databases, newDatabases, nil
}

// SyncDatabaseSchema will sync the schema for a database.
//
// Two callers can ask for the same database at once (the periodic checker and
// the API both call this function), so the sync is serialized per database: a
// caller that arrives while one is running waits for it and reuses its result.
func (s *Syncer) SyncDatabaseSchema(ctx context.Context, database *store.DatabaseMessage) (err error) {
	if database == nil {
		return nil
	}

	key := database.String()
	call, owner := s.databaseSyncGate.acquire(key)
	if !owner {
		select {
		case <-call.done:
			return call.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() {
		if r := recover(); r != nil {
			// The waiters must be released, and a panic is not a success they may
			// reuse.
			s.databaseSyncGate.finish(key, call, errors.Errorf("database schema sync panicked: %v", r))
			panic(r)
		}
		s.databaseSyncGate.finish(key, call, err)
	}()

	instance, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &database.InstanceID})
	if err != nil {
		return errors.Wrapf(err, "failed to get instance %q", database.InstanceID)
	}
	if instance == nil {
		return errors.Errorf("instance %q not found", database.InstanceID)
	}
	release, err := s.acquireInstanceConnection(instance)
	if err != nil {
		return err
	}
	defer release()

	driver, err := s.dbFactory.GetAdminDatabaseDriver(ctx, instance, database, db.ConnectionContext{})
	if err != nil {
		return err
	}
	defer driver.Close(ctx)

	deadlineCtx, cancelFunc := context.WithDeadline(ctx, time.Now().Add(syncTimeout))
	defer cancelFunc()
	databaseMetadata, err := driver.SyncDBSchema(deadlineCtx)
	if err != nil {
		// The target no longer has this database. Every further sync of the row
		// would fail the same way, so mirror the deletion here instead of leaving
		// a database the target does not have visible until the next instance
		// enumeration notices. Only that enumeration can show the row again.
		if common.ErrorCode(err) == common.NotFound {
			if hideErr := s.markDatabaseDeleted(ctx, database, err); hideErr != nil {
				return errors.Wrapf(hideErr, "failed to hide database %q the target no longer has", database.DatabaseName)
			}
			return nil
		}
		return errors.Wrapf(err, "failed to sync database schema for database %q", database.DatabaseName)
	}

	databaseGUID := buildGUID(database.InstanceID, database.DatabaseName)

	// Only the GUID, object type and meta hash are needed to diff the snapshot
	// against what is stored; the digest listing avoids parsing every JSONB row
	// on every sync.
	storedMetadatas, err := s.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{GUIDPrefix: &databaseGUID})
	if err != nil {
		return errors.Wrapf(err, "failed to list existing meta registry for database %q", database.DatabaseName)
	}

	bmc := &batchMetaCreate{
		exist:    storedMetadatas,
		guidList: make([]*store.CreateMetaRegistryResourceMessage, 0),
	}

	for _, schema := range databaseMetadata.Schemas {
		schemaGUIDPrefix := buildGUID(databaseGUID, schema.Name)
		for _, table := range schema.Tables {
			meta := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: table}}

			err = bmc.StoreMetaResource(ctx, schemaGUIDPrefix, storepb.MetaType_TABLE, meta)
			if err != nil {
				return errors.Wrapf(err, "failed to store table metadata for table %q in database %q", table.Name, database.DatabaseName)
			}
		}

		schema.Tables = nil

		for _, view := range schema.Views {
			meta := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: view}}

			err = bmc.StoreMetaResource(ctx, schemaGUIDPrefix, storepb.MetaType_VIEW, meta)
			if err != nil {
				return errors.Wrapf(err, "failed to store view metadata for view %q in database %q", view.Name, database.DatabaseName)
			}
		}
		schema.Views = nil

		for _, materializedView := range schema.MaterializedViews {
			err = bmc.StoreMetaResource(ctx, buildGUID(databaseGUID, schema.Name), storepb.MetaType_MATERIALIZED_VIEW, &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: materializedView}})
			if err != nil {
				return errors.Wrapf(err, "failed to store materialized view metadata for materialized view %q in database %q", materializedView.Name, database.DatabaseName)
			}
		}
		schema.MaterializedViews = nil

		for _, sequence := range schema.Sequences {
			err = bmc.StoreMetaResource(ctx, schemaGUIDPrefix, storepb.MetaType_SEQUENCE, &storepb.StoredMetadata{Type: &storepb.StoredMetadata_SequenceMetadata{SequenceMetadata: sequence}})
			if err != nil {
				return errors.Wrapf(err, "failed to store sequence metadata for sequence %q in database %q", sequence.Name, database.DatabaseName)
			}
		}
		schema.Sequences = nil

		for _, function := range schema.Functions {
			err = bmc.StoreMetaResource(ctx, schemaGUIDPrefix, storepb.MetaType_FUNCTION, &storepb.StoredMetadata{Type: &storepb.StoredMetadata_FunctionMetadata{FunctionMetadata: function}})
			if err != nil {
				return errors.Wrapf(err, "failed to store function metadata for function %q in database %q", function.Name, database.DatabaseName)
			}
		}
		schema.Functions = nil

		for _, procedure := range schema.Procedures {
			err = bmc.StoreMetaResource(ctx, schemaGUIDPrefix, storepb.MetaType_PROCEDURE, &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ProcedureMetadata{ProcedureMetadata: procedure}})
			if err != nil {
				return errors.Wrapf(err, "failed to store procedure metadata for procedure %q in database %q", procedure.Name, database.DatabaseName)
			}
		}
		schema.Procedures = nil

		for _, externalTable := range schema.ExternalTables {
			err = bmc.StoreMetaResource(ctx, schemaGUIDPrefix, storepb.MetaType_EXTERNAL_TABLE, &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ExternalTableMetadata{ExternalTableMetadata: externalTable}})
			if err != nil {
				return errors.Wrapf(err, "failed to store external table metadata for external table %q in database %q", externalTable.Name, database.DatabaseName)
			}
		}
		schema.ExternalTables = nil

		{
			meta := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_SchemaMetadata{SchemaMetadata: schema}}
			err = bmc.StoreMetaResource(ctx, databaseGUID, storepb.MetaType_SCHEMA, meta)
			if err != nil {
				return errors.Wrapf(err, "failed to store schema metadata for schema %q in database %q", schema.Name, database.DatabaseName)
			}
		}
	}
	databaseMetadata.Schemas = nil
	{
		meta := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_DatabaseSchemaMetadata{DatabaseSchemaMetadata: databaseMetadata}}
		err = bmc.StoreMetaResource(ctx, database.InstanceID, storepb.MetaType_DATABASE, meta)
		if err != nil {
			return errors.Wrapf(err, "failed to store database metadata for  database %q", database.DatabaseName)
		}
	}

	// The diff is pure computation and the DDL fetch below is up to one network
	// round trip per object; both run before the transaction is opened so the row
	// locks on meta_registry_resource are held only for the writes instead of
	// across the whole snapshot's round trips.
	if err := bmc.prepare(); err != nil {
		return errors.Wrapf(err, "failed to diff metadata for database %q", database.DatabaseName)
	}

	// deadlineCtx is used for the driver round trips so a hung target cannot
	// outlive the sync deadline. Engines without the capability are skipped.
	var definitions *objectDefinitionBatch
	if reader, ok := driver.(db.ObjectDefinitionReader); ok {
		definitions, err = prepareObjectDefinitions(deadlineCtx, s.store, reader, bmc)
		if err != nil {
			return errors.Wrapf(err, "failed to sync object definitions for database %q", database.DatabaseName)
		}
	}

	// From here on the transaction only writes: the metadata rows first, then the
	// DDL rows, which are a strict subset of them, then the lineage rows of the
	// deleted objects. A rolled-back sync therefore cannot leave a definition
	// behind that its metadata row does not have.
	tx, err := s.store.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := bmc.write(ctx, s.store, tx); err != nil {
		return errors.Wrapf(err, "failed to batch store metadata for database %q", database.DatabaseName)
	}

	if err := definitions.write(ctx, s.store, tx); err != nil {
		return errors.Wrapf(err, "failed to sync object definitions for database %q", database.DatabaseName)
	}

	logSchemaSyncDeletion(common.FormatDatabase(database.InstanceID, database.DatabaseName), bmc.deletes)

	// Clean lineage rows for every deleted object, not only views: a dropped
	// table or column leaves edges whose endpoint GUID no longer exists, and a
	// dropped view also stops being the producer of its edges.
	for _, item := range bmc.deletes {
		if err := deleteColumnLineageByMetaTx(ctx, tx, item.GUID, item.ObjectType); err != nil {
			return errors.Wrapf(err, "failed to delete column lineage for guid %q", item.GUID)
		}
	}

	err = tx.Commit()
	if err != nil {
		return errors.Wrapf(err, "failed to commit transaction for database %q", database.DatabaseName)
	}

	// The metadata cache is invalidated only after a successful commit, so a
	// rolled-back sync cannot leave stale or uncommitted entries behind.
	invalidated := make([]*store.MetaRegistryResource, 0, len(bmc.updates)+len(bmc.deletes))
	for _, item := range bmc.updates {
		invalidated = append(invalidated, &item.MetaRegistryResource)
	}
	invalidated = append(invalidated, bmc.deletes...)
	s.store.InvalidateMetaRegistryCache(invalidated)

	// Queue changed VIEWs and MATERIALIZED_VIEWs before touching the db row: if
	// the LastSyncTime update below fails, the next sync sees unchanged hashes
	// and would never queue them again.
	if s.lineageAnalyzer != nil {
		for _, item := range bmc.updates {
			if item.ObjectType == storepb.MetaType_VIEW || item.ObjectType == storepb.MetaType_MATERIALIZED_VIEW {
				s.lineageAnalyzer.QueueAnalysis(item.GUID, item.ObjectType)
			}
		}
	}

	// Metadata this sync added or changed can turn a relation an ingested edge
	// named from unknown into known, which is the one thing that lets its column
	// claims be checked. Signal that only when there is something new to check,
	// so a sync that found no change does not schedule a pass over the same
	// pending edges. The runner coalesces the signals of one sync round.
	if s.lineageRevalidator != nil && len(bmc.updates) > 0 {
		s.lineageRevalidator.Trigger()
	}

	// LastSyncTime is recorded only after the metadata transaction committed.
	// Writing it first would mark the database as synced even when the commit
	// failed, skipping it for a whole sync interval.
	//
	// The deleted flag is deliberately not written here. A database becomes
	// visible again only when the instance enumeration lists it (SyncInstance
	// creates or revives the row), because only the enumeration answers "which
	// databases does the instance have"; a single-database read is not that
	// answer. Writing it here would let a sync of a database that a partial
	// instance snapshot just hid resurrect it, and the next snapshot would hide
	// it again.
	if _, err := s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{
		InstanceID:   database.InstanceID,
		DatabaseName: database.DatabaseName,
		MetadataUpdates: []func(*storepb.DatabaseMetadata){
			func(md *storepb.DatabaseMetadata) {
				md.LastSyncTime = timestamppb.Now()
			},
		},
	}); err != nil {
		return errors.Wrapf(err, "failed to update database %q for instance %q", database.DatabaseName, database.InstanceID)
	}

	return nil
}

// markDatabaseDeleted hides a database the target reports it no longer has, and
// records the driver error that established it. The metadata rows are kept: they
// are the history of what the database held, and the instance enumeration
// revives the row if the name reappears in a snapshot.
func (s *Syncer) markDatabaseDeleted(ctx context.Context, database *store.DatabaseMessage, cause error) error {
	deleted := true
	if _, err := s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{
		InstanceID:   database.InstanceID,
		DatabaseName: database.DatabaseName,
		Deleted:      &deleted,
	}); err != nil {
		return err
	}
	slog.Warn("Hiding a database the target no longer has",
		slog.String("instance", database.InstanceID),
		slog.String("database", database.DatabaseName),
		log.WithError(cause))
	return nil
}

type batchMetaCreate struct {
	exist    []*store.MetaRegistryResource
	guidList []*store.CreateMetaRegistryResourceMessage
	updates  []*store.CreateMetaRegistryResourceMessage // populated by prepare()
	deletes  []*store.MetaRegistryResource              // populated by prepare()
}

func (b *batchMetaCreate) StoreMetaResource(_ context.Context, prefixName string, objectType storepb.MetaType, data *storepb.StoredMetadata) error {
	guid, err := convertMetadataToGUID(prefixName, objectType, data)
	if err != nil {
		return err
	}

	for _, child := range getChildMetadataResources(guid, objectType, data) {
		b.add(child.GUID, child.ObjectType, child.Metadata)
	}

	b.add(guid, objectType, data)
	return nil
}

func (b *batchMetaCreate) add(guid string, mt storepb.MetaType, data *storepb.StoredMetadata) {
	registry := &store.CreateMetaRegistryResourceMessage{
		MetaRegistryResource: store.MetaRegistryResource{
			GUID:       guid,
			ObjectType: mt,
			Metadata:   data,
		},
	}
	b.guidList = append(b.guidList, registry)
}

// prepare computes the diff between the snapshot and the stored rows. It is
// pure computation and must run before the write transaction is opened, so no
// row lock is held while the target instance is queried.
func (b *batchMetaCreate) prepare() error {
	updates, deletes, err := b.diff()
	if err != nil {
		return errors.Wrap(err, "batchMetaCreateRunDiff")
	}
	b.updates = updates
	b.deletes = deletes
	return nil
}

// write persists a prepared diff inside the caller's transaction.
func (b *batchMetaCreate) write(ctx context.Context, s *store.Store, tx *sql.Tx) error {
	observedAt := time.Now().UTC()

	if len(b.deletes) > 0 {
		if err := s.BatchDeleteMetaRegistryAt(ctx, tx, b.deletes, observedAt); err != nil {
			return errors.Wrap(err, "BatchDeleteMetaRegistryResourceByID")
		}
	}
	if len(b.updates) > 0 {
		if _, err := s.BatchCreateMetaRegistryResourceAt(ctx, tx, b.updates, observedAt); err != nil {
			return errors.Wrap(err, "BatchCreateMetaRegistryResource")
		}
	}
	return nil
}

func (b *batchMetaCreate) diff() (updates []*store.CreateMetaRegistryResourceMessage, deletes []*store.MetaRegistryResource, err error) {
	existMap := make(map[store.MetaGUIDKey]*store.MetaRegistryResource)
	for _, item := range b.exist {
		if !isSchemaSyncManagedMetaType(item.ObjectType) {
			continue
		}
		existMap[item.GUIDKey()] = item
	}

	for _, item := range b.guidList {
		metadataBytes, hash, err := store.CalcStoreMetaHash(item.Metadata)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to calculate metadata hash for guid %q", item.GUID)
		}

		item.MetaHash = hash
		item.MetadataBytes = metadataBytes

		existing, ok := existMap[item.GUIDKey()]
		if !ok {
			// new item
			updates = append(updates, item)
			continue
		}

		if existing.MetaHash == nil || item.MetaHash == nil || !bytes.Equal(existing.MetaHash, item.MetaHash) {
			updates = append(updates, item)
		}
		delete(existMap, item.GUIDKey())
	}
	for _, item := range existMap {
		deletes = append(deletes, item)
	}

	return updates, deletes, nil
}

func isSchemaSyncManagedMetaType(metaType storepb.MetaType) bool {
	switch metaType {
	case storepb.MetaType_DATABASE,
		storepb.MetaType_SCHEMA,
		storepb.MetaType_TABLE,
		storepb.MetaType_COLUMN,
		storepb.MetaType_VIEW,
		storepb.MetaType_EXTERNAL_TABLE,
		storepb.MetaType_FUNCTION,
		storepb.MetaType_PROCEDURE,
		storepb.MetaType_MATERIALIZED_VIEW,
		storepb.MetaType_SEQUENCE:
		return true
	default:
		return false
	}
}

func convertMetadataToGUID(prefix string, objectType storepb.MetaType, data *storepb.StoredMetadata) (string, error) {
	name, err := metaObjectName(objectType, data)
	if err != nil {
		return "", err
	}
	return buildGUID(prefix, name), nil
}

func getChildMetadataResources(parentGUID string, objectType storepb.MetaType, data *storepb.StoredMetadata) []*store.CreateMetaRegistryResourceMessage {
	switch objectType {
	case storepb.MetaType_TABLE:
		return buildColumnMetadataResources(parentGUID, data.GetTableMetadata().Columns)
	default:
		return nil
	}
}

// buildGUID appends new segments to a prefix. The first argument is an opaque
// prefix (an instance ID or an already-built GUID) and is passed through; the
// remaining arguments are names that get the separator escaped.
func buildGUID(prefix string, names ...string) string {
	return prefix + common.MetaGUIDSplit + common.BuildMetaGUID(names...)
}

func buildColumnMetadataResources(prefix string, cols []*storepb.ColumnMetadata) []*store.CreateMetaRegistryResourceMessage {
	resources := make([]*store.CreateMetaRegistryResourceMessage, 0, len(cols))
	for _, col := range cols {
		if col == nil {
			continue
		}
		columnMetadata, ok := proto.Clone(col).(*storepb.ColumnMetadata)
		if !ok {
			continue
		}
		resources = append(resources, &store.CreateMetaRegistryResourceMessage{
			MetaRegistryResource: store.MetaRegistryResource{
				GUID:       buildGUID(prefix, columnMetadata.Name),
				ObjectType: storepb.MetaType_COLUMN,
				Metadata: &storepb.StoredMetadata{
					Type: &storepb.StoredMetadata_ColumnMetadata{
						ColumnMetadata: columnMetadata,
					},
				},
			},
		})
	}
	return resources
}

// maxLoggedDeletedGUIDs bounds how many deleted GUIDs a single log record names.
const maxLoggedDeletedGUIDs = 20

// logSchemaSyncDeletion records the metadata resources a sync is about to
// delete. The driver snapshot is treated as the source of truth, so an empty or
// partial snapshot silently removes rows — MySQL's information_schema only
// lists objects the connecting user may see, with no error. This log is the
// only trace of that happening.
func logSchemaSyncDeletion(database string, deletes []*store.MetaRegistryResource) {
	if len(deletes) == 0 {
		return
	}
	counts := map[storepb.MetaType]int{}
	guids := make([]string, 0, min(len(deletes), maxLoggedDeletedGUIDs))
	for _, item := range deletes {
		counts[item.ObjectType]++
		if len(guids) < maxLoggedDeletedGUIDs {
			guids = append(guids, item.GUID)
		}
	}
	slog.Warn("Deleting metadata resources missing from the synced snapshot",
		slog.String("database", database),
		slog.Int("count", len(deletes)),
		slog.Int("omittedGUIDs", len(deletes)-len(guids)),
		slog.Any("countByObjectType", counts),
		slog.Any("sampleGUIDs", guids))
}

// shouldSyncNow reports whether a resource whose last successful sync was
// lastSyncTime, under an instance whose sync interval is interval, is due at
// now. An interval of defaultSyncInterval means "never sync": it must be
// rejected explicitly, because lastSyncTime.Add(0) is never after now and would
// otherwise schedule every deactivated instance on every tick.
func shouldSyncNow(interval time.Duration, lastSyncTime, now time.Time) bool {
	if interval == defaultSyncInterval {
		return false
	}
	return !now.Before(lastSyncTime.Add(interval))
}

func getOrDefaultSyncInterval(instance *store.InstanceMessage) time.Duration {
	if !instance.Metadata.GetActivation() {
		return defaultSyncInterval
	}
	if !instance.Metadata.GetSyncInterval().IsValid() {
		return defaultSyncInterval
	}
	if instance.Metadata.GetSyncInterval().GetSeconds() == 0 && instance.Metadata.GetSyncInterval().GetNanos() == 0 {
		return defaultSyncInterval
	}
	return instance.Metadata.GetSyncInterval().AsDuration()
}

func getOrDefaultLastSyncTime(t *timestamppb.Timestamp) time.Time {
	if t.IsValid() {
		return t.AsTime()
	}
	return time.Unix(0, 0)
}

// deleteColumnLineageByMetaTx removes every lineage row that mentions a deleted
// object: the edges it produced (meta_guid) and the edges that point at it as a
// source or target. Rows left behind would reference a GUID that no longer
// exists.
func deleteColumnLineageByMetaTx(ctx context.Context, tx *sql.Tx, metaGUID string, metaType storepb.MetaType) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM column_lineage
		 WHERE (meta_guid = $1 AND meta_type = $2)
		    OR (source_guid = $1 AND source_type = $2)
		    OR (target_guid = $1 AND target_type = $2)`,
		metaGUID, metaType,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM column_lineage_version WHERE meta_guid = $1 AND meta_type = $2`,
		metaGUID, metaType,
	); err != nil {
		return err
	}
	return nil
}
