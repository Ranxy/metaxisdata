package v1

import (
	"context"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	openlineageplugin "github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// OpenLineageService implements the OpenLineageServiceHandler for managing namespace mappings and API keys.
type OpenLineageService struct {
	v1connect.UnimplementedOpenLineageServiceHandler
	store *store.Store
}

// NewOpenLineageService creates a new OpenLineageService.
func NewOpenLineageService(s *store.Store) *OpenLineageService {
	return &OpenLineageService{store: s}
}

func (s *OpenLineageService) ListOpenLineageTasks(ctx context.Context, req *connect.Request[v1pb.ListOpenLineageTasksRequest]) (*connect.Response[v1pb.ListOpenLineageTasksResponse], error) {
	find := &store.FindOpenLineageTaskMessage{}
	if req.Msg.GetPageSize() > 0 {
		limit := int(req.Msg.GetPageSize())
		find.Limit = &limit
	}
	if req.Msg.GetOffset() > 0 {
		offset := int(req.Msg.GetOffset())
		find.Offset = &offset
	}
	if req.Msg.GetJobNamespace() != "" {
		jobNamespace := req.Msg.GetJobNamespace()
		find.JobNamespace = &jobNamespace
	}
	if req.Msg.GetJobName() != "" {
		jobName := req.Msg.GetJobName()
		find.JobName = &jobName
	}
	if req.Msg.GetJobType() != "" {
		jobType := req.Msg.GetJobType()
		find.JobType = &jobType
	}
	if req.Msg.GetLineageOnly() {
		lineageOnly := true
		find.LineageOnly = &lineageOnly
	}

	list, err := s.store.ListOpenLineageTask(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list openlineage tasks"))
	}

	resp := &v1pb.ListOpenLineageTasksResponse{}
	for _, task := range list {
		resp.Tasks = append(resp.Tasks, convertOpenLineageTask(task))
	}
	return connect.NewResponse(resp), nil
}

func (s *OpenLineageService) GetOpenLineageTask(ctx context.Context, req *connect.Request[v1pb.GetOpenLineageTaskRequest]) (*connect.Response[v1pb.OpenLineageTaskResource], error) {
	if req.Msg.GetGuid() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}

	guid := req.Msg.GetGuid()
	task, err := s.store.GetOpenLineageTask(ctx, &store.FindOpenLineageTaskMessage{GUID: &guid})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get openlineage task"))
	}
	if task == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("openlineage task %q not found", req.Msg.GetGuid()))
	}

	return connect.NewResponse(convertOpenLineageTask(task)), nil
}

func (s *OpenLineageService) ListOpenLineageRuns(ctx context.Context, req *connect.Request[v1pb.ListOpenLineageRunsRequest]) (*connect.Response[v1pb.ListOpenLineageRunsResponse], error) {
	find := &store.FindOpenLineageRunMessage{}
	if req.Msg.GetPageSize() > 0 {
		limit := int(req.Msg.GetPageSize())
		find.Limit = &limit
	}
	if req.Msg.GetOffset() > 0 {
		offset := int(req.Msg.GetOffset())
		find.Offset = &offset
	}
	if req.Msg.GetJobNamespace() != "" {
		jobNamespace := req.Msg.GetJobNamespace()
		find.JobNamespace = &jobNamespace
	}
	if req.Msg.GetJobName() != "" {
		jobName := req.Msg.GetJobName()
		find.JobName = &jobName
	}
	if req.Msg.GetTaskGuid() != "" {
		taskGUID := req.Msg.GetTaskGuid()
		find.TaskGUID = &taskGUID
	}
	if req.Msg.GetJobType() != "" {
		jobType := req.Msg.GetJobType()
		find.JobType = &jobType
	}

	list, err := s.store.ListOpenLineageRun(ctx, find)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list openlineage runs"))
	}

	resp := &v1pb.ListOpenLineageRunsResponse{}
	for _, run := range list {
		resp.Runs = append(resp.Runs, convertOpenLineageRun(run, false))
	}
	return connect.NewResponse(resp), nil
}

