package agentrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
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
		IdempotencyKey:  fmt.Sprintf("agentmsg:%d:%d:%x", sender.ID, recipient.ID, contentHash(content)),
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
	// Queued is still delivered: the message waits in the transcript. Wakeable says
	// whether the recipient can consume it now, so the lead knows to involve the owner
	// instead of expecting a report.
	wakeable := subagent.State != storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED ||
		store.QualityAgentSessionBlockedCodeRetryable(subagent.BlockedCode)
	return marshalJSON(map[string]any{
		"delivered":  true,
		"wakeable":   wakeable,
		"message_id": message.ID,
		"agent":      subagent.SubagentName,
		"state":      subagent.State.String(),
		"blocked":    subagent.BlockedCode,
	})
}

// sendLeadMessage is the subagent's model tool.
func (e *Engine) sendLeadMessage(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	if !isSubagentSession(session) {
		return "", errors.New("only a subagent can message its lead")
	}
	var input struct {
		Message     string           `json:"message"`
		Intent      string           `json:"intent"`
		Case        string           `json:"case"`
		QuestionKey string           `json:"question_key"`
		Options     []askOptionInput `json:"options"`
		MultiSelect bool             `json:"multi_select"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid send_lead_message arguments")
	}
	intent := parseMessageIntent(input.Intent)
	if strings.TrimSpace(input.Intent) == "" {
		intent = storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_REPORT
	}
	if intent == storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION {
		if strings.TrimSpace(input.Message) == "" {
			return "", errors.New("a QUESTION needs a message")
		}
		caseID := normalizeCaseResourceID(input.Case)
		questionKey := strings.TrimSpace(input.QuestionKey)
		if questionKey != "" && !questionKeyPattern.MatchString(questionKey) {
			return "", errors.New("question_key must be a short lowercase slug such as timeout or pause-check: 2-41 characters, starting with a letter, then letters, digits and hyphens")
		}
		dedupe := questionDedupeKey(session.TargetGUID, caseID, questionKey, input.Message)
		options := make([]*storepb.QualityAgentAskQuestionOption, 0, len(input.Options))
		for _, option := range input.Options {
			options = append(options, &storepb.QualityAgentAskQuestionOption{
				Label:       option.Label,
				Description: option.Description,
			})
		}
		if err := dataquality.ValidateAskQuestions([]*storepb.QualityAgentAskQuestion{{
			Id:          "q1",
			Question:    input.Message,
			Options:     options,
			MultiSelect: input.MultiSelect,
		}}); err != nil {
			return "", err
		}
		request, created, err := e.raiseRequest(ctx, session, store.QualityAgentRequestMessage{
			Kind:                        store.QualityAgentRequestKindQuestion,
			CaseResourceID:              input.Case,
			Title:                       "Subagent question",
			Question:                    input.Message,
			Options:                     options,
			MultiSelect:                 input.MultiSelect,
			DedupeKey:                   dedupe,
			QuestionKey:                 questionKey,
			ConflictOnDifferentQuestion: true,
		})
		if err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{
			"delivered":    true,
			"to":           "lead",
			"request":      requestName(request.ResourceID),
			"duplicate":    !created,
			"question_key": request.QuestionKey,
		})
	}
	lead, err := e.leadFor(ctx, session)
	if err != nil {
		return "", err
	}
	message, err := e.deliverAgentMessage(ctx, session, session.SubagentName, lead, input.Message, intent, "")
	if err != nil {
		return "", err
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
			if lead.AgentID != session.AgentID || lead.ArchivedAt != nil {
				return nil, common.Errorf(common.NotAuthorized, "the subagent's lead is not available")
			}
			return lead, nil
		}
	}
	lead, err := e.store.GetQualityAgentConversation(ctx, session.AgentID)
	if err != nil {
		return nil, err
	}
	if lead == nil || lead.AgentID != session.AgentID || lead.ArchivedAt != nil {
		return nil, errors.New("the subagent has no lead conversation")
	}
	return lead, nil
}

// raiseRequest records one subagent request and tells the lead once. A retried
// request returns the existing active row and does not append a second message. The
// second result reports whether this call created the row, which lets a caller that
// raised it before a write undo its own request when that write is refused.
func (e *Engine) raiseRequest(ctx context.Context, session *store.QualityAgentSessionMessage, message store.QualityAgentRequestMessage) (*store.QualityAgentRequestMessage, bool, error) {
	message.WorkspaceID = session.WorkspaceID
	message.AgentID = session.AgentID
	message.OriginSessionID = &session.ID
	message.TargetGUID = session.TargetGUID
	message.CaseResourceID = normalizeCaseResourceID(message.CaseResourceID)
	if message.CaseResourceID != "" {
		// A case id is a namespace, not a free-form suffix: a colon would let "c1:timeout"
		// collide with case c1 plus key timeout. The row must also belong to this target, so
		// a subagent cannot address another target's case.
		if strings.Contains(message.CaseResourceID, ":") {
			return nil, false, common.Errorf(common.Invalid, "case %q is not a valid case id", message.CaseResourceID)
		}
		openCase, err := e.store.GetQualityAgentCaseByResourceID(ctx, message.CaseResourceID)
		if err != nil {
			return nil, false, err
		}
		if openCase == nil || openCase.AgentID != session.AgentID || openCase.TargetGUID != session.TargetGUID {
			return nil, false, common.Errorf(common.Invalid, "case %q does not belong to this target", message.CaseResourceID)
		}
	}
	message.TargetType = storepb.MetaType(session.TargetType)
	if message.DedupeKey == "" {
		message.DedupeKey = fmt.Sprintf("target:%s:kind:%d", session.TargetGUID, message.Kind)
	}
	lead, err := e.leadFor(ctx, session)
	if err != nil {
		return nil, false, err
	}
	content := fmt.Sprintf("Agent %s sent a message:\n[question] %s", session.SubagentName, message.Question)
	notice := &store.QualityAgentTranscriptMessage{
		Wake:            lead.WakeCount + 1,
		Role:            storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:          storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:         content,
		SenderSessionID: &session.ID,
		SenderName:      session.SubagentName,
		Intent:          storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION,
		Audience:        storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		IdempotencyKey:  fmt.Sprintf("request-notice:%s:%d", message.DedupeKey, session.ID),
	}
	created, isNew, err := e.store.RaiseQualityAgentRequest(ctx, &message, lead, notice)
	if err != nil {
		return nil, false, err
	}
	return created, isNew, nil
}

// dismissRaisedRequest throws away a request this wake raised before the write it
// describes was refused. A request that already existed is left alone: it reports a
// real earlier escalation, not this attempt.
func (e *Engine) dismissRaisedRequest(ctx context.Context, request *store.QualityAgentRequestMessage, created bool, reason string) {
	if !created || request == nil || request.ResourceID == "" {
		return
	}
	if _, _, err := e.store.UpdateQualityAgentRequestStatus(ctx, request.ResourceID,
		store.QualityAgentRequestStatusOpen, store.QualityAgentRequestStatusDismissed,
		nil, reason, nil, e.now()); err != nil {
		slog.Warn("failed to dismiss a request whose write was refused",
			slog.String("request", request.ResourceID), log.WithError(err))
	}
}

// listSubagents returns a bounded page of the lead's roster. Filtering and paging are
// SQL predicates: a ten-thousand-target agent cannot be filtered in Go after a LIMIT,
// because the page would miss most matches.
func (e *Engine) listSubagents(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	if !isLeadSession(session) {
		return "", errors.New("only the lead has a subagent roster")
	}
	var input struct {
		Query  string `json:"query"`
		State  string `json:"state"`
		Mode   string `json:"mode"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
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
	offset := input.Offset
	if offset < 0 {
		offset = 0
	}
	find := &store.FindQualityAgentSessionMessage{
		AgentID: &session.AgentID,
		Role:    storepb.QualityAgentSessionRole_QUALITY_AGENT_SESSION_ROLE_SUBAGENT,
		Search:  strings.TrimSpace(input.Query),
		Limit:   &limit,
		Offset:  &offset,
	}
	if state := strings.TrimSpace(input.State); state != "" {
		key := strings.ToUpper(state)
		if !strings.HasPrefix(key, "QUALITY_AGENT_SESSION_STATE_") {
			key = "QUALITY_AGENT_SESSION_STATE_" + key
		}
		value, ok := storepb.QualityAgentSessionState_value[key]
		if !ok {
			return "", errors.Errorf("unknown subagent state %q", input.State)
		}
		find.States = []storepb.QualityAgentSessionState{storepb.QualityAgentSessionState(value)}
	}
	if mode := strings.TrimSpace(input.Mode); mode != "" {
		key := strings.ToUpper(mode)
		if !strings.HasPrefix(key, "QUALITY_AGENT_SESSION_MODE_") {
			key = "QUALITY_AGENT_SESSION_MODE_" + key
		}
		value, ok := storepb.QualityAgentSessionMode_value[key]
		if !ok {
			return "", errors.Errorf("unknown subagent mode %q", input.Mode)
		}
		find.Mode = storepb.QualityAgentSessionMode(value)
	}
	sessions, err := e.store.ListQualityAgentSessions(ctx, find)
	if err != nil {
		return "", err
	}
	rows := make([]map[string]any, 0, len(sessions))
	for _, subagent := range sessions {
		rows = append(rows, map[string]any{
			"name":          subagent.SubagentName,
			"target":        subagent.TargetGUID,
			"resource":      common.FormatWorkspace(subagent.WorkspaceID) + "/qualityAgentSessions/" + subagent.ResourceID,
			"state":         subagent.State.String(),
			"mode":          subagent.Mode.String(),
			"blocked":       subagent.BlockedCode,
			"next_wake":     subagent.NextWakeAt,
			"last_reviewed": subagent.LastReviewedAt,
		})
	}
	return marshalJSON(map[string]any{
		"subagents":   rows,
		"count":       len(rows),
		"next_offset": offset + len(rows),
		"has_more":    len(rows) == limit,
	})
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
			"case":          "qualityAgentCases/" + openCase.ResourceID,
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
		statuses = []store.QualityAgentRequestStatus{
			store.QualityAgentRequestStatusAnswered,
			store.QualityAgentRequestStatusResolved,
			store.QualityAgentRequestStatusDismissed,
		}
	case "":
	default:
		return "", errors.Errorf("unknown request status %q", input.Status)
	}
	requests, err := e.store.ListQualityAgentRequests(ctx, session.AgentID, statuses, input.Target, input.Limit)
	if err != nil {
		return "", err
	}
	askIDs := make([]int64, 0, len(requests))
	for _, request := range requests {
		if request.AskID != nil {
			askIDs = append(askIDs, *request.AskID)
		}
	}
	asksByID, err := e.store.ListQualityAgentAsksByIDs(ctx, askIDs)
	if err != nil {
		return "", err
	}
	rows := make([]map[string]any, 0, len(requests))
	for _, request := range requests {
		ask := ""
		if request.AskID != nil {
			ask = askResourceName(asksByID[*request.AskID])
		}
		rows = append(rows, map[string]any{
			"request":      requestName(request.ResourceID),
			"ask":          ask,
			"target":       request.TargetGUID,
			"kind":         int(request.Kind),
			"status":       int(request.Status),
			"title":        request.Title,
			"question":     request.Question,
			"case":         request.CaseResourceID,
			"options":      requestOptionsView(request.Options),
			"multi_select": request.MultiSelect,
		})
	}
	return marshalJSON(map[string]any{"requests": rows, "count": len(rows)})
}

