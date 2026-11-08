package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
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
	counts, err := s.store.CountQualityAgentEnrollmentsByState(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	databases, err := s.store.QualityAgentEnrollmentSummary(ctx, workspaceID)
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

// UpdateQualityAgentEnrollment applies a person's queue change (exclude, include or
// switch the mode) with a revision compare-and-set.
func (s *DataQualityService) UpdateQualityAgentEnrollment(ctx context.Context, req *connect.Request[v1pb.UpdateQualityAgentEnrollmentRequest]) (*connect.Response[v1pb.QualityAgentEnrollment], error) {
	workspaceID, resourceID, err := s.qualityAgentEnrollmentResourceID(ctx, req.Msg.GetEnrollment().GetName())
	if err != nil {
		return nil, err
	}
	enrollment, err := s.fetchQualityAgentEnrollment(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	expectedRevision := req.Msg.GetExpectedRevision()
	if expectedRevision <= 0 {
		return nil, connectErrorInvalid("expected_revision is required")
	}
	paths := req.Msg.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		return nil, connectErrorInvalid("an update needs at least one field in update_mask")
	}
	for _, path := range paths {
		switch path {
		case "state":
			state := req.Msg.GetEnrollment().GetState()
			switch state {
			case v1pb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_EXCLUDED:
				updated, applied, err := s.store.ExcludeQualityAgentEnrollment(ctx, resourceID, expectedRevision, int32(currentUserID(ctx)))
				if err != nil {
					return nil, err
				}
				if !applied {
					return nil, connect.NewError(connect.CodeAborted,
						errors.New("the enrollment changed since it was read; reload and try again"))
				}
				enrollment = updated
			case v1pb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING:
				updated, applied, err := s.store.IncludeQualityAgentEnrollment(ctx, resourceID, expectedRevision)
				if err != nil {
					return nil, err
				}
				if !applied {
					return nil, connect.NewError(connect.CodeAborted,
						errors.New("the enrollment changed since it was read; reload and try again"))
				}
				enrollment = updated
			default:
				return nil, connectErrorInvalid("only PENDING and EXCLUDED are accepted as a requested state")
			}
			expectedRevision = enrollment.Revision
		case "mode":
			mode := req.Msg.GetEnrollment().GetMode()
			switch mode {
			case v1pb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR,
				v1pb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE:
			default:
				return nil, connectErrorInvalid("mode must be MONITOR or TRIAGE")
			}
			updated, applied, err := s.store.SetQualityAgentEnrollmentMode(ctx, resourceID, expectedRevision,
				storepb.QualityAgentSessionMode(mode))
			if err != nil {
				return nil, err
			}
			if !applied {
				return nil, connect.NewError(connect.CodeAborted,
					errors.New("the enrollment changed since it was read; reload and try again"))
			}
			enrollment = updated
			expectedRevision = enrollment.Revision
		default:
			return nil, connectErrorInvalid("unsupported update path %q", path)
		}
	}
	// A mode change must reach the live session too; otherwise an enrollment switched to
	// TRIAGE would keep monitoring until the session were recreated.
	if requestHasPath(paths, "mode") {
		if session, err := s.store.GetQualityAgentSessionByTarget(ctx, workspaceID, enrollment.TargetGUID); err == nil && session != nil && session.ArchivedAt == nil {
			if _, modeErr := s.store.UpdateQualityAgentSessionMode(ctx, session.ResourceID, enrollment.Mode); modeErr != nil {
				return nil, modeErr
			}
		}
	}
	// Excluding a target pauses the agent's conversation with it; keep the session as
	// the readable transcript and stop its wakes instead of leaving it enabled.
	if requestHasPath(paths, "state") && req.Msg.GetEnrollment().GetState() == v1pb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_EXCLUDED {
		if err := s.archiveAgentSessionOfTarget(ctx, workspaceID, enrollment.TargetGUID); err != nil {
			return nil, err
		}
		// Archiving wrote a second revision bump on the enrollment; return the current row.
		refreshed, err := s.fetchQualityAgentEnrollment(ctx, resourceID)
		if err != nil {
			return nil, err
		}
		enrollment = refreshed
	}
	sessionNames, err := s.enrollmentSessionNames(ctx, workspaceID, enrollment)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityAgentEnrollmentToV1(workspaceID, enrollment, sessionNames)), nil
}

