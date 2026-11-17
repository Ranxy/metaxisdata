package v1

import (
	"context"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityAgentSessionPrefix = "qualityAgentSessions/"
)

// AgentRunnerStatusProvider is the process-local liveness signal of the session runner.
// A zero time means the runner has not ticked yet or has stopped.
type AgentRunnerStatusProvider interface {
	LastTick() time.Time
}

func (s *DataQualityService) ListQualityAgentSessions(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentSessionsRequest]) (*connect.Response[v1pb.ListQualityAgentSessionsResponse], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      qualityListMaximum,
		defaultLimit: qualityDefaultPageSize,
	})
	if err != nil {
		return nil, err
	}
	limit := offset.limit + 1
	find := &store.FindQualityAgentSessionMessage{WorkspaceID: &workspaceID, Kind: 1, Limit: &limit, Offset: &offset.offset}
	if agentName := strings.TrimSpace(req.Msg.GetAgent()); agentName != "" {
		_, agentResourceID, err := s.qualityAgentResourceID(ctx, agentName)
		if err != nil {
			return nil, err
		}
		agent, err := s.fetchQualityAgent(ctx, agentResourceID)
		if err != nil {
			return nil, err
		}
		find.AgentID = &agent.ID
	}
	if guid := strings.TrimSpace(req.Msg.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	if database := strings.TrimSpace(req.Msg.GetDatabase()); database != "" {
		find.DatabaseRef = &database
	}
	find.Source = storepb.QualityAgentSessionSource(req.Msg.GetSource())
	find.Mode = storepb.QualityAgentSessionMode(req.Msg.GetMode())
	switch state := req.Msg.GetState(); state {
	case v1pb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_UNSPECIFIED:
		// Every non-archived session.
	case v1pb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ARCHIVED:
		find.IncludeArchived = true
		find.States = []storepb.QualityAgentSessionState{storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ARCHIVED}
	case v1pb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED,
		v1pb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_PAUSED,
		v1pb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED:
		find.States = []storepb.QualityAgentSessionState{storepb.QualityAgentSessionState(state)}
	default:
		return nil, connectErrorInvalid("unknown session state %d", state)
	}
	messages, err := s.store.ListQualityAgentSessions(ctx, find)
	if err != nil {
		return nil, err
	}
	messages, nextPageToken, err := paginate(messages, offset)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	response := &v1pb.ListQualityAgentSessionsResponse{NextPageToken: nextPageToken}
	for _, message := range messages {
		response.QualityAgentSessions = append(response.QualityAgentSessions, convertQualityAgentSessionToV1(workspaceID, message, now))
	}
	return connect.NewResponse(response), nil
}

// GetQualityAgentSession reads one session.
func (s *DataQualityService) GetQualityAgentSession(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentSessionRequest]) (*connect.Response[v1pb.QualityAgentSession], error) {
	workspaceID, resourceID, err := s.qualityAgentSessionResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	message, err := s.fetchQualityAgentSession(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityAgentSessionToV1(workspaceID, message, time.Now().UTC())), nil
}

func (s *DataQualityService) ResumeQualityAgentSession(ctx context.Context, req *connect.Request[v1pb.ResumeQualityAgentSessionRequest]) (*connect.Response[v1pb.QualityAgentSession], error) {
	now := time.Now().UTC()
	session, err := s.transitionQualityAgentSession(ctx, req.Msg.GetName(), req.Msg.GetExpectedRevision(),
		storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED, &now)
	if err != nil {
		return nil, err
	}
	// The queued row may still carry the admission hint that it was skipped because a
	// person paused the session; resuming clears it.
	workspaceID, err := s.workspaceID(ctx, "workspaces/-")
	if err == nil && session != nil {
		_, resourceID, nameErr := s.qualityAgentSessionResourceID(ctx, req.Msg.GetName())
		if nameErr == nil {
			if stored, readErr := s.store.GetQualityAgentSession(ctx, resourceID); readErr == nil && stored != nil {
				if enrollment, findErr := s.store.GetQualityAgentEnrollmentByTarget(ctx, workspaceID, stored.AgentID, stored.TargetGUID); findErr == nil && enrollment != nil {
					_ = s.store.SetQualityAgentEnrollmentBlockedReason(ctx, enrollment.ResourceID, "")
				}
			}
		}
	}
	return session, nil
}

