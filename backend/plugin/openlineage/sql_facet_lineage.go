package openlineage

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// sqlFacet is the subset of the OpenLineage SQL job facet this package reads.
type sqlFacet struct {
	Query string `json:"query"`
}

// sqlFacetQuery returns the SQL text an event's producer attached, if any. A
// producer that states its own column lineage without SQL (DataX, a Python task
// with explicit facets) has no such facet and keeps the facet path.
func sqlFacetQuery(event *RunEvent) (string, bool) {
	raw, ok := event.Job.Facets["sql"]
	if !ok {
		return "", false
	}
	var facet sqlFacet
	if err := json.Unmarshal(raw, &facet); err != nil {
		slog.Warn("OpenLineage event carries a SQL facet that does not decode",
			"jobNamespace", event.Job.Namespace,
			"jobName", event.Job.Name,
			"runId", event.Run.RunID,
			"error", err,
		)
		return "", false
	}
	query := strings.TrimSpace(facet.Query)
	return query, query != ""
}

// anchorDataset returns the dataset whose instance and namespace the SQL was
// written for: the first output, because a write names the connection the
// statement ran on, and otherwise the first input.
func anchorDataset(event *RunEvent) (Dataset, bool) {
	if len(event.Outputs) > 0 {
		return event.Outputs[0], true
	}
	if len(event.Inputs) > 0 {
		return event.Inputs[0], true
	}
	return Dataset{}, false
}

// analyzeEventSQL runs the event's SQL through the statement-scoped analyzer and
// returns the column relations it found, mapped onto the run.
//
// It returns false when the SQL cannot be analyzed at all - no registered
// analyzer for the engine, an unparseable script, or an anchor dataset that
// belongs to no instance - which the caller answers with table-level edges
// rather than with the producer's column claims.
func (p *Processor) analyzeEventSQL(ctx context.Context, event *RunEvent, meta lineageMeta) ([]*store.ColumnLineage, bool) {
	query, ok := sqlFacetQuery(event)
	if !ok {
		return nil, false
	}
	anchor, ok := anchorDataset(event)
	if !ok {
		return nil, false
	}

	analysisContext, engine, ok := p.analysisContextFor(ctx, anchor)
	if !ok {
		return nil, false
	}

	relations, err := p.lineage.Analyze(catalog.WithAnalysisContext(ctx, analysisContext), engine, query)
	warnAnalysisGaps(event, err)
	switch {
	case err == nil:
	case errors.As(err, new(*lineage.UnsupportedStatementError)):
		// The analyzers answer a partial analysis with the relations they did find
		// beside the gap, so the edges are still used.
	case errors.Is(err, lineage.ErrorEngineNotSupported):
		slog.Warn("OpenLineage event carries SQL for an engine with no lineage analyzer",
			"jobName", event.Job.Name, "runId", event.Run.RunID, "engine", engine.String())
		return nil, false
	default:
		slog.Warn("failed to analyze the SQL an OpenLineage event carries",
			"jobNamespace", event.Job.Namespace,
			"jobName", event.Job.Name,
			"runId", event.Run.RunID,
			"engine", engine.String(),
			"error", err,
		)
		return nil, false
	}

	lineages := mapAnalyzedRelations(meta, analysisContext, relations)
	if len(lineages) == 0 {
		slog.Debug("the SQL of an OpenLineage event produced no column relations",
			"jobName", event.Job.Name, "runId", event.Run.RunID)
		return nil, false
	}
	return lineages, true
}

// analysisContextFor resolves the engine and the default database/schema the
// SQL's unqualified names belong to. Both come from the anchor dataset: it is the
// one dataset the producer derived from the connection the statement ran on.
func (p *Processor) analysisContextFor(ctx context.Context, anchor Dataset) (catalog.AnalysisContext, storepb.Engine, bool) {
	resolved, err := p.resolver.ResolveDatasetPreview(ctx, anchor.Namespace, anchor.Name)
	if err != nil || !resolved.Internal {
		return catalog.AnalysisContext{}, storepb.Engine_ENGINE_UNSPECIFIED, false
	}
	instanceID, ok := common.GetInstanceFromGUID(resolved.GUID)
	if !ok {
		return catalog.AnalysisContext{}, storepb.Engine_ENGINE_UNSPECIFIED, false
	}
	instance, err := p.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil || instance == nil || instance.Metadata == nil {
		return catalog.AnalysisContext{}, storepb.Engine_ENGINE_UNSPECIFIED, false
	}

	engine := instance.Metadata.GetEngine()
	database, schema, _ := splitDatasetName(engine, anchor.Name)
	return catalog.AnalysisContext{InstanceID: instanceID, Database: database, Schema: schema}, engine, true
}

// mapAnalyzedRelations converts the analyzer's relations into the edges the run
// stores. The target of a relation the analyzer marked temporary is the
// statement's own result, not a stored object, so it is skipped: those edges
// describe a query, and a query has no registry object to hang them on.
func mapAnalyzedRelations(meta lineageMeta, analysisContext catalog.AnalysisContext, relations []model.ColumnRelation) []*store.ColumnLineage {
	lineages := make([]*store.ColumnLineage, 0, len(relations))
	for _, relation := range relations {
		if relation.IsTemp {
			continue
		}
		sourceID := analysisContext.Complete(relation.Source.Table)
		targetID := analysisContext.Complete(relation.Target.Table)
		if isSyntheticRelation(sourceID) || isSyntheticRelation(targetID) {
			continue
		}

		transformations := relation.Transformation
		if transformations == nil {
			transformations = []model.Transformation{}
		}
		lineages = append(lineages, &store.ColumnLineage{
			MetaGUID:       meta.GUID,
			MetaType:       meta.Type,
			SourceGUID:     sourceID.GUID(),
			SourceColumn:   relation.Source.Name,
			TargetGUID:     targetID.GUID(),
			TargetColumn:   relation.Target.Name,
			RelationType:   relation.RelationType,
			Transformation: transformations,
		})
	}
	return lineages
}

// isSyntheticRelation reports whether an identifier names one of the analyzer's
// placeholders rather than a stored object: the statement's own result, or the
// file a loading statement reads.
func isSyntheticRelation(id model.ObjectIdentifier) bool {
	return id.Name == model.ResultTableName || id.Name == model.FileSourceName
}

// warnAnalysisGaps reports what a partial analysis could not represent, so a
// coverage gap in the analyzer stays visible beside the edges it did produce.
func warnAnalysisGaps(event *RunEvent, err error) {
	var unsupported *lineage.UnsupportedStatementError
	if !errors.As(err, &unsupported) {
		return
	}
	slog.Warn("OpenLineage SQL analysis could not represent part of a statement",
		"jobNamespace", event.Job.Namespace,
		"jobName", event.Job.Name,
		"runId", event.Run.RunID,
		"gaps", unsupported.Error(),
	)
}
