package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityAgentEnrollmentPrefix = "qualityAgentEnrollments/"
	// maxQualityAgentEnrollmentPageSize is the page size the list documents.
	maxQualityAgentEnrollmentPageSize = 1000
)

// AgentEnrollmentStatusProvider is the enrollment runner's process-local scan signal.
type AgentEnrollmentStatusProvider interface {
	LastEnrollmentScan() time.Time
}

// ListQualityAgentEnrollments lists the runner's enrollment queue.
func (s *DataQualityService) ListQualityAgentEnrollments(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentEnrollmentsRequest]) (*connect.Response[v1pb.ListQualityAgentEnrollmentsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      maxQualityAgentEnrollmentPageSize,
		defaultLimit: qualityDefaultPageSize,
	})
	if err != nil {
		return nil, err
	}
	limit := offset.limit + 1
	find := &store.FindQualityAgentEnrollmentMessage{
		WorkspaceID: workspaceID,
		Limit:       &limit,
		Offset:      &offset.offset,
	}
	if agentName := strings.TrimSpace(req.Msg.GetAgent()); agentName != "" {
		_, agentResourceID, err := s.qualityAgentResourceID(ctx, agentName)
		if err != nil {
			return nil, err
		}
		agent, err := s.fetchQualityAgent(ctx, agentResourceID)
		if err != nil {
			return nil, err
		}
		find.AgentID = agent.ID
	}
	if database := strings.TrimSpace(req.Msg.GetDatabase()); database != "" {
		find.DatabaseRef = &database
	}
	switch state := req.Msg.GetState(); state {
	case v1pb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_UNSPECIFIED:
		// Every state.
	default:
		find.States = []storepb.QualityAgentEnrollmentState{storepb.QualityAgentEnrollmentState(state)}
	}
	find.Mode = storepb.QualityAgentSessionMode(req.Msg.GetMode())
	find.Search = req.Msg.GetSearch()
	enrollments, err := s.store.ListQualityAgentEnrollments(ctx, find)
	if err != nil {
		return nil, err
	}
	enrollments, nextPageToken, err := paginate(enrollments, offset)
	if err != nil {
		return nil, err
	}
	sessionIDs := make([]int64, 0, len(enrollments))
	for _, enrollment := range enrollments {
		if enrollment.SessionID != nil {
			sessionIDs = append(sessionIDs, *enrollment.SessionID)
		}
	}
	sessionNames, err := s.store.ListQualityAgentSessionResourceIDs(ctx, workspaceID, sessionIDs)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityAgentEnrollmentsResponse{NextPageToken: nextPageToken}
	for _, enrollment := range enrollments {
		response.QualityAgentEnrollments = append(response.QualityAgentEnrollments,
			convertQualityAgentEnrollmentToV1(workspaceID, enrollment, sessionNames))
	}
	return connect.NewResponse(response), nil
}

// GetQualityAgentEnrollmentSummary reads the queue's counts by state and database.
func (s *DataQualityService) GetQualityAgentEnrollmentSummary(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentEnrollmentSummaryRequest]) (*connect.Response[v1pb.QualityAgentEnrollmentSummary], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	counts, err := s.store.CountQualityAgentEnrollmentsByStateAll(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	databases, err := s.store.QualityAgentEnrollmentSummaryAll(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	response := &v1pb.QualityAgentEnrollmentSummary{}
	for state, count := range counts {
		response.States = append(response.States, &v1pb.QualityAgentEnrollmentStateCount{
			State: v1pb.QualityAgentEnrollmentState(state),
			Count: int32(count),
		})
	}
	for _, database := range databases {
		response.Databases = append(response.Databases, &v1pb.QualityAgentDatabaseEnrollmentSummary{
			Database:   database.DatabaseRef,
			Pending:    int32(database.Pending),
			Active:     int32(database.Active),
			Dormant:    int32(database.Dormant),
			Excluded:   int32(database.Excluded),
			OutOfScope: int32(database.OutOfScope),
			Gone:       int32(database.Gone),
		})
	}
	if s.enrollmentRunner != nil {
		if scan := s.enrollmentRunner.LastEnrollmentScan(); !scan.IsZero() {
			response.LastScanTime = timestamppb.New(scan)
		}
	}
	return connect.NewResponse(response), nil
}

func convertQualityAgentEnrollmentToV1(workspaceID string, enrollment *store.QualityAgentEnrollmentMessage, sessionNames map[int64]string) *v1pb.QualityAgentEnrollment {
	if enrollment == nil {
		return nil
	}
	converted := &v1pb.QualityAgentEnrollment{
		Name:            common.FormatWorkspace(workspaceID) + "/" + qualityAgentEnrollmentPrefix + enrollment.ResourceID,
		Target:          convertQualityTargetIdentityToV1(enrollment.TargetGUID, enrollment.TargetType),
		Database:        enrollment.DatabaseRef,
		State:           v1pb.QualityAgentEnrollmentState(enrollment.State),
		Mode:            v1pb.QualityAgentSessionMode(enrollment.Mode),
		Priority:        enrollment.Priority,
		PriorityReason:  enrollment.PriorityReason,
		BlockedReason:   enrollment.BlockedReason,
		TriageCount:     enrollment.TriageCount,
		LastTriagedTime: timestampOrNil(enrollment.LastTriagedAt),
		NextReviewTime:  timestampOrNil(enrollment.NextReviewAt),
		DiscoveredTime:  timestamppb.New(enrollment.DiscoveredAt),
		LastSeenTime:    timestamppb.New(enrollment.LastSeenAt),
		Revision:        enrollment.Revision,
		CreateTime:      timestamppb.New(enrollment.CreatedAt),
		UpdateTime:      timestamppb.New(enrollment.UpdatedAt),
	}
	if enrollment.SessionID != nil {
		if resourceID := sessionNames[*enrollment.SessionID]; resourceID != "" {
			converted.Session = common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + resourceID
		}
	}
	return converted
}
