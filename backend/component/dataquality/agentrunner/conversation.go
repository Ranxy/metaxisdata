package agentrunner

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// conversationWake answers without a model: it records the current scope summary as
// an assistant message so the owner still gets a useful reply.
func (e *Engine) conversationWake(ctx context.Context, session *store.QualityAgentSessionMessage, wake int32, pending []*store.QualityAgentTranscriptMessage) (Outcome, error) {
	ownerAsked := false
	for _, message := range pending {
		if message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK {
			ownerAsked = true
			break
		}
	}
	if ownerAsked {
		handled, err := e.answerAsksDeterministically(ctx, session, wake, pending)
		if err != nil {
			return Outcome{}, err
		}
		if handled > 0 {
			processed := session.ProcessedSeq
			for _, message := range pending {
				if message.Seq > processed {
					processed = message.Seq
				}
			}
			return Outcome{
				ProcessedSeq: processed,
				EngineState:  cloneEngineState(session.EngineState),
				Summary:      fmt.Sprintf("answered %d ask(s) without a model", handled),
			}, nil
		}
	}
	if !ownerAsked {
		requests, err := e.store.ListQualityAgentRequests(ctx, session.AgentID,
			[]store.QualityAgentRequestStatus{store.QualityAgentRequestStatusOpen}, "", 20)
		if err != nil {
			return Outcome{}, err
		}
		if len(requests) > 0 {
			return e.forwardRequestsDeterministically(ctx, session, wake, pending, requests)
		}
	}
	summary, err := e.scopeSummary(ctx, session)
	if err != nil {
		return Outcome{}, err
	}
	_, appendErr := e.appendMessage(ctx, session, &store.QualityAgentTranscriptMessage{
		Wake:           wake,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_RUNNER,
		Content:        summary,
		IdempotencyKey: fmt.Sprintf("conversation-wake:%d", wake),
	})
	if appendErr != nil {
		return Outcome{}, appendErr
	}
	processed := session.ProcessedSeq
	for _, message := range pending {
		if message.Seq > processed {
			processed = message.Seq
		}
	}
	return Outcome{
		ProcessedSeq: processed,
		EngineState:  cloneEngineState(session.EngineState),
		Summary:      "answered from the deterministic scope summary",
	}, nil
}

// answerAsksDeterministically routes the owner's reply to the requests behind the ask.
// A model-less lead cannot decide, but the owner already did: delivering that answer is
// bookkeeping, not judgment. It returns how many asks it could close this way.
func (e *Engine) answerAsksDeterministically(ctx context.Context, session *store.QualityAgentSessionMessage, _ int32, pending []*store.QualityAgentTranscriptMessage) (int, error) {
	answers := make([]*store.QualityAgentTranscriptMessage, 0, len(pending))
	for _, message := range pending {
		if message.Origin == storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_ASK {
			answers = append(answers, message)
		}
	}
	if len(answers) == 0 {
		return 0, nil
	}
	asks, err := e.store.ListQualityAgentAsks(ctx, session.AgentID,
		[]store.QualityAgentAskStatus{store.QualityAgentAskStatusAnswered, store.QualityAgentAskStatusOpen}, 100)
	if err != nil {
		return 0, errors.Wrap(err, "failed to read the agent's asks")
	}
	handled := 0
	for _, answer := range answers {
		for _, ask := range asks {
			if ask.AnswerMessageSeq != answer.Seq {
				continue
			}
			requests, err := e.store.ListQualityAgentRequestsByAskIDs(ctx, []int64{ask.ID})
			if err != nil {
				return handled, errors.Wrap(err, "failed to read the asks' requests")
			}
			for _, request := range requests[ask.ID] {
				if request.Status != store.QualityAgentRequestStatusOpen &&
					request.Status != store.QualityAgentRequestStatusForwarded {
					continue
				}
				if request.OriginSessionID == nil {
					continue
				}
				subagent, err := e.store.GetQualityAgentSessionByID(ctx, *request.OriginSessionID)
				if err != nil {
					return handled, err
				}
				if subagent == nil || subagent.ArchivedAt != nil {
					continue
				}
				_, _, applied, err := e.store.AnswerQualityAgentRequestAtomic(ctx, request.ResourceID,
					request.Status, answer.Content, subagent, &store.QualityAgentTranscriptMessage{
						Wake:            subagent.WakeCount + 1,
						Role:            storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
						Origin:          storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
						Content:         fmt.Sprintf("Agent lead sent a message:\n[answer] %s", answer.Content),
						SenderSessionID: &session.ID,
						SenderName:      "lead",
						Intent:          storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_ANSWER,
						Audience:        storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
						ReplyTo:         common.FormatWorkspace(session.WorkspaceID) + "/qualityAgentRequests/" + request.ResourceID,
						IdempotencyKey:  fmt.Sprintf("deterministic-answer:%s:%d", request.ResourceID, answer.Seq),
					}, e.now())
				if err != nil {
					return handled, err
				}
				if applied {
					handled++
				}
			}
			if ask.Kind == store.QualityAgentAskKindAnomaly {
				if _, err := e.store.ResolveQualityAgentAsk(ctx, ask.ResourceID,
					store.QualityAgentAskStatusResolved, e.now()); err != nil {
					return handled, err
				}
				handled++
			}
			break
		}
	}
	return handled, nil
}

