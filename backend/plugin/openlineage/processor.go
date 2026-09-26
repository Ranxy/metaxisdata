package openlineage

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type lineageMeta struct {
	GUID string
	Type storepb.MetaType
}

// Processor handles incoming OpenLineage events and stores derived lineage.
type Processor struct {
	store    *store.Store
	resolver *Resolver
}

// NewProcessor creates a new event processor.
func NewProcessor(s *store.Store) *Processor {
	return &Processor{
		store:    s,
		resolver: NewResolver(s),
	}
}

// ProcessRunEvent processes a single OpenLineage RunEvent.
// Only COMPLETE events are processed since lineage is final at that point.
func (p *Processor) ProcessRunEvent(ctx context.Context, event *RunEvent, persistedRun *store.OpenLineageRunMessage) error {
	if event.EventType != "COMPLETE" {
		slog.Debug("skipping non-COMPLETE OpenLineage event", "eventType", event.EventType, "runId", event.Run.RunID)
		return nil
	}

	meta := buildLineageMeta(persistedRun)
	var lineages []*store.ColumnLineage

	for _, output := range event.Outputs {
		hasColumnLineage := output.Facets.ColumnLineage != nil && len(output.Facets.ColumnLineage.Fields) > 0
		hasDatasetLineage := output.Facets.ColumnLineage != nil && len(output.Facets.ColumnLineage.Dataset) > 0
		hasInputs := len(event.Inputs) > 0

		if hasColumnLineage || hasDatasetLineage {
			outputLineages, err := p.processOutputDataset(ctx, &output, meta)
			if err != nil {
				slog.Error("failed to process OpenLineage output dataset",
					"namespace", output.Namespace,
					"name", output.Name,
					"error", err,
				)
				return errors.Wrapf(err, "failed to process output dataset %s/%s", output.Namespace, output.Name)
			}
			lineages = append(lineages, outputLineages...)
		} else if hasInputs {
			outputLineages, inferred, err := p.processSchemaInferredLineage(ctx, event.Inputs, &output, meta)
			if err != nil {
				slog.Error("failed to process schema-inferred lineage",
					"namespace", output.Namespace,
					"name", output.Name,
					"error", err,
				)
				return errors.Wrapf(err, "failed to process schema-inferred lineage for %s/%s", output.Namespace, output.Name)
			}
			if inferred {
				lineages = append(lineages, outputLineages...)
				continue
			}

			// Table-level lineage: Airflow and other integrations may emit events
			// with input/output datasets but without column-level detail.
			outputLineages, err = p.processTableLevelLineage(ctx, event.Inputs, &output, meta)
			if err != nil {
				slog.Error("failed to process table-level lineage",
					"namespace", output.Namespace,
					"name", output.Name,
					"error", err,
				)
				return errors.Wrapf(err, "failed to process table-level lineage for %s/%s", output.Namespace, output.Name)
			}
			lineages = append(lineages, outputLineages...)
		}
	}

	if len(lineages) == 0 {
		warnMissingSQLLineage(event)
		return nil
	}

	kept, report, err := p.validateIngestedLineage(ctx, lineages)
	if err != nil {
		slog.Error("failed to validate ingested OpenLineage lineage",
			"jobNamespace", event.Job.Namespace,
			"jobName", event.Job.Name,
			"runId", event.Run.RunID,
			"error", err,
		)
		return errors.Wrap(err, "failed to validate ingested lineage")
	}
	report.log(event)

	slog.Info("storing OpenLineage lineage",
		"metaGUID", meta.GUID,
		"metaType", meta.Type,
		"jobNamespace", event.Job.Namespace,
		"jobName", event.Job.Name,
		"edges", len(kept),
		"degradedEdges", report.degradedEdges,
	)

	return p.store.BatchReplaceColumnLineage(ctx, meta.GUID, meta.Type, kept)
}

// warnMissingSQLLineage reports an event whose producer attached an SQL facet
// but no datasets at all. That is what a SQL extractor leaves behind when it
// cannot resolve a statement - unqualified table names resolved against the
// wrong default schema is the common cause - and the run record shows only
// "no lineage" without saying why.
func warnMissingSQLLineage(event *RunEvent) {
	if !missingSQLLineage(event) {
		return
	}
	slog.Warn("OpenLineage event carries a SQL facet but no datasets, so no lineage could be extracted",
		"jobNamespace", event.Job.Namespace,
		"jobName", event.Job.Name,
		"runId", event.Run.RunID,
		"hint", "check whether the extracted statement qualifies its tables, since the extractor resolves unqualified names against one default schema",
	)
}

