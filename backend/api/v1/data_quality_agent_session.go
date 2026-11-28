package v1

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	qualityAgentSessionPrefix = "qualityAgentSessions/"
)

// GetQualityAgentSession reads one target work thread. The owner and
// administrators may read; an unauthorized caller gets NotFound so the response
// does not disclose that the session exists. Reads are product reads and are not
// audited; resuming a stuck session is the audited administrator action.
func (s *DataQualityService) GetQualityAgentSession(ctx context.Context, req *connect.Request[v1pb.GetQualityAgentSessionRequest]) (*connect.Response[v1pb.QualityAgentSession], error) {
	workspaceID, resourceID, err := s.qualityAgentSessionResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	session, err := s.fetchQualityAgentSession(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if session.Kind != 1 {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent session %q not found", resourceID))
	}
	allowed, err := s.callerCanReadQualityAgent(ctx, session.AgentID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent session %q not found", resourceID))
	}
	return connect.NewResponse(s.convertQualityAgentSession(ctx, workspaceID, session)), nil
}

// convertQualityAgentSession renders one session with its parent's resource name.
func (s *DataQualityService) convertQualityAgentSession(ctx context.Context, workspaceID string, session *store.QualityAgentSessionMessage) *v1pb.QualityAgentSession {
	parents := map[int64]string{}
	if session != nil && session.ParentSessionID != nil {
		if stored, err := s.store.ListQualityAgentSessionsByIDs(ctx, workspaceID, []int64{*session.ParentSessionID}); err == nil {
			for id, parent := range stored {
				parents[id] = parent.ResourceID
			}
		}
	}
	return convertQualityAgentSessionToV1(workspaceID, session, time.Now().UTC(), parents)
}

// ListQualityAgentSessions lists the workspace's target work threads. An
// administrator may read every session; an owner must name an agent they own,
// which keeps a workspace-wide walk out of the product read surface.
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
	switch role := req.Msg.GetRole(); role {
	case v1pb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_UNSPECIFIED:
		find.Kind = 1
	case v1pb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_LEAD,
		v1pb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_SUBAGENT:
		find.Role = storepb.QualityAgentSessionRole(role)
	default:
		return nil, connectErrorInvalid("unknown session role %d", role)
	}
	agentName := strings.TrimSpace(req.Msg.GetAgent())
	if agentName != "" {
		_, agentResourceID, err := s.qualityAgentResourceID(ctx, agentName)
		if err != nil {
			return nil, err
		}
		agent := s.fetchQualityAgentForCaller(ctx, agentResourceID)
		if agent == nil {
			return nil, connect.NewError(connect.CodeNotFound,
				errors.Errorf("quality agent %q not found", agentResourceID))
		}
		find.AgentID = &agent.ID
	} else if admin, adminErr := s.callerIsQualityAgentAdmin(ctx); adminErr != nil {
		return nil, adminErr
	} else if !admin {
		return nil, connectErrorInvalid("agent is required for a non-administrator")
	}
	if guid := strings.TrimSpace(req.Msg.GetTargetGuid()); guid != "" {
		find.TargetGUID = &guid
	}
	if database := strings.TrimSpace(req.Msg.GetDatabase()); database != "" {
		find.DatabaseRef = &database
	}
	if name := strings.TrimSpace(req.Msg.GetSubagentName()); name != "" {
		find.SubagentName = &name
	}
	if parentName := strings.TrimSpace(req.Msg.GetParentSession()); parentName != "" {
		_, parentID, parentErr := s.qualityAgentSessionResourceID(ctx, parentName)
		if parentErr != nil {
			return nil, parentErr
		}
		parent, parentErr := s.fetchQualityAgentSession(ctx, parentID)
		if parentErr != nil {
			return nil, parentErr
		}
		if find.AgentID != nil && parent.AgentID != *find.AgentID {
			return nil, connectErrorInvalid("the parent session belongs to another agent")
		}
		find.ParentSessionID = &parent.ID
	}
	find.Mode = storepb.QualityAgentSessionMode(req.Msg.GetMode())
	switch state := req.Msg.GetState(); state {
	case v1pb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_UNSPECIFIED:
		// Every non-archived session.
	case v1pb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ARCHIVED:
		find.IncludeArchived = true
		find.States = []storepb.QualityAgentSessionState{storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ARCHIVED}
	case v1pb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED,
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
	parentIDs := make([]int64, 0, len(messages))
	for _, message := range messages {
		if message.ParentSessionID != nil {
			parentIDs = append(parentIDs, *message.ParentSessionID)
		}
	}
	parents := map[int64]string{}
	if stored, err := s.store.ListQualityAgentSessionsByIDs(ctx, workspaceID, parentIDs); err == nil {
		for id, parent := range stored {
			parents[id] = parent.ResourceID
		}
	}
	response := &v1pb.ListQualityAgentSessionsResponse{NextPageToken: nextPageToken}
	for _, message := range messages {
		response.QualityAgentSessions = append(response.QualityAgentSessions, convertQualityAgentSessionToV1(workspaceID, message, now, parents))
	}
	return connect.NewResponse(response), nil
}

// ResumeQualityAgentSession resumes a stuck BLOCKED session.
func (s *DataQualityService) ResumeQualityAgentSession(ctx context.Context, req *connect.Request[v1pb.ResumeQualityAgentSessionRequest]) (*connect.Response[v1pb.QualityAgentSession], error) {
	now := time.Now().UTC()
	session, err := s.transitionQualityAgentSession(ctx, req.Msg.GetName(), req.Msg.GetExpectedRevision(),
		storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED, &now)
	if err != nil {
		return nil, err
	}
	// A resume is a person's decision, and the next budget check must not silently overrule it: the
	// marker buys exactly one wake past the hourly ceiling, then it is consumed.
	if _, resourceID, nameErr := s.qualityAgentSessionResourceID(ctx, req.Msg.GetName()); nameErr == nil {
		if markErr := s.store.MarkQualityAgentSessionManualResume(ctx, resourceID, now); markErr != nil {
			slog.Warn("failed to record the manual resume", log.WithError(markErr))
		}
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
	if session.Kind != 1 {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent session %q not found", resourceID))
	}
	allowed, err := s.callerCanReadQualityAgent(ctx, session.AgentID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent session %q not found", resourceID))
	}
	afterSeq := req.Msg.GetAfterSeq()
	beforeSeq := req.Msg.GetBeforeSeq()
	if afterSeq < 0 {
		return nil, connectErrorInvalid("after_seq cannot be negative")
	}
	if beforeSeq < 0 {
		return nil, connectErrorInvalid("before_seq cannot be negative")
	}
	if afterSeq > 0 && beforeSeq > 0 {
		return nil, connectErrorInvalid("after_seq and before_seq are mutually exclusive")
	}
	if (afterSeq > 0 || beforeSeq > 0) && req.Msg.GetPageToken() != "" {
		return nil, connectErrorInvalid("page_token cannot be combined with after_seq or before_seq")
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
	switch {
	case beforeSeq > 0:
		// Older history anchored to the oldest loaded seq, so a live append cannot
		// shift the page the caller is walking.
		find.BeforeSeq = beforeSeq
		find.NewestFirst = true
	case afterSeq > 0:
		find.AfterSeq = afterSeq
	default:
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
	if find.AfterSeq == 0 && find.BeforeSeq == 0 && more {
		nextPageToken, tokenErr := offset.getNextPageToken()
		if tokenErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(tokenErr, "failed to marshal next page token"))
		}
		response.NextPageToken = nextPageToken
	}
	senders := s.qualityAgentMessageSenders(ctx, workspaceID, messages)
	for _, message := range messages {
		response.QualityAgentSessionMessages = append(response.QualityAgentSessionMessages,
			convertQualityAgentMessageToV1(workspaceID, session.ResourceID, message, senders))
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

// callerCanReadQualityAgent reports whether the caller owns the session's agent or
// is an administrator. It is the authorization boundary for the product session
// reads; the ACL only proves the member baseline permission.
func (s *DataQualityService) callerCanReadQualityAgent(ctx context.Context, agentID int64) (bool, error) {
	agent, err := s.store.GetQualityAgentByID(ctx, agentID)
	if err != nil {
		return false, err
	}
	if agent == nil {
		return false, nil
	}
	admin, err := s.callerIsQualityAgentAdmin(ctx)
	if err != nil {
		return false, err
	}
	if admin {
		return true, nil
	}
	return agent.OwnerPrincipalID != nil && int32(currentUserID(ctx)) == *agent.OwnerPrincipalID, nil
}

// convertQualityAgentSessionToV1 renders one session. "Running" is derived from the
// lease, never from the durable state.
func convertQualityAgentSessionToV1(workspaceID string, message *store.QualityAgentSessionMessage, now time.Time, parentNames ...map[int64]string) *v1pb.QualityAgentSession {
	if message == nil {
		return nil
	}
	parent := ""
	subagentName := ""
	if message.Role == storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_SUBAGENT {
		subagentName = message.SubagentName
		if message.ParentSessionID != nil && len(parentNames) > 0 && parentNames[0] != nil {
			if name, ok := parentNames[0][*message.ParentSessionID]; ok {
				parent = common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + name
			}
		}
	}
	return &v1pb.QualityAgentSession{
		Name:             common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + message.ResourceID,
		Target:           convertQualityAgentSessionTargetToV1(message),
		Database:         message.DatabaseRef,
		Agent:            common.FormatUserUID(int(message.AgentPrincipalID)),
		State:            v1pb.QualityAgentSessionState(message.State),
		BlockedCode:      message.BlockedCode,
		BlockedReason:    message.BlockedReason,
		Revision:         message.Revision,
		WakeCount:        message.WakeCount,
		NextWakeTime:     timestampOrNil(message.NextWakeAt),
		LastActivityTime: timestampOrNil(message.LastActivityAt),
		LeaseExpireTime:  timestampOrNil(message.LeaseUntil),
		RestartCount:     message.RestartCount,
		Running:          message.LeaseUntil != nil && message.LeaseUntil.After(now),
		Mode:             v1pb.QualityAgentSessionMode(message.Mode),
		ContextUsage:     convertQualityAgentSessionContextUsageToV1(message.ContextUsage),
		Role:             v1pb.QualityAgentSessionRole(message.Role),
		Parent:           parent,
		SubagentName:     subagentName,
		CreateTime:       timestamppb.New(message.CreatedAt),
		UpdateTime:       timestamppb.New(message.UpdatedAt),
		ArchiveTime:      timestampOrNil(message.ArchivedAt),
	}
}

// convertQualityAgentSessionContextUsageToV1 renders the engine's last model-input
// sample. An empty sample stays nil so the rail can tell "not measured yet" from zero.
func convertQualityAgentSessionContextUsageToV1(usage *storepb.QualityAgentSessionContextUsage) *v1pb.QualityAgentSessionContextUsage {
	if usage == nil {
		return nil
	}
	if usage.UsedTokens == 0 && usage.ContextWindow == 0 && usage.Model == "" && usage.MeasuredAt == nil {
		return nil
	}
	return &v1pb.QualityAgentSessionContextUsage{
		UsedTokens:              usage.UsedTokens,
		ContextWindow:           usage.ContextWindow,
		MaxOutputTokens:         usage.MaxOutputTokens,
		CompactionTriggerTokens: usage.CompactionTriggerTokens,
		Model:                   usage.Model,
		Wake:                    usage.Wake,
		Turn:                    usage.Turn,
		MeasuredAt:              usage.GetMeasuredAt(),
		Estimated:               usage.Estimated,
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

// qualityAgentMessageSenders resolves the session resource ids named by a page's
// messages in one read, so a relay row renders its sender without an N+1.
func (s *DataQualityService) qualityAgentMessageSenders(ctx context.Context, workspaceID string, messages []*store.QualityAgentTranscriptMessage) map[int64]string {
	ids := make([]int64, 0, len(messages))
	for _, message := range messages {
		if message.SenderSessionID != nil {
			ids = append(ids, *message.SenderSessionID)
		}
	}
	names := map[int64]string{}
	if stored, err := s.store.ListQualityAgentSessionsByIDs(ctx, workspaceID, ids); err == nil {
		for id, session := range stored {
			names[id] = session.ResourceID
		}
	}
	return names
}

// convertQualityAgentMessageToV1 renders one transcript entry.
func convertQualityAgentMessageToV1(workspaceID, sessionResourceID string, message *store.QualityAgentTranscriptMessage, senderNames ...map[int64]string) *v1pb.QualityAgentSessionMessage {
	if message == nil {
		return nil
	}
	sender := ""
	if message.SenderSessionID != nil && len(senderNames) > 0 && senderNames[0] != nil {
		if name, ok := senderNames[0][*message.SenderSessionID]; ok {
			sender = common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + name
		}
	}
	converted := &v1pb.QualityAgentSessionMessage{
		Name:       common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + sessionResourceID + "/messages/" + strconv.FormatInt(message.Seq, 10),
		Seq:        message.Seq,
		Wake:       message.Wake,
		Turn:       message.Turn,
		Role:       v1pb.QualityAgentMessageRole(message.Role),
		Origin:     v1pb.QualityAgentMessageOrigin(message.Origin),
		Content:    message.Content,
		Reasoning:  message.Reasoning,
		ToolCallId: message.ToolCallID,
		ToolName:   message.ToolName,
		Reference:  message.Reference,
		Tokens:     message.Tokens,
		Truncated:  message.Truncated,
		Error:      message.Error,
		Sender:     sender,
		SenderName: message.SenderName,
		Intent:     v1pb.QualityAgentMessageIntent(message.Intent),
		Audience:   v1pb.QualityAgentMessageAudience(message.Audience),
		ReplyTo:    message.ReplyTo,
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
