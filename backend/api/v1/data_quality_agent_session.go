package v1

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityAgentSessionPrefix = "qualityAgentSessions/"
	// maxAgentSessionText bounds a session objective and one person's question.
	maxAgentSessionText = 4000
)

// AgentRunnerStatusProvider is the process-local liveness signal of the session runner.
// A zero time means the runner has not ticked yet or has stopped.
type AgentRunnerStatusProvider interface {
	LastTick() time.Time
}

// CreateQualityAgentSession records one target's long-lived agent session. The runner
// must be enabled, the target's database must be in the allowlist and an enabled quality
// agent must exist; every check happens before the write.
func (s *DataQualityService) CreateQualityAgentSession(ctx context.Context, req *connect.Request[v1pb.CreateQualityAgentSessionRequest]) (*connect.Response[v1pb.QualityAgentSession], error) {
	workspaceID, err := s.workspaceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	settings, err := dataquality.LoadSettings(ctx, s.store)
	if err != nil {
		return nil, errLoadQualitySettings(err)
	}
	if !settings.AgentRunnerEnabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the quality agent runner is disabled in this workspace"))
	}
	if len(settings.AgentRunnerDatabases) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the runner has no database in scope; enable at least one"))
	}
	targetGUID := strings.TrimSpace(req.Msg.GetTargetGuid())
	if targetGUID == "" {
		return nil, connectErrorInvalid("a session needs a target so the agent can scope it")
	}
	metaType := storepb.MetaType(req.Msg.GetMetaType())
	if metaType == storepb.MetaType_UNSPECIFIED {
		rules, err := s.store.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
		if err != nil {
			return nil, errors.Wrap(err, "failed to read the target's rules")
		}
		if len(rules) == 0 {
			return nil, connectErrorInvalid("meta_type is required when the target has no rules")
		}
		metaType = rules[0].TargetType
	}
	databaseRef, err := databaseRefOfTarget(targetGUID)
	if err != nil {
		return nil, err
	}
	if !settings.AgentRunnerDatabaseEnabled(databaseRef) {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.Errorf("database %q is not in the runner's allowlist", databaseRef))
	}
	agent, err := s.resolveQualityAgent(ctx, req.Msg.GetAgent())
	if err != nil {
		return nil, err
	}
	objective := strings.TrimSpace(req.Msg.GetObjective())
	if len(objective) > maxAgentSessionText {
		return nil, connectErrorInvalid("the objective is too long (max %d bytes)", maxAgentSessionText)
	}
	message, err := s.store.CreateQualityAgentSession(ctx, &store.CreateQualityAgentSessionInput{
		WorkspaceID: workspaceID,
		TargetGUID:  targetGUID,
		TargetType:  metaType,
		DatabaseRef: databaseRef,
		AgentID:     agent.ID,
		Objective:   objective,
		CreatedBy:   principalIDPointer(currentUserID(ctx)),
	})
	if err != nil {
		if common.ErrorCode(err) == common.Conflict {
			return nil, connect.NewError(connect.CodeAlreadyExists, err)
		}
		return nil, err
	}
	return connect.NewResponse(convertQualityAgentSessionToV1(workspaceID, message, time.Now().UTC())), nil
}