func (s *DataQualityService) ListQualityAgentSessionMessages(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentSessionMessagesRequest]) (*connect.Response[v1pb.ListQualityAgentSessionMessagesResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentSessionResourceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	session, err := s.fetchQualityAgentSession(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	afterSeq := req.Msg.GetAfterSeq()
	if afterSeq < 0 {
		return nil, connectErrorInvalid("after_seq cannot be negative")
	}
	if afterSeq > 0 && req.Msg.GetPageToken() != "" {
		return nil, connectErrorInvalid("after_seq and page_token are mutually exclusive")
	}
	offset, err := parseLimitAndOffset(&pageSize{
		token:        req.Msg.GetPageToken(),
		limit:        int(req.Msg.GetPageSize()),
		maximum:      qualityListMaximum,
		defaultLimit: qualityDefaultPageSize,
	})
	if err != nil {
		return nil, err
	}
	limit := offset.limit + 1
	find := &store.FindQualityAgentTranscriptMessage{SessionID: session.ID, Limit: limit}
	if afterSeq > 0 {
		find.AfterSeq = afterSeq
	} else {
		// The newest page: read backwards, then hand the client chronological order.
		find.NewestFirst = true
		find.Offset = offset.offset
	}
	messages, err := s.store.ListQualityAgentTranscriptMessages(ctx, find)
	if err != nil {
		return nil, err
	}
	more := len(messages) > offset.limit
	if more {
		messages = messages[:offset.limit]
	}
	if find.NewestFirst {
		slicesReverse(messages)
	}
	response := &v1pb.ListQualityAgentSessionMessagesResponse{}
	if find.AfterSeq == 0 && more {
		nextPageToken, tokenErr := offset.getNextPageToken()
		if tokenErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(tokenErr, "failed to marshal next page token"))
		}
		response.NextPageToken = nextPageToken
	}
	for _, message := range messages {
		response.QualityAgentSessionMessages = append(response.QualityAgentSessionMessages,
			convertQualityAgentMessageToV1(workspaceID, session.ResourceID, message))
	}
	return connect.NewResponse(response), nil
}

