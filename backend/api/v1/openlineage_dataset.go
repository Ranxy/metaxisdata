package v1

import (
	"context"
	"encoding/json"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	openlineageplugin "github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// openLineageDatasetAggregate is one dataset as the dataset pages show it: the
// stored aggregate plus the resolution only the app can do. The store groups
// the references in SQL, which is what keeps a page load from reading (and
// JSON-parsing) the payload of every recent run.
type openLineageDatasetAggregate struct {
	GUID                  string
	Namespace             string
	Name                  string
	DatasetType           string
	ResolvedTarget        string
	ResolvedMetaType      v1pb.MetaType
	Internal              bool
	SupportsColumnLineage bool
	LastSeen              *timestamppb.Timestamp
	SourceJobCount        int32
	TargetJobCount        int32
	Integrations          []string
	Sources               []string
}

func (s *OpenLineageService) ListOpenLineageDatasets(ctx context.Context, req *connect.Request[v1pb.ListOpenLineageDatasetsRequest]) (*connect.Response[v1pb.ListOpenLineageDatasetsResponse], error) {
	aggregates, err := s.store.ListOpenLineageDatasetAggregate(ctx, openLineageDatasetFind(req.Msg))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list openlineage datasets"))
	}
	datasets := filterOpenLineageDatasets(s.resolveOpenLineageDatasetAggregates(ctx, aggregates), req.Msg)

	size := int(req.Msg.GetPageSize())
	if size <= 0 {
		size = 200
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   size,
		maximum: 1000,
	})
	if err != nil {
		return nil, err
	}
	if offset.offset >= len(datasets) {
		return connect.NewResponse(&v1pb.ListOpenLineageDatasetsResponse{}), nil
	}

	// The datasets come from an in-memory aggregate, so probe one row past the
	// page exactly like the SQL-backed lists do.
	probe := datasets[offset.offset:]
	if len(probe) > offset.limit+1 {
		probe = probe[:offset.limit+1]
	}
	page, nextPageToken, err := paginate(probe, offset)
	if err != nil {
		return nil, err
	}

	resp := &v1pb.ListOpenLineageDatasetsResponse{NextPageToken: nextPageToken}
	for _, dataset := range page {
		resp.Datasets = append(resp.Datasets, convertOpenLineageDatasetResource(dataset))
	}

	return connect.NewResponse(resp), nil
}

// GetOpenLineageDataset resolves the requested GUID back to the spellings the
// runs reported and reads their references. Resolution is per distinct dataset
// rather than per run, which is why the GUID is the only lookup key the request
// needs, and it runs over the same window the list reads: a dataset the list
// shows is always one this can open.
func (s *OpenLineageService) GetOpenLineageDataset(ctx context.Context, req *connect.Request[v1pb.GetOpenLineageDatasetRequest]) (*connect.Response[v1pb.OpenLineageDatasetDetailResource], error) {
	if req.Msg.GetGuid() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}

	window, err := s.store.ListOpenLineageDatasetWindow(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to resolve openlineage dataset"))
	}
	identity, pairs := s.resolveOpenLineageDatasetWindow(ctx, window, req.Msg.GetGuid())
	if identity == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("openlineage dataset %q not found", req.Msg.GetGuid()))
	}

	detail, err := s.store.GetOpenLineageDatasetDetail(ctx, pairs)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get openlineage dataset detail"))
	}
	if detail == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("openlineage dataset %q not found", req.Msg.GetGuid()))
	}

	resource := convertOpenLineageDatasetResource(identity)
	resource.SourceJobCount = detail.SourceJobCount
	resource.TargetJobCount = detail.TargetJobCount
	resource.Integrations = detail.Integrations
	resource.Sources = detail.Sources
	resource.SupportsColumnLineage = detail.SupportsColumnLineage
	if detail.LastSeen != nil {
		resource.LastSeen = timestamppb.New(*detail.LastSeen)
	}

	return connect.NewResponse(&v1pb.OpenLineageDatasetDetailResource{
		Dataset:      resource,
		SchemaFields: convertOpenLineageDatasetSchemaFields(detail),
		RelatedJobs:  convertOpenLineageDatasetJobs(detail.Jobs),
		RecentRuns:   convertOpenLineageDatasetRuns(detail.Runs),
	}), nil
}

// resolveOpenLineageDatasetWindow resolves the window's spellings and keeps the
// ones that resolve to guid. The first match stands for the dataset's identity;
// every match is one of the spellings whose references describe it.
func (s *OpenLineageService) resolveOpenLineageDatasetWindow(ctx context.Context, window []store.OpenLineageDatasetPair, guid string) (*openLineageDatasetAggregate, []store.OpenLineageDatasetPair) {
	resolver := openlineageplugin.NewRequestScopedResolver(s.store)
	var identity *openLineageDatasetAggregate
	var pairs []store.OpenLineageDatasetPair
	for _, pair := range window {
		resolved, err := resolver.ResolveDatasetPreview(ctx, pair.Namespace, pair.Name)
		if err != nil || resolved == nil {
			resolved = externalDatasetFallback(pair.Namespace, pair.Name)
		}
		if resolved.GUID != guid {
			continue
		}
		if identity == nil {
			identity = openLineageDatasetIdentity(pair.Namespace, pair.Name, resolved)
		}
		pairs = append(pairs, pair)
	}
	return identity, pairs
}

