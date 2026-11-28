package qualityrunner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentattention"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentlang"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// stallSweepInterval is how often the stall sweep runs. A check that has not run for two of its
	// target's cadences does not become urgent inside ten seconds, and the sweep costs one read per
	// target, so it runs once a minute rather than on every scheduler tick.
	stallSweepInterval = time.Minute
	// stallSweepGrace keeps the sweep quiet right after the process starts. The scheduler runs every
	// target's overdue batch within the first minute, so a sweep that spoke first would report a
	// whole target as dead and then watch it execute a second later — the restart noise that made
	// one start produce two contradictory events per check.
	stallSweepGrace = 2 * time.Minute
)

// sweepFactCases settles the work items whose failure class describes the target rather than a
// defect. Intake already refuses to open them and resolves them when a batch runs, but a target
// that produces no execution for hours leaves the case open, and an open fact is worse than no
// case: a wake can only run it again or escalate it, and escalating it hands a person a question
// with no answer.
func (r *Runner) sweepFactCases(ctx context.Context, now time.Time) {
	for _, class := range dataquality.FactNotWorkClasses() {
		settled, err := r.stores.ResolveQualityAgentCasesByClass(ctx, string(class), dataquality.FactResolution(class), now)
		if err != nil {
			slog.Warn("failed to settle the quality agent cases that are facts, not work",
				slog.String("class", string(class)), log.WithError(err))
			continue
		}
		for _, group := range settled {
			r.metrics.ObserveAgentCaseTransitions(store.QualityAgentCaseStateResolved.Name(), group.Cases)
			slog.Info("settled quality agent cases that are facts, not work",
				slog.String("class", string(class)), slog.String("target", group.TargetGUID),
				slog.Int64("cases", group.Cases))
		}
	}
}

// sweepStalledChecks tells each target session about the checks that have stopped running.
//
// It exists because such a check produces nothing: no execution, no failure event, no health event,
// so an agent responsible for a target could not learn that half of it is silently dead. The batch
// path cannot see it either — it only looks at checks that ran — so the sweep is the one reader that
// starts from the schedule instead of from an execution.
func (r *Runner) sweepStalledChecks(ctx context.Context) {
	now := time.Now().UTC()
	if stallSweepWithinGrace(r.startedAt, now) || !r.stallSweepDue(now) {
		return
	}
	if !r.currentSettings().AgentRunnerEnabled {
		return
	}
	// Same tick, different question: which work items are not work at all. It runs before the
	// staleness read because it settles rows that would otherwise be rendered to a wake as an
	// item no verb can advance — the one situation a model answers with an escalation.
	r.sweepFactCases(ctx, now)
	workspaceID, err := r.stores.GetWorkspaceID(ctx)
	if err != nil {
		slog.Warn("failed to resolve the workspace for the stall sweep", log.WithError(err))
		return
	}
	sessions, err := r.stores.ListQualityAgentSessions(ctx, &store.FindQualityAgentSessionMessage{
		WorkspaceID: &workspaceID,
		Kind:        1,
	})
	if err != nil {
		slog.Warn("failed to read the agent sessions for the stall sweep", log.WithError(err))
		return
	}
	day := now.UTC().Format("2006-01-02")
	for _, session := range sessions {
		if session == nil || session.TargetGUID == "" {
			continue
		}
		staleness, err := r.stores.ListQualityRuleStaleness(ctx, session.TargetGUID)
		if err != nil {
			slog.Warn("failed to read a target's check staleness",
				slog.String("target", session.TargetGUID), log.WithError(err))
			continue
		}
		for _, check := range staleness {
			r.intakeStalledCheck(ctx, workspaceID, session, check, day, now)
		}
	}
}

// intakeStalledCheck tells one session about one check that stopped running. The same hourly event
// budget as the failure path applies, because it is the same conversation.
func (r *Runner) intakeStalledCheck(ctx context.Context, workspaceID string, session *store.QualityAgentSessionMessage, check *store.QualityRuleStalenessMessage, day string, now time.Time) {
	event, stalled := stalledCheckEvent(check, day, now)
	if !stalled {
		return
	}
	r.appendSessionEvent(ctx, session, workspaceID, event, now)
}

// stalledCheckEvent renders the event one stalled check produces, and reports whether the check is
// stalled at all. The key carries the day, so a sweep every minute wakes the agent once; the
// reference names the check rather than an execution, because there is no execution to name.
func stalledCheckEvent(check *store.QualityRuleStalenessMessage, day string, now time.Time) (agentEvent, bool) {
	if check == nil {
		return agentEvent{}, false
	}
	lastFinished := time.Time{}
	if check.LastFinishedAt != nil {
		lastFinished = *check.LastFinishedAt
	}
	health := dataquality.DiagnoseStaleCheck(check.CreatedAt, lastFinished, check.Cadence, now)
	if health.Class != dataquality.HealthStalled {
		return agentEvent{}, false
	}
	title := check.Title
	if title == "" {
		title = "a check"
	}
	return agentEvent{
		Content: fmt.Sprintf("[event] Check %q (qualityRules/%s) is not running: %s. "+
			"Nothing it was watching is being judged. Decide whether the check, its schedule or the target is the problem, "+
			"and use run_check to see what happens when it runs now.",
			title, check.RuleResourceID, health.Reason),
		Reference: "qualityRules/" + check.RuleResourceID,
		Key:       fmt.Sprintf("stalled:%d:%s", check.RuleID, day),
	}, true
}

