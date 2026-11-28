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
	maxQualityAgentConversationText = 16 << 10
	defaultConversationMessagesHour = 60
	qualityAgentAskPrefix           = "qualityAgentAsks/"
)

// SendQualityAgentMessage appends the owner's message to the agent conversation and
// wakes it. Only the owner may send; an administrator may read the conversation but
// cannot speak as the owner.
//
// A message with reply_to answers that ask. The answer is validated against the ask's
// questions, written and delivered to the requests behind the ask in one transaction,
// so neither a model nor the UI has to infer which question a sentence belongs to. A
// message without reply_to is ordinary conversation and closes no ask.
func (s *DataQualityService) SendQualityAgentMessage(ctx context.Context, req *connect.Request[v1pb.SendQualityAgentMessageRequest]) (*connect.Response[v1pb.QualityAgentSessionMessage], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetName())
	if err != nil {
		return nil, err
	}
	agent, err := s.qualityAgentConversationAccess(ctx, resourceID, true)
	if err != nil {
		return nil, err
	}
	if agent.State == store.QualityAgentStateDisabled {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			errors.New("the agent is disabled; it can no longer answer"))
	}
	content := strings.TrimSpace(req.Msg.GetMessageBody())
	answers := req.Msg.GetAnswers()
	if content == "" && len(answers) == 0 {
		return nil, connectErrorInvalid("a message or an answer is required")
	}
	if len(content) > maxQualityAgentConversationText {
		return nil, connectErrorInvalid("the message is too long (max %d bytes)", maxQualityAgentConversationText)
	}
	limit := agent.ConversationMessagesPerHour
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
	replyTo := strings.TrimSpace(req.Msg.GetReplyTo())
	dismiss := req.Msg.GetIntent() == v1pb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_DISMISS
	if dismiss && replyTo == "" {
		return nil, connectErrorInvalid("a dismissal needs the ask it closes")
	}
	if dismiss && len(answers) > 0 {
		return nil, connectErrorInvalid("a dismissal carries no answer")
	}
	if len(answers) > 0 && replyTo == "" {
		return nil, connectErrorInvalid("an answer needs the ask it answers")
	}
	var ask *store.QualityAgentAskMessage
	if replyTo != "" {
		stored, askErr := s.qualityAgentAskForReply(ctx, workspaceID, agent.ID, replyTo)
		if askErr != nil {
			return nil, askErr
		}
		ask = stored
		if ask.Status != store.QualityAgentAskStatusOpen {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				errors.New("the question is no longer open"))
		}
	}
	if ask != nil && !dismiss {
		if len(answers) == 0 {
			// A legacy text answer is still a valid custom answer to a single
			// question; a multi-question ask has no way to tell which one it means.
			if len(ask.Questions) > 1 {
				return nil, connectErrorInvalid("this question has several parts; answer each one")
			}
			questionID := "q1"
			if len(ask.Questions) == 1 && strings.TrimSpace(ask.Questions[0].Id) != "" {
				questionID = strings.TrimSpace(ask.Questions[0].Id)
			}
			answers = []*v1pb.QualityAgentAnswer{{Id: questionID, Custom: content}}
		}
		return s.answerQualityAgentAsk(ctx, workspaceID, agent, ask, answers, key, int(limit))
	}
	if ask != nil && dismiss {
		return s.dismissQualityAgentAsk(ctx, workspaceID, agent, ask, content, key, int(limit))
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
			Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_MESSAGE,
			Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
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

// answerQualityAgentAsk validates one owner answer, builds the lead and per-request
// envelopes, and writes them atomically with the ask and request states.
func (s *DataQualityService) answerQualityAgentAsk(ctx context.Context, workspaceID string, agent *store.QualityAgentMessage, ask *store.QualityAgentAskMessage, answers []*v1pb.QualityAgentAnswer, key string, limit int) (*connect.Response[v1pb.QualityAgentSessionMessage], error) {
	normalized := make([]*storepb.QualityAgentAskAnswer, 0, len(answers))
	for _, answer := range answers {
		if answer == nil {
			continue
		}
		normalized = append(normalized, &storepb.QualityAgentAskAnswer{
			Id:       answer.GetId(),
			Selected: append([]string(nil), answer.GetSelected()...),
			Custom:   answer.GetCustom(),
		})
	}
	questions := dataquality.QuestionsOf(ask)
	if len(questions) == 1 {
		for _, answer := range normalized {
			if strings.TrimSpace(answer.GetId()) == "" {
				answer.Id = questions[0].GetId()
			}
		}
	}
	validated, err := dataquality.ValidateAnswers(questions, normalized)
	if err != nil {
		return nil, connectErrorInvalid("%s", err.Error())
	}
	requestsByAsk, err := s.store.ListQualityAgentRequestsByAskIDs(ctx, []int64{ask.ID})
	if err != nil {
		return nil, err
	}
	active := make([]*store.QualityAgentRequestMessage, 0, len(requestsByAsk[ask.ID]))
	requestNames := make([]string, 0, len(requestsByAsk[ask.ID]))
	byRequestName := make(map[string]*store.QualityAgentRequestMessage, len(requestsByAsk[ask.ID]))
	for _, request := range requestsByAsk[ask.ID] {
		if request.Status != store.QualityAgentRequestStatusOpen &&
			request.Status != store.QualityAgentRequestStatusForwarded {
			continue
		}
		name := dataquality.RequestName(request)
		active = append(active, request)
		requestNames = append(requestNames, name)
		byRequestName[name] = request
	}
	// A question's requests decide which answer reaches which subagent. A legacy
	// single question has no subset and covers every active request.
	answersByID := make(map[string]*storepb.QualityAgentAskAnswer, len(validated))
	for _, answer := range validated {
		answersByID[answer.GetId()] = answer
	}
	questionForRequest := make(map[string]*storepb.QualityAgentAskQuestion, len(requestNames))
	answerForRequest := make(map[string]*storepb.QualityAgentAskAnswer, len(requestNames))
	for _, question := range questions {
		answer := answersByID[question.GetId()]
		if answer == nil {
			continue
		}
		names := question.GetRequests()
		if len(names) == 0 {
			names = requestNames
		}
		for _, name := range names {
			if byRequestName[name] == nil {
				// A request that settled between the ask's creation and this answer
				// no longer needs a delivery; the ask's other requests still do.
				continue
			}
			if existing := questionForRequest[name]; existing != nil {
				return nil, connectErrorInvalid("request %q is decided by both question %q and question %q", name, existing.GetId(), question.GetId())
			}
			questionForRequest[name] = question
			answerForRequest[name] = answer
		}
	}
	for _, name := range requestNames {
		if questionForRequest[name] == nil {
			return nil, connectErrorInvalid("no question in the answer decides request %q", name)
		}
	}
	envelope, err := dataquality.BuildAnswerEnvelope(ask, validated, requestNames)
	if err != nil {
		return nil, err
	}
	deliveries := make([]*store.QualityAgentRequestDelivery, 0, len(active))
	for _, request := range active {
		name := dataquality.RequestName(request)
		question := questionForRequest[name]
		answer := answerForRequest[name]
		requestEnvelope, text, buildErr := dataquality.BuildRequestAnswerEnvelope(ask, request, question, answer)
		if buildErr != nil {
			return nil, buildErr
		}
		var subagent *store.QualityAgentSessionMessage
		if request.OriginSessionID != nil {
			subagent, buildErr = s.store.GetQualityAgentSessionByID(ctx, *request.OriginSessionID)
			if buildErr != nil {
				return nil, buildErr
			}
			if subagent != nil && subagent.ArchivedAt != nil {
				subagent = nil
			}
		}
		delivery := &store.QualityAgentRequestDelivery{
			Request:    request,
			Subagent:   subagent,
			Resolution: text,
		}
		if subagent != nil {
			delivery.Message = storepbRequestAnswerMessage(subagent, request, requestEnvelope)
		}
		deliveries = append(deliveries, delivery)
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
		for _, delivery := range deliveries {
			if delivery != nil && delivery.Message != nil {
				delivery.Message.SenderSessionID = &conversation.ID
			}
		}
		leadMessage := &store.QualityAgentTranscriptMessage{
			Wake:           conversation.WakeCount + 1,
			Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
			Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
			Content:        envelope,
			Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
			Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
			ReplyTo:        dataquality.AskName(ask),
			IdempotencyKey: key,
		}
		_, message, _, err = s.store.AnswerQualityAgentAskAtomic(ctx, ask.ResourceID, validated,
			int32(currentUserID(ctx)), leadMessage, deliveries, time.Now().UTC())
		return err
	})
	if lockErr != nil {
		if errors.Is(lockErr, store.ErrQualityAgentBusy) {
			return nil, connect.NewError(connect.CodeAborted,
				errors.New("another message is being sent; retry"))
		}
		switch common.ErrorCode(lockErr) {
		case common.Conflict:
			return nil, connect.NewError(connect.CodeFailedPrecondition, lockErr)
		case common.NotFound:
			return nil, connect.NewError(connect.CodeNotFound, lockErr)
		}
		return nil, lockErr
	}
	_, _ = s.store.AdvanceQualityAgentSessionWake(ctx, conversation.ResourceID, time.Now().UTC())
	return connect.NewResponse(convertQualityAgentMessageToV1(workspaceID, conversation.ResourceID, message)), nil
}

