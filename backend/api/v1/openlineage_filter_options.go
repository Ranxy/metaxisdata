package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The dimensions the filter options answer in, matching the store's labels.
const (
	openLineageFilterJobNamespace = "job_namespace"
	openLineageFilterJobType      = "job_type"
	openLineageFilterEventType    = "event_type"
	openLineageFilterSource       = "source"

	// The dimensions only the dataset page offers. They come from the dataset
	// references rather than from the runs' payloads.
	openLineageFilterDatasetNamespace   = "dataset_namespace"
	openLineageFilterDatasetIntegration = "dataset_integration"
	openLineageFilterDatasetSource      = "dataset_source"
)

// ListOpenLineageFilterOptions answers the filter menus of the index pages. Those
// pages read one page of rows at a time, so a menu built from what is on screen
// would only ever offer the values of the current page — one or two namespaces in
// a workspace that has twenty.
func (s *OpenLineageService) ListOpenLineageFilterOptions(ctx context.Context, _ *connect.Request[v1pb.ListOpenLineageFilterOptionsRequest]) (*connect.Response[v1pb.ListOpenLineageFilterOptionsResponse], error) {
	values, err := s.store.ListOpenLineageFilterValues(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list openlineage filter values"))
	}

	byDimension := make(map[string][]*v1pb.OpenLineageFilterOption, 4)
	for _, value := range values {
		byDimension[value.Dimension] = append(byDimension[value.Dimension], &v1pb.OpenLineageFilterOption{
			Value: value.Value,
			Count: value.Count,
		})
	}

	datasets, err := s.openLineageDatasetFilterOptions(ctx)
	if err != nil {
		return nil, err
	}

	totals, err := s.store.CountOpenLineageTotals(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to count openlineage totals"))
	}

	return connect.NewResponse(&v1pb.ListOpenLineageFilterOptionsResponse{
		JobNamespaces:      byDimension[openLineageFilterJobNamespace],
		JobTypes:           byDimension[openLineageFilterJobType],
		EventTypes:         byDimension[openLineageFilterEventType],
		DatasetNamespaces:  datasets.namespaces,
		Integrations:       datasets.integrations,
		Sources:            datasets.sources,
		TotalRuns:          totals.Runs,
		TotalJobs:          totals.Jobs,
		TotalJobNamespaces: totals.JobNamespaces,
	}), nil
}

// openLineageDatasetFilterOptions collects the dimensions the Datasets page
// filters by. The store aggregates them from the dataset references and already
// orders and caps each dimension, so this only labels them.
func (s *OpenLineageService) openLineageDatasetFilterOptions(ctx context.Context) (*datasetFilterOptions, error) {
	values, err := s.store.ListOpenLineageDatasetFilterValues(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list openlineage dataset filter values"))
	}
	return datasetFilterOptionsFromValues(values), nil
}

func datasetFilterOptionsFromValues(values []*store.OpenLineageFilterValue) *datasetFilterOptions {
	options := &datasetFilterOptions{}
	for _, value := range values {
		option := &v1pb.OpenLineageFilterOption{Value: value.Value, Count: value.Count}
		switch value.Dimension {
		case openLineageFilterDatasetNamespace:
			options.namespaces = append(options.namespaces, option)
		case openLineageFilterDatasetIntegration:
			options.integrations = append(options.integrations, option)
		case openLineageFilterDatasetSource:
			options.sources = append(options.sources, option)
		default:
		}
	}
	return options
}

type datasetFilterOptions struct {
	namespaces   []*v1pb.OpenLineageFilterOption
	integrations []*v1pb.OpenLineageFilterOption
	sources      []*v1pb.OpenLineageFilterOption
}
