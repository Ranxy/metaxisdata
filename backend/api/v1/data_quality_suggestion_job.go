package v1

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// qualitySuggestionJobTargetLimit is how many targets one job may cover.
	qualitySuggestionJobTargetLimit = 20
	// qualitySuggestionJobDefaultPageSize is how many jobs a list returns when the
	// request does not say.
	qualitySuggestionJobDefaultPageSize = 20
)

// qualitySuggestionJobResourceID parses a job name and proves its workspace.
func (s *DataQualityService) qualitySuggestionJobResourceID(ctx context.Context, name string) (string, string, error) {
	workspace, resourceID, err := parseQualitySuggestionJobName(name)
	if err != nil {
		return "", "", connectErrorInvalid("%v", err)
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, resourceID, nil
}

// fetchQualitySuggestionJob reads one job by resource id, reporting a missing row
// as NotFound.
func (s *DataQualityService) fetchQualitySuggestionJob(ctx context.Context, resourceID string) (*store.QualitySuggestionJobMessage, error) {
	job, err := s.store.GetQualitySuggestionJob(ctx, &store.FindQualitySuggestionJobMessage{ResourceID: &resourceID})
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality suggestion job")
	}
	if job == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality suggestion job %q not found", resourceID))
	}
	return job, nil
}

