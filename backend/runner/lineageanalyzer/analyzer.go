// Package lineageanalyzer is a runner that analyzes SQL definitions to produce column-level lineage.
package lineageanalyzer

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	lineageAnalysisInterval = 1 * time.Hour
	analyzeCheckerInterval  = 10 * time.Second
	// MaxGoroutines is the max number of concurrent analysis jobs.
	MaxGoroutines = 10
)

// analyzeKey uniquely identifies an object to analyze.
type analyzeKey struct {
	MetaGUID string
	MetaType storepb.MetaType
}

// Analyzer is the column-level lineage analysis runner.
type Analyzer struct {
	store      *store.Store
	analyzeMap sync.Map // map[analyzeKey]struct{}
	// retryMap tracks how many times a failed analysis was retried and when it
	// is due again, so a transient failure does not wait for the hourly scan.
	retryMap sync.Map // map[analyzeKey]analysisRetry
}

// analysisRetry is the backoff state of a failed analysis.
type analysisRetry struct {
	attempts int
	nextAt   time.Time
}

// maxAnalysisRetries bounds the retries before the object waits for the next
// full scan.
const maxAnalysisRetries = 3

// analysisRetryBackoff returns the delay before the next attempt.
func analysisRetryBackoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
}

// NewAnalyzer creates a new lineage Analyzer.
func NewAnalyzer(stores *store.Store) *Analyzer {
	return &Analyzer{
		store: stores,
	}
}

// QueueAnalysis enqueues an object for lineage analysis.
func (a *Analyzer) QueueAnalysis(metaGUID string, metaType storepb.MetaType) {
	key := analyzeKey{MetaGUID: metaGUID, MetaType: metaType}
	// A fresh queue request supersedes any pending backoff.
	a.retryMap.Delete(key)
	a.analyzeMap.Store(key, struct{}{})
}

// scheduleRetry re-queues a failed analysis with a bounded backoff. After the
// last attempt the object is left to the next full scan.
func (a *Analyzer) scheduleRetry(key analyzeKey) {
	attempts := 1
	if v, ok := a.retryMap.Load(key); ok {
		if entry, ok := v.(analysisRetry); ok {
			attempts = entry.attempts + 1
		}
	}
	if attempts > maxAnalysisRetries {
		// Drop the key entirely: leaving it queued would restart the backoff on
		// the very next tick, which is the hourly retry loop this replaces.
		a.retryMap.Delete(key)
		a.analyzeMap.Delete(key)
		slog.Error("Lineage analysis gave up after repeated failures",
			slog.String("guid", key.MetaGUID),
			slog.String("type", key.MetaType.String()))
		return
	}
	a.retryMap.Store(key, analysisRetry{attempts: attempts, nextAt: time.Now().Add(analysisRetryBackoff(attempts))})
	a.analyzeMap.Store(key, struct{}{})
}

// Run starts the analyzer. It blocks until ctx is cancelled, then signals wg.Done().
func (a *Analyzer) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	sp := pool.New()

	// Goroutine 1: periodic full scan.
	sp.Go(func() {
		slog.Debug(fmt.Sprintf("Lineage analyzer started and will run every %v", lineageAnalysisInterval))
		a.queueAll(ctx)
		ticker := time.NewTicker(lineageAnalysisInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.queueAll(ctx)
			case <-ctx.Done():
				return
			}
		}
	})

	// Goroutine 2: queue consumer.
	sp.Go(func() {
		ticker := time.NewTicker(analyzeCheckerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.drainAndAnalyze(ctx)
			case <-ctx.Done():
				return
			}
		}
	})

	sp.Wait()
}

// queueAll scans all lineage-analyzable objects and queues those whose metahash
// has changed since the last analysis. Each object type costs two queries
// (objects and their analysis state) instead of one query per object.
func (a *Analyzer) queueAll(ctx context.Context) {
	viewType := storepb.MetaType_VIEW
	mvType := storepb.MetaType_MATERIALIZED_VIEW
	manualSQLType := storepb.MetaType_MANUAL_SQL

	for _, objType := range []storepb.MetaType{viewType, mvType, manualSQLType} {
		t := objType
		list, err := a.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{
			ObjectType: &t,
		})
		if err != nil {
			slog.Error("Lineage analyzer failed to list meta registry", slog.String("type", t.String()), log.WithError(err))
			continue
		}
		versions, err := a.store.ListColumnLineageVersions(ctx, t)
		if err != nil {
			slog.Error("Lineage analyzer failed to list analysis versions", slog.String("type", t.String()), log.WithError(err))
			continue
		}
		for _, res := range list {
			// Queue if never analyzed or hash changed.
			ver := versions[res.GUID]
			if ver == nil || !bytes.Equal(ver.MetaHash, res.MetaHash) {
				a.QueueAnalysis(res.GUID, res.ObjectType)
			}
		}
	}
}

