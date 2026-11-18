package agentrunner

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/tool"
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
	case "get_target_summary", "list_recent_executions", "get_memory",
		"record_asset_fact", "block_session", "schedule_wake",
		"get_table_context", "draft_checks",
		"get_scope_summary", "list_scope_targets", "list_pending_proposals":
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
	name, arguments string,
	control *wakeControl,
) (string, error) {
	agentSession := agentSessionOf(session)
	tools := e.agentTools()
	switch name {
	case "get_target_summary":
		return tools.GetTargetSummary(ctx, agentSession)
	case "list_recent_executions":
		return tools.ListRecentExecutions(ctx, agentSession)
	case "get_memory":
		return tools.GetMemory(ctx, agentSession)
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
		control.blocked = true
		control.blockedCode = input.Code
		control.blockedWhy = input.Reason
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
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}
}

// agentSessionOf maps the stored session onto the shared tool package's view.
func agentSessionOf(session *store.QualityAgentSessionMessage) quality.AgentSession {
	return quality.AgentSession{
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
	})
}