// ListQualityAgentSessions lists the workspace's sessions by newest activity first.
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
	find := &store.FindQualityAgentSessionMessage{WorkspaceID: &workspaceID, Limit: &limit, Offset: &offset.offset}
	if guid := strings.TrimSpace(req.Msg.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	if database := strings.TrimSpace(req.Msg.GetDatabase()); database != "" {
		find.DatabaseRef = &database
	}
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

// PauseQualityAgentSession stops new wakes; the conversation stays.
func (s *DataQualityService) PauseQualityAgentSession(ctx context.Context, req *connect.Request[v1pb.PauseQualityAgentSessionRequest]) (*connect.Response[v1pb.QualityAgentSession], error) {
	return s.transitionQualityAgentSession(ctx, req.Msg.GetName(), req.Msg.GetExpectedRevision(),
		storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_PAUSED, nil)
}

// ResumeQualityAgentSession puts a paused or blocked session back to enabled and wakes
// it immediately.
func (s *DataQualityService) ResumeQualityAgentSession(ctx context.Context, req *connect.Request[v1pb.ResumeQualityAgentSessionRequest]) (*connect.Response[v1pb.QualityAgentSession], error) {
	now := time.Now().UTC()
	return s.transitionQualityAgentSession(ctx, req.Msg.GetName(), req.Msg.GetExpectedRevision(),
		storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED, &now)
}

// ArchiveQualityAgentSession tombstones a session; the transcript stays readable.
func (s *DataQualityService) ArchiveQualityAgentSession(ctx context.Context, req *connect.Request[v1pb.ArchiveQualityAgentSessionRequest]) (*connect.Response[emptypb.Empty], error) {
	_, resourceID, err := s.qualityAgentSessionResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	archived, err := s.store.ArchiveQualityAgentSession(ctx, resourceID, req.Msg.GetExpectedRevision(), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !archived {
		return nil, connect.NewError(connect.CodeAborted,
			errors.New("the session changed since it was read; reload and try again"))
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// WakeQualityAgentSession appends a person's message and makes the session due now. A
// wake already running keeps its lease; the runner picks the message up immediately
// after it finishes.
func (s *DataQualityService) WakeQualityAgentSession(ctx context.Context, req *connect.Request[v1pb.WakeQualityAgentSessionRequest]) (*connect.Response[v1pb.QualityAgentSession], error) {
	workspaceID, resourceID, err := s.qualityAgentSessionResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	session, err := s.fetchQualityAgentSession(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if session.ArchivedAt != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("an archived session cannot be woken"))
	}
	content := strings.TrimSpace(req.Msg.GetContent())
	if content == "" {
		return nil, connectErrorInvalid("a wake needs a message for the agent")
	}
	if len(content) > maxAgentSessionText {
		return nil, connectErrorInvalid("the message is too long (max %d bytes)", maxAgentSessionText)
	}
	now := time.Now().UTC()
	sum := sha256.Sum256([]byte(content))
	_, err = s.store.AppendQualityAgentTranscriptMessage(ctx, &store.QualityAgentTranscriptMessage{
		SessionID:      session.ID,
		WorkspaceID:    workspaceID,
		Wake:           session.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
		Content:        content,
		IdempotencyKey: fmt.Sprintf("ask:%x", sum[:8]),
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.store.AdvanceQualityAgentSessionWake(ctx, resourceID, now); err != nil {
		return nil, err
	}
	updated, err := s.fetchQualityAgentSession(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityAgentSessionToV1(workspaceID, updated, now)), nil
}

// ListQualityAgentSessionMessages reads one session's transcript. after_seq polls for
// new messages; without it the newest page is returned in chronological order.
func (s *DataQualityService) ListQualityAgentSessionMessages(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentSessionMessagesRequest]) (*connect.Response[v1pb.ListQualityAgentSessionMessagesResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentSessionResourceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	session, err := s.fetchQualityAgentSession(ctx, resourceID)
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
	find := &store.FindQualityAgentTranscriptMessage{SessionID: session.ID, Limit: limit}
	if afterSeq := req.Msg.GetAfterSeq(); afterSeq > 0 {
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
	running, err := s.store.CountQualityAgentSessionsByLease(ctx, workspaceID, now)
	if err != nil {
		return nil, err
	}
	enabled, err := s.store.CountQualityAgentSessionsByState(ctx, workspaceID, storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED)
	if err != nil {
		return nil, err
	}
	blocked, err := s.store.CountQualityAgentSessionsByState(ctx, workspaceID, storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED)
	if err != nil {
		return nil, err
	}
	tokens, err := s.store.SumQualityAgentSessionTokensSince(ctx, workspaceID, now.Add(-time.Hour))
	if err != nil {
		return nil, err
	}
	wakes, err := s.store.CountQualityAgentSessionWakesSince(ctx, workspaceID, now.Add(-time.Hour))
	if err != nil {
		return nil, err
	}
	lastActivity, err := s.store.LastQualityAgentSessionActivity(ctx, workspaceID)
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
	}
	if s.agentRunner != nil {
		if tick := s.agentRunner.LastTick(); !tick.IsZero() {
			response.RunnerLastTickTime = timestamppb.New(tick)
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
	if err := s.validateResourceWorkspace(ctx, tokens[0]); err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
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
		Target:           &v1pb.QualityTarget{MetaGuid: message.TargetGUID, MetaType: v1pb.MetaType(message.TargetType)},
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
		CreateTime:       timestamppb.New(message.CreatedAt),
		UpdateTime:       timestamppb.New(message.UpdatedAt),
		ArchiveTime:      timestampOrNil(message.ArchivedAt),
	}
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

// resolveQualityAgent picks the agent a session runs as: the requested one, or the first
// enabled quality agent of the workspace.
func (s *DataQualityService) resolveQualityAgent(ctx context.Context, name string) (*store.QualityAgentMessage, error) {
	if name = strings.TrimSpace(name); name != "" {
		principalID, err := parseUserResourceID(name)
		if err != nil {
			return nil, connectErrorInvalid("%v", err)
		}
		agent, err := s.store.GetQualityAgentByPrincipalID(ctx, principalID)
		if err != nil {
			return nil, err
		}
		if agent == nil || !agent.Enabled {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.Errorf("agent %q is not an enabled quality agent", name))
		}
		return agent, nil
	}
	agents, err := s.store.ListQualityAgents(ctx)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		if agent.Enabled {
			return agent, nil
		}
	}
	return nil, connect.NewError(connect.CodeFailedPrecondition,
		errors.New("create an enabled quality agent before creating a session"))
}

// llmConfigured reports whether a model wake can run. A disabled or unconfigured
// analysis is not an error: the runner falls back to the deterministic survey.
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