// CreateQualitySuggestionJob queues one background drafting job. The request
// returns as soon as the job is stored; every target is resolved and its columns
// checked here, so the job the runner starts can only fail inside the model call.
func (s *DataQualityService) CreateQualitySuggestionJob(ctx context.Context, req *connect.Request[v1pb.CreateQualitySuggestionJobRequest]) (*connect.Response[v1pb.QualitySuggestionJob], error) {
	if req == nil {
		return nil, connectErrorInvalid("a request is required")
	}
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	intent := strings.TrimSpace(req.Msg.GetIntent())
	if intent == "" {
		return nil, connectErrorInvalid("intent is required")
	}
	if len(intent) > dataquality.MaxRuleDraftIntentChars {
		return nil, connectErrorInvalid("intent is longer than %d characters", dataquality.MaxRuleDraftIntentChars)
	}
	language := dataquality.NormalizePromptLanguage(req.Msg.GetLanguage())
	targets, err := s.snapshotSuggestionJobTargets(ctx, req.Msg.GetTargets())
	if err != nil {
		return nil, err
	}
	// The provider configuration is checked before the job is stored: a disabled
	// LLM must fail the submission, not every target of a queued job.
	if _, _, err := s.suggester.ResolveConfig(ctx); err != nil {
		return nil, err
	}
	// Best effort: the quota is not reserved, so concurrent submissions may
	// over-admit by one job. Each executed target still spends through Begin.
	if remaining := s.quota.RemainingAnalyses(); remaining < len(targets.GetTargets()) {
		return nil, connect.NewError(connect.CodeResourceExhausted, errors.Errorf(
			"the job needs %d analyses but only %d remain in today's data quality LLM quota; it resets at 00:00 UTC",
			len(targets.GetTargets()), remaining))
	}

	resourceID, err := common.RandomString(12)
	if err != nil {
		return nil, errors.New("failed to generate the quality suggestion job id")
	}
	created, err := s.store.CreateQualitySuggestionJob(ctx, &store.QualitySuggestionJobMessage{
		ResourceID: resourceID,
		Status:     storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
		Intent:     intent,
		Language:   language,
		Targets:    targets,
		CreatedBy:  principalIDPointer(currentUserID(ctx)),
		CreatedAt:  time.Now().UTC(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create the quality suggestion job")
	}
	return connect.NewResponse(convertQualitySuggestionJob(workspaceID, created)), nil
}

// snapshotSuggestionJobTargets validates the request's targets and renders the
// progress snapshot the job stores. Every target resolves against the registry and
// every named column must exist, so the runner starts from a known-good job.
func (s *DataQualityService) snapshotSuggestionJobTargets(ctx context.Context, request []*v1pb.QualityTarget) (*storepb.QualitySuggestionJobTargets, error) {
	if len(request) == 0 {
		return nil, connectErrorInvalid("at least one target is required")
	}
	if len(request) > qualitySuggestionJobTargetLimit {
		return nil, connectErrorInvalid("at most %d targets may be drafted at once", qualitySuggestionJobTargetLimit)
	}

	targets := &storepb.QualitySuggestionJobTargets{Targets: make([]*storepb.QualitySuggestionJobTarget, 0, len(request))}
	seen := make(map[string]struct{}, len(request))
	for index, target := range request {
		if target == nil {
			return nil, connectErrorInvalid("target %d is required", index+1)
		}
		guid := strings.TrimSpace(target.GetMetaGuid())
		if guid == "" {
			return nil, connectErrorInvalid("target %d needs a meta_guid", index+1)
		}
		metaType := storepb.MetaType(target.GetMetaType())
		if metaType == storepb.MetaType_UNSPECIFIED {
			return nil, connectErrorInvalid("target %d needs a meta_type", index+1)
		}
		key := fmt.Sprintf("%d|%s", metaType, guid)
		if _, ok := seen[key]; ok {
			return nil, connectErrorInvalid("target %q is listed more than once", guid)
		}
		seen[key] = struct{}{}

		columns := trimmedQualityColumns(target.GetColumns())
		if len(columns) > dataquality.MaxDraftColumns {
			return nil, connectErrorInvalid("target %q names more than %d columns", guid, dataquality.MaxDraftColumns)
		}
		resolution, err := dataquality.ResolveTarget(ctx, s.store, guid, metaType)
		if err != nil {
			return nil, err
		}
		for _, column := range columns {
			if _, ok := resolution.Target.Column(column); !ok {
				return nil, connectErrorInvalid("column %q does not exist on target %q", column, guid)
			}
		}
		targets.Targets = append(targets.Targets, &storepb.QualitySuggestionJobTarget{
			TargetGuid: guid,
			TargetType: int32(metaType),
			Path:       resolution.Target.Path,
			Columns:    columns,
			State:      storepb.QualitySuggestionJobTargetState_QUALITY_SUGGESTION_JOB_TARGET_STATE_PENDING,
		})
	}
	return targets, nil
}

// GetQualitySuggestionJob reads one job, including per-target progress.
func (s *DataQualityService) GetQualitySuggestionJob(ctx context.Context, req *connect.Request[v1pb.GetQualitySuggestionJobRequest]) (*connect.Response[v1pb.QualitySuggestionJob], error) {
	workspaceID, resourceID, err := s.qualitySuggestionJobResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	job, err := s.fetchQualitySuggestionJob(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualitySuggestionJob(workspaceID, job)), nil
}

// ListQualitySuggestionJobs lists jobs scoped to the caller's workspace, newest
// first.
func (s *DataQualityService) ListQualitySuggestionJobs(ctx context.Context, req *connect.Request[v1pb.ListQualitySuggestionJobsRequest]) (*connect.Response[v1pb.ListQualitySuggestionJobsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	limit := int(req.Msg.GetPageSize())
	if limit == 0 && req.Msg.GetPageToken() == "" {
		limit = qualitySuggestionJobDefaultPageSize
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   limit,
		maximum: qualityListMaximum,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindQualitySuggestionJobMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	if err := applyQualitySuggestionJobListFilters(find, req.Msg); err != nil {
		return nil, err
	}
	jobs, err := s.store.ListQualitySuggestionJobs(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality suggestion jobs")
	}
	jobs, nextPageToken, err := paginate(jobs, offset)
	if err != nil {
		return nil, err
	}

	response := &v1pb.ListQualitySuggestionJobsResponse{NextPageToken: nextPageToken}
	for _, job := range jobs {
		response.QualitySuggestionJobs = append(response.QualitySuggestionJobs, convertQualitySuggestionJob(workspaceID, job))
	}
	return connect.NewResponse(response), nil
}

// applyQualitySuggestionJobListFilters maps the request filters onto the store
// filter.
func applyQualitySuggestionJobListFilters(find *store.FindQualitySuggestionJobMessage, request *v1pb.ListQualitySuggestionJobsRequest) error {
	switch status := storepb.QualitySuggestionJobStatus(request.GetStatus()); status {
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_UNSPECIFIED:
	case storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_PENDING,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_RUNNING,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_COMPLETED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_FAILED,
		storepb.QualitySuggestionJobStatus_QUALITY_SUGGESTION_JOB_STATUS_CANCELED:
		find.Statuses = []storepb.QualitySuggestionJobStatus{status}
	default:
		return connectErrorInvalid("unknown quality suggestion job status %d", status)
	}
	return nil
}

// CancelQualitySuggestionJob asks one job to stop. Canceling a terminal job is
// idempotent and returns it unchanged.
func (s *DataQualityService) CancelQualitySuggestionJob(ctx context.Context, req *connect.Request[v1pb.CancelQualitySuggestionJobRequest]) (*connect.Response[v1pb.QualitySuggestionJob], error) {
	workspaceID, resourceID, err := s.qualitySuggestionJobResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	job, err := s.store.CancelQualitySuggestionJob(ctx, resourceID, time.Now().UTC())
	if err != nil {
		return nil, errors.Wrap(err, "failed to cancel the quality suggestion job")
	}
	return connect.NewResponse(convertQualitySuggestionJob(workspaceID, job)), nil
}

// GetQualitySuggestionJobTargetDebug returns the raw request/response bodies of
// one failed target when runtime debug mode captured them. The bodies live in the
// debug log for seven days, so a pruned capture is reported as unavailable rather
// than an error; the correlation id and classifier code always come back so the
// client can still explain the failure.
func (s *DataQualityService) GetQualitySuggestionJobTargetDebug(ctx context.Context, req *connect.Request[v1pb.GetQualitySuggestionJobTargetDebugRequest]) (*connect.Response[v1pb.GetQualitySuggestionJobTargetDebugResponse], error) {
	if req == nil {
		return nil, connectErrorInvalid("a request is required")
	}
	_, resourceID, err := s.qualitySuggestionJobResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	job, err := s.fetchQualitySuggestionJob(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	targetIndex := int(req.Msg.GetTargetIndex())
	if targetIndex < 0 || targetIndex >= len(job.Targets.GetTargets()) {
		return nil, connectErrorInvalid("target_index %d is out of range for a job with %d targets",
			targetIndex, len(job.Targets.GetTargets()))
	}
	diagnostic := job.Targets.GetTargets()[targetIndex].GetDiagnostic()
	response := &v1pb.GetQualitySuggestionJobTargetDebugResponse{
		CorrelationId: diagnostic.GetCorrelationId(),
		ErrorCode:     diagnostic.GetErrorCode(),
	}
	// Without runtime debug there was no capture; a missing correlation id means
	// the same thing. Both are reported as unavailable, not as an error.
	if !diagnostic.GetDebugCaptured() || diagnostic.GetCorrelationId() == "" {
		return connect.NewResponse(response), nil
	}
	entry, err := s.store.GetLLMDebugLogByCorrelation(ctx, diagnostic.GetCorrelationId())
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the LLM debug log")
	}
	if entry == nil {
		// The seven-day retention pruned it.
		return connect.NewResponse(response), nil
	}
	response.Available = true
	response.RequestBody = entry.RequestBody
	response.ResponseBody = entry.ResponseBody
	if response.ErrorCode == "" {
		response.ErrorCode = entry.ErrorCode
	}
	return connect.NewResponse(response), nil
}