// drainAndAnalyze drains the analyzeMap and runs analysis for each object.
func (a *Analyzer) drainAndAnalyze(ctx context.Context) {
	now := time.Now()
	var keys []analyzeKey
	a.analyzeMap.Range(func(k, _ any) bool {
		key, ok := k.(analyzeKey)
		if !ok {
			return true
		}
		// A failed object stays queued until its backoff elapses.
		if v, ok := a.retryMap.Load(key); ok {
			if entry, ok := v.(analysisRetry); ok && now.Before(entry.nextAt) {
				return true
			}
		}
		keys = append(keys, key)
		a.analyzeMap.Delete(k)
		return true
	})
	if len(keys) == 0 {
		return
	}

	wp := pool.New().WithMaxGoroutines(MaxGoroutines)
	for _, key := range keys {
		k := key
		wp.Go(func() {
			// A panic in one object must not take the process down or stop the
			// other objects in this batch.
			defer func() {
				if r := recover(); r != nil {
					slog.Error("Lineage analysis panicked",
						slog.String("guid", k.MetaGUID),
						slog.String("type", k.MetaType.String()),
						slog.Any("panic", r))
					a.scheduleRetry(k)
				}
			}()
			if err := a.analyzeObject(ctx, k.MetaGUID, k.MetaType); err != nil {
				slog.Error("Lineage analysis failed",
					slog.String("guid", k.MetaGUID),
					slog.String("type", k.MetaType.String()),
					log.WithError(err))
				a.scheduleRetry(k)
				return
			}
			a.retryMap.Delete(k)
		})
	}
	wp.Wait()
}

// analyzeObject runs lineage analysis for a single lineage-analyzable object.
func (a *Analyzer) analyzeObject(ctx context.Context, metaGUID string, metaType storepb.MetaType) error {
	// Extract context from GUID: instanceID;database;schema;name
	parts := common.SplitMetaGUID(metaGUID)
	if len(parts) != 4 {
		return storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("invalid GUID format %q", metaGUID))
	}
	instanceID, database, schema, name := parts[0], parts[1], parts[2], parts[3]

	// Look up instance to get engine type.
	instance, err := a.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to get instance")
	}
	if instance == nil {
		return storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("instance %q not found", instanceID))
	}
	engine := instance.Metadata.GetEngine()

	// Load the stored metadata.
	res, err := a.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &metaGUID})
	if err != nil {
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to get meta registry")
	}
	if res == nil {
		return storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("meta registry entry not found for GUID %q", metaGUID))
	}

	// Get SQL definition and build the full CREATE VIEW statement.
	definition, wrappedSQL, err := buildSQL(name, engine, metaType, res)
	if err != nil {
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to build SQL")
	}
	if definition == "" {
		slog.Debug("Lineage analyzer skipping object with empty definition", slog.String("guid", metaGUID))
		return markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash, "")
	}

	// Run lineage analysis with context so unqualified names resolve correctly.
	ac := catalog.AnalysisContext{InstanceID: instanceID, Database: database, Schema: schema}
	analysisCtx := catalog.WithAnalysisContext(ctx, ac)
	relations, err := lineage.GetAnalyzeRelation(analysisCtx, engine, wrappedSQL)
	if err != nil {
		if errors.Is(err, lineage.ErrorEngineNotSupported) {
			// No analyzer is registered for this engine. Record the skip together
			// with the current hash so the hourly scan stops re-queueing the
			// object, and keep the reason visible in error_message.
			slog.Warn("Lineage analysis skipped: the engine has no lineage analyzer",
				slog.String("guid", metaGUID), slog.String("engine", engine.String()))
			return markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash,
				fmt.Sprintf("engine %s has no lineage analyzer; analysis skipped", engine))
		}
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to analyze lineage")
	}

	// Convert relations to ColumnLineage rows and collect GUIDs whose meta types
	// should be resolved from the registry.
	guidTypeMap := map[string]storepb.MetaType{metaGUID: metaType}
	guidLookupSet := make(map[string]struct{})
	var lineages []*store.ColumnLineage
	for _, rel := range relations {
		// Fill missing source GUID parts from analysis context.
		sourceID := rel.Source.Table
		if sourceID.InstanceID == "" {
			sourceID.InstanceID = instanceID
		}
		if sourceID.Database == "" {
			sourceID.Database = database
		}
		if sourceID.Schema == "" {
			sourceID.Schema = schema
		}

		targetID := rel.Target.Table
		if targetID.InstanceID == "" {
			targetID.InstanceID = instanceID
		}
		if targetID.Database == "" {
			targetID.Database = database
		}
		if targetID.Schema == "" {
			targetID.Schema = schema
		}

		if rel.Transformation == nil {
			rel.Transformation = make([]model.Transformation, 0)
		}
		srcGUID := sourceID.GUID()

		if metaType == storepb.MetaType_MANUAL_SQL {
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

		targetGUID := targetID.GUID()
		guidLookupSet[srcGUID] = struct{}{}
		lineages = append(lineages, &store.ColumnLineage{
			MetaGUID:       metaGUID,
			MetaType:       metaType,
			SourceGUID:     srcGUID,
			SourceColumn:   rel.Source.Name,
			TargetGUID:     targetGUID,
			TargetColumn:   rel.Target.Name,
			TargetType:     metaType,
			RelationType:   rel.RelationType,
			Transformation: rel.Transformation,
		})
	}

	for guid := range guidLookupSet {
		if guid == metaGUID {
			continue
		}
		g := guid
		srcMeta, err := a.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &g})
		if err != nil {
			slog.Warn("Lineage analyzer failed to look up source meta type", slog.String("guid", g), log.WithError(err))
			continue
		}
		if srcMeta != nil {
			guidTypeMap[g] = srcMeta.ObjectType
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

	// Persist results.
	if err := a.store.BatchReplaceColumnLineage(ctx, metaGUID, metaType, lineages); err != nil {
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to replace column lineage")
	}

	return markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash, "")
}