// GetQualityAgentRunnerStatus reads the runner's live state and budget spend.
func (s *DataQualityService) GetQualityAgentRunnerStatus(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentRunnerStatusRequest]) (*connect.Response[v1pb.QualityAgentRunnerStatus], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return nil, errLoadQualitySettings(err)
	}
	now := time.Now().UTC()
	running, err := s.store.CountQualityAgentSessionsByLeaseAll(ctx, workspaceID, now)
	if err != nil {
		return nil, err
	}
	enabled, err := s.store.CountQualityAgentSessionsByStateAll(ctx, workspaceID, storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED)
	if err != nil {
		return nil, err
	}
	blocked, err := s.store.CountQualityAgentSessionsByStateAll(ctx, workspaceID, storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED)
	if err != nil {
		return nil, err
	}
	tokens, err := s.store.SumQualityAgentSessionTokensSinceAll(ctx, workspaceID, now.Add(-time.Hour))
	if err != nil {
		return nil, err
	}
	wakes, err := s.store.CountQualityAgentSessionWakesSinceAll(ctx, workspaceID, now.Add(-time.Hour))
	if err != nil {
		return nil, err
	}
	lastActivity, err := s.store.LastQualityAgentSessionActivityAll(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	enrollments, err := s.store.CountQualityAgentEnrollmentsByStateAll(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	idle := enabled - running
	if idle < 0 {
		idle = 0
	}
	response := &v1pb.QualityAgentRunnerStatus{
		Enabled:                 settings.AgentRunnerEnabled,
		AutonomyEnabled:         settings.AgentAutonomyEnabled,
		LlmConfigured:           s.llmConfigured(ctx),
		RunningSessions:         running,
		IdleSessions:            idle,
		BlockedSessions:         blocked,
		TokensLastHour:          tokens,
		TokensPerHourLimit:      settings.AgentRunnerTokensPerHour,
		WakesLastHour:           int32(wakes),
		WakesPerHourLimit:       int32(settings.AgentRunnerWakesPerHour),
		MaxConcurrentSessions:   int32(settings.AgentRunnerMaxConcurrentSessions),
		CompactionTriggerTokens: int32(settings.AgentRunnerCompactionTriggerTokens),
		IdleWakeSeconds:         int32(settings.AgentRunnerIdleWakeSeconds),
		LastActivityTime:        timestampOrNil(lastActivity),
		PendingEnrollments:      int32(enrollments[storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_PENDING]),
		ActiveEnrollments:       int32(enrollments[storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_ACTIVE]),
		DormantEnrollments:      int32(enrollments[storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_DORMANT]),
		ExcludedEnrollments:     int32(enrollments[storepb.QualityAgentEnrollmentState_QUALITY_AGENT_ENROLLMENT_STATE_EXCLUDED]),
		MaxNewSessionsPerHour:   int32(settings.AgentRunnerMaxNewSessionsPerHour),
		MaxActiveSessions:       int32(settings.AgentRunnerMaxActiveSessions),
		AutoEnrollEnabled:       settings.AgentRunnerAutoEnroll,
	}
	if s.agentRunner != nil {
		if tick := s.agentRunner.LastTick(); !tick.IsZero() {
			response.RunnerLastTickTime = timestamppb.New(tick)
		}
	}
	if s.enrollmentRunner != nil {
		if scan := s.enrollmentRunner.LastEnrollmentScan(); !scan.IsZero() {
			response.EnrollmentLastScanTime = timestamppb.New(scan)
		}
	}
	return connect.NewResponse(response), nil
}

// GetQualityAgentMetrics reads the runner's evaluation: session states, token spend,
// the acceptance of its proposed change requests and its investigation verdicts.
func (s *DataQualityService) GetQualityAgentMetrics(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentMetricsRequest]) (*connect.Response[v1pb.QualityAgentMetrics], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	metrics, err := s.store.GetQualityAgentSessionMetrics(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	response := &v1pb.QualityAgentMetrics{
		TokensTotal:                      metrics.TokensTotal,
		TokensLastHour:                   metrics.TokensLastHour,
		AgentChangeRequestsTotal:         metrics.AgentChangeRequestsTotal,
		AgentChangeRequestsApplied:       metrics.AgentChangeRequestsApplied,
		AgentChangeRequestsRejected:      metrics.AgentChangeRequestsRejected,
		AgentChangeRequestsPending:       metrics.AgentChangeRequestsPending,
		AgentChangeRequestsApproved:      metrics.AgentChangeRequestsApproved,
		AgentChangeRequestsExpired:       metrics.AgentChangeRequestsExpired,
		InvestigationsTotal:              metrics.InvestigationsTotal,
		InvestigationsInconclusive:       metrics.InvestigationsInconclusive,
		InvestigationsRootCause:          metrics.InvestigationsRootCause,
		InvestigationsContributingFactor: metrics.InvestigationsContributing,
		InvestigationsUnrelated:          metrics.InvestigationsUnrelated,
	}
	for _, state := range []storepb.QualityAgentSessionState{
		storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED,
		storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_PAUSED,
		storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED,
		storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ARCHIVED,
	} {
		response.Sessions = append(response.Sessions, &v1pb.QualityAgentSessionStateCount{
			State: v1pb.QualityAgentSessionState(state),
			Count: metrics.StateCounts[state],
		})
	}
	for _, target := range metrics.TopTargets {
		response.TopTargets = append(response.TopTargets, &v1pb.QualityAgentTargetCost{
			Target:       &v1pb.QualityTarget{MetaGuid: target.TargetGUID, MetaType: v1pb.MetaType(target.TargetType)},
			TokensUsed:   target.TokensUsed,
			SessionCount: target.SessionCount,
		})
	}
	return connect.NewResponse(response), nil
}

// transitionQualityAgentSession applies one human state change with a revision CAS.
func (s *DataQualityService) transitionQualityAgentSession(ctx context.Context, name string, expectedRevision int32, state storepb.QualityAgentSessionState, nextWake *time.Time) (*connect.Response[v1pb.QualityAgentSession], error) {
	workspaceID, resourceID, err := s.qualityAgentSessionResourceID(ctx, name)
	if err != nil {
		return nil, err
	}
	if _, err := s.fetchQualityAgentSession(ctx, resourceID); err != nil {
		return nil, err
	}
	message, applied, err := s.store.UpdateQualityAgentSessionState(ctx, resourceID, expectedRevision, state, "", "", nextWake)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, connect.NewError(connect.CodeAborted,
			errors.New("the session changed since it was read; reload and try again"))
	}
	return connect.NewResponse(convertQualityAgentSessionToV1(workspaceID, message, time.Now().UTC())), nil
}