func storepbRequestAnswerMessage(subagent *store.QualityAgentSessionMessage, request *store.QualityAgentRequestMessage, content string) *store.QualityAgentTranscriptMessage {
	return &store.QualityAgentTranscriptMessage{
		SessionID:      subagent.ID,
		WorkspaceID:    subagent.WorkspaceID,
		Wake:           subagent.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:        content,
		SenderName:     "lead",
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		ReplyTo:        dataquality.RequestName(request),
		IdempotencyKey: fmt.Sprintf("ask-answer:%s", request.ResourceID),
	}
}

// storepbRequestDismissedMessage tells a subagent its request was dismissed. It is a
// notice, not an answer: the request is already closed and nothing will resolve it.
func storepbRequestDismissedMessage(subagent *store.QualityAgentSessionMessage, request *store.QualityAgentRequestMessage, content string) *store.QualityAgentTranscriptMessage {
	return &store.QualityAgentTranscriptMessage{
		SessionID:      subagent.ID,
		WorkspaceID:    subagent.WorkspaceID,
		Wake:           subagent.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:        content,
		SenderName:     "lead",
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_DISMISS,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		ReplyTo:        dataquality.RequestName(request),
		IdempotencyKey: fmt.Sprintf("ask-dismissed:%s", request.ResourceID),
	}
}