// buildSQL extracts the object definition and wraps it as needed for lineage parsing.
func buildSQL(name string, engine storepb.Engine, metaType storepb.MetaType, res *store.MetaRegistryResource) (definition string, wrapped string, err error) {
	switch metaType {
	case storepb.MetaType_VIEW:
		definition = res.Metadata.GetViewMetadata().GetDefinition()
	case storepb.MetaType_MATERIALIZED_VIEW:
		definition = res.Metadata.GetMaterializedViewMetadata().GetDefinition()
	case storepb.MetaType_MANUAL_SQL:
		definition = res.Metadata.GetManualSqlMetadata().GetSqlText()
	default:
		return "", "", errors.Errorf("unsupported meta type %v", metaType)
	}
	if definition == "" {
		return "", "", nil
	}
	if metaType == storepb.MetaType_MANUAL_SQL {
		return definition, definition, nil
	}
	// A stored definition that is already a complete statement is used as-is.
	// StarRocks keeps the whole SHOW CREATE MATERIALIZED VIEW output in
	// MaterializedViewMetadata.Definition, so wrapping it in
	// "CREATE MATERIALIZED VIEW <name> AS ..." would produce
	// "... AS CREATE MATERIALIZED VIEW ...", which no parser accepts.
	if isCompleteStatement(definition) {
		return definition, definition, nil
	}

	keyword := "CREATE VIEW"
	if metaType == storepb.MetaType_MATERIALIZED_VIEW {
		keyword = "CREATE MATERIALIZED VIEW"
	}

	var identifier string
	switch engine {
	case storepb.Engine_MYSQL, storepb.Engine_TIDB:
		identifier = "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case storepb.Engine_POSTGRES:
		identifier = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	default:
		identifier = name
	}

	return definition, fmt.Sprintf("%s %s AS %s", keyword, identifier, definition), nil
}

// isCompleteStatement reports whether a stored definition is already a whole
// CREATE statement rather than a bare query body. A view definition is a query
// and can never start with CREATE, so the check is safe for every engine.
func isCompleteStatement(definition string) bool {
	return strings.HasPrefix(strings.ToUpper(strings.TrimSpace(definition)), "CREATE ")
}

// storeError records the error in column_lineage_version and returns a wrapped error.
func storeError(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, cause error, msg string) error {
	var full string
	if cause != nil {
		full = fmt.Sprintf("%s: %v", msg, cause)
	} else {
		full = msg
	}
	_ = s.UpsertColumnLineageVersion(ctx, &store.ColumnLineageVersion{
		MetaGUID:     metaGUID,
		MetaType:     metaType,
		ErrorMessage: &full,
	})
	if cause != nil {
		return errors.Wrap(cause, msg)
	}
	return errors.New(msg)
}

// markAnalyzed records a successful analysis (or a deliberate skip) with the
// current metahash. A non-empty errorMessage documents why analysis was skipped
// while still recording the hash, so the object is not retried until its
// metadata changes.
func markAnalyzed(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, metaHash []byte, errorMessage string) error {
	v := &store.ColumnLineageVersion{
		MetaGUID: metaGUID,
		MetaType: metaType,
		MetaHash: metaHash,
	}
	if errorMessage != "" {
		v.ErrorMessage = &errorMessage
	}
	return s.UpsertColumnLineageVersion(ctx, v)
}