// requestHasPath reports whether an update mask names one path.
func requestHasPath(paths []string, path string) bool {
	for _, candidate := range paths {
		if candidate == path {
			return true
		}
	}
	return false
}

// RetryQualityAgentEnrollment requeues one enrollment now with a priority bump.
func (s *DataQualityService) RetryQualityAgentEnrollment(ctx context.Context, req *connect.Request[v1pb.RetryQualityAgentEnrollmentRequest]) (*connect.Response[v1pb.QualityAgentEnrollment], error) {
	workspaceID, resourceID, err := s.qualityAgentEnrollmentResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	if _, err := s.fetchQualityAgentEnrollment(ctx, resourceID); err != nil {
		return nil, err
	}
	expectedRevision := req.Msg.GetExpectedRevision()
	if expectedRevision <= 0 {
		return nil, connectErrorInvalid("expected_revision is required")
	}
	updated, applied, err := s.store.RetryQualityAgentEnrollment(ctx, resourceID, expectedRevision)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, connect.NewError(connect.CodeAborted,
			errors.New("the enrollment changed since it was read; reload and try again"))
	}
	// If the target already has a live session, let the next wake use the new queue
	// decision instead of waiting for its idle cadence.
	if session, err := s.store.GetQualityAgentSessionByTarget(ctx, workspaceID, updated.TargetGUID); err == nil && session != nil && session.ArchivedAt == nil {
		if _, wakeErr := s.store.AdvanceQualityAgentSessionWake(ctx, session.ResourceID, time.Now().UTC()); wakeErr != nil {
			return nil, wakeErr
		}
	}
	sessionNames, err := s.enrollmentSessionNames(ctx, workspaceID, updated)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityAgentEnrollmentToV1(workspaceID, updated, sessionNames)), nil
}

// enrollmentSessionNames resolves the session name of one enrollment row.
func (s *DataQualityService) enrollmentSessionNames(ctx context.Context, workspaceID string, enrollment *store.QualityAgentEnrollmentMessage) (map[int64]string, error) {
	if enrollment == nil || enrollment.SessionID == nil {
		return map[int64]string{}, nil
	}
	return s.store.ListQualityAgentSessionResourceIDs(ctx, workspaceID, []int64{*enrollment.SessionID})
}

// archiveAgentSessionOfTarget archives the target's live session after a person
// excluded the enrollment. A missing or already archived session is not an error.
func (s *DataQualityService) archiveAgentSessionOfTarget(ctx context.Context, workspaceID, targetGUID string) error {
	session, err := s.store.GetQualityAgentSessionByTarget(ctx, workspaceID, targetGUID)
	if err != nil {
		return err
	}
	if session == nil || session.ArchivedAt != nil {
		return nil
	}
	archived, err := s.store.ArchiveQualityAgentSession(ctx, session.ResourceID, session.Revision, time.Now().UTC())
	if err != nil {
		return err
	}
	if !archived {
		return connect.NewError(connect.CodeAborted,
			errors.New("the session changed since it was read; reload and try again"))
	}
	return nil
}

// qualityAgentEnrollmentResourceID resolves an enrollment name to its workspace and id.
func (s *DataQualityService) qualityAgentEnrollmentResourceID(ctx context.Context, name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityAgentEnrollmentPrefix)
	if err != nil {
		return "", "", connectErrorInvalid("invalid quality agent enrollment name")
	}
	if err := s.validateResourceWorkspace(ctx, tokens[0]); err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
}

// fetchQualityAgentEnrollment reads one enrollment and maps absence to NotFound.
func (s *DataQualityService) fetchQualityAgentEnrollment(ctx context.Context, resourceID string) (*store.QualityAgentEnrollmentMessage, error) {
	enrollment, err := s.store.GetQualityAgentEnrollment(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if enrollment == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent enrollment %q not found", resourceID))
	}
	return enrollment, nil
}

// convertQualityAgentEnrollmentToV1 renders one queue row.
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
