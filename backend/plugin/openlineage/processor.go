package openlineage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const openLineageTaskGUIDPrefix = "openlineage:task:"

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
func (p *Processor) ProcessRunEvent(ctx context.Context, event *RunEvent) error {
	if event.EventType != "COMPLETE" {
		slog.Debug("skipping non-COMPLETE OpenLineage event", "eventType", event.EventType, "runId", event.Run.RunID)
		return nil
	}

	meta := buildLineageMeta(event)
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
			// Table-level lineage: Airflow and other integrations may emit events
			// with input/output datasets but without column-level detail.
			outputLineages, err := p.processTableLevelLineage(ctx, event.Inputs, &output, meta)
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
		return nil
	}

	slog.Info("storing OpenLineage lineage",
		"metaGUID", meta.GUID,
		"metaType", meta.Type,
		"jobNamespace", event.Job.Namespace,
		"jobName", event.Job.Name,
		"edges", len(lineages),
	)

	return p.store.BatchReplaceColumnLineage(ctx, meta.GUID, meta.Type, lineages)
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

			relationType := mapRelationType(input.Transformations)
			transformation := mapTransformations(input.Transformations)

			lineages = append(lineages, buildColumnLineage(meta, sourceResolved, targetResolved, input.Field, outputColumn, relationType, transformation))
		}
	}

	// Process dataset-level lineage references (table-level within column lineage facet).
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

		lineages = append(lineages, buildColumnLineage(meta, sourceResolved, targetResolved, "", "", model.RelationTypeDirect, []model.Transformation{}))
	}

	return lineages, nil
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

		lineages = append(lineages, buildColumnLineage(meta, sourceResolved, targetResolved, "", "", model.RelationTypeDirect, []model.Transformation{}))
	}

	return lineages, nil
}

func buildLineageMeta(event *RunEvent) lineageMeta {
	return lineageMeta{
		GUID: buildOpenLineageTaskGUID(event.Job.Namespace, event.Job.Name, event.Run.RunID),
		Type: storepb.MetaType_OPENLINEAGE,
	}
}

func buildOpenLineageTaskGUID(namespace, name, runID string) string {
	namespace = strings.TrimSpace(namespace)
	name = strings.TrimSpace(name)
	if namespace == "" && name == "" {
		return fmt.Sprintf("%srun:%s", openLineageTaskGUIDPrefix, url.PathEscape(strings.TrimSpace(runID)))
	}
	if namespace == "" {
		return openLineageTaskGUIDPrefix + url.PathEscape(name)
	}
	if name == "" {
		return openLineageTaskGUIDPrefix + url.PathEscape(namespace)
	}
	return fmt.Sprintf("%s%s:%s", openLineageTaskGUIDPrefix, url.PathEscape(namespace), url.PathEscape(name))
}

func buildColumnLineage(
	meta lineageMeta,
	sourceResolved *ResolvedDataset,
	targetResolved *ResolvedDataset,
	sourceColumn string,
	targetColumn string,
	relationType model.RelationType,
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
		RelationType:   relationType,
		Transformation: transformation,
	}
}

// mapRelationType converts OpenLineage transformation types to our internal RelationType.
func mapRelationType(transforms []OLTransform) model.RelationType {
	if len(transforms) == 0 {
		return model.RelationTypeDirect
	}
	for _, t := range transforms {
		switch t.Type {
		case "DIRECT":
			switch t.Subtype {
			case "IDENTITY":
				return model.RelationTypeDirect
			case "TRANSFORMATION", "AGGREGATION":
				return model.RelationTypeIndirect
			default:
				return model.RelationTypeDirect
			}
		case "INDIRECT":
			return model.RelationTypeIndirect
		default:
			return model.RelationTypeDirect
		}
	}
	return model.RelationTypeDirect
}

// mapTransformations converts OpenLineage transforms to our internal Transformation model.
func mapTransformations(transforms []OLTransform) []model.Transformation {
	if len(transforms) == 0 {
		return []model.Transformation{}
	}

	result := make([]model.Transformation, 0, len(transforms))
	for _, t := range transforms {
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
		return model.OperationProject
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