// forwardRequestsDeterministically is the no-model lead's safety net: it puts every
// open request in front of the owner instead of leaving it in the lead's inbox.
func (e *Engine) forwardRequestsDeterministically(ctx context.Context, session *store.QualityAgentSessionMessage, _ int32, pending []*store.QualityAgentTranscriptMessage, requests []*store.QualityAgentRequestMessage) (Outcome, error) {
	var builder strings.Builder
	_, _ = builder.WriteString("The quality agent cannot reason right now (no model is configured). These targets need a decision:\n")
	for index, request := range requests {
		if index >= 5 {
			break
		}
		fmt.Fprintf(&builder, "\n%d. %s: %s", index+1, request.TargetGUID, truncateRunes(request.Question, 300))
	}
	if _, err := e.createOwnerAsk(ctx, session, []*storepb.QualityAgentAskQuestion{{
		Id:       "q1",
		Question: builder.String(),
	}}, requests, nil); err != nil {
		return Outcome{}, err
	}
	processed := session.ProcessedSeq
	for _, message := range pending {
		if message.Seq > processed {
			processed = message.Seq
		}
	}
	return Outcome{
		ProcessedSeq: processed,
		EngineState:  cloneEngineState(session.EngineState),
		Summary:      fmt.Sprintf("forwarded %d request(s) to the owner without a model", len(requests)),
	}, nil
}

// scopeSummary renders the agent's queue health as bounded text.
func (e *Engine) scopeSummary(ctx context.Context, session *store.QualityAgentSessionMessage) (string, error) {
	agent, err := e.store.GetQualityAgentByID(ctx, session.AgentID)
	if err != nil {
		return "", errors.Wrap(err, "failed to read the session agent")
	}
	language := e.sessionLanguage(ctx, agent)
	rows, err := e.store.QualityAgentEnrollmentSummary(ctx, session.WorkspaceID, session.AgentID)
	if err != nil {
		return "", errors.Wrap(err, "failed to read the agent scope summary")
	}
	total, pending, active, dormant, outOfScope, gone := 0, 0, 0, 0, 0, 0
	for _, row := range rows {
		total += row.Pending + row.Active + row.Dormant + row.OutOfScope + row.Gone
		pending += row.Pending
		active += row.Active
		dormant += row.Dormant
		outOfScope += row.OutOfScope
		gone += row.Gone
	}
	return agentlang.ScopeSummary(language, total, pending, active, dormant, outOfScope, gone), nil
}

// conversationToolResult executes one front-office tool. The result is small JSON or
// bounded text; the tool never mutates a check, plan or target.
func intValue(value int) *int { return &value }

func (e *Engine) conversationToolResult(ctx context.Context, session *store.QualityAgentSessionMessage, name string) (string, error) {
	switch name {
	case "get_scope_summary":
		summary, err := e.scopeSummary(ctx, session)
		if err != nil {
			return "", err
		}
		return summary, nil
	case "list_scope_targets":
		rows, err := e.store.ListQualityAgentEnrollments(ctx, &store.FindQualityAgentEnrollmentMessage{
			WorkspaceID: session.WorkspaceID,
			AgentID:     session.AgentID,
			Limit:       intValue(50),
		})
		if err != nil {
			return "", errors.Wrap(err, "failed to list the agent scope targets")
		}
		type targetView struct {
			Target   string `json:"target"`
			Database string `json:"database"`
			State    string `json:"state"`
			Mode     string `json:"mode"`
			Priority int32  `json:"priority"`
		}
		views := make([]targetView, 0, len(rows))
		for _, row := range rows {
			views = append(views, targetView{
				Target:   row.TargetGUID,
				Database: row.DatabaseRef,
				State:    row.State.String(),
				Mode:     row.Mode.String(),
				Priority: row.Priority,
			})
		}
		return marshalJSON(map[string]any{"targets": views})
	case "list_pending_proposals":
		rows, err := e.store.ListQualitySuggestions(ctx, &store.FindQualitySuggestionMessage{
			AgentID:  &session.AgentID,
			Statuses: []storepb.QualitySuggestionStatus{storepb.QualitySuggestionStatus_QUALITY_SUGGESTION_STATUS_PENDING},
			Kinds:    []storepb.QualitySuggestionKind{storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT},
			Limit:    intValue(20),
		})
		if err != nil {
			return "", errors.Wrap(err, "failed to list the agent pending proposals")
		}
		type proposalView struct {
			ID         string  `json:"id"`
			Target     string  `json:"target"`
			Title      string  `json:"title"`
			RuleType   string  `json:"ruleType,omitempty"`
			Confidence float64 `json:"confidence"`
		}
		views := make([]proposalView, 0, len(rows))
		for _, row := range rows {
			view := proposalView{
				ID:         common.FormatWorkspace(session.WorkspaceID) + "/qualitySuggestions/" + row.ResourceID,
				Target:     row.TargetGUID,
				Title:      row.Payload.GetRule().GetTitle(),
				Confidence: row.Confidence,
			}
			if ruleType, ok := dataquality.RuleTypeForDraftConfig(row.Payload.GetRule().GetConfig()); ok {
				view.RuleType = strings.TrimPrefix(ruleType.String(), "QUALITY_RULE_TYPE_")
			}
			views = append(views, view)
		}
		return marshalJSON(map[string]any{"proposals": views})
	default:
		return "", fmt.Errorf("unknown conversation tool %q", name)
	}
}