// stallSweepWithinGrace reports whether the process is still too young for the sweep to speak. A
// zero start (a test, or a runner built before the field existed) is never inside the grace window.
func stallSweepWithinGrace(startedAt, now time.Time) bool {
	if startedAt.IsZero() {
		return false
	}
	return now.Sub(startedAt) < stallSweepGrace
}

// stallSweepDue reports whether the sweep may run now, and records the pass. It is throttled rather
// than scheduled so the sweep shares the scheduler's tick without a second timer.
func (r *Runner) stallSweepDue(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.stallSweepAt.IsZero() && now.Sub(r.stallSweepAt) < stallSweepInterval {
		return false
	}
	r.stallSweepAt = now
	return true
}

// escalateFrozenCase turns "the agent exhausted its repair budget on this check" into a
// request for the target's lead. The lead owns the owner conversation, so the runner never
// writes the old attention row: the case row is the durable record and the request is the
// coordination record. Best effort, like every runner-side escalation.
func (r *Runner) escalateFrozenCase(ctx context.Context, openCase *store.QualityAgentCaseMessage, rule *store.QualityRuleMessage) {
	r.raiseCaseRequest(ctx, openCase, rule,
		store.QualityAgentRequestKindFrozen, "A check exhausted its repair budget", "frozen",
		func(language, title string) string {
			return agentlang.CaseFrozenQuestion(language, title, openCase.Attempts)
		})
}

// escalatePausedCase makes a runner-side ESCALATED transition visible to the lead. The
// deterministic verifier can promote a case when a repair's check was paused; without a
// request nobody in the coordination path knows a person owes a decision.
func (r *Runner) escalatePausedCase(ctx context.Context, openCase *store.QualityAgentCaseMessage, rule *store.QualityRuleMessage) {
	r.raiseCaseRequest(ctx, openCase, rule,
		store.QualityAgentRequestKindDecision, "A paused check needs a decision", "decision",
		func(language, title string) string {
			return agentlang.CaseEscalationQuestion(language, title,
				"the repair cannot be verified because the check is paused")
		})
}

// raiseCaseRequest is the one runner-side writer for a case request: it resolves the
// target's subagent and lead, renders the question, and raises the request plus the
// lead notice in one call.
func (r *Runner) raiseCaseRequest(ctx context.Context, openCase *store.QualityAgentCaseMessage, rule *store.QualityRuleMessage, kind store.QualityAgentRequestKind, requestTitle, dedupeSuffix string, render func(language, title string) string) {
	sessions, err := r.stores.ListQualityAgentSessionsByTarget(ctx, openCase.WorkspaceID, openCase.TargetGUID)
	if err != nil {
		slog.Warn("failed to read a case's sessions",
			slog.String("case", openCase.ResourceID), log.WithError(err))
		return
	}
	var origin *store.QualityAgentSessionMessage
	for _, session := range sessions {
		if session.AgentID == openCase.AgentID && session.ArchivedAt == nil {
			origin = session
			break
		}
	}
	if origin == nil {
		return
	}
	lead, err := r.stores.GetQualityAgentConversation(ctx, openCase.AgentID)
	if err != nil || lead == nil {
		return
	}
	agent, err := r.stores.GetQualityAgentByID(ctx, openCase.AgentID)
	if err != nil || agent == nil {
		return
	}
	language, _ := agentattention.OwnerLanguage(ctx, r.stores, agent)
	title := ""
	if rule != nil {
		title = rule.Title
		if title == "" {
			title = "qualityRules/" + rule.ResourceID
		}
	}
	question := render(language, title)
	content := fmt.Sprintf("Agent %s sent a message:\n[question] %s", origin.SubagentName, question)
	_, _, err = r.stores.RaiseQualityAgentRequest(ctx, &store.QualityAgentRequestMessage{
		WorkspaceID:     openCase.WorkspaceID,
		AgentID:         openCase.AgentID,
		OriginSessionID: &origin.ID,
		TargetGUID:      openCase.TargetGUID,
		TargetType:      storepb.MetaType(openCase.TargetType),
		Kind:            kind,
		Title:           requestTitle,
		Question:        question,
		CaseResourceID:  openCase.ResourceID,
		DedupeKey:       fmt.Sprintf("target:%s:case:%s:%s", openCase.TargetGUID, openCase.ResourceID, dedupeSuffix),
	}, lead, &store.QualityAgentTranscriptMessage{
		Wake:            lead.WakeCount + 1,
		Role:            storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER,
		Origin:          storepb.QualityAgentMessageOrigin_QUALITY_AGENT_MESSAGE_ORIGIN_AGENT,
		Content:         content,
		SenderSessionID: &origin.ID,
		SenderName:      origin.SubagentName,
		Intent:          storepb.QualityAgentMessageIntent_QUALITY_AGENT_MESSAGE_INTENT_QUESTION,
		Audience:        storepb.QualityAgentMessageAudience_QUALITY_AGENT_MESSAGE_AUDIENCE_INTERNAL,
		IdempotencyKey:  fmt.Sprintf("%s-request:%s", dedupeSuffix, openCase.ResourceID),
	})
	if err != nil {
		slog.Warn("failed to raise a request for a case",
			slog.String("case", openCase.ResourceID), log.WithError(err))
	}
}
