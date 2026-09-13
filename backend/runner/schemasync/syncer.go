// Package schemasync is a runner that synchronize database schemas.
package schemasync

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
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
)

// NewSyncer creates a schema syncer.
func NewSyncer(stores *store.Store, dbFactory *dbfactory.DBFactory, stateCfg *state.State, lineageAnalyzer *lineageanalyzer.Analyzer) *Syncer {
	return &Syncer{
		store:           stores,
		dbFactory:       dbFactory,
		stateCfg:        stateCfg,
		lineageAnalyzer: lineageAnalyzer,
	}
}

// Syncer is the schema syncer.
type Syncer struct {
	store           *store.Store
	dbFactory       *dbfactory.DBFactory
	stateCfg        *state.State
	lineageAnalyzer *lineageanalyzer.Analyzer
	databaseSyncMap sync.Map // map[string]*store.DatabaseMessage
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
				dbwp := pool.New().WithMaxGoroutines(MaximumOutstanding)
				s.databaseSyncMap.Range(func(key, value any) bool {
					database, ok := value.(*store.DatabaseMessage)
					if !ok {
						return true
					}

					if _, ok := instanceMap[database.InstanceID]; !ok {
						slog.Debug("Instance not found",
							slog.String("instance", database.InstanceID))
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
							}
						}()
						slog.Debug("Sync database schema", slog.String("instance", database.InstanceID), slog.String("database", database.DatabaseName))
						if err := s.SyncDatabaseSchema(ctx, database); err != nil {
							if errors.Is(err, errInstanceConnectionsExhausted) {
								// The per-instance limiter is saturated; keep the
								// database queued and retry on a later tick.
								s.databaseSyncMap.Store(database.String(), database)
								return
							}
							slog.Warn("Failed to sync database schema",
								slog.String("instance", database.InstanceID),
								slog.String("databaseName", database.DatabaseName),
								log.WithError(err))
						}
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
		if database.Deleted {
			continue
		}
		instance, ok := instancesMap[database.InstanceID]
		if !ok {
			continue
		}
		// The database inherits the sync interval from the instance.
		if !shouldSyncNow(getOrDefaultSyncInterval(instance), getOrDefaultLastSyncTime(database.Metadata.LastSyncTime), now) {
			continue
		}

		s.databaseSyncMap.Store(database.String(), database)
	}
}

func (s *Syncer) SyncAllDatabases(ctx context.Context, instance *store.InstanceMessage) {
	find := &store.FindDatabaseMessage{}
	if instance != nil {
		find.InstanceID = &instance.ResourceID
	}
	databases, err := s.store.ListDatabases(ctx, find)
	if err != nil {
		slog.Debug("Failed to find databases to sync",
			slog.String("error", err.Error()))
		return
	}

	for _, database := range databases {
		// Skip deleted databases.
		if database.Deleted {
			continue
		}
		s.databaseSyncMap.Store(database.String(), database)
	}
}

func (s *Syncer) SyncDatabaseAsync(database *store.DatabaseMessage) {
	if database == nil || database.Deleted {
		return
	}
	s.databaseSyncMap.Store(database.String(), database)
}

func (s *Syncer) SyncDatabasesAsync(databases []*store.DatabaseMessage) {
	for _, database := range databases {
		s.SyncDatabaseAsync(database)
	}
}

func (s *Syncer) QueueLineageAnalysis(metaGUID string, metaType storepb.MetaType) {
	if s == nil || s.lineageAnalyzer == nil || metaGUID == "" {
		return
	}
	s.lineageAnalyzer.QueueAnalysis(metaGUID, metaType)
}

// errInstanceConnectionsExhausted signals that the per-instance connection
// limiter is saturated; the caller should retry the sync later.
var errInstanceConnectionsExhausted = errors.New("instance connection limit reached")

