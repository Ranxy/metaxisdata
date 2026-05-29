package v1

import (
	"context"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	openlineageplugin "github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type openLineageDatasetAggregate struct {
	GUID                  string
	Namespace             string
	Name                  string
	DatasetType           string
	ResolvedTarget        string
	ResolvedMetaType      v1pb.MetaType
	Internal              bool
	SupportsColumnLineage bool
	LastSeen              *time.Time
	SourceJobCount        int32
	TargetJobCount        int32
	Integrations          []string
	Sources               []string

	sourceJobKeys  map[string]struct{}
	targetJobKeys  map[string]struct{}
	integrationSet map[string]struct{}
	sourceSet      map[string]struct{}
}

func (s *OpenLineageService) ListOpenLineageDatasets(ctx context.Context, req *connect.Request[v1pb.ListOpenLineageDatasetsRequest]) (*connect.Response[v1pb.ListOpenLineageDatasetsResponse], error) {
	runs, err := s.store.ListOpenLineageRun(ctx, &store.FindOpenLineageRunMessage{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list openlineage runs for dataset aggregation"))
	}

	resolver := openlineageplugin.NewResolver(s.store)
	aggregates := aggregateOpenLineageDatasets(ctx, runs, func(ctx context.Context, namespace, name string) (*openlineageplugin.ResolvedDataset, error) {
		return resolver.ResolveDatasetPreview(ctx, namespace, name)
	})
	aggregates = filterOpenLineageDatasets(aggregates, req.Msg)

	pageSize := int(req.Msg.GetPageSize())
	if pageSize <= 0 {
		pageSize = 200
	}
	offset := int(req.Msg.GetOffset())
	if offset < 0 {
		offset = 0
	}
	if offset >= len(aggregates) {
		return connect.NewResponse(&v1pb.ListOpenLineageDatasetsResponse{}), nil
	}

	end := offset + pageSize
	if end > len(aggregates) {
		end = len(aggregates)
	}

	resp := &v1pb.ListOpenLineageDatasetsResponse{}
	for _, dataset := range aggregates[offset:end] {
		resource := &v1pb.OpenLineageDatasetResource{
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
		}
		if dataset.LastSeen != nil {
			resource.LastSeen = timestamppb.New(*dataset.LastSeen)
		}
		resp.Datasets = append(resp.Datasets, resource)
	}

	return connect.NewResponse(resp), nil
}

type datasetPreviewResolver func(context.Context, string, string) (*openlineageplugin.ResolvedDataset, error)

type openLineageDatasetDetail struct {
	Dataset      *openLineageDatasetAggregate
	SchemaFields []*v1pb.OpenLineageDatasetField
	RelatedJobs  []*v1pb.OpenLineageDatasetJobResource
	RecentRuns   []*v1pb.OpenLineageDatasetRunResource
}

type openLineageDatasetJobAggregate struct {
	TaskGUID      string
	JobNamespace  string
	JobName       string
	JobType       string
	Integration   string
	LastSeen      *time.Time
	RunCount      int32
	ReadsDataset  bool
	WritesDataset bool
}

func (s *OpenLineageService) GetOpenLineageDataset(ctx context.Context, req *connect.Request[v1pb.GetOpenLineageDatasetRequest]) (*connect.Response[v1pb.OpenLineageDatasetDetailResource], error) {
	if req.Msg.GetGuid() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}

	runs, err := s.store.ListOpenLineageRun(ctx, &store.FindOpenLineageRunMessage{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list openlineage runs for dataset detail"))
	}

	resolver := openlineageplugin.NewResolver(s.store)
	detail, found := buildOpenLineageDatasetDetail(
		ctx,
		runs,
		req.Msg.GetGuid(),
		req.Msg.GetNamespace(),
		req.Msg.GetName(),
		func(ctx context.Context, namespace, name string) (*openlineageplugin.ResolvedDataset, error) {
			return resolver.ResolveDatasetPreview(ctx, namespace, name)
		},
	)
	if !found || detail.Dataset == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("openlineage dataset %q not found", req.Msg.GetGuid()))
	}

	resource := &v1pb.OpenLineageDatasetResource{
		Guid:                  detail.Dataset.GUID,
		Namespace:             detail.Dataset.Namespace,
		Name:                  detail.Dataset.Name,
		DatasetType:           detail.Dataset.DatasetType,
		ResolvedTarget:        detail.Dataset.ResolvedTarget,
		ResolvedMetaType:      detail.Dataset.ResolvedMetaType,
		Internal:              detail.Dataset.Internal,
		SupportsColumnLineage: detail.Dataset.SupportsColumnLineage,
		SourceJobCount:        detail.Dataset.SourceJobCount,
		TargetJobCount:        detail.Dataset.TargetJobCount,
		Integrations:          detail.Dataset.Integrations,
		Sources:               detail.Dataset.Sources,
	}
	if detail.Dataset.LastSeen != nil {
		resource.LastSeen = timestamppb.New(*detail.Dataset.LastSeen)
	}

	return connect.NewResponse(&v1pb.OpenLineageDatasetDetailResource{
		Dataset:      resource,
		SchemaFields: detail.SchemaFields,
		RelatedJobs:  detail.RelatedJobs,
		RecentRuns:   detail.RecentRuns,
	}), nil
}