func (s *OpenLineageService) GetOpenLineageRun(ctx context.Context, req *connect.Request[v1pb.GetOpenLineageRunRequest]) (*connect.Response[v1pb.OpenLineageRunResource], error) {
	if req.Msg.GetGuid() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("guid is required"))
	}

	guid := req.Msg.GetGuid()
	run, err := s.store.GetOpenLineageRun(ctx, &store.FindOpenLineageRunMessage{GUID: &guid})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to get openlineage run"))
	}
	if run == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("openlineage run %q not found", req.Msg.GetGuid()))
	}

	return connect.NewResponse(convertOpenLineageRun(run, true)), nil
}

func (s *OpenLineageService) CreateNamespaceMapping(ctx context.Context, req *connect.Request[v1pb.CreateNamespaceMappingRequest]) (*connect.Response[v1pb.NamespaceMappingResource], error) {
	mapping := req.Msg.GetMapping()
	if mapping.GetNamespace() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("namespace is required"))
	}
	if mapping.GetInstanceResourceId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("instance_resource_id is required"))
	}

	result, err := s.store.CreateNamespaceMapping(ctx, &store.NamespaceMappingMessage{
		Namespace:          mapping.GetNamespace(),
		InstanceResourceID: mapping.GetInstanceResourceId(),
		DatabaseName:       mapping.GetDatabaseName(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create namespace mapping"))
	}

	return connect.NewResponse(convertNamespaceMapping(result)), nil
}

func (s *OpenLineageService) ListNamespaceMapping(ctx context.Context, _ *connect.Request[v1pb.ListNamespaceMappingRequest]) (*connect.Response[v1pb.ListNamespaceMappingResponse], error) {
	list, err := s.store.ListNamespaceMapping(ctx, &store.FindNamespaceMappingMessage{})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list namespace mappings"))
	}

	resp := &v1pb.ListNamespaceMappingResponse{}
	for _, m := range list {
		resp.Mappings = append(resp.Mappings, convertNamespaceMapping(m))
	}
	return connect.NewResponse(resp), nil
}

func (s *OpenLineageService) UpdateNamespaceMapping(ctx context.Context, req *connect.Request[v1pb.UpdateNamespaceMappingRequest]) (*connect.Response[v1pb.NamespaceMappingResource], error) {
	mapping := req.Msg.GetMapping()
	result, err := s.store.UpdateNamespaceMapping(ctx, req.Msg.GetId(), &store.NamespaceMappingMessage{
		Namespace:          mapping.GetNamespace(),
		InstanceResourceID: mapping.GetInstanceResourceId(),
		DatabaseName:       mapping.GetDatabaseName(),
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to update namespace mapping"))
	}
	return connect.NewResponse(convertNamespaceMapping(result)), nil
}

func (s *OpenLineageService) DeleteNamespaceMapping(ctx context.Context, req *connect.Request[v1pb.DeleteNamespaceMappingRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.store.DeleteNamespaceMapping(ctx, req.Msg.GetId()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to delete namespace mapping"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (s *OpenLineageService) CreateAPIKey(ctx context.Context, req *connect.Request[v1pb.CreateAPIKeyRequest]) (*connect.Response[v1pb.CreateAPIKeyResponse], error) {
	if req.Msg.GetDescription() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("description is required"))
	}

	user, ok := GetUserFromContext(ctx)
	createdBy := ""
	if ok && user != nil {
		createdBy = user.Email
	}

	plainKey, keyMsg, err := s.store.CreateOpenLineageAPIKey(ctx, req.Msg.GetDescription(), createdBy)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to create API key"))
	}

	return connect.NewResponse(&v1pb.CreateAPIKeyResponse{
		Key:    plainKey,
		ApiKey: convertAPIKey(keyMsg),
	}), nil
}

func (s *OpenLineageService) ListAPIKey(ctx context.Context, _ *connect.Request[v1pb.ListAPIKeyRequest]) (*connect.Response[v1pb.ListAPIKeyResponse], error) {
	list, err := s.store.ListOpenLineageAPIKey(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to list API keys"))
	}

	resp := &v1pb.ListAPIKeyResponse{}
	for _, k := range list {
		resp.ApiKeys = append(resp.ApiKeys, convertAPIKey(k))
	}
	return connect.NewResponse(resp), nil
}

