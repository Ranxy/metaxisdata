package v1

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// qualityRunDefaultPageSize is what an unset page_size means for the run list,
// matching the RPC's documented default. A follow-up page keeps the size its token
// was issued with (parseLimitAndOffset restores it).
const qualityRunDefaultPageSize = 50

// ListQualityRuns lists the internal dispatch batches, newest first. A run is a
// read-only projection: it groups the executions one scheduler tick or one manual
// request planned for one target, and the run itself names that target.
func (s *DataQualityService) ListQualityRuns(ctx context.Context, req *connect.Request[v1pb.ListQualityRunsRequest]) (*connect.Response[v1pb.ListQualityRunsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	requested := int(req.Msg.GetPageSize())
	if requested == 0 && req.Msg.GetPageToken() == "" {
		requested = qualityRunDefaultPageSize
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:   req.Msg.GetPageToken(),
		limit:   requested,
		maximum: qualityListMaximum,
	})
	if err != nil {
		return nil, err
	}
	limitPlusOne := offset.limit + 1

	find := &store.FindQualityRunMessage{Limit: &limitPlusOne, Offset: &offset.offset}
	if err := applyQualityRunListFilters(find, req.Msg); err != nil {
		return nil, err
	}
	runs, err := s.store.ListQualityRuns(ctx, find)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list the quality runs")
	}
	runs, nextPageToken, err := paginate(runs, offset)
	if err != nil {
		return nil, err
	}

	runIDs := make([]int64, 0, len(runs))
	for _, run := range runs {
		runIDs = append(runIDs, run.ID)
	}
	if err := s.fillQualityRunRevisionRanges(ctx, runs, runIDs); err != nil {
		return nil, err
	}

	response := &v1pb.ListQualityRunsResponse{NextPageToken: nextPageToken}
	for _, run := range runs {
		response.QualityRuns = append(response.QualityRuns, convertQualityRun(workspaceID, run))
	}
	return connect.NewResponse(response), nil
}

// GetQualityRun reads one run: its target, state, verdict and counts.
func (s *DataQualityService) GetQualityRun(ctx context.Context, req *connect.Request[v1pb.GetQualityRunRequest]) (*connect.Response[v1pb.QualityRun], error) {
	workspaceID, runID, err := s.runResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	run, err := s.store.GetQualityRun(ctx, runID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read the quality run")
	}
	if run == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.Errorf("quality run %d not found", runID))
	}
	ranges, err := s.store.ListQualityRunRevisionRanges(ctx, []int64{runID})
	if err != nil {
		return nil, err
	}
	if revisionRange, ok := ranges[runID]; ok {
		run.RuleRevisionMin = revisionRange.Min
		run.RuleRevisionMax = revisionRange.Max
		run.RetryCount = revisionRange.RetryCount
	}
	return connect.NewResponse(convertQualityRun(workspaceID, run)), nil
}

// fillQualityRunRevisionRanges reads the revision range each run judged and puts
// it on the message. It is one aggregate query for the whole page, not one per
// run.
func (s *DataQualityService) fillQualityRunRevisionRanges(ctx context.Context, runs []*store.QualityRunMessage, runIDs []int64) error {
	ranges, err := s.store.ListQualityRunRevisionRanges(ctx, runIDs)
	if err != nil {
		return errors.Wrap(err, "failed to read the runs' rule revision ranges")
	}
	for _, run := range runs {
		if revisionRange, ok := ranges[run.ID]; ok {
			run.RuleRevisionMin = revisionRange.Min
			run.RuleRevisionMax = revisionRange.Max
			run.RetryCount = revisionRange.RetryCount
		}
	}
	return nil
}

// runResourceID parses a run name, proving its workspace, and returns the numeric
// row id.
func (s *DataQualityService) runResourceID(ctx context.Context, name string) (string, int64, error) {
	workspace, runID, err := parseQualityRunID(name)
	if err != nil {
		return "", 0, err
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", 0, err
	}
	return workspace, runID, nil
}

// applyQualityRunListFilters maps the request filters onto the store filter. Every
// enum filter is validated: an unknown value is a caller mistake, not "no
// filter", because silently widening the directory hides the mistake.
func applyQualityRunListFilters(find *store.FindQualityRunMessage, request *v1pb.ListQualityRunsRequest) error {
	switch trigger := storepb.QualityExecutionTrigger(request.GetTrigger()); trigger {
	case storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_UNSPECIFIED:
	case storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
		storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED:
		find.Trigger = &trigger
	default:
		return connectErrorInvalid("unknown quality execution trigger %d", trigger)
	}

	switch state := storepb.QualityExecutionState(request.GetState()); state {
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_UNSPECIFIED:
	case storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
		storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED:
		find.Status = &state
	default:
		return connectErrorInvalid("unknown quality execution state %d", state)
	}

	switch outcome := storepb.QualityStatus(request.GetOutcome()); outcome {
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
	case storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		find.Outcome = &outcome
	default:
		return connectErrorInvalid("unknown quality status %d", outcome)
	}

	if targetGUID := strings.TrimSpace(request.GetTargetGuid()); targetGUID != "" {
		find.TargetGUID = &targetGUID
	}
	if metaType := storepb.MetaType(request.GetMetaType()); metaType != storepb.MetaType_UNSPECIFIED {
		if _, ok := v1pb.MetaType_name[int32(metaType)]; !ok {
			return connectErrorInvalid("unknown meta type %d", metaType)
		}
		find.TargetType = &metaType
	}
	if instance := strings.TrimSpace(request.GetInstance()); instance != "" {
		instanceID := qualityInstanceSegment(instance)
		find.InstanceResourceID = &instanceID
	}
	if request.GetStartTime() != nil {
		startAt := request.GetStartTime().AsTime()
		find.StartedAfter = &startAt
	}
	if request.GetEndTime() != nil {
		endAt := request.GetEndTime().AsTime()
		find.StartedBefore = &endAt
	}
	return nil
}
