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
	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
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
	profile    *config.Profile
	analyzeMap sync.Map // map[analyzeKey]struct{}
}

// NewAnalyzer creates a new lineage Analyzer.
func NewAnalyzer(stores *store.Store, profile *config.Profile) *Analyzer {
	return &Analyzer{
		store:   stores,
		profile: profile,
	}
}

// QueueAnalysis enqueues an object for lineage analysis.
func (a *Analyzer) QueueAnalysis(metaGUID string, metaType storepb.MetaType) {
	a.analyzeMap.Store(analyzeKey{MetaGUID: metaGUID, MetaType: metaType}, struct{}{})
}

// Run starts the analyzer. It blocks until ctx is cancelled, then signals wg.Done().
func (a *Analyzer) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	sp := pool.New()

	// Goroutine 1: periodic full scan.
	sp.Go(func() {
		slog.Debug(fmt.Sprintf("Lineage analyzer started and will run every %v", lineageAnalysisInterval))
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

// queueAll scans all VIEW and MATERIALIZED_VIEW objects and queues those whose
// metahash has changed since the last analysis.
func (a *Analyzer) queueAll(ctx context.Context) {
	viewType := storepb.MetaType_VIEW
	mvType := storepb.MetaType_MATERIALIZED_VIEW

	for _, objType := range []storepb.MetaType{viewType, mvType} {
		t := objType
		list, err := a.store.ListMetaRegistryResource(ctx, &store.FindMetaRegistryResourceMessage{
			ObjectType: &t,
		})
		if err != nil {
			slog.Error("Lineage analyzer failed to list meta registry", slog.String("type", t.String()), log.WithError(err))
			continue
		}
		for _, res := range list {
			ver, err := a.store.GetColumnLineageVersion(ctx, res.GUID, res.ObjectType)
			if err != nil {
				slog.Error("Lineage analyzer failed to get version", slog.String("guid", res.GUID), log.WithError(err))
				continue
			}
			// Queue if never analyzed or hash changed.
			if ver == nil || !bytes.Equal(ver.MetaHash, res.MetaHash) {
				a.QueueAnalysis(res.GUID, res.ObjectType)
			}
		}
	}
}

// drainAndAnalyze drains the analyzeMap and runs analysis for each object.
func (a *Analyzer) drainAndAnalyze(ctx context.Context) {
	var keys []analyzeKey
	a.analyzeMap.Range(func(k, _ any) bool {
		key, ok := k.(analyzeKey)
		if ok {
			keys = append(keys, key)
			a.analyzeMap.Delete(k)
		}
		return true
	})
	if len(keys) == 0 {
		return
	}

	wp := pool.New().WithMaxGoroutines(MaxGoroutines)
	for _, key := range keys {
		k := key
		wp.Go(func() {
			if err := a.analyzeObject(ctx, k.MetaGUID, k.MetaType); err != nil {
				slog.Error("Lineage analysis failed",
					slog.String("guid", k.MetaGUID),
					slog.String("type", k.MetaType.String()),
					log.WithError(err))
			}
		})
	}
	wp.Wait()
}

// analyzeObject runs lineage analysis for a single VIEW or MATERIALIZED_VIEW.
func (a *Analyzer) analyzeObject(ctx context.Context, metaGUID string, metaType storepb.MetaType) error {
	// Extract context from GUID: instanceID;database;schema;name
	parts := strings.SplitN(metaGUID, common.MetaGUIDSplit, 4)
	if len(parts) != 4 {
		return storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("invalid GUID format %q", metaGUID))
	}
	instanceID, database, schema, name := parts[0], parts[1], parts[2], parts[3]

	// Look up instance to get engine type.
	instance, err := a.store.GetInstanceV2(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
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
	definition, wrappedSQL, err := buildSQL(name, metaType, res)
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
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to analyze lineage")
	}

	// Convert relations to ColumnLineage rows, skipping temp targets.
	var lineages []*store.ColumnLineage
	for _, rel := range relations {
		if rel.IsTemp {
			continue
		}
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
		lineages = append(lineages, &store.ColumnLineage{
			MetaGUID:       metaGUID,
			MetaType:       metaType,
			SourceGUID:     sourceID.GUID(),
			SourceColumn:   rel.Source.Name,
			TargetColumn:   rel.Target.Name,
			RelationType:   rel.RelationType,
			Transformation: rel.Transformation,
		})
	}

	// Persist results.
	if err := a.store.BatchReplaceColumnLineage(ctx, metaGUID, metaType, lineages); err != nil {
		return storeError(ctx, a.store, metaGUID, metaType, err, "failed to replace column lineage")
	}

	return markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash, "")
}

// buildSQL extracts the view definition and wraps it as a CREATE VIEW statement.
func buildSQL(name string, metaType storepb.MetaType, res *store.MetaRegistryResource) (definition string, wrapped string, err error) {
	switch metaType {
	case storepb.MetaType_VIEW:
		definition = res.Metadata.GetViewMetadata().GetDefinition()
	case storepb.MetaType_MATERIALIZED_VIEW:
		definition = res.Metadata.GetMaterializedViewMetadata().GetDefinition()
	default:
		return "", "", errors.Errorf("unsupported meta type %v", metaType)
	}
	if definition == "" {
		return "", "", nil
	}
	return definition, fmt.Sprintf("CREATE VIEW `%s` AS %s", name, definition), nil
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

// markAnalyzed records a successful analysis with the current metahash.
func markAnalyzed(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, metaHash []byte, _ string) error {
	return s.UpsertColumnLineageVersion(ctx, &store.ColumnLineageVersion{
		MetaGUID: metaGUID,
		MetaType: metaType,
		MetaHash: metaHash,
	})
}
