package agentrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/component/tools/quality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// toolExecutor adapts one wake's resolved tool set to the loop's executor
// contract. It journals nothing itself: the wake recorder persists the assistant
// message, the tool result and the engine notes from the event stream.
func toolExecutor(ctx context.Context, set *tool.Set, truncated map[string]bool, invocation *tool.Invocation) agent.ToolExecutor {
	// Parallel-safe calls may finish in any order, so the truncation flags they
	// report need their own lock.
	var truncatedMu sync.Mutex
	return func(call llm.ToolCall) ([]llm.ToolResult, error) {
		result := set.Execute(ctx, tool.Call{
			ID:        call.ID,
			Name:      call.GetName(),
			Arguments: json.RawMessage(call.Function.Arguments),
		}, invocation)
		if result.Truncated {
			truncatedMu.Lock()
			truncated[call.ID] = true
			truncatedMu.Unlock()
		}
		if result.IsError {
			if result.Error != nil {
				return nil, result.Error
			}
			return nil, errors.New(result.Content)
		}
		return []llm.ToolResult{{ToolCallID: call.ID, Content: result.Content}}, nil
	}
}

// handlesTool reports whether executeTool handles name. ValidateSpecs uses it so
// a declaration typo fails at startup instead of surfacing to the model as
// "unknown tool" during a wake.
func handlesTool(name string) bool {
	switch name {
	case "get_target_summary", "list_recent_executions", "inspect_check", "control_check", "run_check",
		"repair_check", "propose_revision", "flag_proposal", "escalate_case", "get_memory",
		"record_asset_fact", "block_session", "schedule_wake", "send_lead_message",
		"get_table_context", "draft_checks",
		"get_scope_summary", "list_scope_targets", "list_pending_proposals",
		"list_subagents", "get_subagent", "send_subagent_message", "list_requests",
		"answer_request", "forward_request", "reply_owner", "ask_owner", "list_open_cases",
		"adopt_pending_drafts":
		return true
	default:
		return false
	}
}