// qualityAgentSessionResourceID resolves a session name to its workspace and resource id.
func (s *DataQualityService) qualityAgentSessionResourceID(ctx context.Context, name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityAgentSessionPrefix)
	if err != nil {
		return "", "", connectErrorInvalid("invalid quality agent session name")
	}
	workspace, err := s.canonicalAgentWorkspace(ctx, tokens[0])
	if err != nil {
		return "", "", err
	}
	if err := s.validateResourceWorkspace(ctx, workspace); err != nil {
		return "", "", err
	}
	return workspace, tokens[1], nil
}

func (s *DataQualityService) fetchQualityAgentSession(ctx context.Context, resourceID string) (*store.QualityAgentSessionMessage, error) {
	message, err := s.store.GetQualityAgentSession(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if message == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent session %q not found", resourceID))
	}
	return message, nil
}

// convertQualityAgentSessionToV1 renders one session. "Running" is derived from the
// lease, never from the durable state.
func convertQualityAgentSessionToV1(workspaceID string, message *store.QualityAgentSessionMessage, now time.Time) *v1pb.QualityAgentSession {
	if message == nil {
		return nil
	}
	return &v1pb.QualityAgentSession{
		Name:             common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + message.ResourceID,
		Target:           convertQualityAgentSessionTargetToV1(message),
		Database:         message.DatabaseRef,
		Agent:            common.FormatUserUID(int(message.AgentPrincipalID)),
		Objective:        message.Objective,
		State:            v1pb.QualityAgentSessionState(message.State),
		BlockedCode:      message.BlockedCode,
		BlockedReason:    message.BlockedReason,
		Revision:         message.Revision,
		WakeCount:        message.WakeCount,
		TokensUsed:       message.TokensUsed,
		ContextTokens:    message.ContextTokens,
		NextWakeTime:     timestampOrNil(message.NextWakeAt),
		LastActivityTime: timestampOrNil(message.LastActivityAt),
		LeaseExpireTime:  timestampOrNil(message.LeaseUntil),
		RestartCount:     message.RestartCount,
		Running:          message.LeaseUntil != nil && message.LeaseUntil.After(now),
		Source:           v1pb.QualityAgentSessionSource(message.Source),
		Mode:             v1pb.QualityAgentSessionMode(message.Mode),
		CreateTime:       timestamppb.New(message.CreatedAt),
		UpdateTime:       timestamppb.New(message.UpdatedAt),
		ArchiveTime:      timestampOrNil(message.ArchivedAt),
	}
}