// openLineageDatasetFind maps the request's filters onto the ones the SQL
// aggregate answers; the free-text search and the scope stay in Go because they
// need the dataset's resolution.
func openLineageDatasetFind(req *v1pb.ListOpenLineageDatasetsRequest) *store.FindOpenLineageDatasetMessage {
	find := &store.FindOpenLineageDatasetMessage{}
	if namespace := req.GetNamespace(); namespace != "" {
		find.Namespace = &namespace
	}
	if integration := req.GetIntegration(); integration != "" {
		find.Integration = &integration
	}
	if source := req.GetSource(); source != "" {
		find.Source = &source
	}
	if req.GetColumnLineageOnly() {
		columnLineageOnly := true
		find.ColumnLineageOnly = &columnLineageOnly
	}
	return find
}

// datasetPreviewResolver resolves one (namespace, name) without creating an
// external-dataset row. It is the seam unit tests use in place of the store.
type datasetPreviewResolver func(context.Context, string, string) (*openlineageplugin.ResolvedDataset, error)

// resolveOpenLineageDatasetAggregates resolves one request's worth of datasets.
func (s *OpenLineageService) resolveOpenLineageDatasetAggregates(ctx context.Context, aggregates []*store.OpenLineageDatasetAggregateMessage) []*openLineageDatasetAggregate {
	resolver := openlineageplugin.NewRequestScopedResolver(s.store)
	return resolveOpenLineageDatasetAggregates(ctx, aggregates, resolver.ResolveDatasetPreview)
}

// resolveOpenLineageDatasetAggregates attaches each stored aggregate to its
// resolution. An unresolvable dataset falls back to its external identity, which
// is what the read paths did when they resolved per run: a resolver error must
// not empty the page.
func resolveOpenLineageDatasetAggregates(ctx context.Context, aggregates []*store.OpenLineageDatasetAggregateMessage, resolve datasetPreviewResolver) []*openLineageDatasetAggregate {
	result := make([]*openLineageDatasetAggregate, 0, len(aggregates))
	for _, aggregate := range aggregates {
		resolved, err := resolve(ctx, aggregate.Namespace, aggregate.Name)
		if err != nil || resolved == nil {
			resolved = externalDatasetFallback(aggregate.Namespace, aggregate.Name)
		}

		dataset := openLineageDatasetIdentity(aggregate.Namespace, aggregate.Name, resolved)
		if aggregate.LastSeen != nil {
			dataset.LastSeen = timestamppb.New(*aggregate.LastSeen)
		}
		dataset.SupportsColumnLineage = aggregate.SupportsColumnLineage
		dataset.SourceJobCount = aggregate.SourceJobCount
		dataset.TargetJobCount = aggregate.TargetJobCount
		dataset.Integrations = aggregate.Integrations
		dataset.Sources = aggregate.Sources
		result = append(result, dataset)
	}
	return result
}

// openLineageDatasetIdentity is the part of a dataset's description that comes
// from resolving its namespace and name, which is all a detail request has before
// it reads the aggregate.
func openLineageDatasetIdentity(namespace, name string, resolved *openlineageplugin.ResolvedDataset) *openLineageDatasetAggregate {
	return &openLineageDatasetAggregate{
		GUID:             resolved.GUID,
		Namespace:        namespace,
		Name:             name,
		DatasetType:      openlineageplugin.InferDatasetType(namespace),
		ResolvedTarget:   formatResolvedTarget(resolved.GUID, resolved.Internal),
		ResolvedMetaType: v1pb.MetaType(resolved.MetaType),
		Internal:         resolved.Internal,
	}
}

// externalDatasetFallback keeps a dataset the resolver cannot answer under its
// external identity instead of dropping it from the page.
func externalDatasetFallback(namespace, name string) *openlineageplugin.ResolvedDataset {
	return &openlineageplugin.ResolvedDataset{
		GUID:     openlineageplugin.FormatExternalGUID(namespace, name),
		MetaType: storepb.MetaType_EXTERNAL_DATASET,
		Internal: false,
	}
}

func convertOpenLineageDatasetResource(dataset *openLineageDatasetAggregate) *v1pb.OpenLineageDatasetResource {
	return &v1pb.OpenLineageDatasetResource{
		Guid:                  dataset.GUID,
		Namespace:             dataset.Namespace,
		Name:                  dataset.Name,
		DatasetType:           dataset.DatasetType,
		ResolvedTarget:        dataset.ResolvedTarget,
		ResolvedMetaType:      dataset.ResolvedMetaType,
		Internal:              dataset.Internal,
		SupportsColumnLineage: dataset.SupportsColumnLineage,
		SourceJobCount:        dataset.SourceJobCount,
		TargetJobCount:        dataset.TargetJobCount,
		Integrations:          dataset.Integrations,
		Sources:               dataset.Sources,
		LastSeen:              dataset.LastSeen,
	}
}