// executeTool dispatches one model tool call. The product tools are the shared
// quality package's agent set; block_session and schedule_wake are this product's
// wake control and stay here.
func (e *Engine) executeTool(
	ctx context.Context,
	session *store.QualityAgentSessionMessage,
	agent *store.QualityAgentMessage,
	name, arguments string,
	control *wakeControl,
) (string, error) {
	agentSession := agentSessionOf(session)
	tools := e.agentTools()
	switch name {
	case "send_lead_message":
		return e.sendLeadMessage(ctx, session, arguments)
	case "list_subagents":
		return e.listSubagents(ctx, session, arguments)
	case "get_subagent":
		return e.getSubagent(ctx, session, arguments)
	case "send_subagent_message":
		return e.sendSubagentMessage(ctx, session, arguments)
	case "list_requests":
		return e.listRequests(ctx, session, arguments)
	case "answer_request":
		return e.answerRequest(ctx, session, arguments)
	case "forward_request":
		return e.forwardRequest(ctx, session, arguments, control)
	case "reply_owner":
		return e.replyOwner(ctx, session, arguments, control)
	case "ask_owner":
		return e.askOwner(ctx, session, arguments, control)
	case "get_target_summary":
		if isLeadSession(session) {
			target, err := e.leadTargetSession(ctx, session, arguments)
			if err != nil {
				return "", err
			}
			return tools.GetTargetSummary(ctx, target)
		}
		return tools.GetTargetSummary(ctx, agentSession)
	case "list_recent_executions":
		if isLeadSession(session) {
			target, err := e.leadTargetSession(ctx, session, arguments)
			if err != nil {
				return "", err
			}
			return tools.ListRecentExecutions(ctx, target)
		}
		return tools.ListRecentExecutions(ctx, agentSession)
	case "inspect_check":
		if isLeadSession(session) {
			target, err := e.leadTargetSession(ctx, session, arguments)
			if err != nil {
				return "", err
			}
			return tools.InspectCheck(ctx, target, arguments)
		}
		return tools.InspectCheck(ctx, agentSession, arguments)
	case "get_memory":
		if isLeadSession(session) {
			target, err := e.leadTargetSession(ctx, session, arguments)
			if err != nil {
				return "", err
			}
			return tools.GetMemory(ctx, target)
		}
		return tools.GetMemory(ctx, agentSession)
	case "list_open_cases":
		return e.listOpenCases(ctx, session, arguments)
	case "control_check":
		var input struct {
			Case   string `json:"case"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid control_check arguments")
		}
		if strings.TrimSpace(input.Case) == "" {
			return "", errors.New("control_check needs a case")
		}
		// The request is the decision record; the pause is the action. Raise it first
		// so a pause can never succeed without a lead-visible question, and dismiss it
		// if the action is refused.
		request, created, reqErr := e.raiseRequest(ctx, session, store.QualityAgentRequestMessage{
			Kind:           store.QualityAgentRequestKindDecision,
			CaseResourceID: input.Case,
			Title:          "A paused check needs a decision",
			Question:       "The check was paused because it could not produce a verdict; decide whether to resume, archive or replace it.",
			DedupeKey:      fmt.Sprintf("target:%s:case:%s:control", session.TargetGUID, normalizeCaseResourceID(input.Case)),
		})
		if reqErr != nil {
			return "", reqErr
		}
		result, err := tools.ControlCheck(ctx, agentSession, arguments)
		if err != nil {
			e.dismissRaisedRequest(ctx, request, created, "the control action was refused")
			return "", err
		}
		return result, nil
	case "run_check":
		return tools.RunCheck(ctx, agentSession, arguments)
	case "repair_check":
		return tools.RepairCheck(ctx, agentSession, arguments)
	case "propose_revision":
		var input struct {
			Case      string `json:"case"`
			Field     string `json:"field"`
			Value     string `json:"value"`
			Rationale string `json:"rationale"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid propose_revision arguments")
		}
		if strings.TrimSpace(input.Case) == "" || strings.TrimSpace(input.Field) == "" || input.Value == "" {
			return "", errors.New("propose_revision needs a case, a field and a value")
		}
		question := fmt.Sprintf("propose %s = %s: %s", input.Field, input.Value, input.Rationale)
		request, created, reqErr := e.raiseRequest(ctx, session, store.QualityAgentRequestMessage{
			Kind:           store.QualityAgentRequestKindProposal,
			CaseResourceID: input.Case,
			Title:          "A check change needs approval",
			Question:       question,
			DedupeKey:      fmt.Sprintf("target:%s:case:%s:proposal", session.TargetGUID, normalizeCaseResourceID(input.Case)),
		})
		if reqErr != nil {
			return "", reqErr
		}
		result, err := tools.ProposeRevision(ctx, agentSession, arguments)
		if err != nil {
			e.dismissRaisedRequest(ctx, request, created, "the proposal was refused")
			return "", err
		}
		return result, nil
	case "flag_proposal":
		return tools.FlagProposal(ctx, agentSession, arguments)
	case "escalate_case":
		var input struct {
			Case   string `json:"case"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid escalate_case arguments")
		}
		if strings.TrimSpace(input.Case) == "" || strings.TrimSpace(input.Reason) == "" {
			return "", errors.New("escalate_case needs a case and a reason")
		}
		request, created, reqErr := e.raiseRequest(ctx, session, store.QualityAgentRequestMessage{
			Kind:           store.QualityAgentRequestKindDecision,
			CaseResourceID: input.Case,
			Title:          "A work item needs a decision",
			Question:       input.Reason,
			DedupeKey:      fmt.Sprintf("target:%s:case:%s:decision", session.TargetGUID, normalizeCaseResourceID(input.Case)),
		})
		if reqErr != nil {
			return "", reqErr
		}
		result, err := tools.EscalateCase(ctx, agentSession, arguments)
		if err != nil {
			e.dismissRaisedRequest(ctx, request, created, "the escalation was refused")
			return "", err
		}
		return result, nil
	case "record_asset_fact":
		return tools.RecordAssetFact(ctx, agentSession, arguments)
	case "get_table_context":
		return tools.GetTableContext(ctx, agentSession)
	case "draft_checks":
		return tools.DraftChecks(ctx, agentSession, arguments)
	case "get_scope_summary", "list_scope_targets", "list_pending_proposals":
		return e.conversationToolResult(ctx, session, name)
	case "block_session":
		var input struct {
			Code   string `json:"code"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid block_session arguments")
		}
		if input.Code == "" || input.Reason == "" {
			return "", errors.New("block_session needs a code and a reason")
		}
		// A parked session silences every target it covers and nothing but a retryable budget block can
		// wake it, so a decision never parks one: it becomes an escalated case, which the owner's
		// attention list tracks and which blocks exactly one question.
		if !store.QualityAgentSessionBlockedCodeInfrastructure(input.Code) {
			return "", errors.Errorf("block_session is for infrastructure failures only; escalate the work item with "+
				"escalate_case instead of parking the session (code %q)", input.Code)
		}
		control.blocked = true
		control.blockedCode = input.Code
		control.blockedWhy = input.Reason
		// The runner parks the session after this wake and tells the lead; sending a
		// second notice here would duplicate the same fact.
		return marshalJSON(map[string]any{"blocked": true, "threshold": blockedThreshold})
	case "schedule_wake":
		var input struct {
			AfterMinutes int32  `json:"after_minutes"`
			Reason       string `json:"reason"`
		}
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid schedule_wake arguments")
		}
		minutes := int(input.AfterMinutes)
		if minutes < 1 {
			minutes = 1
		}
		if minutes > 10080 {
			minutes = 10080
		}
		control.nextWake = ptrTime(e.now().Add(time.Duration(minutes) * time.Minute))
		control.scheduleReason = truncateRunes(input.Reason, 200)
		return marshalJSON(map[string]any{"scheduled": true, "after_minutes": minutes})
	case "adopt_pending_drafts":
		return e.executeAdoptPendingDrafts(ctx, session, agent, arguments, control)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// maxAdoptionDraftIDs bounds one owner-requested adoption call. The review queue
// itself is bounded, and an oversized list is a malformed request, not a batch.
const maxAdoptionDraftIDs = 20

// executeAdoptPendingDrafts runs the one publish-class tool: an adoption the owner
// explicitly asked for in this wake. It refuses without the owner question, even
// though the tool is filtered out of the schema in that case.
func (e *Engine) executeAdoptPendingDrafts(
	ctx context.Context,
	session *store.QualityAgentSessionMessage,
	agent *store.QualityAgentMessage,
	arguments string,
	control *wakeControl,
) (string, error) {
	if !control.ownerAsked {
		return "", errors.New("adopt_pending_drafts is available only for the owner's explicit request in the current wake")
	}
	if !dataquality.AgentAdoptionReady(agent) {
		return "", errors.New("adoption is not enabled for this agent")
	}
	if e.ownerAdoption == nil {
		return "", errors.New("owner-requested adoption is not configured in this process")
	}
	var input struct {
		DraftIDs []string `json:"draft_ids"`
		Target   string   `json:"target"`
	}
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &input); err != nil {
			return "", errors.Wrap(err, "invalid adopt_pending_drafts arguments")
		}
	}
	if len(input.DraftIDs) > maxAdoptionDraftIDs {
		return "", errors.Errorf("at most %d drafts can be adopted in one call", maxAdoptionDraftIDs)
	}
	target := strings.TrimSpace(input.Target)
	if isSubagentSession(session) && target != "" && target != session.TargetGUID {
		return "", errors.New("a target session can adopt only drafts of its own target")
	}
	report, err := e.ownerAdoption(ctx, dataquality.AgentAdoptionRequest{
		Session:    session,
		TargetGUID: target,
		DraftIDs:   input.DraftIDs,
		Wake:       control.wake,
	})
	if err != nil {
		return "", err
	}
	control.adoptionUsed = true
	return marshalJSON(report)
}

