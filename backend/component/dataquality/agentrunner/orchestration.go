package agentrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// maxAgentMessageBytes bounds one agent-to-agent message. The lead does not see
	// the subagent transcript, so a report must fit in one bounded message.
	maxAgentMessageBytes = 8 << 10
	// maxListedSubagents bounds one list_subagents page.
	maxListedSubagents = 50
)

// resolveSubagent resolves a model-facing address to one live subagent of the
// caller's agent. Resolution grants no authority; the caller is already the lead.
func (e *Engine) resolveSubagent(ctx context.Context, session *store.QualityAgentSessionMessage, address string) (*store.QualityAgentSessionMessage, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, errors.New("a subagent name or target id is required")
	}
	subagent, err := e.store.GetQualityAgentSubagentByAddress(ctx, session.AgentID, address)
	if err != nil {
		return nil, err
	}
	if subagent == nil {
		return nil, common.Errorf(common.NotFound, "no live subagent matches %q", address)
	}
	if subagent.ParentSessionID == nil || *subagent.ParentSessionID != session.ID {
		return nil, common.Errorf(common.NotAuthorized, "subagent %q is not a child of this lead", address)
	}
	return subagent, nil
}

// deliverAgentMessage appends one relay message to the recipient and wakes it.
func (e *Engine) deliverAgentMessage(ctx context.Context, sender *store.QualityAgentSessionMessage, senderName string, recipient *store.QualityAgentSessionMessage, body string, intent storepb.QualityAgentMessageIntent, replyTo string) (*store.QualityAgentTranscriptMessage, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("a message body is required")
	}
	if len(body) > maxAgentMessageBytes {
		return nil, errors.Errorf("the message is too long (max %d bytes)", maxAgentMessageBytes)
	}
	content := fmt.Sprintf("Agent %s sent a message:\n%s", senderName, body)
	input := &store.QualityAgentTranscriptMessage{
		SessionID:       recipient.ID,
		WorkspaceID:     recipient.WorkspaceID,
		Wake:            recipient.WakeCount + 1,
		Role:            storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:          storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:         content,
		SenderSessionID: &sender.ID,
		SenderName:      senderName,
		Intent:          intent,
		Audience:        storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		ReplyTo:         replyTo,
		IdempotencyKey:  fmt.Sprintf("agentmsg:%d:%d:%s", sender.ID, recipient.ID, contentHash(content)),
	}
	return e.store.DeliverQualityAgentMessage(ctx, recipient, input)
}

// sendSubagentMessage is the lead's model tool.
func (e *Engine) sendSubagentMessage(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	if !isLeadSession(session) {
		return "", errors.New("only the lead can message a subagent")
	}
	var input struct {
		Agent   string `json:"agent"`
		Message string `json:"message"`
		Intent  string `json:"intent"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid send_subagent_message arguments")
	}
	subagent, err := e.resolveSubagent(ctx, session, input.Agent)
	if err != nil {
		return "", err
	}
	intent := parseMessageIntent(input.Intent)
	message, err := e.deliverAgentMessage(ctx, session, "lead", subagent, input.Message, intent, "")
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{
		"delivered":  true,
		"message_id": message.ID,
		"agent":      subagent.SubagentName,
		"state":      subagent.State.String(),
	})
}

// sendLeadMessage is the subagent's model tool.
func (e *Engine) sendLeadMessage(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	if !isSubagentSession(session) {
		return "", errors.New("only a subagent can message its lead")
	}
	var input struct {
		Message string `json:"message"`
		Intent  string `json:"intent"`
		Case    string `json:"case"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid send_lead_message arguments")
	}
	lead, err := e.leadFor(ctx, session)
	if err != nil {
		return "", err
	}
	intent := parseMessageIntent(input.Intent)
	if intent == storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_UNSPECIFIED {
		intent = storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_REPORT
	}
	message, err := e.deliverAgentMessage(ctx, session, session.SubagentName, lead, input.Message, intent, "")
	if err != nil {
		return "", err
	}
	if input.Case != "" {
		if _, _, err := e.raiseRequest(ctx, session, store.QualityAgentRequestMessage{
			Kind:           store.QualityAgentRequestKindQuestion,
			CaseResourceID: input.Case,
			Title:          "Subagent question",
			Question:       input.Message,
			DedupeKey:      fmt.Sprintf("target:%s:case:%s:question", session.TargetGUID, input.Case),
		}); err != nil {
			return "", err
		}
	}
	return marshalJSON(map[string]any{"delivered": true, "message_id": message.ID, "to": "lead"})
}

