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
	"github.com/Ranxy/metaxisdata/backend/store"
)

// stallSweepInterval is how often the stall sweep runs. A check that has not run for two of its
// target's cadences does not become urgent inside ten seconds, and the sweep costs one read per
// target, so it runs once a minute rather than on every scheduler tick.
const stallSweepInterval = time.Minute

// sweepStalledChecks tells each target session about the checks that have stopped running.
//
// It exists because such a check produces nothing: no execution, no failure event, no health event,
// so an agent responsible for a target could not learn that half of it is silently dead. The batch
// path cannot see it either — it only looks at checks that ran — so the sweep is the one reader that
// starts from the schedule instead of from an execution.
func (r *Runner) sweepStalledChecks(ctx context.Context) {
	now := time.Now().UTC()
	if !r.stallSweepDue(now) {
		return
	}
	if !r.currentSettings().AgentRunnerEnabled {
		return
	}
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
	lastCompleted := time.Time{}
	if check.LastCompletedAt != nil {
		lastCompleted = *check.LastCompletedAt
	}
	health := dataquality.DiagnoseStaleCheck(check.CreatedAt, lastCompleted, check.Cadence, now)
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

// escalateFrozenCase tells the owner that the agent exhausted its repair budget on one check. It is
// best effort, like every other escalation: the case row is the durable record, and a failed notice
// must never fail the batch that produced it.
func (r *Runner) escalateFrozenCase(ctx context.Context, openCase *store.QualityAgentCaseMessage, rule *store.QualityRuleMessage) {
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
	raiser := &agentattention.Raiser{Store: r.stores, Notifier: r.notifier}
	raiser.Raise(ctx, openCase, agent, agentattention.Notice{
		Kind:     store.QualityAgentAttentionKindBlocked,
		Question: agentlang.CaseFrozenQuestion(language, title, openCase.Attempts),
	})
}