func aggregateOpenLineageDatasets(ctx context.Context, runs []*store.OpenLineageRunMessage, resolve datasetPreviewResolver) []*openLineageDatasetAggregate {
	datasetMap := make(map[string]*openLineageDatasetAggregate)

	for _, run := range runs {
		event, err := openlineageplugin.ParseRunEvent(run.RawPayload)
		if err != nil {
			continue
		}

		jobKey := run.TaskGUID
		if strings.TrimSpace(jobKey) == "" {
			jobKey = openlineageplugin.BuildOpenLineageTaskGUID(run.JobNamespace, run.JobName, run.JobType)
		}

		for _, dataset := range event.Inputs {
			upsertOpenLineageDatasetAggregate(ctx, datasetMap, resolve, dataset, run, jobKey, false)
		}
		for _, dataset := range event.Outputs {
			upsertOpenLineageDatasetAggregate(ctx, datasetMap, resolve, dataset, run, jobKey, true)
		}
	}

	result := make([]*openLineageDatasetAggregate, 0, len(datasetMap))
	for _, dataset := range datasetMap {
		dataset.SourceJobCount = int32(len(dataset.sourceJobKeys))
		dataset.TargetJobCount = int32(len(dataset.targetJobKeys))
		dataset.Integrations = sortedKeys(dataset.integrationSet)
		dataset.Sources = sortedKeys(dataset.sourceSet)
		result = append(result, dataset)
	}

	slices.SortFunc(result, func(left, right *openLineageDatasetAggregate) int {
		if left.LastSeen != nil && right.LastSeen != nil && !left.LastSeen.Equal(*right.LastSeen) {
			if left.LastSeen.After(*right.LastSeen) {
				return -1
			}
			return 1
		}
		if left.LastSeen != nil && right.LastSeen == nil {
			return -1
		}
		if left.LastSeen == nil && right.LastSeen != nil {
			return 1
		}
		if left.Name != right.Name {
			return strings.Compare(left.Name, right.Name)
		}
		return strings.Compare(left.Namespace, right.Namespace)
	})

	return result
}