func (e *Engine) leadFor(ctx context.Context, session *store.QualityAgentSessionMessage) (*store.QualityAgentSessionMessage, error) {
	if session.ParentSessionID != nil {
		lead, err := e.store.GetQualityAgentSessionByID(ctx, *session.ParentSessionID)
		if err != nil {
			return nil, err
		}
		if lead != nil {
			return lead, nil
		}
	}
	lead, err := e.store.GetQualityAgentConversation(ctx, session.AgentID)
	if err != nil {
		return nil, err
	}
	if lead == nil {
		return nil, errors.New("the subagent has no lead conversation")
	}
	return lead, nil
}

// raiseRequest records one subagent request and tells the lead once. A retried
// request returns the existing active row and does not append a second message.
func (e *Engine) raiseRequest(ctx context.Context, session *store.QualityAgentSessionMessage, message store.QualityAgentRequestMessage) (*store.QualityAgentRequestMessage, bool, error) {
	message.WorkspaceID = session.WorkspaceID
	message.AgentID = session.AgentID
	message.OriginSessionID = &session.ID
	message.TargetGUID = session.TargetGUID
	message.TargetType = storepb.MetaType(session.TargetType)
	if message.DedupeKey == "" {
		message.DedupeKey = fmt.Sprintf("target:%s:kind:%d", session.TargetGUID, message.Kind)
	}
	created, isNew, err := e.store.CreateQualityAgentRequest(ctx, &message)
	if err != nil {
		return nil, false, err
	}
	if !isNew {
		return created, false, nil
	}
	lead, err := e.leadFor(ctx, session)
	if err != nil {
		return nil, false, err
	}
	body := fmt.Sprintf("[question] request %s: %s", requestName(created.ResourceID), created.Question)
	if _, err := e.deliverAgentMessage(ctx, session, session.SubagentName, lead, body,
		storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION, created.ResourceID); err != nil {
		return nil, false, err
	}
	return created, true, nil
}

// listSubagents returns a bounded page of the lead's roster.
func (e *Engine) listSubagents(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	if !isLeadSession(session) {
		return "", errors.New("only the lead has a subagent roster")
	}
	var input struct {
		Query string `json:"query"`
		State string `json:"state"`
		Limit int    `json:"limit"`
	}
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid list_subagents arguments")
		}
	}
	limit := input.Limit
	if limit <= 0 || limit > maxListedSubagents {
		limit = maxListedSubagents
	}
	find := &store.FindQualityAgentSessionMessage{
		AgentID: &session.AgentID,
		Role:    storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_SUBAGENT,
		Limit:   &limit,
	}
	sessions, err := e.store.ListQualityAgentSessions(ctx, find)
	if err != nil {
		return "", err
	}
	query := strings.ToLower(strings.TrimSpace(input.Query))
	rows := make([]map[string]any, 0, len(sessions))
	for _, subagent := range sessions {
		if query != "" && !strings.Contains(strings.ToLower(subagent.SubagentName+" "+subagent.TargetGUID), query) {
			continue
		}
		if input.State != "" && !strings.EqualFold(input.State, subagent.State.String()) {
			continue
		}
		row := map[string]any{
			"name":          subagent.SubagentName,
			"target":        subagent.TargetGUID,
			"state":         subagent.State.String(),
			"mode":          subagent.Mode.String(),
			"next_wake":     subagent.NextWakeAt,
			"last_reviewed": subagent.LastReviewedAt,
		}
		rows = append(rows, row)
	}
	return marshalJSON(map[string]any{"subagents": rows, "count": len(rows)})
}