// convertOpenLineageDatasetSchemaFields renders the stored schema facet. A field
// the dataset's columnLineage facet described is marked ready, which is the
// column-level lineage the detail drawer advertises.
func convertOpenLineageDatasetSchemaFields(detail *store.OpenLineageDatasetDetailMessage) []*v1pb.OpenLineageDatasetField {
	if len(detail.SchemaFields) == 0 {
		return nil
	}
	var fields []openlineageplugin.SchemaField
	if err := json.Unmarshal(detail.SchemaFields, &fields); err != nil {
		// A schema the store wrote and the app cannot read is not fatal to the
		// page: the rest of the detail still describes the dataset.
		return nil
	}

	ready := make(map[string]struct{}, len(detail.ColumnLineageFields))
	for _, name := range detail.ColumnLineageFields {
		ready[name] = struct{}{}
	}

	result := make([]*v1pb.OpenLineageDatasetField, 0, len(fields))
	for _, field := range fields {
		_, columnLineageReady := ready[field.Name]
		result = append(result, &v1pb.OpenLineageDatasetField{
			Name:               field.Name,
			Type:               field.Type,
			Description:        field.Description,
			ColumnLineageReady: columnLineageReady,
		})
	}
	return result
}

func convertOpenLineageDatasetJobs(jobs []*store.OpenLineageDatasetJobMessage) []*v1pb.OpenLineageDatasetJobResource {
	result := make([]*v1pb.OpenLineageDatasetJobResource, 0, len(jobs))
	for _, job := range jobs {
		resource := &v1pb.OpenLineageDatasetJobResource{
			TaskGuid:      job.TaskGUID,
			JobNamespace:  job.JobNamespace,
			JobName:       job.JobName,
			JobType:       job.JobType,
			Integration:   job.Integration,
			RunCount:      job.RunCount,
			ReadsDataset:  job.ReadsDataset,
			WritesDataset: job.WritesDataset,
		}
		if job.LastSeen != nil {
			resource.LastSeen = timestamppb.New(*job.LastSeen)
		}
		result = append(result, resource)
	}
	return result
}

func convertOpenLineageDatasetRuns(runs []*store.OpenLineageDatasetRunMessage) []*v1pb.OpenLineageDatasetRunResource {
	result := make([]*v1pb.OpenLineageDatasetRunResource, 0, len(runs))
	for _, run := range runs {
		resource := &v1pb.OpenLineageDatasetRunResource{
			Guid:          run.RunGUID,
			TaskGuid:      run.TaskGUID,
			RunId:         run.RunID,
			JobNamespace:  run.JobNamespace,
			JobName:       run.JobName,
			JobType:       run.JobType,
			EventType:     run.EventType,
			HasLineage:    run.HasLineage,
			ReadsDataset:  run.ReadsDataset,
			WritesDataset: run.WritesDataset,
		}
		if run.EventTime != nil {
			resource.EventTime = timestamppb.New(*run.EventTime)
		}
		result = append(result, resource)
	}
	return result
}

func filterOpenLineageDatasets(datasets []*openLineageDatasetAggregate, req *v1pb.ListOpenLineageDatasetsRequest) []*openLineageDatasetAggregate {
	query := strings.ToLower(strings.TrimSpace(req.GetSearch()))
	result := make([]*openLineageDatasetAggregate, 0, len(datasets))
	for _, dataset := range datasets {
		if req.GetNamespace() != "" && dataset.Namespace != req.GetNamespace() {
			continue
		}
		if req.GetIntegration() != "" && !containsString(dataset.Integrations, req.GetIntegration()) {
			continue
		}
		if req.GetSource() != "" && !containsString(dataset.Sources, req.GetSource()) {
			continue
		}
		if req.GetColumnLineageOnly() && !dataset.SupportsColumnLineage {
			continue
		}
		switch req.GetDatasetScope() {
		case v1pb.OpenLineageDatasetScope_OPENLINEAGE_DATASET_SCOPE_INTERNAL:
			if !dataset.Internal {
				continue
			}
		case v1pb.OpenLineageDatasetScope_OPENLINEAGE_DATASET_SCOPE_EXTERNAL:
			if dataset.Internal {
				continue
			}
		default:
		}
		if query != "" {
			// The search box advertises the resolved target and the integrations
			// and sources, and the list shows them, so they belong in the
			// haystack alongside the identity fields.
			haystack := strings.ToLower(strings.Join([]string{
				dataset.Name,
				dataset.Namespace,
				dataset.DatasetType,
				dataset.ResolvedTarget,
				strings.Join(dataset.Integrations, " "),
				strings.Join(dataset.Sources, " "),
			}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		result = append(result, dataset)
	}
	return result
}

// formatResolvedTarget renders the database.schema.table tail of a resolved
// internal GUID. Segments are handled positionally: dropping the empty ones
// first shifted a MySQL guid (instance;db;;table) down to three segments, so it
// was no longer trimmed and the instance id was rendered as the database.
func formatResolvedTarget(guid string, internal bool) string {
	if !internal {
		return ""
	}
	parts := strings.Split(guid, ";")
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	if len(parts) == 0 {
		return guid
	}
	return strings.Join(parts, ".")
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