// acquireInstanceConnection reserves one of the instance's outstanding
// connection slots and returns the release func. Every path that opens a driver
// goes through it, so instance-level and API-triggered syncs are throttled by
// the same limit as the periodic checker.
func (s *Syncer) acquireInstanceConnection(instance *store.InstanceMessage) (func(), error) {
	maximumConnections := int(instance.Metadata.GetMaximumConnections())
	if maximumConnections <= 0 {
		maximumConnections = common.DefaultInstanceMaximumConnections
	}
	if s.stateCfg.InstanceOutstandingConnections.Increment(instance.ResourceID, maximumConnections) {
		return nil, errors.Wrapf(errInstanceConnectionsExhausted, "instance %q already has %d outstanding connections", instance.ResourceID, maximumConnections)
	}
	return func() { s.stateCfg.InstanceOutstandingConnections.Decrement(instance.ResourceID) }, nil
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
// filterSyncedDatabases applies the instance's sync_databases allowlist. An
// empty allowlist means every database in the snapshot is synced.
func filterSyncedDatabases(databases []*storepb.DatabaseSchemaMetadata, syncDatabases []string) []*storepb.DatabaseSchemaMetadata {
	if len(syncDatabases) == 0 {
		return databases
	}
	filtered := make([]*storepb.DatabaseSchemaMetadata, 0, len(databases))
	for _, database := range databases {
		if slices.Contains(syncDatabases, database.Name) {
			filtered = append(filtered, database)
		}
	}
	return filtered
}

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
	filteredDatabaseMetadatas := filterSyncedDatabases(instanceMeta.Databases, instance.Metadata.GetSyncDatabases())

	for _, databaseMetadata := range filteredDatabaseMetadatas {
		idx := slices.IndexFunc(databases, func(db *store.DatabaseMessage) bool { return db.DatabaseName == databaseMetadata.Name })

		if idx < 0 {
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
	}

	// Databases that vanished from the instance snapshot are soft-deleted. The
	// snapshot is privilege-filtered on some engines (MySQL's information_schema
	// only lists what the connecting user may see) and an incomplete
	// instanceMeta.Databases would stop their sync, so log what disappears.
	var missingDatabases []string
	for _, database := range databases {
		if slices.IndexFunc(filteredDatabaseMetadatas, func(db *storepb.DatabaseSchemaMetadata) bool { return db.Name == database.DatabaseName }) < 0 {
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

	// Report only the databases the sync_databases filter selected: returning
	// the whole snapshot told the caller it had synced databases that were
	// deliberately skipped.
	return updatedInstance, filteredDatabaseMetadatas, newDatabases, nil
}

// SyncDatabaseSchema will sync the schema for a database.
func (s *Syncer) SyncDatabaseSchema(ctx context.Context, database *store.DatabaseMessage) error {
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
		return errors.Wrapf(err, "failed to sync database schema for database %q", database.DatabaseName)
	}

	tx, err := s.store.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

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

	err = bmc.Run(ctx, s.store, tx)
	if err != nil {
		return errors.Wrapf(err, "failed to batch store metadata for database %q", database.DatabaseName)
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

	// LastSyncTime is recorded only after the metadata transaction committed.
	// Writing it first would mark the database as synced even when the commit
	// failed, skipping it for a whole sync interval.
	if _, err := s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{
		InstanceID:   database.InstanceID,
		DatabaseName: database.DatabaseName,
		Deleted:      proto.Bool(false),
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

type batchMetaCreate struct {
	exist    []*store.MetaRegistryResource
	guidList []*store.CreateMetaRegistryResourceMessage
	updates  []*store.CreateMetaRegistryResourceMessage // populated after Run()
	deletes  []*store.MetaRegistryResource              // populated after Run()
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

func (b *batchMetaCreate) Run(ctx context.Context, s *store.Store, tx *sql.Tx) error {
	updates, deletes, err := b.diff()
	if err != nil {
		return errors.Wrap(err, "batchMetaCreateRunDiff")
	}
	observedAt := time.Now().UTC()

	if len(deletes) > 0 {
		if err := s.BatchDeleteMetaRegistryAt(ctx, tx, deletes, observedAt); err != nil {
			return errors.Wrap(err, "BatchDeleteMetaRegistryResourceByID")
		}
	}
	if len(updates) > 0 {
		_, err := s.BatchCreateMetaRegistryResourceAt(ctx, tx, updates, observedAt)
		if err != nil {
			return errors.Wrap(err, "BatchCreateMetaRegistryResource")
		}
	}
	b.updates = updates
	b.deletes = deletes
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
		metadataBytes, _, err := store.CalcStoreMetaHash(item.Metadata)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to calculate metadata hash for guid %q", item.GUID)
		}

		normalized := normalizeMetadataForHash(item.Metadata)
		hash, err := store.CalcMetaHash(normalized)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to calculate normalized metadata hash for guid %q", item.GUID)
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

// normalizeMetadataForHash returns a clone of the given metadata with volatile
// statistics fields zeroed out, so the hash is stable across syncs when only
// statistics (row counts, data sizes, etc.) change rather than the actual schema.
func normalizeMetadataForHash(meta *storepb.StoredMetadata) *storepb.StoredMetadata {
	cloned, ok := proto.Clone(meta).(*storepb.StoredMetadata)
	if !ok {
		return meta
	}

	switch {
	case cloned.GetTableMetadata() != nil:
		tm := cloned.GetTableMetadata()
		tm.RowCount = 0
		tm.DataSize = 0
		tm.IndexSize = 0
		tm.DataFree = 0
	case cloned.GetSequenceMetadata() != nil:
		sm := cloned.GetSequenceMetadata()
		sm.LastValue = ""
	default:
	}

	return cloned
}

func convertMetadataToGUID(prefix string, objectType storepb.MetaType, data *storepb.StoredMetadata) (string, error) {
	switch objectType {
	case storepb.MetaType_DATABASE:
		return buildGUID(prefix, data.GetDatabaseSchemaMetadata().Name), nil
	case storepb.MetaType_SCHEMA:
		return buildGUID(prefix, data.GetSchemaMetadata().Name), nil
	case storepb.MetaType_TABLE:
		return buildGUID(prefix, data.GetTableMetadata().Name), nil
	case storepb.MetaType_VIEW:
		return buildGUID(prefix, data.GetViewMetadata().Name), nil
	case storepb.MetaType_EXTERNAL_TABLE:
		return buildGUID(prefix, data.GetExternalTableMetadata().Name), nil
	case storepb.MetaType_FUNCTION:
		return buildGUID(prefix, data.GetFunctionMetadata().Name), nil
	case storepb.MetaType_PROCEDURE:
		return buildGUID(prefix, data.GetProcedureMetadata().Name), nil
	case storepb.MetaType_MATERIALIZED_VIEW:
		return buildGUID(prefix, data.GetMaterializedViewMetadata().Name), nil
	case storepb.MetaType_SEQUENCE:
		return buildGUID(prefix, data.GetSequenceMetadata().Name), nil
	default:
		return "", errors.Errorf("unsupported meta type %v", objectType)
	}
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
