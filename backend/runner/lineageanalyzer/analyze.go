package lineageanalyzer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// drainAndAnalyze analyzes one bounded batch of queued objects and leaves the rest queued.
// Event-driven work is selected first and the scan backlog fills only what is left of the
// budget, so a full-registry backfill delays a just-saved statement by at most one batch.
func (a *Analyzer) drainAndAnalyze(ctx context.Context) {
	now := time.Now()
	batch := a.takeAnalyzeBatch(&a.analyzeMap, now, maxAnalyzeBatch)
	if remaining := maxAnalyzeBatch - len(batch); remaining > 0 {
		batch = append(batch, a.takeAnalyzeBatch(&a.scanMap, now, remaining)...)
	}
	if len(batch) == 0 {
		return
	}

	wp := pool.New().WithMaxGoroutines(MaxGoroutines)
	for _, item := range batch {
		it := item
		wp.Go(func() {
			// A panic in one object must not take the process down or stop the
			// other objects in this batch.
			defer func() {
				if r := recover(); r != nil {
					slog.Error("Lineage analysis panicked",
						slog.String("guid", it.key.MetaGUID),
						slog.String("type", it.key.MetaType.String()),
						slog.Any("panic", r))
					a.metrics.ObserveLineageAnalysis(it.key.MetaType, storepb.Engine_ENGINE_UNSPECIFIED, metrics.AnalysisPanic)
					a.retryAfterFailure(it)
				}
			}()
			a.metrics.IncLineageAnalysisInFlight()
			defer a.metrics.DecLineageAnalysisInFlight()
			objCtx, cancel := context.WithTimeout(ctx, analyzeObjectTimeout)
			defer cancel()
			if err := a.analyzeObject(objCtx, it.key.MetaGUID, it.key.MetaType); err != nil {
				if errors.Is(err, errAnalysisStale) {
					// The text changed under the analysis. Queue the new text now
					// instead of waiting for the hourly scan; the write was dropped,
					// so nothing stale was persisted.
					a.metrics.ObserveLineageAnalysis(it.key.MetaType, storepb.Engine_ENGINE_UNSPECIFIED, metrics.AnalysisStale)
					a.QueueAnalysis(it.key.MetaGUID, it.key.MetaType)
					return
				}
				if analysisTimedOut(objCtx, err) {
					// Nothing was learned about the SQL: retrying is right and recording a
					// failure is not, because that would clear lineage this attempt never
					// inspected.
					slog.Warn("Lineage analysis timed out; it will be retried",
						slog.String("guid", it.key.MetaGUID),
						slog.String("type", it.key.MetaType.String()),
						log.WithError(err))
					// The interrupted branch inside analyzeObject already recorded
					// its own outcome; every other deadline hit is a timeout here.
					if drainRecordsTimeout(err) {
						a.metrics.ObserveLineageAnalysis(it.key.MetaType, storepb.Engine_ENGINE_UNSPECIFIED, metrics.AnalysisTimeout)
					}
					a.retryAfterFailure(it)
					return
				}
				slog.Error("Lineage analysis failed",
					slog.String("guid", it.key.MetaGUID),
					slog.String("type", it.key.MetaType.String()),
					log.WithError(err))
				a.retryAfterFailure(it)
				return
			}
			// Remove the entry this attempt served. A request that re-queued the
			// object while the analysis ran stored a newer generation, so its
			// entry stays for the next tick.
			consumeQueuedAnalysis(it)
			a.retryMap.Delete(it.key)
		})
	}
	wp.Wait()
}

// analysisTimedOut reports whether an analysis was cut short by its deadline or by the runner
// stopping, rather than by the statement itself. Either way the SQL was not read, so the
// object is retried and its stored lineage is left alone; only a real parse failure clears it.
func analysisTimedOut(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil
}

