package v1

import (
	"context"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	openlineageplugin "github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// The dimensions the filter options answer in, matching the store's labels.
const (
	openLineageFilterJobNamespace = "job_namespace"
	openLineageFilterJobType      = "job_type"
	openLineageFilterEventType    = "event_type"
	openLineageFilterSource       = "source"
)

// maxOpenLineageFilterOptions bounds each dimension of the in-memory answer the
// same way the store bounds the dimensions it queries.
const maxOpenLineageFilterOptions = 200

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
// filters by, from the same window of recent runs the dataset list aggregates.
func (s *OpenLineageService) openLineageDatasetFilterOptions(ctx context.Context) (*datasetFilterOptions, error) {
	limit := defaultOpenLineageRunLimit
	runs, err := s.store.ListOpenLineageRun(ctx, &store.FindOpenLineageRunMessage{Limit: &limit})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list openlineage runs for dataset filter options"))
	}
	return collectDatasetFilterOptions(runs), nil
}

// collectDatasetFilterOptions reads the three dataset dimensions off the runs.
// None of them is a column: a dataset's namespace comes from the payload, and an
// integration or a source belongs to the run that carried the dataset, so this
// walks the same payloads the dataset aggregate walks and resolves nothing.
func collectDatasetFilterOptions(runs []*store.OpenLineageRunMessage) *datasetFilterOptions {
	namespaces := map[string]int64{}
	integrations := map[string]int64{}
	sources := map[string]int64{}
	for _, run := range runs {
		event, err := openlineageplugin.ParseRunEvent(run.RawPayload)
		if err != nil {
			continue
		}
		// A run only contributes an integration or a source when it actually
		// carries a dataset, which is what the aggregate counts them from.
		carried := false
		for _, dataset := range slices.Concat(event.Inputs, event.Outputs) {
			if namespace := strings.TrimSpace(dataset.Namespace); namespace != "" {
				namespaces[namespace]++
				carried = true
			}
		}
		if !carried {
			continue
		}
		if integration := strings.TrimSpace(run.Integration); integration != "" {
			integrations[integration]++
		}
		if source := strings.TrimSpace(run.Source); source != "" {
			sources[source]++
		}
	}

	return &datasetFilterOptions{
		namespaces:   toFilterOptions(namespaces),
		integrations: toFilterOptions(integrations),
		sources:      toFilterOptions(sources),
	}
}

type datasetFilterOptions struct {
	namespaces   []*v1pb.OpenLineageFilterOption
	integrations []*v1pb.OpenLineageFilterOption
	sources      []*v1pb.OpenLineageFilterOption
}

// toFilterOptions orders by count, then value, and caps the list the same way the
// store caps its own dimensions.
func toFilterOptions(counts map[string]int64) []*v1pb.OpenLineageFilterOption {
	options := make([]*v1pb.OpenLineageFilterOption, 0, len(counts))
	for value, count := range counts {
		options = append(options, &v1pb.OpenLineageFilterOption{Value: value, Count: count})
	}
	slices.SortFunc(options, func(left, right *v1pb.OpenLineageFilterOption) int {
		if left.Count != right.Count {
			return int(right.Count - left.Count)
		}
		return strings.Compare(left.Value, right.Value)
	})
	if len(options) > maxOpenLineageFilterOptions {
		options = options[:maxOpenLineageFilterOptions]
	}
	return options
}
