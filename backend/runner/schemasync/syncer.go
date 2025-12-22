// Package schemasync is a runner that synchronize database schemas.
package schemasync

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strings"
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
	"github.com/Ranxy/metaxisdata/backend/config"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
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
func NewSyncer(stores *store.Store, dbFactory *dbfactory.DBFactory, profile *config.Profile, stateCfg *state.State) *Syncer {
	return &Syncer{
		store:     stores,
		dbFactory: dbFactory,
		profile:   profile,
		stateCfg:  stateCfg,
	}
}

// Syncer is the schema syncer.
type Syncer struct {
	sync.Mutex

	store           *store.Store
	dbFactory       *dbfactory.DBFactory
	profile         *config.Profile
	stateCfg        *state.State
	databaseSyncMap sync.Map // map[string]*store.DatabaseMessage
}

// Run will run the schema syncer once.
func (s *Syncer) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	sp := pool.New()
	sp.Go(func() {
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
				instances, err := s.store.ListInstancesV2(ctx, &store.FindInstanceMessage{})
				if err != nil {
					if err != nil {
						slog.Error("Failed to list instance", log.WithError(err))
						return
					}
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

					instance, ok := instanceMap[database.InstanceID]
					if !ok {
						slog.Debug("Instance not found",
							slog.String("instance", database.InstanceID),
							log.WithError(err))
						return true
					}
					maximumConnections := int(instance.Metadata.GetMaximumConnections())
					if maximumConnections <= 0 {
						maximumConnections = common.DefaultInstanceMaximumConnections
					}
					if s.stateCfg.InstanceOutstandingConnections.Increment(instance.ResourceID, maximumConnections) {
						return true
					}

					s.databaseSyncMap.Delete(key)
					dbwp.Go(func() {
						defer func() {
							s.stateCfg.InstanceOutstandingConnections.Decrement(instance.ResourceID)
						}()
						slog.Debug("Sync database schema", slog.String("instance", database.InstanceID), slog.String("database", database.DatabaseName))
						if err := s.SyncDatabaseSchema(ctx, database); err != nil {
							slog.Debug("Failed to sync database schema",
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
	instances, err := s.store.ListInstancesV2(ctx, &store.FindInstanceMessage{})
	if err != nil {
		slog.Error("Failed to retrieve instances", log.WithError(err))
		return
	}
	now := time.Now()
	for _, instance := range instances {
		interval := getOrDefaultSyncInterval(instance)
		if interval == defaultSyncInterval {
			continue
		}
		lastSyncTime := getOrDefaultLastSyncTime(instance.Metadata.LastSyncTime)
		// lastSyncTime + syncInterval > now
		// Next round not started yet.
		nextSyncTime := lastSyncTime.Add(interval)
		if now.Before(nextSyncTime) {
			continue
		}

		wp.Go(func() {
			slog.Debug("Sync instance schema", slog.String("instance", instance.ResourceID))
			if _, _, _, err := s.SyncInstance(ctx, instance); err != nil {
				slog.Debug("Failed to sync instance",
					slog.String("instance", instance.ResourceID),
					slog.String("error", err.Error()))
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
		interval := getOrDefaultSyncInterval(instance)
		if interval == defaultSyncInterval {
			continue
		}
		lastSyncTime := getOrDefaultLastSyncTime(database.Metadata.LastSyncTime)
		// lastSyncTime + syncInterval > now
		// Next round not started yet.
		nextSyncTime := lastSyncTime.Add(interval)
		if now.Before(nextSyncTime) {
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

// GetInstanceMeta gets the instance metadata.
func (s *Syncer) GetInstanceMeta(ctx context.Context, instance *store.InstanceMessage) (*db.InstanceMetadata, error) {
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
	metadata.Roles = instanceMeta.Metadata.Roles

	updateInstance := &store.UpdateInstanceMessage{
		ResourceID: instance.ResourceID,
		Metadata:   metadata,
	}
	if instanceMeta.Version != instance.Metadata.GetVersion() {
		metadata.Version = instanceMeta.Version
	}
	updatedInstance, err := s.store.UpdateInstanceV2(ctx, updateInstance)
	if err != nil {
		return nil, nil, nil, err
	}

	databases, err := s.store.ListDatabases(ctx, &store.FindDatabaseMessage{InstanceID: &instance.ResourceID})
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, "failed to sync database for instance: %s. Failed to find database list", instance.ResourceID)
	}
	var newDatabases []*store.DatabaseMessage
	var filteredDatabaseMetadatas []*storepb.DatabaseSchemaMetadata

	for _, databaseMetadata := range instanceMeta.Databases {
		if len(instance.Metadata.GetSyncDatabases()) > 0 && !slices.Contains(instance.Metadata.GetSyncDatabases(), databaseMetadata.Name) {
			continue
		}
		filteredDatabaseMetadatas = append(filteredDatabaseMetadatas, databaseMetadata)
		idx := slices.IndexFunc(databases, func(db *store.DatabaseMessage) bool { return db.DatabaseName == databaseMetadata.Name })

		if idx < 0 {
			// Create the database in the default project.
			newDatabase, err := s.store.CreateDatabaseDefault(ctx, &store.DatabaseMessage{
				InstanceID:   instance.ResourceID,
				DatabaseName: databaseMetadata.Name,
				ProjectID:    common.DefaultProjectID,
			})
			if err != nil {
				return nil, nil, nil, errors.Wrapf(err, "failed to create instance %q database %q in sync runner", instance.ResourceID, databaseMetadata.Name)
			}
			if newDatabase != nil {
				newDatabases = append(newDatabases, newDatabase)
			}
		}
	}

	for _, database := range databases {
		idx := slices.IndexFunc(filteredDatabaseMetadatas, func(db *storepb.DatabaseSchemaMetadata) bool { return db.Name == database.DatabaseName })
		if idx < 0 {
			d := true
			if _, err := s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{
				InstanceID:   instance.ResourceID,
				DatabaseName: database.DatabaseName,
				Deleted:      &d,
			}); err != nil {
				return nil, nil, nil, errors.Errorf("failed to update database %q for instance %q", database.DatabaseName, instance.ResourceID)
			}
		}
	}

	return updatedInstance, instanceMeta.Databases, newDatabases, nil
}

// SyncDatabaseSchema will sync the schema for a database.
func (s *Syncer) SyncDatabaseSchema(ctx context.Context, database *store.DatabaseMessage) (retErr error) {
	// TODO get schema and get previous schema from store, compare and update
	instance, err := s.store.GetInstanceV2(ctx, &store.FindInstanceMessage{ResourceID: &database.InstanceID})
	if err != nil {
		return errors.Wrapf(err, "failed to get instance %q", database.InstanceID)
	}
	if instance == nil {
		return errors.Errorf("instance %q not found", database.InstanceID)
	}
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

	databaseGuid := buildGuid(database.InstanceID, database.DatabaseName)

	storedMetadatas, err := s.store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GuidPrefix: &databaseGuid})
	if err != nil {
		return errors.Wrapf(err, "failed to list existing meta registry for database %q", database.DatabaseName)
	}

	bmc := &batchMetaCreate{
		exist:    storedMetadatas,
		guidList: make([]*store.CreateMetaRegistryResourceMessage, 0),
	}

	for _, schema := range databaseMetadata.Schemas {
		schemaGuiPrefix := buildGuid(databaseGuid, schema.Name)
		for _, table := range schema.Tables {
			meta := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_TableMetadata{TableMetadata: table}}

			err = bmc.StoreMetaResourceV2(ctx, schemaGuiPrefix, storepb.MetaType_TABLE, meta)
			if err != nil {
				return errors.Wrapf(err, "failed to store table metadata for table %q in database %q", table.Name, database.DatabaseName)
			}
		}

		schema.Tables = nil

		for _, view := range schema.Views {
			meta := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_ViewMetadata{ViewMetadata: view}}

			err = bmc.StoreMetaResourceV2(ctx, schemaGuiPrefix, storepb.MetaType_VIEW, meta)
			if err != nil {
				return errors.Wrapf(err, "failed to store view metadata for view %q in database %q", view.Name, database.DatabaseName)
			}
		}
		schema.Views = nil

		for _, materializedView := range schema.MaterializedViews {
			err = bmc.StoreMetaResourceV2(ctx, buildGuid(databaseGuid, schema.Name), storepb.MetaType_MATERIALIZED_VIEW, &storepb.StoredMetadata{Type: &storepb.StoredMetadata_MaterializedViewMetadata{MaterializedViewMetadata: materializedView}})
			if err != nil {
				return errors.Wrapf(err, "failed to store materialized view metadata for materialized view %q in database %q", materializedView.Name, database.DatabaseName)
			}
		}
		schema.MaterializedViews = nil

		for _, sequence := range schema.Sequences {
			err = bmc.StoreMetaResourceV2(ctx, schemaGuiPrefix, storepb.MetaType_SEQUENCE, &storepb.StoredMetadata{Type: &storepb.StoredMetadata_SequenceMetadata{SequenceMetadata: sequence}})
			if err != nil {
				return errors.Wrapf(err, "failed to store sequence metadata for sequence %q in database %q", sequence.Name, database.DatabaseName)
			}
		}
		schema.Sequences = nil

		{
			meta := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_SchemaMetadata{SchemaMetadata: schema}}
			err = bmc.StoreMetaResourceV2(ctx, databaseGuid, storepb.MetaType_SCHEMA, meta)
			if err != nil {
				return errors.Wrapf(err, "failed to store schema metadata for schema %q in database %q", schema.Name, database.DatabaseName)
			}
		}

	}
	databaseMetadata.Schemas = nil
	{
		meta := &storepb.StoredMetadata{Type: &storepb.StoredMetadata_DatabaseSchemaMetadata{DatabaseSchemaMetadata: databaseMetadata}}
		err = bmc.StoreMetaResourceV2(ctx, database.InstanceID, storepb.MetaType_DATABASE, meta)
		if err != nil {
			return errors.Wrapf(err, "failed to store database metadata for  database %q", database.DatabaseName)
		}
	}

	err = bmc.Run(ctx, s.store, tx)
	if err != nil {
		return errors.Wrapf(err, "failed to batch store metadata for database %q", database.DatabaseName)
	}

	// Build metadata updates
	metadataUpdates := []func(*storepb.DatabaseMetadata){
		func(md *storepb.DatabaseMetadata) {
			md.LastSyncTime = timestamppb.Now()
		},
	}

	if _, err := s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{
		InstanceID:      database.InstanceID,
		DatabaseName:    database.DatabaseName,
		Deleted:         proto.Bool(false),
		MetadataUpdates: metadataUpdates,
	}); err != nil {
		return errors.Wrapf(err, "failed to update database %q for instance %q", database.DatabaseName, database.InstanceID)
	}

	tx.Commit()

	return nil
}

type batchMetaCreate struct {
	exist    []*store.MetaRegistryResource
	guidList []*store.CreateMetaRegistryResourceMessage
}

func (b *batchMetaCreate) StoreMetaResourceV2(ctx context.Context, prefixName string, objectType storepb.MetaType, data *storepb.StoredMetadata) error {
	guid, son, err := convertMetadataToGuid(prefixName, objectType, data)
	if err != nil {
		return err
	}

	for _, sguid := range son {
		b.add(sguid, storepb.MetaType_COLUMN, nil)
	}

	b.add(guid, objectType, data)
	return nil
}

func (b *batchMetaCreate) add(guid string, mt storepb.MetaType, data *storepb.StoredMetadata) {
	registry := &store.CreateMetaRegistryResourceMessage{
		MetaRegistryResource: store.MetaRegistryResource{
			Guid:       guid,
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

	if len(deletes) > 0 {
		fmt.Println("DELETE S ", len(deletes))
		if err := s.BatchDeleteMetaRegistry(ctx, tx, deletes); err != nil {
			return errors.Wrap(err, "BatchDeleteMetaRegistryResourceByID")
		}
	}
	if len(updates) > 0 {
		fmt.Println("LEN UPDATE ", len(updates))
		_, err := s.BatchCreateMetaRegistryResource(ctx, tx, updates)
		if err != nil {
			return errors.Wrap(err, "BatchCreateMetaRegistryResource")
		}
	}
	return nil
}

func (b *batchMetaCreate) diff() (updates []*store.CreateMetaRegistryResourceMessage, deletes []*store.MetaRegistryResource, err error) {

	existMap := make(map[store.MetaGuidKey]*store.MetaRegistryResource)
	for _, item := range b.exist {
		existMap[item.GuidKey()] = item
	}

	for _, item := range b.guidList {
		metadataBytes, hash, err := store.CalcStoreMetaHash(item.Metadata)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to calculate metadata hash for guid %q", item.Guid)
		}

		item.MetaHash = hash
		item.MetadataBytes = metadataBytes

		existing, ok := existMap[item.GuidKey()]
		if !ok {
			// new item
			updates = append(updates, item)
			continue
		}

		if existing.MetaHash == nil || item.MetaHash == nil || !bytes.Equal(existing.MetaHash, item.MetaHash) {
			fmt.Println("UPDATE >>> ", item.Guid)
			updates = append(updates, item)
		}
		delete(existMap, item.GuidKey())
	}
	for _, item := range existMap {
		deletes = append(deletes, item)
	}

	return updates, deletes, nil
}

func convertMetadataToGuid(prefix string, objectType storepb.MetaType, data *storepb.StoredMetadata) (target string, son []string, err error) {

	switch objectType {
	case storepb.MetaType_DATABASE:
		return buildGuid(prefix, data.GetDatabaseSchemaMetadata().Name), nil, nil
	case storepb.MetaType_SCHEMA:
		return buildGuid(prefix, data.GetSchemaMetadata().Name), nil, nil
	case storepb.MetaType_TABLE:
		tguid := buildGuid(prefix, data.GetTableMetadata().Name)
		return tguid, getTableColumnsGuid(tguid, data.GetTableMetadata().Columns), nil
	case storepb.MetaType_VIEW:
		return buildGuid(prefix, data.GetViewMetadata().Name), nil, nil
	case storepb.MetaType_EXTERNAL_TABLE:
		return buildGuid(prefix, data.GetExternalTableMetadata().Name), nil, nil
	case storepb.MetaType_FUNCTION:
		return buildGuid(prefix, data.GetFunctionMetadata().Name), nil, nil
	case storepb.MetaType_PROCEDURE:
		return buildGuid(prefix, data.GetProcedureMetadata().Name), nil, nil
	case storepb.MetaType_STREAM:
		return buildGuid(prefix, data.GetStreamMetadata().Name), nil, nil
	case storepb.MetaType_MATERIALIZED_VIEW:
		return buildGuid(prefix, data.GetMaterializedViewMetadata().Name), nil, nil
	case storepb.MetaType_SEQUENCE:
		return buildGuid(prefix, data.GetSequenceMetadata().Name), nil, nil
	case storepb.MetaType_PACKAGE:
		return buildGuid(prefix, data.GetPackageMetadata().Name), nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported meta type %v", objectType)
	}
}

func buildGuid(list ...string) string {
	return strings.Join(list, common.MetaGuidSplit)
}

func getTableColumnsGuid(prefix string, cols []*storepb.ColumnMetadata) []string {
	var guids []string
	for _, col := range cols {
		guids = append(guids, buildGuid(prefix, col.Name))
	}
	return guids
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
