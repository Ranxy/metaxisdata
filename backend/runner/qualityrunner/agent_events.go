package qualityrunner

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// maxSessionEventsPerHour bounds how many failed executions one target's session is
// told about per hour, so a storm of failing checks cannot flood the conversation.
const maxSessionEventsPerHour = 20

// intakeAgentEvents appends a failed or errored execution as an event to every
// agent session covering the target. Each session is checked against its own agent's
// configuration and scope; failures are logged and never fail the batch.
func (r *Runner) intakeAgentEvents(ctx context.Context, workspaceID string, executions []*ruleExecution) {
	settings := r.currentSettings()
	if !settings.AgentRunnerEnabled || !settings.AgentRunnerAutoInvestigateFailures {
		return
	}
	now := time.Now().UTC()
	for _, execution := range executions {
		if execution == nil || execution.result == nil {
			continue
		}
		status := execution.result.Status
		if status != storepb.QualityStatus_QUALITY_STATUS_FAIL && status != storepb.QualityStatus_QUALITY_STATUS_ERROR {
			continue
		}
		sessions, err := r.stores.ListQualityAgentSessionsByTarget(ctx, workspaceID, execution.result.TargetGUID)
		if err != nil {
			slog.Warn("failed to read the target's agent sessions for an event",
				slog.Int64("execution", execution.result.ID), log.WithError(err))
			continue
		}
		for _, session := range sessions {
			r.intakeAgentEvent(ctx, workspaceID, execution, session, now)
		}
	}
}

// intakeAgentEvent appends one execution event to one session when that session's
// agent is active and still covers the target's database.
func (r *Runner) intakeAgentEvent(ctx context.Context, workspaceID string, execution *ruleExecution, session *store.QualityAgentSessionMessage, now time.Time) {
	if !sessionAcceptsAgentEvents(session) {
		return
	}
	config, err := r.stores.GetQualityAgentConfig(ctx, session.AgentID)
	if err != nil || config == nil || config.State != store.QualityAgentStateActive {
		return
	}
	databases, err := r.stores.ListQualityAgentDatabases(ctx, session.AgentID)
	if err != nil || !slices.Contains(databases, session.DatabaseRef) {
		return
	}
	events, err := r.stores.CountQualityAgentSessionEventsSince(ctx, session.ID, now.Add(-time.Hour))
	if err != nil {
		slog.Warn("failed to count the session events", slog.String("session", session.ResourceID), log.WithError(err))
		return
	}
	if events >= maxSessionEventsPerHour {
		slog.Warn("skipped a quality agent event: the session already received its hourly events",
			slog.String("session", session.ResourceID), slog.Int64("execution", execution.result.ID))
		return
	}
	_, appendErr := r.stores.AppendQualityAgentTranscriptMessage(ctx, &store.QualityAgentTranscriptMessage{
		SessionID:      session.ID,
		WorkspaceID:    workspaceID,
		Wake:           session.WakeCount + 1,
		Role:           storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:         storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_EVENT,
		Content:        executionEventContent(execution),
		Reference:      fmt.Sprintf("qualityExecutions/%d", execution.result.ID),
		IdempotencyKey: fmt.Sprintf("event:execution:%d", execution.result.ID),
	})
	if appendErr != nil {
		slog.Warn("failed to append a quality agent event",
			slog.String("session", session.ResourceID), slog.Int64("execution", execution.result.ID), log.WithError(appendErr))
		return
	}
	if _, err := r.stores.AdvanceQualityAgentSessionWake(ctx, session.ResourceID, now); err != nil {
		slog.Warn("failed to wake the session after an event",
			slog.String("session", session.ResourceID), log.WithError(err))
	}
}

// executionEventContent renders one failed execution for the agent. It carries the
// identifiers and numbers a conclusion must cite, and no sample rows.
func executionEventContent(execution *ruleExecution) string {
	reference := fmt.Sprintf("qualityExecutions/%d", execution.result.ID)
	title := ""
	if execution.head != nil {
		title = execution.head.Title
	}
	if title == "" {
		title = "a check"
	}
	if execution.result.Status == storepb.QualityStatus_QUALITY_STATUS_ERROR {
		message := execution.result.ErrorMessage
		if message == "" {
			message = execution.result.ErrorCode
		}
		return fmt.Sprintf("[event] Execution %s (check %q) ended ERROR: %s. Decide whether the check or the data needs a person.",
			reference, title, message)
	}
	return fmt.Sprintf("[event] Execution %s (check %q) finished FAIL with %d failed rows. Investigate whether this is a real data problem or a broken check.",
		reference, title, execution.result.FailedRows)
}

// sessionAcceptsAgentEvents reports whether a failure should be appended to this session.
// An enabled session always accepts one; a session parked on a transient block (a budget
// window, autonomy off, an internal error) still records the event so the agent sees the
// failure once it resumes. A paused, archived or needs-human session does not.
func sessionAcceptsAgentEvents(session *store.QualityAgentSessionMessage) bool {
	if session == nil {
		return false
	}
	switch session.State {
	case storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED:
		return true
	case storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED:
		return store.QualityAgentSessionBlockedCodeRetryable(session.BlockedCode)
	default:
		return false
	}
}