// analyzeObject runs lineage analysis for a single lineage-analyzable object.
func (a *Analyzer) analyzeObject(ctx context.Context, metaGUID string, metaType storepb.MetaType) error {
	// Extract context from GUID: instanceID;database;schema;name
	parts := common.SplitMetaGUID(metaGUID)
	if len(parts) != 4 {
		return a.finishFailedAnalysis(ctx, metaType, storepb.Engine_ENGINE_UNSPECIFIED, storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("invalid GUID format %q", metaGUID)))
	}
	instanceID, database, schema, name := parts[0], parts[1], parts[2], parts[3]

	// Look up instance to get engine type.
	instance, err := a.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return a.finishFailedAnalysis(ctx, metaType, storepb.Engine_ENGINE_UNSPECIFIED, storeError(ctx, a.store, metaGUID, metaType, err, "failed to get instance"))
	}
	if instance == nil {
		return a.finishFailedAnalysis(ctx, metaType, storepb.Engine_ENGINE_UNSPECIFIED, storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("instance %q not found", instanceID)))
	}
	engine := instance.Metadata.GetEngine()

	// Load the stored metadata. The object type is known, so it is passed with the GUID and
	// the lookup can be answered from the GUID+type cache.
	res, err := a.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &metaGUID, ObjectType: &metaType})
	if err != nil {
		return a.finishFailedAnalysis(ctx, metaType, engine, storeError(ctx, a.store, metaGUID, metaType, err, "failed to get meta registry"))
	}
	if res == nil {
		return a.finishFailedAnalysis(ctx, metaType, engine, storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("meta registry entry not found for GUID %q", metaGUID)))
	}

	// Get SQL definition and build the full CREATE VIEW statement.
	definition, wrappedSQL, err := buildSQL(name, engine, metaType, res)
	if err != nil {
		return a.finishFailedAnalysis(ctx, metaType, engine, storeError(ctx, a.store, metaGUID, metaType, err, "failed to build SQL"))
	}
	if definition == "" {
		if emptyDefinitionClearsLineage(metaType) {
			// A statement's text was cleared: the object no longer has the SQL the
			// stored edges describe. Clear them and record why, in the same
			// transaction that marks the new hash analyzed.
			slog.Debug("Lineage analyzer clearing the edges of an object with no definition",
				slog.String("guid", metaGUID), slog.String("type", metaType.String()))
			return a.finishAnalysisWrite(ctx, metaType, engine, metrics.AnalysisDefinitionCleared,
				markAnalysisFailed(ctx, a.store, metaGUID, metaType, res.MetaHash, nil))
		}
		// A view cannot exist without a defining query, so an empty stored
		// definition means this snapshot could not read the body (a privilege-limited
		// information_schema read, a driver partial snapshot) rather than that the SQL
		// was removed. Keep the last correct edges and record why; the next sync that
		// reads the definition changes the hash and re-analyzes.
		slog.Warn("Lineage analyzer kept the edges of an object whose definition was unavailable",
			slog.String("guid", metaGUID), slog.String("type", metaType.String()))
		return a.finishAnalysisWrite(ctx, metaType, engine, metrics.AnalysisDefinitionUnavailable,
			markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash,
				"lineage analysis skipped: the definition was unavailable in this sync"))
	}

	// Run lineage analysis with context so unqualified names resolve correctly.
	ac := catalog.AnalysisContext{InstanceID: instanceID, Database: database, Schema: schema}
	analysisCtx := catalog.WithAnalysisContext(ctx, ac)
	analysisStarted := time.Now()
	relations, err := a.lineage.Analyze(analysisCtx, engine, wrappedSQL)
	a.metrics.ObserveLineageAnalyzeDuration(engine, metaType, time.Since(analysisStarted))

	// What an analysis could not represent does not condemn the rest of it: the
	// analyzers return the edges they did find together with the gap, so the
	// lineage is stored and the gap is recorded beside it.
	var unsupported *lineage.UnsupportedStatementError
	switch {
	case err == nil:
	case errors.As(err, &unsupported):
	case errors.Is(err, lineage.ErrorEngineNotSupported):
		// No analyzer is registered for this engine. Record the skip together
		// with the current hash so the hourly scan stops re-queueing the
		// object, and keep the reason visible in error_message.
		slog.Warn("Lineage analysis skipped: the engine has no lineage analyzer",
			slog.String("guid", metaGUID), slog.String("engine", engine.String()))
		return a.finishAnalysisWrite(ctx, metaType, engine, metrics.AnalysisEngineUnsupported,
			markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash,
				fmt.Sprintf("engine %s has no lineage analyzer; analysis skipped", engine)))
	case ctx.Err() != nil:
		// The deadline hit while the statement was being read. What it says is unknown,
		// so it must not be recorded as a deterministic parse failure; the drain treats
		// the interrupted analysis as transient and retries it.
		slog.Warn("Lineage analysis interrupted",
			slog.String("guid", metaGUID), slog.String("engine", engine.String()), log.WithError(err))
		// The drain owns timeout/stale/panic; this branch owns interrupted, so it is
		// recorded here and the error carries the sentinel that tells the drain not
		// to record a timeout for the same attempt.
		a.metrics.ObserveLineageAnalysis(metaType, engine, metrics.AnalysisInterrupted)
		return fmt.Errorf("%w: %w", errAnalysisInterrupted, ctx.Err())
	default:
		// A parse error is deterministic — the text does not change between
		// retries — so the object is recorded as analyzed at the current hash
		// instead of being re-queued forever by the hourly scan. The analyzers do
		// no other failing I/O: a catalog lookup that fails degrades one relation's
		// columns to a wildcard and is reported as a gap on the version row, which
		// is what keeps a degraded analysis distinguishable from a complete one.
		slog.Warn("Lineage analysis failed on an unsupported statement",
			slog.String("guid", metaGUID), slog.String("engine", engine.String()), log.WithError(err))
		return a.finishAnalysisWrite(ctx, metaType, engine, metrics.AnalysisFailed,
			markAnalysisFailed(ctx, a.store, metaGUID, metaType, res.MetaHash, err))
	}

	// What a partial analysis could not model is stored on the version row
	// alongside the lineage written below.
	partialMessage := ""
	if unsupported != nil {
		partialMessage = unsupported.Error()
	}

	// Convert relations to ColumnLineage rows and collect GUIDs whose meta types
	// should be resolved from the registry.
	guidTypeMap := map[string]storepb.MetaType{metaGUID: metaType}
	guidLookupSet := make(map[string]struct{})
	var lineages []*store.ColumnLineage
	for _, rel := range relations {
		// A statement only names an object down to database/schema level, so the
		// rest comes from the analysis context. AnalyzeSQL resolves its scopes
		// through the same helper.
		sourceID := ac.Complete(rel.Source.Table)

		if rel.Transformation == nil {
			rel.Transformation = make([]model.Transformation, 0)
		}
		srcGUID := sourceID.GUID()

		if metaType == storepb.MetaType_MANUAL_SQL || metaType == storepb.MetaType_APPLICATION_SQL {
			targetID := ac.Complete(rel.Target.Table)
			guidLookupSet[srcGUID] = struct{}{}
			lineages = append(lineages, &store.ColumnLineage{
				MetaGUID:       metaGUID,
				MetaType:       metaType,
				SourceGUID:     srcGUID,
				SourceColumn:   rel.Source.Name,
				TargetGUID:     metaGUID,
				TargetColumn:   rel.Target.Name,
				TargetType:     metaType,
				RelationType:   rel.RelationType,
				Transformation: rel.Transformation,
			})

			if !rel.IsTemp {
				targetGUID := targetID.GUID()
				guidLookupSet[targetGUID] = struct{}{}
				lineages = append(lineages, &store.ColumnLineage{
					MetaGUID:       metaGUID,
					MetaType:       metaType,
					SourceGUID:     metaGUID,
					SourceColumn:   rel.Target.Name,
					SourceType:     metaType,
					TargetGUID:     targetGUID,
					TargetColumn:   rel.Target.Name,
					RelationType:   rel.RelationType,
					Transformation: rel.Transformation,
				})
			}
			continue
		}

		if rel.IsTemp {
			continue
		}

		// A VIEW's or materialized view's relation target is the view itself. Pin it
		// to the analyzed object's GUID instead of re-deriving it from the wrapped
		// name: the wrapper quotes the name for the parser, and a spelling that
		// canonicalizes elsewhere would write an edge into a GUID no registry row has.
		guidLookupSet[srcGUID] = struct{}{}
		lineages = append(lineages, &store.ColumnLineage{
			MetaGUID:       metaGUID,
			MetaType:       metaType,
			SourceGUID:     srcGUID,
			SourceColumn:   rel.Source.Name,
			TargetGUID:     metaGUID,
			TargetColumn:   rel.Target.Name,
			TargetType:     metaType,
			RelationType:   rel.RelationType,
			Transformation: rel.Transformation,
		})
	}

	// A reported statement is classified here and nowhere else: ingestion stores the
	// statement without deciding, and this is the one place the read/write rule lives.
	// The kind is held back until the edges commit (see below), because it is derived
	// state about them.
	var applicationKind store.ApplicationSQLKind
	if metaType == storepb.MetaType_APPLICATION_SQL {
		kind, targetEdges := a.classifyApplicationSQL(ctx, ac, engine, definition, relations, metaGUID)
		applicationKind = kind
		lineages = append(lineages, targetEdges...)
		// The edge names its target by GUID alone, so the lookup below is what
		// gives the target its type. Without it the relation reaches the API with
		// an unspecified target type, and every client that routes by type — the
		// metadata browser's lineage table, for one — loses the link.
		for _, edge := range targetEdges {
			guidLookupSet[edge.TargetGUID] = struct{}{}
		}
	}

	// The relation GUIDs are resolved with one batch query rather than one lookup each: a
	// large join can name hundreds of sources, and each per-GUID lookup would be a serial
	// round trip. A GUID the registry does not know is simply absent from the result.
	if guids := typeLookupGUIDs(guidLookupSet, metaGUID); len(guids) > 0 {
		metas, err := a.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{GUIDs: &guids})
		if err != nil {
			slog.Warn("Lineage analyzer failed to look up source meta types", log.WithError(err))
		} else {
			for _, meta := range metas {
				guidTypeMap[meta.GUID] = meta.ObjectType
			}
		}
	}
	for _, l := range lineages {
		if t, ok := guidTypeMap[l.SourceGUID]; ok {
			l.SourceType = t
		}
		if t, ok := guidTypeMap[l.TargetGUID]; ok {
			l.TargetType = t
		}
	}

	if len(lineages) == 0 {
		slog.Debug("Lineage analyzer found no lineage relations (after filtering temp targets)", slog.String("guid", metaGUID))
	}

	// Persist the result only if the object still holds the text this analysis
	// read. The edges, the version row and any gap message commit together, so a
	// rolled-back write cannot leave the object marked as analyzed, and a result
	// computed for a deleted object is dropped instead of re-creating its edges.
	var partialMessagePtr *string
	if partialMessage != "" {
		partialMessagePtr = &partialMessage
	}
	if err := a.store.ReplaceColumnLineageIfCurrent(ctx, metaGUID, metaType, res.MetaHash, lineage.AnalyzerVersion, lineages, partialMessagePtr); err != nil {
		return a.finishFailedAnalysis(ctx, metaType, engine,
			analysisWriteError(ctx, a.store, metaGUID, metaType, err, "failed to replace column lineage"))
	}
	a.metrics.ObserveLineageAnalysis(metaType, engine, metrics.AnalysisStored)
	columnEdges, tableEdges := analysisEdgeCounts(lineages)
	a.metrics.AddLineageEdges(metaType, metrics.EdgeKindColumn, columnEdges)
	a.metrics.AddLineageEdges(metaType, metrics.EdgeKindTable, tableEdges)
	// The classification is a derived cache for the list page's filter, so it is
	// written only after the edges commit: a kind that claims a classification the
	// graph does not have misleads the filter, while a missing kind is merely stale
	// until the next analysis. A failure to write the cache must not discard edges
	// that were just committed, so it is logged rather than returned.
	if metaType == storepb.MetaType_APPLICATION_SQL {
		if err := a.store.SetApplicationSQLKind(ctx, metaGUID, applicationKind); err != nil {
			slog.Warn("Lineage analyzer failed to record the statement kind",
				slog.String("guid", metaGUID), log.WithError(err))
		}
	}
	return nil
}