// askResourceName renders the ask a forwarded request belongs to. An empty string
// means the request has not been forwarded yet.
func askResourceName(ask *store.QualityAgentAskMessage) string {
	if ask == nil {
		return ""
	}
	return common.FormatWorkspace(ask.WorkspaceID) + "/" + askName(ask.ResourceID)
}

// requestOptionsView renders a subagent's offered choices for list_requests.
func requestOptionsView(options []*storepb.QualityAgentAskQuestionOption) []map[string]string {
	view := make([]map[string]string, 0, len(options))
	for _, option := range options {
		if option == nil {
			continue
		}
		view = append(view, map[string]string{"label": option.Label, "description": option.Description})
	}
	return view
}

// askOptionInput is one selectable answer in the ask_owner/forward_request
// arguments. The label is both the owner-facing text and the answer value.
type askOptionInput struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// askQuestionInput is the model-facing shape of one owner decision.
type askQuestionInput struct {
	ID          string           `json:"id"`
	Question    string           `json:"question"`
	Header      string           `json:"header"`
	Detail      string           `json:"detail"`
	Options     []askOptionInput `json:"options"`
	MultiSelect bool             `json:"multi_select"`
	// Requests are the request names this question decides. A single-question ask
	// may leave it empty to cover all of the ask's requests.
	Requests []string `json:"requests"`
}