// missingSQLLineage reports an event that asked for SQL lineage and produced no
// dataset to derive any from.
func missingSQLLineage(event *RunEvent) bool {
	if len(event.Inputs) > 0 || len(event.Outputs) > 0 {
		return false
	}
	_, ok := event.Job.Facets["sql"]
	return ok
}

func (p *Processor) processOutputDataset(ctx context.Context, output *Dataset, meta lineageMeta) ([]*store.ColumnLineage, error) {
	// Resolve the output dataset
	targetResolved, err := p.resolver.ResolveDataset(ctx, output.Namespace, output.Name)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve output dataset")
	}

	var lineages []*store.ColumnLineage

	// Process column-level lineage fields.
	for outputColumn, field := range output.Facets.ColumnLineage.Fields {
		for _, input := range field.InputFields {
			sourceResolved, err := p.resolver.ResolveDataset(ctx, input.Namespace, input.Name)
			if err != nil {
				slog.Warn("failed to resolve input dataset, skipping",
					"namespace", input.Namespace,
					"name", input.Name,
					"error", err,
				)
				continue
			}

			transformation := mapTransformations(input.Transformations)

			lineages = append(lineages, buildColumnLineage(meta, sourceResolved, targetResolved, input.Field, outputColumn, transformation))
		}
	}

	// Process dataset-level lineage references: a field that influences the whole
	// output (a filter, join, grouping or sort) rather than one column. The field
	// is the real source column, and the target column stays empty, because the
	// influence is on the rows the dataset produces, not on any single column.
	for _, dsRef := range output.Facets.ColumnLineage.Dataset {
		sourceResolved, err := p.resolver.ResolveDataset(ctx, dsRef.Namespace, dsRef.Name)
		if err != nil {
			slog.Warn("failed to resolve dataset-level reference, skipping",
				"namespace", dsRef.Namespace,
				"name", dsRef.Name,
				"error", err,
			)
			continue
		}

		lineages = append(lineages, datasetReferenceLineage(meta, sourceResolved, targetResolved, dsRef))
	}

	return lineages, nil
}

// datasetReferenceLineage builds the edge for a dataset-level column lineage
// reference. Such a reference names a field that influences the whole output — a
// filter, join, grouping or sort — so the field is the source column and the
// target column stays empty, because no single output column is derived from it.
func datasetReferenceLineage(meta lineageMeta, sourceResolved, targetResolved *ResolvedDataset, ref ColumnLineageDatasetReference) *store.ColumnLineage {
	return buildColumnLineage(
		meta, sourceResolved, targetResolved,
		ref.Field, "",
		mapTransformations(ref.Transformations),
	)
}

func (p *Processor) processSchemaInferredLineage(ctx context.Context, inputs []Dataset, output *Dataset, meta lineageMeta) ([]*store.ColumnLineage, bool, error) {
	columnPairs, input, inferred := inferSchemaColumnPairs(inputs, output)
	if !inferred {
		return nil, false, nil
	}

	targetResolved, err := p.resolver.ResolveDataset(ctx, output.Namespace, output.Name)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to resolve output dataset")
	}
	sourceResolved, err := p.resolver.ResolveDataset(ctx, input.Namespace, input.Name)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to resolve input dataset")
	}

	lineages := make([]*store.ColumnLineage, 0, len(columnPairs))
	for _, name := range columnPairs {
		lineages = append(lineages, buildColumnLineage(
			meta,
			sourceResolved,
			targetResolved,
			name,
			name,
			[]model.Transformation{},
		))
	}

	if len(lineages) == 0 {
		return nil, false, nil
	}

	return lineages, true, nil
}

func inferSchemaColumnPairs(inputs []Dataset, output *Dataset) ([]string, Dataset, bool) {
	if len(inputs) != 1 {
		return nil, Dataset{}, false
	}
	if output.Facets.Schema == nil || len(output.Facets.Schema.Fields) == 0 {
		return nil, Dataset{}, false
	}

	input := inputs[0]
	if input.Facets.Schema == nil || len(input.Facets.Schema.Fields) == 0 {
		return nil, Dataset{}, false
	}

	targetColumns := make(map[string]struct{}, len(output.Facets.Schema.Fields))
	for _, field := range output.Facets.Schema.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		targetColumns[name] = struct{}{}
	}

	columnPairs := make([]string, 0, len(input.Facets.Schema.Fields))
	for _, field := range input.Facets.Schema.Fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		if _, ok := targetColumns[name]; !ok {
			continue
		}
		columnPairs = append(columnPairs, name)
	}
	if len(columnPairs) == 0 {
		return nil, Dataset{}, false
	}

	return columnPairs, input, true
}