// getSubagent returns one subagent's status and open work items.
func (e *Engine) getSubagent(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	var input struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid get_subagent arguments")
	}
	subagent, err := e.resolveSubagent(ctx, session, input.Agent)
	if err != nil {
		return "", err
	}
	cases, _ := e.store.ListOpenQualityAgentCasesByTarget(ctx, session.AgentID, subagent.TargetGUID, 5)
	caseRows := make([]map[string]any, 0, len(cases))
	for _, openCase := range cases {
		caseRows = append(caseRows, map[string]any{
			"case":          requestName(openCase.ResourceID),
			"state":         openCase.State.Name(),
			"failure_class": openCase.FailureClass,
			"summary":       openCase.Summary,
		})
	}
	return marshalJSON(map[string]any{
		"name":          subagent.SubagentName,
		"target":        subagent.TargetGUID,
		"state":         subagent.State.String(),
		"mode":          subagent.Mode.String(),
		"next_wake":     subagent.NextWakeAt,
		"last_reviewed": subagent.LastReviewedAt,
		"open_cases":    caseRows,
	})
}

func (e *Engine) listRequests(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	var input struct {
		Status string `json:"status"`
		Target string `json:"target"`
		Limit  int    `json:"limit"`
	}
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid list_requests arguments")
		}
	}
	statuses := []store.QualityAgentRequestStatus{
		store.QualityAgentRequestStatusOpen,
		store.QualityAgentRequestStatusForwarded,
	}
	switch strings.ToUpper(input.Status) {
	case "OPEN":
		statuses = []store.QualityAgentRequestStatus{store.QualityAgentRequestStatusOpen}
	case "FORWARDED":
		statuses = []store.QualityAgentRequestStatus{store.QualityAgentRequestStatusForwarded}
	case "CLOSED":
		statuses = nil
	case "":
	default:
		return "", errors.Errorf("unknown request status %q", input.Status)
	}
	requests, err := e.store.ListQualityAgentRequests(ctx, session.AgentID, statuses, input.Target, input.Limit)
	if err != nil {
		return "", err
	}
	rows := make([]map[string]any, 0, len(requests))
	for _, request := range requests {
		rows = append(rows, map[string]any{
			"request":  requestName(request.ResourceID),
			"target":   request.TargetGUID,
			"kind":     int(request.Kind),
			"status":   int(request.Status),
			"title":    request.Title,
			"question": request.Question,
			"case":     request.CaseResourceID,
		})
	}
	return marshalJSON(map[string]any{"requests": rows, "count": len(rows)})
}

func (e *Engine) answerRequest(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	var input struct {
		Request string `json:"request"`
		Answer  string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid answer_request arguments")
	}
	request, err := e.openRequest(ctx, session, input.Request)
	if err != nil {
		return "", err
	}
	if request.OriginSessionID == nil {
		return "", errors.New("the request has no origin subagent")
	}
	subagent, err := e.store.GetQualityAgentSessionByID(ctx, *request.OriginSessionID)
	if err != nil {
		return "", err
	}
	if subagent == nil {
		return "", common.Errorf(common.NotFound, "the request's subagent no longer exists")
	}
	if _, applied, err := e.store.UpdateQualityAgentRequestStatus(ctx, request.ResourceID,
		request.Status, store.QualityAgentRequestStatusAnswered, nil, input.Answer, nil, e.now()); err != nil {
		return "", err
	} else if !applied {
		return marshalJSON(map[string]any{"answered": false, "reason": "the request already moved on"})
	}
	if _, err := e.deliverAgentMessage(ctx, session, "lead", subagent, "[answer] "+input.Answer,
		storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER, request.ResourceID); err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"answered": true, "request": requestName(request.ResourceID)})
}

func (e *Engine) forwardRequest(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string, control *wakeControl) (string, error) {
	var input struct {
		Requests []string `json:"requests"`
		Question string   `json:"question"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid forward_request arguments")
	}
	if strings.TrimSpace(input.Question) == "" {
		return "", errors.New("a forward_request needs a question for the owner")
	}
	requests := make([]*store.QualityAgentRequestMessage, 0, len(input.Requests))
	for _, name := range input.Requests {
		request, err := e.openRequest(ctx, session, name)
		if err != nil {
			return "", err
		}
		requests = append(requests, request)
	}
	ask, err := e.createOwnerAsk(ctx, session, input.Question, requests, control)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"ask": askName(ask.ResourceID), "requests": len(requests)})
}

func (e *Engine) askOwner(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string, control *wakeControl) (string, error) {
	var input struct {
		Question string   `json:"question"`
		Requests []string `json:"requests"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid ask_owner arguments")
	}
	if strings.TrimSpace(input.Question) == "" {
		return "", errors.New("ask_owner needs a question")
	}
	requests := make([]*store.QualityAgentRequestMessage, 0, len(input.Requests))
	for _, name := range input.Requests {
		request, err := e.openRequest(ctx, session, name)
		if err != nil {
			return "", err
		}
		requests = append(requests, request)
	}
	ask, err := e.createOwnerAsk(ctx, session, input.Question, requests, control)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"ask": askName(ask.ResourceID), "requests": len(requests)})
}