// toStore converts one model argument into the stored question.
func (input askQuestionInput) toStore(id string) *storepb.QualityAgentAskQuestion {
	options := make([]*storepb.QualityAgentAskQuestionOption, 0, len(input.Options))
	for _, option := range input.Options {
		options = append(options, &storepb.QualityAgentAskQuestionOption{
			Label:       option.Label,
			Description: option.Description,
		})
	}
	return &storepb.QualityAgentAskQuestion{
		Id:          id,
		Header:      input.Header,
		Question:    input.Question,
		Detail:      input.Detail,
		Options:     options,
		MultiSelect: input.MultiSelect,
		Requests:    append([]string(nil), input.Requests...),
	}
}

// askRequestInput is the model-facing shape of an ask_owner/forward_request call:
// either a questions batch or the legacy single question.
type askRequestInput struct {
	Requests  []string           `json:"requests"`
	Questions []askQuestionInput `json:"questions"`
	// The legacy single-question form, still accepted for an in-flight transcript.
	Question    string           `json:"question"`
	Header      string           `json:"header"`
	Detail      string           `json:"detail"`
	Options     []askOptionInput `json:"options"`
	MultiSelect bool             `json:"multi_select"`
}

// questionInputs returns the questions the call asked for. A call may use either
// the batch form or the legacy single form, never both.
func (input askRequestInput) questionInputs() ([]askQuestionInput, error) {
	if len(input.Questions) > 0 && strings.TrimSpace(input.Question) != "" {
		return nil, errors.New("use questions or question, not both")
	}
	if len(input.Questions) > 0 {
		return input.Questions, nil
	}
	if strings.TrimSpace(input.Question) == "" {
		return nil, errors.New("an ask needs a question")
	}
	return []askQuestionInput{{
		Question:    input.Question,
		Header:      input.Header,
		Detail:      input.Detail,
		Options:     input.Options,
		MultiSelect: input.MultiSelect,
	}}, nil
}