func buildOpenLineageDatasetDetail(
	ctx context.Context,
	runs []*store.OpenLineageRunMessage,
	guid string,
	namespace string,
	name string,
	resolve datasetPreviewResolver,
) (*openLineageDatasetDetail, bool) {
	aggregates := aggregateOpenLineageDatasets(ctx, runs, resolve)
	dataset := findOpenLineageDatasetAggregate(aggregates, guid, namespace, name)
	if dataset == nil {
		return nil, false
	}

	columnLineageReadyFields := make(map[string]struct{})
	relatedJobs := make(map[string]*openLineageDatasetJobAggregate)
	recentRuns := make([]*v1pb.OpenLineageDatasetRunResource, 0)
	var bestSchema []openlineageplugin.SchemaField
	var bestSchemaUpdatedAt *time.Time

	for _, run := range runs {
		event, err := openlineageplugin.ParseRunEvent(run.RawPayload)
		if err != nil {
			continue
		}

		readsDataset, writesDataset, matchedDataset := matchDatasetInRun(ctx, event, run, dataset, resolve)
		if matchedDataset == nil {
			continue
		}

		if matchedDataset.Facets.Schema != nil {
			fields := matchedDataset.Facets.Schema.Fields
			if len(fields) > len(bestSchema) || (len(fields) > 0 && isRunMoreRecent(run.EventTime, bestSchemaUpdatedAt)) {
				bestSchema = fields
				bestSchemaUpdatedAt = run.EventTime
			}
		}

		collectDatasetColumnReadiness(columnLineageReadyFields, event, dataset)

		jobKey := run.TaskGUID
		if strings.TrimSpace(jobKey) == "" {
			jobKey = openlineageplugin.BuildOpenLineageTaskGUID(run.JobNamespace, run.JobName, run.JobType)
		}
		job := relatedJobs[jobKey]
		if job == nil {
			job = &openLineageDatasetJobAggregate{
				TaskGUID:     jobKey,
				JobNamespace: run.JobNamespace,
				JobName:      run.JobName,
				JobType:      run.JobType,
				Integration:  run.Integration,
			}
			relatedJobs[jobKey] = job
		}
		job.RunCount++
		job.ReadsDataset = job.ReadsDataset || readsDataset
		job.WritesDataset = job.WritesDataset || writesDataset
		if run.EventTime != nil && (job.LastSeen == nil || run.EventTime.After(*job.LastSeen)) {
			seen := *run.EventTime
			job.LastSeen = &seen
		}

		runID := run.RunID
		if runID == "" {
			runID = event.Run.RunID
		}

		runResource := &v1pb.OpenLineageDatasetRunResource{
			Guid:          run.GUID,
			TaskGuid:      run.TaskGUID,
			RunId:         runID,
			JobNamespace:  run.JobNamespace,
			JobName:       run.JobName,
			JobType:       run.JobType,
			EventType:     run.EventType,
			HasLineage:    run.HasLineage,
			ReadsDataset:  readsDataset,
			WritesDataset: writesDataset,
		}
		if run.EventTime != nil {
			runResource.EventTime = timestamppb.New(*run.EventTime)
		}
		recentRuns = append(recentRuns, runResource)
	}

	jobList := make([]*v1pb.OpenLineageDatasetJobResource, 0, len(relatedJobs))
	for _, job := range relatedJobs {
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
		jobList = append(jobList, resource)
	}
	slices.SortFunc(jobList, func(left, right *v1pb.OpenLineageDatasetJobResource) int {
		return compareMaybeTimestampDesc(left.LastSeen, right.LastSeen, left.JobName, right.JobName)
	})
	if len(jobList) > 8 {
		jobList = jobList[:8]
	}

	slices.SortFunc(recentRuns, func(left, right *v1pb.OpenLineageDatasetRunResource) int {
		return compareMaybeTimestampDesc(left.EventTime, right.EventTime, left.RunId, right.RunId)
	})
	if len(recentRuns) > 10 {
		recentRuns = recentRuns[:10]
	}

	schemaFields := make([]*v1pb.OpenLineageDatasetField, 0, len(bestSchema))
	for _, field := range bestSchema {
		_, ready := columnLineageReadyFields[field.Name]
		schemaFields = append(schemaFields, &v1pb.OpenLineageDatasetField{
			Name:               field.Name,
			Type:               field.Type,
			Description:        field.Description,
			ColumnLineageReady: ready,
		})
	}

	return &openLineageDatasetDetail{
		Dataset:      dataset,
		SchemaFields: schemaFields,
		RelatedJobs:  jobList,
		RecentRuns:   recentRuns,
	}, true
}

func findOpenLineageDatasetAggregate(aggregates []*openLineageDatasetAggregate, guid, namespace, name string) *openLineageDatasetAggregate {
	for _, dataset := range aggregates {
		if namespace != "" && name != "" {
			if dataset.Namespace == namespace && dataset.Name == name {
				return dataset
			}
			continue
		}
		if dataset.GUID == guid {
			return dataset
		}
	}
	return nil
}

func matchDatasetInRun(
	ctx context.Context,
	event *openlineageplugin.RunEvent,
	run *store.OpenLineageRunMessage,
	target *openLineageDatasetAggregate,
	resolve datasetPreviewResolver,
) (bool, bool, *openlineageplugin.Dataset) {
	var matched *openlineageplugin.Dataset
	readsDataset := false
	writesDataset := false

	for _, dataset := range event.Inputs {
		if datasetMatchesAggregate(ctx, dataset, target, resolve) {
			readsDataset = true
			matchedDataset := dataset
			matched = &matchedDataset
		}
	}
	for _, dataset := range event.Outputs {
		if datasetMatchesAggregate(ctx, dataset, target, resolve) {
			writesDataset = true
			matchedDataset := dataset
			matched = &matchedDataset
		}
	}

	if matched == nil && run.TaskGUID == target.GUID {
		return false, false, nil
	}

	return readsDataset, writesDataset, matched
}