// leadTargetSession resolves a lead's target argument into the tool package's
// session shape. A live subagent supplies the target type; an unadmitted target is
// still readable with an unspecified type because the evidence reads key on GUID.
func (e *Engine) leadTargetSession(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (quality.AgentSession, error) {
	var input struct {
		Target string `json:"target"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return quality.AgentSession{}, errors.Wrap(err, "invalid target argument")
	}
	target := strings.TrimSpace(input.Target)
	if target == "" {
		return quality.AgentSession{}, errors.New("a target is required")
	}
	targetType := storepb.MetaType(0)
	if subagent, err := e.store.GetQualityAgentSubagentByAddress(ctx, session.AgentID, target); err != nil {
		return quality.AgentSession{}, err
	} else if subagent != nil {
		target = subagent.TargetGUID
		targetType = storepb.MetaType(subagent.TargetType)
	}
	return quality.AgentSession{
		WorkspaceID:      session.WorkspaceID,
		AgentID:          session.AgentID,
		TargetGUID:       target,
		TargetType:       targetType,
		ResourceID:       session.ResourceID,
		AgentPrincipalID: session.AgentPrincipalID,
	}, nil
}

// listOpenCases is the lead's target-scoped work-item read.
func (e *Engine) listOpenCases(ctx context.Context, session *store.QualityAgentSessionMessage, arguments string) (string, error) {
	if !isLeadSession(session) {
		if session.TargetGUID == "" {
			return "", errors.New("a target is required")
		}
		cases, err := e.store.ListOpenQualityAgentCasesByTarget(ctx, session.AgentID, session.TargetGUID, 10)
		if err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{"cases": cases})
	}
	var input struct {
		Target string `json:"target"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(arguments), &input); err != nil {
		return "", errors.Wrap(err, "invalid list_open_cases arguments")
	}
	target := strings.TrimSpace(input.Target)
	if target == "" {
		return "", errors.New("a target is required")
	}
	if subagent, err := e.store.GetQualityAgentSubagentByAddress(ctx, session.AgentID, target); err != nil {
		return "", err
	} else if subagent != nil {
		target = subagent.TargetGUID
	}
	limit := input.Limit
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	cases, err := e.store.ListOpenQualityAgentCasesByTarget(ctx, session.AgentID, target, limit)
	if err != nil {
		return "", err
	}
	return marshalJSON(map[string]any{"target": target, "cases": cases})
}

// agentSessionOf maps the stored session onto the shared tool package's view.
func agentSessionOf(session *store.QualityAgentSessionMessage) quality.AgentSession {
	return quality.AgentSession{
		WorkspaceID:      session.WorkspaceID,
		AgentID:          session.AgentID,
		TargetGUID:       session.TargetGUID,
		TargetType:       storepb.MetaType(session.TargetType),
		ResourceID:       session.ResourceID,
		AgentPrincipalID: session.AgentPrincipalID,
	}
}

// agentTools builds the shared agent tool set over this engine's dependencies.
// Construction is cheap (a struct over interfaces), so a wake builds it once.
func (e *Engine) agentTools() *quality.AgentTools {
	return quality.NewAgentTools(quality.AgentDeps{
		Store:        e.store,
		Now:          e.now,
		TableContext: e.tableContext,
		DraftChecks:  e.draftChecks,
		Evidence:     e.evidence,
		Control:      e.control,
		Run:          e.run,
		Propose:      e.propose,
		Revise:       e.revise,
		Flag:         e.flag,
		Escalate:     e.escalate,
	})
}