// convertQualityAgentSessionTargetToV1 renders a session's target with its display
// path. A session stores only the target's identity, while the other quality surfaces
// read the path snapshot that hangs off a rule; without a path the session list and
// the transcript page show the escaped GUID. A target GUID is exactly the identity
// path (`instance;database;schema;object[,column]`, see common.BuildMetaGUID), so the
// display path is reconstructed from it. An unparseable GUID keeps an empty path and
// the UI falls back to the GUID.
func convertQualityAgentSessionTargetToV1(message *store.QualityAgentSessionMessage) *v1pb.QualityTarget {
	return convertQualityTargetIdentityToV1(message.TargetGUID, message.TargetType)
}

// convertQualityTargetIdentityToV1 renders a target from its GUID and object type,
// reconstructing the display path from the GUID segments.
func convertQualityTargetIdentityToV1(targetGUID string, objectType storepb.MetaType) *v1pb.QualityTarget {
	target := &v1pb.QualityTarget{
		MetaGuid: targetGUID,
		MetaType: v1pb.MetaType(objectType),
	}
	segments := common.SplitMetaGUID(targetGUID)
	if len(segments) < 2 {
		return target
	}
	path := &v1pb.QualityTargetPath{Instance: segments[0], Database: segments[1]}
	if len(segments) > 2 {
		path.Schema = segments[2]
	}
	if len(segments) > 3 {
		path.Object = segments[3]
	}
	if len(segments) > 4 {
		path.Column = segments[4]
	}
	target.Path = path
	return target
}

// convertQualityAgentMessageToV1 renders one transcript entry.
func convertQualityAgentMessageToV1(workspaceID, sessionResourceID string, message *store.QualityAgentTranscriptMessage) *v1pb.QualityAgentSessionMessage {
	if message == nil {
		return nil
	}
	converted := &v1pb.QualityAgentSessionMessage{
		Name:       common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + sessionResourceID + "/messages/" + strconv.FormatInt(message.Seq, 10),
		Seq:        message.Seq,
		Wake:       message.Wake,
		Turn:       message.Turn,
		Role:       v1pb.QualityAgentMessageRole(message.Role),
		Origin:     v1pb.QualityAgentMessageOrigin(message.Origin),
		Content:    message.Content,
		ToolCallId: message.ToolCallID,
		ToolName:   message.ToolName,
		Reference:  message.Reference,
		Tokens:     message.Tokens,
		Truncated:  message.Truncated,
		Error:      message.Error,
		CreateTime: timestamppb.New(message.CreatedAt),
	}
	for _, call := range message.ToolCalls.GetToolCalls() {
		converted.ToolCalls = append(converted.ToolCalls, &v1pb.QualityToolCall{
			Id:        call.GetId(),
			Name:      call.GetName(),
			Arguments: call.GetArguments(),
		})
	}
	return converted
}

// databaseRefOfTarget derives the database resource name from a target GUID
// (instance;database;schema;object), the value the allowlist holds.
func databaseRefOfTarget(targetGUID string) (string, error) {
	segments := common.SplitMetaGUID(targetGUID)
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return "", connectErrorInvalid("target GUID %q does not name a database object", targetGUID)
	}
	return common.FormatDatabase(segments[0], segments[1]), nil
}

func (s *DataQualityService) llmConfigured(ctx context.Context) bool {
	if s.suggester == nil {
		return false
	}
	config, _, err := s.suggester.ResolveConfig(ctx)
	return err == nil && (config.BaseURL != "" || config.ModelName != "")
}

// slicesReverse reverses a page in place; the newest-first read is handed to the client
// in chronological order.
func slicesReverse(messages []*store.QualityAgentTranscriptMessage) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

// parseUserResourceID parses users/{principal-id}.
func parseUserResourceID(name string) (int, error) {
	id, err := common.GetUserID(name)
	if err != nil || id <= 0 {
		return 0, errors.New("the agent must be a user resource name")
	}
	return id, nil
}
