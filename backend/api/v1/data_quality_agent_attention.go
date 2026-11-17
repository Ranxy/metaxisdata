package v1

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const qualityAgentAttentionPrefix = "qualityAgentAttentions/"

// ListQualityAgentAttentions lists one agent's escalations. The owner and
// administrators may read; only the owner can reply or dismiss.
func (s *DataQualityService) ListQualityAgentAttentions(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentAttentionsRequest]) (*connect.Response[v1pb.ListQualityAgentAttentionsResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	agent, _, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
	if err != nil {
		return nil, err
	}
	var statuses []store.QualityAgentAttentionStatus
	switch req.Msg.GetStatus() {
	case v1pb.QualityAgentAttentionStatus_QUALITY_AGENT_ATTENTION_STATUS_UNSPECIFIED:
	case v1pb.QualityAgentAttentionStatus_QUALITY_AGENT_ATTENTION_STATUS_OPEN:
		statuses = []store.QualityAgentAttentionStatus{store.QualityAgentAttentionStatusOpen}
	case v1pb.QualityAgentAttentionStatus_QUALITY_AGENT_ATTENTION_STATUS_RESOLVED:
		statuses = []store.QualityAgentAttentionStatus{store.QualityAgentAttentionStatusResolved}
	case v1pb.QualityAgentAttentionStatus_QUALITY_AGENT_ATTENTION_STATUS_DISMISSED:
		statuses = []store.QualityAgentAttentionStatus{store.QualityAgentAttentionStatusDismissed}
	default:
		return nil, connectErrorInvalid("unknown attention status %d", req.Msg.GetStatus())
	}
	attentions, err := s.store.ListQualityAgentAttentions(ctx, agent.ID, statuses, int(req.Msg.GetPageSize()))
	if err != nil {
		return nil, err
	}
	sessionIDs := make([]int64, 0, len(attentions))
	for _, attention := range attentions {
		if attention.SessionID != nil {
			sessionIDs = append(sessionIDs, *attention.SessionID)
		}
	}
	sessionNames, err := s.store.ListQualityAgentSessionResourceIDs(ctx, workspaceID, sessionIDs)
	if err != nil {
		return nil, err
	}
	response := &v1pb.ListQualityAgentAttentionsResponse{}
	for _, attention := range attentions {
		sessionResourceID := ""
		if attention.SessionID != nil {
			sessionResourceID = sessionNames[*attention.SessionID]
		}
		response.QualityAgentAttentions = append(response.QualityAgentAttentions,
			convertQualityAgentAttentionToV1(workspaceID, agent.ResourceID, sessionResourceID, attention))
	}
	return connect.NewResponse(response), nil
}

// ReplyQualityAgentAttention answers an escalation in one transaction: the reply
// becomes an ASK on the target session, a blocked session resumes, the conversation
// records the reply, and the attention resolves. A retry on a closed attention is a
// no-op.
func (s *DataQualityService) ReplyQualityAgentAttention(ctx context.Context, req *connect.Request[v1pb.ReplyQualityAgentAttentionRequest]) (*connect.Response[v1pb.QualityAgentAttention], error) {
	workspaceID, attention, agent, err := s.qualityAgentAttentionForOwner(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	sessionResourceID := ""
	if attention.SessionID != nil {
		if session, err := s.store.GetQualityAgentSessionByID(ctx, *attention.SessionID); err == nil && session != nil {
			sessionResourceID = session.ResourceID
		}
	}
	if attention.Status != store.QualityAgentAttentionStatusOpen {
		return connect.NewResponse(convertQualityAgentAttentionToV1(workspaceID, agent.ResourceID, sessionResourceID, attention)), nil
	}
	content := strings.TrimSpace(req.Msg.GetMessageBody())
	if content == "" {
		return nil, connectErrorInvalid("a reply is required")
	}
	if len(content) > maxQualityAgentConversationText {
		return nil, connectErrorInvalid("the reply is too long (max %d bytes)", maxQualityAgentConversationText)
	}
	requestID := strings.TrimSpace(req.Msg.GetRequestId())
	key := "attention-reply:" + attention.ResourceID
	if requestID != "" {
		key += ":" + requestID
	}
	resolved, _, applied, err := s.store.ReplyQualityAgentAttentionAtomic(ctx,
		attention.ResourceID, workspaceID, content, principalIDPointer(currentUserID(ctx)), key, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !applied {
		resolved = attention
	}
	return connect.NewResponse(convertQualityAgentAttentionToV1(workspaceID, agent.ResourceID, sessionResourceID, resolved)), nil
}

// DismissQualityAgentAttention closes an escalation without resuming its session.
func (s *DataQualityService) DismissQualityAgentAttention(ctx context.Context, req *connect.Request[v1pb.DismissQualityAgentAttentionRequest]) (*connect.Response[v1pb.QualityAgentAttention], error) {
	workspaceID, attention, agent, err := s.qualityAgentAttentionForOwner(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	sessionResourceID := ""
	if attention.SessionID != nil {
		if session, err := s.store.GetQualityAgentSessionByID(ctx, *attention.SessionID); err == nil && session != nil {
			sessionResourceID = session.ResourceID
		}
	}
	if attention.Status != store.QualityAgentAttentionStatusOpen {
		return connect.NewResponse(convertQualityAgentAttentionToV1(workspaceID, agent.ResourceID, sessionResourceID, attention)), nil
	}
	resolved, _, err := s.store.ResolveQualityAgentAttention(ctx, attention.ResourceID,
		store.QualityAgentAttentionStatusDismissed, principalIDPointer(currentUserID(ctx)), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(convertQualityAgentAttentionToV1(workspaceID, agent.ResourceID, sessionResourceID, resolved)), nil
}

// qualityAgentAttentionForOwner loads the attention, its agent, and proves the caller
// is the agent's owner.
func (s *DataQualityService) qualityAgentAttentionForOwner(ctx context.Context, name string) (string, *store.QualityAgentAttentionMessage, *store.QualityAgentMessage, error) {
	workspaceID, resourceID, err := s.qualityAgentAttentionResourceID(ctx, name)
	if err != nil {
		return "", nil, nil, err
	}
	attention, err := s.store.GetQualityAgentAttentionByResourceID(ctx, resourceID)
	if err != nil {
		return "", nil, nil, err
	}
	if attention == nil {
		return "", nil, nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent attention %q not found", resourceID))
	}
	agent, err := s.store.GetQualityAgentByID(ctx, attention.AgentID)
	if err != nil {
		return "", nil, nil, err
	}
	if agent == nil {
		return "", nil, nil, connect.NewError(connect.CodeNotFound, errors.New("the attention's agent no longer exists"))
	}
	if _, _, err := s.qualityAgentConversationAccess(ctx, agent.ResourceID, true); err != nil {
		return "", nil, nil, err
	}
	return workspaceID, attention, agent, nil
}

func (s *DataQualityService) qualityAgentAttentionResourceID(ctx context.Context, name string) (string, string, error) {
	tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityAgentAttentionPrefix)
	if err != nil {
		return "", "", connectErrorInvalid("invalid quality agent attention name")
	}
	if err := s.validateResourceWorkspace(ctx, tokens[0]); err != nil {
		return "", "", err
	}
	return tokens[0], tokens[1], nil
}

func convertQualityAgentAttentionToV1(workspaceID, agentResourceID, sessionResourceID string, attention *store.QualityAgentAttentionMessage) *v1pb.QualityAgentAttention {
	if attention == nil {
		return nil
	}
	view := &v1pb.QualityAgentAttention{
		Name:       common.FormatWorkspace(workspaceID) + "/" + qualityAgentAttentionPrefix + attention.ResourceID,
		Agent:      common.FormatWorkspace(workspaceID) + "/" + qualityAgentPrefix + agentResourceID,
		Kind:       qualityAgentAttentionKindToV1(attention.Kind),
		Status:     qualityAgentAttentionStatusToV1(attention.Status),
		Reference:  attention.Reference,
		Title:      attention.Title,
		Question:   attention.Question,
		CreateTime: timestamppb.New(attention.CreatedAt),
	}
	if sessionResourceID != "" {
		view.Session = common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + sessionResourceID
	}
	if attention.ResolvedAt != nil {
		view.ResolveTime = timestamppb.New(*attention.ResolvedAt)
	}
	if attention.ResolvedBy != nil {
		view.ResolvedBy = common.FormatUserUID(int(*attention.ResolvedBy))
	}
	return view
}

func qualityAgentAttentionKindToV1(kind store.QualityAgentAttentionKind) v1pb.QualityAgentAttentionKind {
	switch kind {
	case store.QualityAgentAttentionKindBlocked:
		return v1pb.QualityAgentAttentionKind_QUALITY_AGENT_ATTENTION_KIND_BLOCKED
	case store.QualityAgentAttentionKindProposalPending:
		return v1pb.QualityAgentAttentionKind_QUALITY_AGENT_ATTENTION_KIND_PROPOSAL_PENDING
	case store.QualityAgentAttentionKindReportAnomaly:
		return v1pb.QualityAgentAttentionKind_QUALITY_AGENT_ATTENTION_KIND_REPORT_ANOMALY
	default:
		return v1pb.QualityAgentAttentionKind_QUALITY_AGENT_ATTENTION_KIND_UNSPECIFIED
	}
}

func qualityAgentAttentionStatusToV1(status store.QualityAgentAttentionStatus) v1pb.QualityAgentAttentionStatus {
	switch status {
	case store.QualityAgentAttentionStatusOpen:
		return v1pb.QualityAgentAttentionStatus_QUALITY_AGENT_ATTENTION_STATUS_OPEN
	case store.QualityAgentAttentionStatusResolved:
		return v1pb.QualityAgentAttentionStatus_QUALITY_AGENT_ATTENTION_STATUS_RESOLVED
	case store.QualityAgentAttentionStatusDismissed:
		return v1pb.QualityAgentAttentionStatus_QUALITY_AGENT_ATTENTION_STATUS_DISMISSED
	default:
		return v1pb.QualityAgentAttentionStatus_QUALITY_AGENT_ATTENTION_STATUS_UNSPECIFIED
	}
}