// dismissQualityAgentAsk closes one open ask without pretending it was answered. The
// owner's message, the ask/request states and the subagent notices share one
// transaction, so a dismissal can never tell only half the story.
func (s *DataQualityService) dismissQualityAgentAsk(ctx context.Context, workspaceID string, agent *store.QualityAgentMessage, ask *store.QualityAgentAskMessage, content, key string, limit int) (*connect.Response[v1pb.QualityAgentSessionMessage], error) {
	requestsByAsk, err := s.store.ListQualityAgentRequestsByAskIDs(ctx, []int64{ask.ID})
	if err != nil {
		return nil, err
	}
	notices := make([]*store.QualityAgentRequestDelivery, 0, len(requestsByAsk[ask.ID]))
	for _, request := range requestsByAsk[ask.ID] {
		if request.Status != store.QualityAgentRequestStatusOpen &&
			request.Status != store.QualityAgentRequestStatusForwarded {
			continue
		}
		requestEnvelope, buildErr := dataquality.BuildRequestDismissedEnvelope(ask, request)
		if buildErr != nil {
			return nil, buildErr
		}
		var subagent *store.QualityAgentSessionMessage
		if request.OriginSessionID != nil {
			subagent, buildErr = s.store.GetQualityAgentSessionByID(ctx, *request.OriginSessionID)
			if buildErr != nil {
				return nil, buildErr
			}
			if subagent != nil && subagent.ArchivedAt != nil {
				subagent = nil
			}
		}
		notice := &store.QualityAgentRequestDelivery{Request: request, Subagent: subagent}
		if subagent != nil {
			notice.Message = storepbRequestDismissedMessage(subagent, request, requestEnvelope)
		}
		notices = append(notices, notice)
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
		for _, notice := range notices {
			if notice != nil && notice.Message != nil {
				notice.Message.SenderSessionID = &conversation.ID
			}
		}
		leadMessage := &store.QualityAgentTranscriptMessage{
			Wake:           conversation.WakeCount + 1,
			Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
			Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK,
			Content:        content,
			Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_DISMISS,
			Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
			ReplyTo:        dataquality.AskName(ask),
			IdempotencyKey: key,
		}
		_, message, _, err = s.store.DismissQualityAgentAskAtomic(ctx, ask.ResourceID, leadMessage, notices, time.Now().UTC())
		return err
	})
	if lockErr != nil {
		if errors.Is(lockErr, store.ErrQualityAgentBusy) {
			return nil, connect.NewError(connect.CodeAborted,
				errors.New("another message is being sent; retry"))
		}
		switch common.ErrorCode(lockErr) {
		case common.Conflict:
			return nil, connect.NewError(connect.CodeFailedPrecondition, lockErr)
		case common.NotFound:
			return nil, connect.NewError(connect.CodeNotFound, lockErr)
		}
		return nil, lockErr
	}
	_, _ = s.store.AdvanceQualityAgentSessionWake(ctx, conversation.ResourceID, time.Now().UTC())
	return connect.NewResponse(convertQualityAgentMessageToV1(workspaceID, conversation.ResourceID, message)), nil
}

