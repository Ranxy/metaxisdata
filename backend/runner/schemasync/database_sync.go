package schemasync

import (
	"context"
	"log/slog"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// ErrDatabaseHidden reports that a database the caller asked to sync has been
// soft-deleted because the target no longer has it. The checker must not retry or
// escalate it — the row is history now — and the API maps it to NotFound.
var ErrDatabaseHidden = errors.New("the target no longer has this database; it has been hidden")

// SyncDatabaseSchema will sync the schema for a database.
//
// Two callers can ask for the same database at once (the periodic checker and
// the API both call this function), so the sync is serialized per database: a
// caller that arrives while one is running waits for it and reuses its result.
func (s *Syncer) SyncDatabaseSchema(ctx context.Context, database *store.DatabaseMessage) (err error) {
	if database == nil {
		return nil
	}
	if database.Deleted {
		// The row is history: only the instance enumeration revives it, and a
		// single-database read is not that answer.
		return errors.Wrapf(ErrDatabaseHidden, "database %q", database.DatabaseName)
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
	// The outcome and duration belong to the owner: a waiter reuses the owner's
	// result and is not a second sync. The clock starts after the gate, so the
	// time a caller spent waiting for another sync is not reported as sync time.
	// The engine is unknown until the instance row is read; a failure before that
	// point is still counted, with the unspecified engine label.
	engine := storepb.Engine_ENGINE_UNSPECIFIED
	started := time.Now()
	defer func() {
		if r := recover(); r != nil {
			// The waiters must be released, and a panic is not a success they may
			// reuse.
			s.databaseSyncGate.finish(key, call, errors.Errorf("database schema sync panicked: %v", r))
			s.metrics.ObserveDatabaseSync(engine, false, time.Since(started))
			s.metrics.CountSchemaSyncFailure(metrics.SyncStageDatabase, metrics.FailureReasonOther)
			panic(r)
		}
		s.databaseSyncGate.finish(key, call, err)
		s.metrics.ObserveDatabaseSync(engine, err == nil, time.Since(started))
		if err != nil {
			s.metrics.CountSchemaSyncFailure(metrics.SyncStageDatabase, schemaSyncFailureReason(err))
		}
	}()

	instance, err := s.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &database.InstanceID})
	if err != nil {
		return errors.Wrapf(err, "failed to get instance %q", database.InstanceID)
	}
	if instance == nil {
		return errors.Errorf("instance %q not found", database.InstanceID)
	}
	engine = instance.Metadata.GetEngine()
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
	driverStarted := time.Now()
	databaseMetadata, err := driver.SyncDBSchema(deadlineCtx)
	s.metrics.ObserveDriverCall(engine, metrics.DriverCallSyncDBSchema, time.Since(driverStarted))
	if err != nil {
		// The target no longer has this database. Every further sync of the row
		// would fail the same way, so mirror the deletion here instead of leaving
		// a database the target does not have visible until the next instance
		// enumeration notices. Only that enumeration can show the row again.
		if common.ErrorCode(err) == common.NotFound {
			if hideErr := s.markDatabaseDeleted(ctx, database, err); hideErr != nil {
				return errors.Wrapf(hideErr, "failed to hide database %q the target no longer has", database.DatabaseName)
			}
			return errors.Wrapf(ErrDatabaseHidden, "database %q", database.DatabaseName)
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

	// The statistics are read apart from the digest because the digest deliberately omits the
	// metadata, and a stats-only refresh must write the metadata column without becoming a
	// metadata update: that would write history and re-queue lineage analysis for a row count.
	// The refresh has its own, slower cadence for the same reason a row count does not need to
	// be rewritten on every sync.
	statisticsKey := database.String()
	refreshStatistics := s.statisticsRefreshDue(statisticsKey)
	statisticsReadFailed := false
	var storedStatistics map[store.MetaGUIDKey]store.MetaRegistryStatistics
	if refreshStatistics {
		stats, statsErr := s.store.ListMetaRegistryStatistics(ctx, databaseGUID)
		if statsErr != nil {
			// Statistics are a derived estimate for the UI, so an unreadable or
			// malformed stats value must not stop this database's schema from
			// converging. The attempt is recorded so a persistent failure logs once
			// per refresh window instead of on every sync.
			slog.Warn("Failed to read table statistics; leaving them unchanged",
				slog.String("instance", database.InstanceID),
				slog.String("database", database.DatabaseName),
				log.WithError(statsErr))
			statisticsReadFailed = true
		} else {
			storedStatistics = stats
		}
	}

	bmc := &batchMetaCreate{
		exist:            storedMetadatas,
		storedStatistics: storedStatistics,
		guidList:         make([]*store.CreateMetaRegistryResourceMessage, 0),
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
	//
	// The DDL refresh is decoupled from the metadata diff: a full refresh of the
	// unchanged objects is periodic (objectDefinitionFullRefreshInterval) and a
	// database above the fetch limit rotates through its objects instead of
	// re-reading them all, so the per-sync SHOW CREATE cost is proportional to
	// what changed rather than to the database's size.
	var definitions *objectDefinitionBatch
	var definitionPlan *definitionFetchPlan
	definitionDBKey := database.String()
	if reader, ok := driver.(db.ObjectDefinitionReader); ok {
		full, cursor := s.definitionRefreshDue(definitionDBKey)
		plan := bmc.planDefinitionFetch(full, cursor)
		definitionPlan = &plan
		definitions, err = prepareObjectDefinitions(deadlineCtx, s.store, reader, plan.items)
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

	// Clean lineage rows for every deleted object, not only views: a dropped
	// table or column leaves edges whose endpoint GUID no longer exists, and a
	// dropped view also stops being the producer of its edges. One statement
	// covers the whole delete set: a destructive sync removes a database's
	// objects at once, and the per-object probes were mostly no-ops that only
	// kept the write transaction open.
	deletedKeys := make([]store.MetaGUIDKey, 0, len(bmc.deletes))
	for _, item := range bmc.deletes {
		deletedKeys = append(deletedKeys, item.GUIDKey())
	}
	if err := s.store.DeleteColumnLineageByMetaGUIDs(ctx, tx, deletedKeys); err != nil {
		return errors.Wrapf(err, "failed to delete column lineage for database %q", database.DatabaseName)
	}

	err = tx.Commit()
	if err != nil {
		return errors.Wrapf(err, "failed to commit transaction for database %q", database.DatabaseName)
	}

	// Only committed rows are schema changes: work a rollback erased must not be
	// counted. The diff merges created and updated rows, so the counter reports
	// them together.
	for metaType, count := range countMetaResourcesByType(bmc.updates) {
		s.metrics.AddMetaResourcesWritten(metaType, count)
	}

	// The deletion log is the only trace of a destructive sync, so it describes
	// what was committed: logging before the commit reported deletions that a
	// rollback erased.
	logSchemaSyncDeletion(common.FormatDatabase(database.InstanceID, database.DatabaseName), bmc.deletes, s.metrics)

	// The statistics refresh is deliberately outside the metadata transaction: it is
	// a derived estimate for the UI, so a failed estimate write must not roll back
	// schema changes that are the source of truth. Its clock advances either way, so
	// a persistent failure does not retry on every sync.
	if len(bmc.statsUpdates) > 0 {
		if _, err := s.store.BatchUpdateMetaRegistryStatistics(ctx, bmc.statsUpdates); err != nil {
			slog.Warn("Failed to refresh table statistics",
				slog.String("instance", database.InstanceID),
				slog.String("database", database.DatabaseName),
				log.WithError(err))
		}
	}

	// Record the completed DDL plan only after the commit: a rolled-back sync must
	// not advance the refresh clock or the rotation, or it would skip objects it
	// never refreshed.
	if definitionPlan != nil {
		s.recordDefinitionRefresh(definitionDBKey, *definitionPlan)
	}
	if refreshStatistics || statisticsReadFailed {
		s.recordStatisticsRefresh(statisticsKey)
	}

	// The metadata cache is invalidated only after a successful commit, so a
	// rolled-back sync cannot leave stale or uncommitted entries behind. The
	// stats-only rows are cached too (TABLE is), so they are invalidated as well.
	invalidated := make([]*store.MetaRegistryResource, 0, len(bmc.updates)+len(bmc.deletes)+len(bmc.statsUpdates))
	for _, item := range bmc.updates {
		invalidated = append(invalidated, &item.MetaRegistryResource)
	}
	invalidated = append(invalidated, bmc.deletes...)
	for _, item := range bmc.statsUpdates {
		invalidated = append(invalidated, &item.MetaRegistryResource)
	}
	s.store.InvalidateMetaRegistryCache(invalidated)

	// Queue changed VIEWs and MATERIALIZED_VIEWs before touching the db row: if
	// the LastSyncTime update below fails, the next sync sees unchanged hashes
	// and would never queue them again.
	if s.lineageQueue != nil {
		for _, item := range bmc.updates {
			if item.ObjectType == storepb.MetaType_VIEW || item.ObjectType == storepb.MetaType_MATERIALIZED_VIEW {
				s.lineageQueue.QueueAnalysis(item.GUID, item.ObjectType)
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
	_, err = s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{
		InstanceID:   database.InstanceID,
		DatabaseName: database.DatabaseName,
		MetadataUpdates: []func(*storepb.DatabaseMetadata){
			func(md *storepb.DatabaseMetadata) {
				md.LastSyncTime = timestamppb.Now()
			},
		},
	})
	if err != nil {
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
	_, err := s.store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{
		InstanceID:   database.InstanceID,
		DatabaseName: database.DatabaseName,
		Deleted:      &deleted,
	})
	if err != nil {
		return err
	}
	slog.Warn("Hiding a database the target no longer has",
		slog.String("instance", database.InstanceID),
		slog.String("database", database.DatabaseName),
		log.WithError(cause))
	return nil
}