// storeQuestions assigns the stable question ids and converts them to the store
// shape. The answer echoes these ids.
func (input askRequestInput) storeQuestions() ([]*storepb.QualityAgentAskQuestion, []string, error) {
	inputs, err := input.questionInputs()
	if err != nil {
		return nil, nil, err
	}
	questions := make([]*storepb.QualityAgentAskQuestion, 0, len(inputs))
	ids := make([]string, 0, len(inputs))
	for index, question := range inputs {
		id := strings.TrimSpace(question.ID)
		if id == "" {
			id = fmt.Sprintf("q%d", index+1)
		}
		questions = append(questions, question.toStore(id))
		ids = append(ids, id)
	}
	return questions, ids, nil
}

func (e *Engine) answerRequest(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	var input struct {
		Request  string   `json:"request"`
		Answer   string   `json:"answer"`
		Selected []string `json:"selected"`
		Custom   string   `json:"custom"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid answer_request arguments")
	}
	resolution := strings.TrimSpace(input.Answer)
	if resolution == "" {
		values := append([]string(nil), input.Selected...)
		if custom := strings.TrimSpace(input.Custom); custom != "" {
			values = append(values, custom)
		}
		resolution = strings.Join(values, ", ")
	}
	if resolution == "" {
		return "", errors.New("an answer needs text or a selection")
	}
	request, err := e.answerableRequest(ctx, session, input.Request)
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
	if subagent == nil || subagent.AgentID != session.AgentID || subagent.ArchivedAt != nil {
		return "", common.Errorf(common.NotFound, "the request's subagent is no longer available")
	}
	content := dataquality.BuildDecisionAnswerEnvelope(request, input.Answer, input.Selected, input.Custom)
	_, _, applied, err := e.store.AnswerQualityAgentRequestAtomic(ctx, request.ResourceID,
		request.Status, resolution, subagent, &store.QualityAgentTranscriptMessage{
			Wake:            subagent.WakeCount + 1,
			Role:            storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
			Origin:          storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
			Content:         content,
			SenderSessionID: &session.ID,
			SenderName:      "lead",
			Intent:          storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
			Audience:        storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
			ReplyTo:         common.FormatWorkspace(session.WorkspaceID) + "/qualityAgentRequests/" + request.ResourceID,
			IdempotencyKey:  fmt.Sprintf("answer:%s", request.ResourceID),
		}, e.now())
	if err != nil {
		return "", err
	}
	if !applied {
		return marshalJSON(map[string]any{"answered": false, "reason": "the request already moved on"})
	}
	return marshalJSON(map[string]any{"answered": true, "request": requestName(request.ResourceID)})
}

func (e *Engine) forwardRequest(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string, control *wakeControl) (string, error) {
	var input askRequestInput
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid forward_request arguments")
	}
	questions, ids, err := input.storeQuestions()
	if err != nil {
		return "", err
	}
	requests := make([]*store.QualityAgentRequestMessage, 0, len(input.Requests))
	for _, name := range input.Requests {
		request, err := e.openRequest(ctx, session, name)
		if err != nil {
			return "", err
		}
		requests = append(requests, request)
	}
	// A single forwarded request can hand its own choices to the owner. With more
	// than one request the choices may conflict, so the lead authors the batch.
	if len(requests) == 1 && len(questions) == 1 && len(questions[0].Options) == 0 && len(requests[0].Options) > 0 {
		questions[0].Options = cloneAskOptions(requests[0].Options)
		questions[0].MultiSelect = requests[0].MultiSelect
	}
	ask, err := e.createOwnerAsk(ctx, session, questions, requests, control)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"ask": askName(ask.ResourceID), "questions": ids, "requests": len(requests)})
}

func (e *Engine) askOwner(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string, control *wakeControl) (string, error) {
	var input askRequestInput
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid ask_owner arguments")
	}
	questions, ids, err := input.storeQuestions()
	if err != nil {
		return "", err
	}
	requests := make([]*store.QualityAgentRequestMessage, 0, len(input.Requests))
	for _, name := range input.Requests {
		request, err := e.openRequest(ctx, session, name)
		if err != nil {
			return "", err
		}
		requests = append(requests, request)
	}
	ask, err := e.createOwnerAsk(ctx, session, questions, requests, control)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"ask": askName(ask.ResourceID), "questions": ids, "requests": len(requests)})
}

func cloneAskOptions(options []*storepb.QualityAgentAskQuestionOption) []*storepb.QualityAgentAskQuestionOption {
	cloned := make([]*storepb.QualityAgentAskQuestionOption, 0, len(options))
	for _, option := range options {
		if option == nil {
			continue
		}
		cloned = append(cloned, &storepb.QualityAgentAskQuestionOption{
			Label:       option.Label,
			Description: option.Description,
		})
	}
	return cloned
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

func (e *Engine) createOwnerAsk(ctx context.Context, session *store.QualityAgentSessionMessage, questions []*storepb.QualityAgentAskQuestion, requests []*store.QualityAgentRequestMessage, control *wakeControl) (*store.QualityAgentAskMessage, error) {
	resolved, err := resolveAskQuestionRequestMessages(questions, requests)
	if err != nil {
		return nil, err
	}
	if err := dataquality.ValidateAskQuestions(resolved); err != nil {
		return nil, err
	}
	requestNames := make([]string, 0, len(requests))
	for _, request := range requests {
		requestNames = append(requestNames, dataquality.RequestName(request))
	}
	if err := dataquality.ValidateAskQuestionCoverage(resolved, requestNames); err != nil {
		return nil, err
	}
	summary := askSummary(resolved)
	dedupeKey := "lead:" + askDedupeKey(resolved, requests)
	now := e.now()
	ask, err := e.store.CreateQualityAgentAskAtomic(ctx, &store.QualityAgentAskMessage{
		WorkspaceID: session.WorkspaceID,
		AgentID:     session.AgentID,
		Kind:        store.QualityAgentAskKindQuestion,
		Title:       "The quality agent needs a decision",
		Question:    summary,
		Questions:   resolved,
		DedupeKey:   dedupeKey,
	}, session, &store.QualityAgentTranscriptMessage{
		Wake:           session.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_MODEL,
		Content:        summary,
		SenderName:     "lead",
		Intent:         storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION,
		Audience:       storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_OWNER,
		IdempotencyKey: fmt.Sprintf("ownerask:%d:%x", session.WakeCount+1, contentHash(summary+"|"+dedupeKey)),
	}, requests, now)
	if err != nil {
		return nil, err
	}
	if control != nil {
		control.ownerFacing = true
		control.ownerMessageWritten = true
	}
	if e.askNotifier != nil {
		targets := make([]string, 0, len(requests))
		for _, request := range requests {
			targets = append(targets, request.TargetGUID)
		}
		e.askNotifier(ctx, AskNotice{AgentID: session.AgentID, Ask: ask, RequestCount: len(requests), Targets: targets})
	}
	return ask, nil
}

// askSummary is the owner-visible prose stored beside the structured questions. The
// card renders the questions; this text is the notification and legacy fallback.
func askSummary(questions []*storepb.QualityAgentAskQuestion) string {
	parts := make([]string, 0, len(questions))
	for _, question := range questions {
		if question == nil || strings.TrimSpace(question.Question) == "" {
			continue
		}
		parts = append(parts, question.Question)
	}
	return strings.Join(parts, "\n\n")
}

func resolveAskQuestionRequestMessages(questions []*storepb.QualityAgentAskQuestion, requests []*store.QualityAgentRequestMessage) ([]*storepb.QualityAgentAskQuestion, error) {
	byID := make(map[string]*store.QualityAgentRequestMessage, len(requests))
	askNames := make([]string, 0, len(requests))
	for _, request := range requests {
		byID[request.ResourceID] = request
		askNames = append(askNames, dataquality.RequestName(request))
	}
	resolved := make([]*storepb.QualityAgentAskQuestion, 0, len(questions))
	for _, question := range questions {
		if question == nil {
			continue
		}
		next := &storepb.QualityAgentAskQuestion{
			Id:          question.Id,
			Header:      question.Header,
			Question:    question.Question,
			Detail:      question.Detail,
			Options:     question.Options,
			MultiSelect: question.MultiSelect,
		}
		seen := make(map[string]bool, len(question.Requests))
		for _, address := range question.Requests {
			request := byID[normalizeRequestResourceID(address)]
			if request == nil {
				return nil, common.Errorf(common.Invalid, "question %q names request %q, which this ask does not forward", question.Id, address)
			}
			name := dataquality.RequestName(request)
			if seen[name] {
				return nil, common.Errorf(common.Invalid, "question %q repeats request %q", question.Id, name)
			}
			seen[name] = true
			next.Requests = append(next.Requests, name)
		}
		if len(next.Requests) == 0 && len(askNames) > 0 {
			if len(questions) > 1 {
				return nil, common.Errorf(common.Invalid, "question %q must name the requests it decides when the ask has several questions", question.Id)
			}
			next.Requests = askNames
		}
		resolved = append(resolved, next)
	}
	return resolved, nil
}

// normalizeRequestResourceID reduces the names the model may use — a bare id, a
// relative qualityAgentRequests/<id>, or a full resource name — to the id the store
// row carries.
func normalizeRequestResourceID(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.LastIndex(value, "qualityAgentRequests/"); index >= 0 {
		value = value[index+len("qualityAgentRequests/"):]
	}
	if index := strings.Index(value, "/"); index >= 0 {
		value = value[:index]
	}
	return value
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
		IdempotencyKey: fmt.Sprintf("ownermsg:%d:%x", session.WakeCount+1, contentHash(content)),
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

// answerableRequest resolves a request the lead may answer. A request the lead already
// forwarded to the owner is FORWARDED, not OPEN: the owner's reply is exactly the answer
// it is waiting for, so both states are valid here.
func (e *Engine) answerableRequest(ctx context.Context, session *store.QualityAgentSessionMessage, name string) (*store.QualityAgentRequestMessage, error) {
	request, err := e.requestByAddress(ctx, session, name)
	if err != nil {
		return nil, err
	}
	if request.Status != store.QualityAgentRequestStatusOpen &&
		request.Status != store.QualityAgentRequestStatusForwarded {
		return nil, common.Errorf(common.Conflict, "request %q is no longer open", name)
	}
	return request, nil
}

func (e *Engine) openRequest(ctx context.Context, session *store.QualityAgentSessionMessage, name string) (*store.QualityAgentRequestMessage, error) {
	request, err := e.requestByAddress(ctx, session, name)
	if err != nil {
		return nil, err
	}
	if request.Status != store.QualityAgentRequestStatusOpen {
		return nil, common.Errorf(common.Conflict, "request %q is no longer open", name)
	}
	return request, nil
}

// requestByAddress resolves a request name and proves it belongs to the caller's agent.
// Status is the caller's concern: forwarding and answering accept different sets.
func (e *Engine) requestByAddress(ctx context.Context, session *store.QualityAgentSessionMessage, name string) (*store.QualityAgentRequestMessage, error) {
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

// askDedupeKey makes a retried ask_owner reuse the open question instead of writing
// the same prose twice. The option set and request set are part of the key: the same
// words with different choices are a different decision.
func askDedupeKey(questions []*storepb.QualityAgentAskQuestion, requests []*store.QualityAgentRequestMessage) string {
	parts := make([]string, 0, len(questions)*4)
	for _, question := range questions {
		if question == nil {
			continue
		}
		parts = append(parts, question.Id, question.Question)
		if question.MultiSelect {
			parts = append(parts, "multi")
		}
		for _, option := range question.Options {
			parts = append(parts, option.Label, option.Description)
		}
		questionRequests := append([]string(nil), question.Requests...)
		slices.Sort(questionRequests)
		parts = append(parts, questionRequests...)
	}
	ids := make([]string, 0, len(requests))
	for _, request := range requests {
		ids = append(ids, request.ResourceID)
	}
	slices.Sort(ids)
	return fmt.Sprintf("%x", contentHash(strings.Join(parts, "|")+"|"+strings.Join(ids, ",")))
}

// normalizeCaseResourceID reduces the three names the model may use — a bare id, a
// relative qualityAgentCases/<id>, or a full workspace resource name — to the id the
// request table stores. Without it a request never matches its case at close time.
func normalizeCaseResourceID(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.LastIndex(value, "qualityAgentCases/"); index >= 0 {
		value = value[index+len("qualityAgentCases/"):]
	}
	if index := strings.Index(value, "/"); index >= 0 {
		value = value[:index]
	}
	return value
}

var questionKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,40}$`)

// questionDedupeKey is the stable identity of one question. A case scopes the first
// question; a second distinct question on that case needs an explicit slug. Without a
// case the slug is the scope, and a missing slug falls back to the question text so the
// model can never create an id it has to remember.
func questionDedupeKey(targetGUID, caseID, questionKey, question string) string {
	switch {
	case caseID != "" && questionKey != "":
		return "target:" + targetGUID + ":case:" + caseID + ":question:" + questionKey
	case caseID != "":
		return "target:" + targetGUID + ":case:" + caseID + ":question"
	case questionKey != "":
		return fmt.Sprintf("target:%s:question:%s", targetGUID, questionKey)
	default:
		normalized := strings.Join(strings.Fields(question), " ")
		return fmt.Sprintf("target:%s:question:%x", targetGUID, contentHash(normalized))
	}
}

func requestName(resourceID string) string { return "qualityAgentRequests/" + resourceID }
func askName(resourceID string) string     { return "qualityAgentAsks/" + resourceID }