// processTableLevelLineage handles the case where Airflow and similar integrations
// emit COMPLETE events with input/output datasets but without column-level lineage.
func (p *Processor) processTableLevelLineage(ctx context.Context, inputs []Dataset, output *Dataset, meta lineageMeta) ([]*store.ColumnLineage, error) {
	targetResolved, err := p.resolver.ResolveDataset(ctx, output.Namespace, output.Name)
	if err != nil {
		return nil, errors.Wrap(err, "failed to resolve output dataset")
	}

	var lineages []*store.ColumnLineage
	for _, input := range inputs {
		sourceResolved, err := p.resolver.ResolveDataset(ctx, input.Namespace, input.Name)
		if err != nil {
			slog.Warn("failed to resolve input dataset for table-level lineage, skipping",
				"namespace", input.Namespace,
				"name", input.Name,
				"error", err,
			)
			continue
		}

		lineages = append(lineages, buildColumnLineage(meta, sourceResolved, targetResolved, "", "", []model.Transformation{}))
	}

	return lineages, nil
}

func buildLineageMeta(persistedRun *store.OpenLineageRunMessage) lineageMeta {
	return lineageMeta{
		GUID: persistedRun.GUID,
		Type: storepb.MetaType_OPENLINEAGE,
	}
}

// buildColumnLineage builds a stored column-lineage row. The relation type is
// derived from the transformations by the rule every producer shares, so an
// ingested join or aggregation is stored as a join or an aggregation rather than
// as an undifferentiated indirect edge.
func buildColumnLineage(
	meta lineageMeta,
	sourceResolved *ResolvedDataset,
	targetResolved *ResolvedDataset,
	sourceColumn string,
	targetColumn string,
	transformation []model.Transformation,
) *store.ColumnLineage {
	return &store.ColumnLineage{
		MetaGUID:       meta.GUID,
		MetaType:       meta.Type,
		SourceGUID:     sourceResolved.GUID,
		SourceColumn:   sourceColumn,
		SourceType:     sourceResolved.MetaType,
		TargetGUID:     targetResolved.GUID,
		TargetColumn:   targetColumn,
		TargetType:     targetResolved.MetaType,
		RelationType:   model.RelationTypeOf(transformation),
		Transformation: transformation,
	}
}

// mapTransformations converts OpenLineage transforms to our internal Transformation model.
func mapTransformations(transforms []OLTransform) []model.Transformation {
	if len(transforms) == 0 {
		return []model.Transformation{}
	}

	result := make([]model.Transformation, 0, len(transforms))
	for _, t := range transforms {
		if t.Type == "DIRECT" && t.Subtype == "IDENTITY" {
			// An identity facet says the value is the source value, which is what
			// a direct edge without a transformation already means. Keeping a
			// PROJECT here would store a direct relation that carries a
			// transformation, which no other producer does.
			continue
		}
		op := mapOperationType(t.Type, t.Subtype)

		desc := t.Description
		if desc == "" {
			desc = t.Type + "/" + t.Subtype
		}

		result = append(result, model.Transformation{
			Operation:  op,
			Expression: desc,
		})
	}
	return result
}

func mapOperationType(olType, olSubtype string) model.OperationType {
	switch olType {
	case "DIRECT":
		switch olSubtype {
		case "IDENTITY":
			return model.OperationProject
		case "AGGREGATION":
			return model.OperationAggregate
		default:
			return model.OperationFunction
		}
	case "INDIRECT":
		// An indirect field influences the output without its value reaching it,
		// and the subtype names how. Keeping it means an ingested filter, join,
		// grouping or sort edge carries the same operation a SQL analyzer records.
		switch olSubtype {
		case "JOIN":
			return model.OperationJoin
		case "GROUP_BY":
			return model.OperationGroupBy
		case "FILTER":
			return model.OperationFilter
		case "SORT":
			return model.OperationSort
		default:
			return model.OperationProject
		}
	default:
		return model.OperationProject
	}
}

// ParseRunEvent parses a raw JSON payload into a RunEvent.
func ParseRunEvent(data []byte) (*RunEvent, error) {
	var event RunEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, errors.Wrap(err, "failed to parse OpenLineage RunEvent")
	}
	if event.Run.RunID == "" {
		return nil, errors.New("missing run.runId in OpenLineage event")
	}
	if event.EventType == "" {
		return nil, errors.New("missing eventType in OpenLineage event")
	}
	event.RawJSON = data
	return &event, nil
}
