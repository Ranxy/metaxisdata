package v1

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	maxQualityAgentConversationText = 16 << 10
	defaultConversationMessagesHour = 60
)

// SendQualityAgentMessage appends the owner's message to the agent conversation and
// wakes it. Only the owner may send; an administrator may read the conversation but
// cannot speak as the owner.
func (s *DataQualityService) SendQualityAgentMessage(ctx context.Context, req *connect.Request[v1pb.SendQualityAgentMessageRequest]) (*connect.Response[v1pb.QualityAgentSessionMessage], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	agent, config, err := s.qualityAgentConversationAccess(ctx, resourceID, true)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(req.Msg.GetMessageBody())
	if content == "" {
		return nil, connectErrorInvalid("a message is required")
	}
	if len(content) > maxQualityAgentConversationText {
		return nil, connectErrorInvalid("the message is too long (max %d bytes)", maxQualityAgentConversationText)
	}
	limit := config.ConversationMessagesPerHour
	if limit <= 0 {
		limit = defaultConversationMessagesHour
	}
	requestID := strings.TrimSpace(req.Msg.GetRequestId())
	if len(requestID) > 128 {
		return nil, connectErrorInvalid("request_id is too long (max 128 bytes)")
	}
	key := "ask:" + requestID
	if requestID == "" {
		random, err := common.RandomString(12)
		if err != nil {
			return nil, errors.Wrap(err, "failed to generate the message id")
		}
		key = "ask:" + random
	}
	var conversation *store.QualityAgentSessionMessage
	var message *store.QualityAgentTranscriptMessage
	lockErr := s.store.WithQualityAgentAdvisoryLock(ctx, fmt.Sprintf("dqchat:%d", agent.ID), func() error {
		count, err := s.store.CountQualityAgentConversationMessagesSince(ctx, agent.ID, time.Now().UTC().Add(-time.Hour))
		if err != nil {
			return err
		}
		if count >= int64(limit) {
			return connect.NewError(connect.CodeResourceExhausted,
				errors.New("the agent conversation reached its hourly message limit"))
		}
		conversation, err = s.store.EnsureQualityAgentConversation(ctx, workspaceID, agent.ID)
		if err != nil {
			return err
		}
		message, err = s.store.AppendQualityAgentTranscriptMessage(ctx, &store.QualityAgentTranscriptMessage{
			SessionID:      conversation.ID,
			WorkspaceID:    workspaceID,
			Wake:           conversation.WakeCount + 1,
			Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
			Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
			Content:        content,
			CreatedBy:      principalIDPointer(currentUserID(ctx)),
			IdempotencyKey: key,
		})
		if err != nil {
			return err
		}
		_, err = s.store.AdvanceQualityAgentSessionWake(ctx, conversation.ResourceID, time.Now().UTC())
		return err
	})
	if lockErr != nil {
		if errors.Is(lockErr, store.ErrQualityAgentBusy) {
			return nil, connect.NewError(connect.CodeAborted,
				errors.New("another message is being sent; retry"))
		}
		return nil, lockErr
	}
	return connect.NewResponse(convertQualityAgentMessageToV1(workspaceID, conversation.ResourceID, message)), nil
}

// ListQualityAgentConversationMessages reads the front-office conversation. The
// owner and administrators may read; the UI polls with after_seq.
func (s *DataQualityService) ListQualityAgentConversationMessages(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentConversationMessagesRequest]) (*connect.Response[v1pb.ListQualityAgentConversationMessagesResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	agent, _, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
	if err != nil {
		return nil, err
	}
	conversation, err := s.store.EnsureQualityAgentConversation(ctx, workspaceID, agent.ID)
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
	find := &store.FindQualityAgentTranscriptMessage{SessionID: conversation.ID, Limit: limit}
	if afterSeq > 0 {
		find.AfterSeq = afterSeq
	} else {
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
	response := &v1pb.ListQualityAgentConversationMessagesResponse{}
	if find.AfterSeq == 0 && more {
		nextPageToken, tokenErr := offset.getNextPageToken()
		if tokenErr != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Wrap(tokenErr, "failed to marshal next page token"))
		}
		response.NextPageToken = nextPageToken
	}
	for _, message := range messages {
		// The front office is product content only: SYSTEM prompts, ENGINE notes,
		// tool results and compaction rows stay behind the audited admin diagnostics
		// RPC. Strip tool calls as well; the model's raw tool arguments are not part
		// of the owner's conversation.
		if message.Role != storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER &&
			message.Role != storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT {
			continue
		}
		message.ToolCalls = &storepb.QualityToolCallList{}
		response.QualityAgentConversationMessages = append(response.QualityAgentConversationMessages,
			convertQualityAgentMessageToV1(workspaceID, conversation.ResourceID, message))
	}
	return connect.NewResponse(response), nil
}

// qualityAgentConversationAccess loads the agent and its configuration and enforces
// the owner/admin rule. requireOwner rejects administrators who are not the owner.
func (s *DataQualityService) qualityAgentConversationAccess(ctx context.Context, resourceID string, requireOwner bool) (*store.QualityAgentMessage, *store.QualityAgentConfigMessage, error) {
	agent, err := s.store.GetQualityAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, nil, err
	}
	if agent == nil {
		return nil, nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent %q not found", resourceID))
	}
	config, err := s.store.GetQualityAgentConfig(ctx, agent.ID)
	if err != nil {
		return nil, nil, err
	}
	if config == nil {
		return nil, nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the agent has no configuration"))
	}
	owner := config.OwnerPrincipalID != nil && int32(currentUserID(ctx)) == *config.OwnerPrincipalID
	if requireOwner {
		if owner {
			return agent, config, nil
		}
		// An ownerless agent (the owner was deleted) falls back to the workspace
		// administrators so its escalations are not permanently stuck.
		if config.OwnerPrincipalID == nil {
			admin, adminErr := s.callerIsQualityAgentAdmin(ctx)
			if adminErr == nil && admin {
				return agent, config, nil
			}
		}
		return nil, nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("only the agent's owner may talk to it"))
	}
	if owner {
		return agent, config, nil
	}
	admin, err := s.callerIsQualityAgentAdmin(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !admin {
		return nil, nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("only the owner or an administrator may read this conversation"))
	}
	return agent, config, nil
}

// qualityAgentConversationName renders the conversation's resource name for an agent
// view; an empty string means no conversation exists yet.
func qualityAgentConversationName(workspaceID string, conversation *store.QualityAgentSessionMessage) string {
	if conversation == nil {
		return ""
	}
	return common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + conversation.ResourceID
}
