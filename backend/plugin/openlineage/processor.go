package openlineage

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/store"
)

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

	for _, output := range event.Outputs {
		hasColumnLineage := output.Facets.ColumnLineage != nil && len(output.Facets.ColumnLineage.Fields) > 0
		hasDatasetLineage := output.Facets.ColumnLineage != nil && len(output.Facets.ColumnLineage.Dataset) > 0
		hasInputs := len(event.Inputs) > 0

		if hasColumnLineage || hasDatasetLineage {
			if err := p.processOutputDataset(ctx, &output); err != nil {
				slog.Error("failed to process OpenLineage output dataset",
					"namespace", output.Namespace,
					"name", output.Name,
					"error", err,
				)
				return errors.Wrapf(err, "failed to process output dataset %s/%s", output.Namespace, output.Name)
			}
		} else if hasInputs {
			// Table-level lineage: Airflow and other integrations may emit events
			// with input/output datasets but without column-level detail.
			if err := p.processTableLevelLineage(ctx, event.Inputs, &output); err != nil {
				slog.Error("failed to process table-level lineage",
					"namespace", output.Namespace,
					"name", output.Name,
					"error", err,
				)
				return errors.Wrapf(err, "failed to process table-level lineage for %s/%s", output.Namespace, output.Name)
			}
		}
	}

	return nil
}

func (p *Processor) processOutputDataset(ctx context.Context, output *Dataset) error {
	// Resolve the output dataset
	targetResolved, err := p.resolver.ResolveDataset(ctx, output.Namespace, output.Name)
	if err != nil {
		return errors.Wrap(err, "failed to resolve output dataset")
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

			lineages = append(lineages, &store.ColumnLineage{
				MetaGUID:       targetResolved.GUID,
				MetaType:       targetResolved.MetaType,
				SourceGUID:     sourceResolved.GUID,
				SourceColumn:   input.Field,
				SourceType:     sourceResolved.MetaType,
				TargetGUID:     targetResolved.GUID,
				TargetColumn:   outputColumn,
				TargetType:     targetResolved.MetaType,
				RelationType:   relationType,
				Transformation: transformation,
			})
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

		lineages = append(lineages, &store.ColumnLineage{
			MetaGUID:       targetResolved.GUID,
			MetaType:       targetResolved.MetaType,
			SourceGUID:     sourceResolved.GUID,
			SourceColumn:   "",
			SourceType:     sourceResolved.MetaType,
			TargetGUID:     targetResolved.GUID,
			TargetColumn:   "",
			TargetType:     targetResolved.MetaType,
			RelationType:   model.RelationTypeDirect,
			Transformation: nil,
		})
	}

	if len(lineages) == 0 {
		return nil
	}

	slog.Info("storing OpenLineage column lineage",
		"target", targetResolved.GUID,
		"metaType", targetResolved.MetaType,
		"edges", len(lineages),
	)

	return p.store.BatchReplaceColumnLineage(ctx, targetResolved.GUID, targetResolved.MetaType, lineages)
}

// processTableLevelLineage handles the case where Airflow and similar integrations
// emit COMPLETE events with input/output datasets but without column-level lineage.
func (p *Processor) processTableLevelLineage(ctx context.Context, inputs []Dataset, output *Dataset) error {
	targetResolved, err := p.resolver.ResolveDataset(ctx, output.Namespace, output.Name)
	if err != nil {
		return errors.Wrap(err, "failed to resolve output dataset")
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

		lineages = append(lineages, &store.ColumnLineage{
			MetaGUID:       targetResolved.GUID,
			MetaType:       targetResolved.MetaType,
			SourceGUID:     sourceResolved.GUID,
			SourceColumn:   "",
			SourceType:     sourceResolved.MetaType,
			TargetGUID:     targetResolved.GUID,
			TargetColumn:   "",
			TargetType:     targetResolved.MetaType,
			RelationType:   model.RelationTypeDirect,
			Transformation: nil,
		})
	}

	if len(lineages) == 0 {
		return nil
	}

	slog.Info("storing OpenLineage table-level lineage",
		"target", targetResolved.GUID,
		"metaType", targetResolved.MetaType,
		"edges", len(lineages),
	)

	return p.store.BatchReplaceColumnLineage(ctx, targetResolved.GUID, targetResolved.MetaType, lineages)
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
		return nil
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