func (s *OpenLineageService) RevokeAPIKey(ctx context.Context, req *connect.Request[v1pb.RevokeAPIKeyRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.store.RevokeOpenLineageAPIKey(ctx, req.Msg.GetId()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.Wrap(err, "failed to revoke API key"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func convertNamespaceMapping(m *store.NamespaceMappingMessage) *v1pb.NamespaceMappingResource {
	return &v1pb.NamespaceMappingResource{
		Id:                 m.ID,
		Namespace:          m.Namespace,
		InstanceResourceId: m.InstanceResourceID,
		DatabaseName:       m.DatabaseName,
		CreatedAt:          timestamppb.New(m.CreatedAt),
		UpdatedAt:          timestamppb.New(m.UpdatedAt),
	}
}

func convertAPIKey(k *store.OpenLineageAPIKeyMessage) *v1pb.APIKeyResource {
	res := &v1pb.APIKeyResource{
		Id:          k.ID,
		MaskedKey:   k.MaskedKey,
		Description: k.Description,
		CreatedBy:   k.CreatedBy,
		CreatedAt:   timestamppb.New(k.CreatedAt),
	}
	if k.LastUsedAt != nil {
		res.LastUsedAt = timestamppb.New(*k.LastUsedAt)
	}
	if k.RevokedAt != nil {
		res.RevokedAt = timestamppb.New(*k.RevokedAt)
	}
	return res
}

func convertOpenLineageRun(run *store.OpenLineageRunMessage, includePayload bool) *v1pb.OpenLineageRunResource {
	airflowLinks := openlineageplugin.DeriveAirflowLinks(run.RawPayload)
	res := &v1pb.OpenLineageRunResource{
		Id:                 run.ID,
		Guid:               run.GUID,
		TaskGuid:           run.TaskGUID,
		RunId:              run.RunID,
		JobNamespace:       run.JobNamespace,
		JobName:            run.JobName,
		JobType:            run.JobType,
		EventType:          run.EventType,
		Producer:           run.Producer,
		Source:             run.Source,
		Integration:        run.Integration,
		ProcessingType:     run.ProcessingType,
		ParentJobNamespace: run.ParentJobNamespace,
		ParentJobName:      run.ParentJobName,
		ParentRunId:        run.ParentRunID,
		RootJobNamespace:   run.RootJobNamespace,
		RootJobName:        run.RootJobName,
		RootRunId:          run.RootRunID,
		InputCount:         run.InputCount,
		OutputCount:        run.OutputCount,
		HasLineage:         run.HasLineage,
		AirflowDagUrl:      airflowLinks.DagURL,
		AirflowRunLogUrl:   airflowLinks.RunLogURL,
		CreatedAt:          timestamppb.New(run.CreatedAt),
		UpdatedAt:          timestamppb.New(run.UpdatedAt),
	}
	if run.EventTime != nil {
		res.EventTime = timestamppb.New(*run.EventTime)
	}
	if includePayload {
		res.RawPayload = string(run.RawPayload)
	}
	return res
}

func convertOpenLineageTask(task *store.OpenLineageTaskMessage) *v1pb.OpenLineageTaskResource {
	airflowLinks := openlineageplugin.DeriveAirflowLinks(task.LatestRawPayload)
	res := &v1pb.OpenLineageTaskResource{
		Id:                 task.ID,
		Guid:               task.GUID,
		JobNamespace:       task.JobNamespace,
		JobName:            task.JobName,
		JobType:            task.JobType,
		Integration:        task.Integration,
		ProcessingType:     task.ProcessingType,
		ParentJobNamespace: task.ParentJobNamespace,
		ParentJobName:      task.ParentJobName,
		RootJobNamespace:   task.RootJobNamespace,
		RootJobName:        task.RootJobName,
		LatestRunGuid:      task.LatestRunGUID,
		LatestRunId:        task.LatestRunID,
		LatestProducer:     task.LatestProducer,
		LatestSource:       task.LatestSource,
		AirflowDagUrl:      airflowLinks.DagURL,
		RunCount:           task.RunCount,
		LineageRunCount:    task.LineageRunCount,
		CreatedAt:          timestamppb.New(task.CreatedAt),
		UpdatedAt:          timestamppb.New(task.UpdatedAt),
	}
	if task.LatestEventTime != nil {
		res.LatestEventTime = timestamppb.New(*task.LatestEventTime)
	}
	return res
}