func datasetMatchesAggregate(ctx context.Context, dataset openlineageplugin.Dataset, target *openLineageDatasetAggregate, resolve datasetPreviewResolver) bool {
	if dataset.Namespace == target.Namespace && dataset.Name == target.Name {
		return true
	}
	resolved, err := resolve(ctx, dataset.Namespace, dataset.Name)
	if err != nil || resolved == nil {
		return false
	}
	return resolved.GUID == target.GUID
}

func collectDatasetColumnReadiness(fieldSet map[string]struct{}, event *openlineageplugin.RunEvent, target *openLineageDatasetAggregate) {
	for _, output := range event.Outputs {
		if output.Namespace != target.Namespace || output.Name != target.Name {
			continue
		}
		if output.Facets.ColumnLineage == nil {
			continue
		}
		for fieldName := range output.Facets.ColumnLineage.Fields {
			fieldSet[fieldName] = struct{}{}
		}
	}
}

func isRunMoreRecent(left *time.Time, right *time.Time) bool {
	if left == nil {
		return false
	}
	if right == nil {
		return true
	}
	return left.After(*right)
}

func compareMaybeTimestampDesc(left *timestamppb.Timestamp, right *timestamppb.Timestamp, leftFallback string, rightFallback string) int {
	if left != nil && right != nil && !left.AsTime().Equal(right.AsTime()) {
		if left.AsTime().After(right.AsTime()) {
			return -1
		}
		return 1
	}
	if left != nil && right == nil {
		return -1
	}
	if left == nil && right != nil {
		return 1
	}
	return strings.Compare(leftFallback, rightFallback)
}

func upsertOpenLineageDatasetAggregate(
	ctx context.Context,
	datasetMap map[string]*openLineageDatasetAggregate,
	resolve datasetPreviewResolver,
	dataset openlineageplugin.Dataset,
	run *store.OpenLineageRunMessage,
	jobKey string,
	isTarget bool,
) {
	key := dataset.Namespace + "\x00" + dataset.Name
	agg, ok := datasetMap[key]
	if !ok {
		resolved, err := resolve(ctx, dataset.Namespace, dataset.Name)
		if err != nil || resolved == nil {
			resolved = &openlineageplugin.ResolvedDataset{
				GUID:     openlineageplugin.FormatExternalGUID(dataset.Namespace, dataset.Name),
				MetaType: 17,
				Internal: false,
			}
		}

		resolvedMetaType := v1pb.MetaType(resolved.MetaType)
		agg = &openLineageDatasetAggregate{
			GUID:             resolved.GUID,
			Namespace:        dataset.Namespace,
			Name:             dataset.Name,
			DatasetType:      openlineageplugin.InferDatasetType(dataset.Namespace),
			ResolvedTarget:   formatResolvedTarget(resolved.GUID, resolved.Internal),
			ResolvedMetaType: resolvedMetaType,
			Internal:         resolved.Internal,
			sourceJobKeys:    make(map[string]struct{}),
			targetJobKeys:    make(map[string]struct{}),
			integrationSet:   make(map[string]struct{}),
			sourceSet:        make(map[string]struct{}),
		}
		datasetMap[key] = agg
	}

	if isTarget {
		agg.targetJobKeys[jobKey] = struct{}{}
		if dataset.Facets.ColumnLineage != nil && (len(dataset.Facets.ColumnLineage.Fields) > 0 || len(dataset.Facets.ColumnLineage.Dataset) > 0) {
			agg.SupportsColumnLineage = true
		}
	} else {
		agg.sourceJobKeys[jobKey] = struct{}{}
	}

	if run.Integration != "" {
		agg.integrationSet[run.Integration] = struct{}{}
	}
	if run.Source != "" {
		agg.sourceSet[run.Source] = struct{}{}
	}
	if run.EventTime != nil && (agg.LastSeen == nil || run.EventTime.After(*agg.LastSeen)) {
		seen := *run.EventTime
		agg.LastSeen = &seen
	}
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
			haystack := strings.ToLower(strings.Join([]string{
				dataset.Name,
				dataset.Namespace,
				dataset.DatasetType,
				dataset.ResolvedTarget,
			}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		result = append(result, dataset)
	}
	return result
}

func formatResolvedTarget(guid string, internal bool) string {
	if !internal {
		return ""
	}
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(guid, ";") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return guid
	}
	if len(parts) > 3 {
		parts = parts[len(parts)-3:]
	}
	return strings.Join(parts, ".")
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	slices.Sort(keys)
	return keys
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