// qualityAgentAskForReply resolves the ask an owner's message answers and proves
// it belongs to the same agent. A stale or foreign ask is refused rather than
// silently answering a question the owner was not looking at.
func (s *DataQualityService) qualityAgentAskForReply(ctx context.Context, workspaceID string, agentID int64, name string) (*store.QualityAgentAskMessage, error) {
	resourceID := strings.TrimPrefix(strings.TrimSpace(name), qualityAgentAskPrefix)
	if resourceID == "" || strings.Contains(resourceID, "/") {
		tokens, err := common.GetNameParentTokens(name, common.WorkspacePrefix, qualityAgentAskPrefix)
		if err != nil {
			return nil, connectErrorInvalid("invalid quality agent ask name")
		}
		if tokens[0] != workspaceID {
			return nil, connectErrorInvalid("the ask belongs to another workspace")
		}
		resourceID = tokens[1]
	}
	ask, err := s.store.GetQualityAgentAskByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if ask == nil || ask.AgentID != agentID {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("the ask was not found"))
	}
	return ask, nil
}

// ListQualityAgentConversationMessages reads the front-office conversation. The
// owner and administrators may read; the UI polls with after_seq.
func (s *DataQualityService) ListQualityAgentConversationMessages(ctx context.Context, req *connect.Request[v1pb.ListQualityAgentConversationMessagesRequest]) (*connect.Response[v1pb.ListQualityAgentConversationMessagesResponse], error) {
	workspaceID, resourceID, err := s.qualityAgentResourceID(ctx, req.Msg.GetParent())
	if err != nil {
		return nil, err
	}
	agent, err := s.qualityAgentConversationAccess(ctx, resourceID, false)
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
	find := &store.FindQualityAgentTranscriptMessage{
		SessionID:       conversation.ID,
		Limit:           limit,
		FrontOfficeOnly: true,
	}
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
		// wake/event inputs, tool results and compaction rows stay on the work-session
		// transcript. Only an owner's question and the assistant's answer belong here.
		// Strip tool calls as well; the model's raw tool arguments are not part of the
		// owner's conversation.
		isOwnerQuestion := message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER &&
			message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK
		if !isOwnerQuestion && message.Role != storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT {
			continue
		}
		message.ToolCalls = &storepb.QualityToolCallList{}
		response.QualityAgentConversationMessages = append(response.QualityAgentConversationMessages,
			convertQualityAgentMessageToV1(workspaceID, conversation.ResourceID, message))
	}
	return connect.NewResponse(response), nil
}

// qualityAgentConversationAccess loads the agent and enforces the owner/admin
// rule. requireOwner rejects administrators who are not the owner.
func (s *DataQualityService) qualityAgentConversationAccess(ctx context.Context, resourceID string, requireOwner bool) (*store.QualityAgentMessage, error) {
	agent, err := s.store.GetQualityAgentByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, connect.NewError(connect.CodeNotFound,
			errors.Errorf("quality agent %q not found", resourceID))
	}
	owner, ownerActive := false, false
	if agent.OwnerPrincipalID != nil {
		if ownerUser, ownerErr := s.store.GetUserByID(ctx, int(*agent.OwnerPrincipalID)); ownerErr == nil &&
			ownerUser != nil && !ownerUser.MemberDeleted {
			ownerActive = true
			owner = int32(currentUserID(ctx)) == *agent.OwnerPrincipalID
		}
	}
	if requireOwner {
		if owner {
			return agent, nil
		}
		// An ownerless agent (the owner was deleted) falls back to the workspace
		// administrators so its escalations are not permanently stuck.
		if !ownerActive {
			admin, adminErr := s.callerIsQualityAgentAdmin(ctx)
			if adminErr == nil && admin {
				return agent, nil
			}
		}
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("only the agent's owner may talk to it"))
	}
	if owner {
		return agent, nil
	}
	admin, err := s.callerIsQualityAgentAdmin(ctx)
	if err != nil {
		return nil, err
	}
	if !admin {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("only the owner or an administrator may read this conversation"))
	}
	return agent, nil
}

// qualityAgentConversationName renders the conversation's resource name for an agent
// view; an empty string means no conversation exists yet.
func qualityAgentConversationName(workspaceID string, conversation *store.QualityAgentSessionMessage) string {
	if conversation == nil {
		return ""
	}
	return common.FormatWorkspace(workspaceID) + "/" + qualityAgentSessionPrefix + conversation.ResourceID
}