func (e *Engine) replyOwner(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string, control *wakeControl) (string, error) {
	var input struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid reply_owner arguments")
	}
	if strings.TrimSpace(input.Message) == "" {
		return "", errors.New("reply_owner needs a message")
	}
	if _, err := e.appendOwnerMessage(ctx, session, input.Message, "", storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_MESSAGE, control); err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"delivered": true, "to": "owner"})
}

func (e *Engine) createOwnerAsk(ctx context.Context, session *store.QualityAgentSessionMessage, question string, requests []*store.QualityAgentRequestMessage, control *wakeControl) (*store.QualityAgentAskMessage, error) {
	message, err := e.appendOwnerMessage(ctx, session, question, "", storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION, control)
	if err != nil {
		return nil, err
	}
	ask, err := e.store.CreateQualityAgentAsk(ctx, &store.QualityAgentAskMessage{
		WorkspaceID:           session.WorkspaceID,
		AgentID:               session.AgentID,
		ConversationSessionID: &session.ID,
		Kind:                  store.QualityAgentAskKindQuestion,
		MessageSeq:            message.Seq,
		Title:                 "The quality agent needs a decision",
		Question:              question,
	})
	if err != nil {
		return nil, err
	}
	for _, request := range requests {
		if _, _, err := e.store.UpdateQualityAgentRequestStatus(ctx, request.ResourceID,
			request.Status, store.QualityAgentRequestStatusForwarded, &ask.ID, "", nil, e.now()); err != nil {
			return nil, err
		}
	}
	return ask, nil
}

func (e *Engine) appendOwnerMessage(ctx context.Context, session *store.QualityAgentSessionMessage, content, replyTo string, intent storepb.QualityAgentMessageIntent, control *wakeControl) (*store.QualityAgentTranscriptMessage, error) {
	message, err := e.appendMessage(ctx, session, &store.QualityAgentTranscriptMessage{
		Wake:           session.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_MODEL,
		Content:        content,
		SenderName:     "lead",
		Intent:         intent,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		ReplyTo:        replyTo,
		IdempotencyKey: fmt.Sprintf("ownermsg:%d:%s", session.WakeCount+1, contentHash(content)),
	})
	if err != nil {
		return nil, err
	}
	if control != nil {
		control.ownerFacing = true
		control.ownerMessageWritten = true
	}
	return message, nil
}

func (e *Engine) openRequest(ctx context.Context, session *store.QualityAgentSessionMessage, name string) (*store.QualityAgentRequestMessage, error) {
	resourceID := strings.TrimPrefix(strings.TrimSpace(name), "qualityAgentRequests/")
	request, err := e.store.GetQualityAgentRequestByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, common.Errorf(common.NotFound, "request %q not found", name)
	}
	if request.AgentID != session.AgentID {
		return nil, common.Errorf(common.NotAuthorized, "request %q belongs to another agent", name)
	}
	if request.Status != store.QualityAgentRequestStatusOpen && request.Status != store.QualityAgentRequestStatusForwarded {
		return nil, common.Errorf(common.Conflict, "request %q is already closed", name)
	}
	return request, nil
}

func parseMessageIntent(value string) storepb.QualityAgentMessageIntent {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "REPORT":
		return storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_REPORT
	case "QUESTION":
		return storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION
	case "ANSWER":
		return storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER
	case "ASSIGN":
		return storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ASSIGN
	case "NOTICE":
		return storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_NOTICE
	default:
		return storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_MESSAGE
	}
}

func requestName(resourceID string) string { return "qualityAgentRequests/" + resourceID }
func askName(resourceID string) string     { return "qualityAgentAsks/" + resourceID }
