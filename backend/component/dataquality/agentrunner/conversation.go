package agentrunner

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// conversationWake answers without a model: it records the current scope summary as
// an assistant message so the owner still gets a useful reply.
func (e *Engine) conversationWake(ctx context.Context, session *store.QualityAgentSessionMessage, wake int32, pending []*store.QualityAgentTranscriptMessage) (Outcome, error) {
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
			Limit:    intValue(20),
		})
		if err != nil {
			return "", errors.Wrap(err, "failed to list the agent pending proposals")
		}
		type proposalView struct {
			Target string `json:"target"`
			Title  string `json:"title"`
		}
		views := make([]proposalView, 0, len(rows))
		for _, row := range rows {
			views = append(views, proposalView{
				Target: row.TargetGUID,
				Title:  row.Payload.GetRule().GetTitle(),
			})
		}
		return marshalJSON(map[string]any{"proposals": views})
	default:
		return "", fmt.Errorf("unknown conversation tool %q", name)
	}
}
