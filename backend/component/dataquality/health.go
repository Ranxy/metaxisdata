package dataquality

import (
	"fmt"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// HealthClass is why a check is, or is not, doing its job. It is a closed set: the
// agent's wake prompt, the report and the attention list render it, so a new member
// is a product change.
type HealthClass string

const (
	// HealthHealthy means a recent execution produced a verdict. The verdict itself
	// may be FAIL; health is about the check working, not about the data passing.
	HealthHealthy HealthClass = "HEALTHY"
	// HealthPending means the check has not run yet and is still inside its first
	// stall window. It is not a fault: a rule that joined the schedule a minute ago
	// has not had a chance to conclude.
	HealthPending HealthClass = "PENDING"
	// HealthNoVerdictEmpty means the newest run was NOT_EVALUATED because the target
	// holds no rows. The check is fine; there is nothing to judge.
	HealthNoVerdictEmpty HealthClass = "NO_VERDICT_EMPTY"
	// HealthNeverJudged means the check runs but has produced no verdict at all:
	// every execution was NOT_EVALUATED or SKIPPED. This is the state that used to be
	// invisible to the agent.
	HealthNeverJudged HealthClass = "NEVER_JUDGED"
	// HealthStalled means no execution completed inside the stall window. It needs the
	// target's cadence, so a caller that does not have one only gets the other classes.
	HealthStalled HealthClass = "STALLED"
)

const (
	// DefaultHealthLookback is how far back a verdict still counts as "this check
	// concludes". A week is long enough to cover a weekly tier and short enough that
	// a check that stopped concluding is visible.
	DefaultHealthLookback = 7 * 24 * time.Hour
	// DefaultHealthStallFactor is how many target cadences may pass without a
	// completed execution before the check counts as stalled.
	DefaultHealthStallFactor = 2
)

// CheckHealth is the deterministic verdict on one check's ability to conclude.
type CheckHealth struct {
	// Class is the state; the zero value means "not assessed" (a rule that is not
	// enabled, or no input at all).
	Class HealthClass
	// Execution names the newest execution the class was decided on, empty when the
	// check has none.
	Execution string
	// LastJudgedAt is when the check last produced PASS, WARN or FAIL.
	LastJudgedAt time.Time
	// LastCompletedAt is when the check last completed, whatever the verdict.
	LastCompletedAt time.Time
	// Reason explains the class in one sentence a person or a model can act on.
	Reason string
}

// HealthInput is everything one health verdict is computed from. The caller supplies
// the window it can see: a post-batch check passes the batch's executions, a sweep
// passes a store read.
type HealthInput struct {
	// Rule is the check; a rule that is not enabled is not assessed.
	Rule *store.QualityRuleMessage
	// Executions are this rule's executions, newest first. Other rules' rows are
	// ignored, so a caller may pass a target's whole list.
	Executions []*store.QualityExecutionMessage
	// Cadence is the target's nominal schedule spacing; zero disables the stall check,
	// which is what a caller that has no schedule (a post-batch check) needs.
	Cadence time.Duration
	// Now is the reference instant. A zero value makes the function report nothing,
	// because every age would be meaningless.
	Now time.Time
	// Lookback and StallFactor override the defaults when non-zero.
	Lookback    time.Duration
	StallFactor int
}

// DiagnoseCheckHealth answers one question deterministically: can this check
// conclude? It is the perception the agent was missing — an execution that never
// produces a verdict is invisible to the failure path, so "my checks never conclude"
// used to reach a person as a guess.
func DiagnoseCheckHealth(input HealthInput) CheckHealth {
	if input.Rule == nil || input.Rule.State != storepb.QualityRuleState_QUALITY_RULE_STATE_ENABLED || input.Now.IsZero() {
		return CheckHealth{}
	}
	lookback := input.Lookback
	if lookback <= 0 {
		lookback = DefaultHealthLookback
	}
	stallFactor := input.StallFactor
	if stallFactor <= 0 {
		stallFactor = DefaultHealthStallFactor
	}
	health := CheckHealth{}
	var newest *store.QualityExecutionMessage
	for _, execution := range input.Executions {
		if execution == nil || execution.RuleID != input.Rule.ID {
			continue
		}
		if newest == nil {
			newest = execution
		}
		if execution.FinishedAt.After(health.LastCompletedAt) {
			health.LastCompletedAt = execution.FinishedAt
		}
		if isJudgedVerdict(execution.Status) && execution.FinishedAt.After(health.LastJudgedAt) {
			health.LastJudgedAt = execution.FinishedAt
		}
	}
	if newest != nil {
		health.Execution = fmt.Sprintf("qualityExecutions/%d", newest.ID)
	}
	windowStart := input.Now.Add(-lookback)

	// A verdict inside the window is the whole point of the check: nothing else it did
	// matters as much.
	if !health.LastJudgedAt.IsZero() && !health.LastJudgedAt.Before(windowStart) {
		health.Class = HealthHealthy
		health.Reason = "the latest execution produced a verdict"
		return health
	}

	// Stalled: nothing completed inside the stall window. A rule younger than that
	// window has simply not had a chance yet, which is Pending, not Stalled.
	if input.Cadence > 0 {
		stalled := stallCheck(input.Rule.CreatedAt, health.LastCompletedAt, input.Cadence, input.Now, stallFactor)
		if stalled.Class != "" {
			return stalled
		}
	}

	if newest == nil {
		health.Class = HealthPending
		health.Reason = "the check has not run yet"
		return health
	}

	// The newest run is the most specific explanation: an empty target is not a broken
	// check, and telling the two apart is what the agent needs to stop guessing.
	if newest.Status == storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED && newest.TotalRows == 0 {
		health.Class = HealthNoVerdictEmpty
		health.Reason = "the target holds no rows, so no verdict is possible"
		return health
	}
	if isJudgedVerdict(newest.Status) {
		// The verdict is outside the window: the check concluded once, long ago.
		health.Class = HealthNeverJudged
		health.Reason = fmt.Sprintf("no verdict in the last %s", lookback.Round(time.Hour))
		return health
	}
	health.Class = HealthNeverJudged
	health.Reason = "the check runs but has produced no verdict"
	return health
}

// DiagnoseStaleCheck answers the stall question from the two instants a sweep can read: when the
// check was saved and when it last finished an execution, whatever the verdict. A check that finishes
// executions is not stalled — it may be failing or never concluding, and those have their own paths
// — so only a check with nothing finished inside its window is reported here. The batch path has the
// executions and reaches the same answer through DiagnoseCheckHealth.
//
// The returned class is STALLED for a check that has stopped running, PENDING for one that is still
// inside its first window, and empty when there is nothing to say.
func DiagnoseStaleCheck(ruleCreatedAt, lastFinishedAt time.Time, cadence time.Duration, now time.Time) CheckHealth {
	if cadence <= 0 || now.IsZero() {
		return CheckHealth{}
	}
	return stallCheck(ruleCreatedAt, lastFinishedAt, cadence, now, DefaultHealthStallFactor)
}

// stallCheck is the one stall rule both callers share: a check is stalled when it has not completed
// an execution for its target's stall window, and pending while it is still younger than that.
func stallCheck(ruleCreatedAt, lastCompletedAt time.Time, cadence time.Duration, now time.Time, stallFactor int) CheckHealth {
	stallWindow := time.Duration(stallFactor) * cadence
	stalledSince := now.Add(-stallWindow)
	youngRule := ruleCreatedAt.After(stalledSince)
	if lastCompletedAt.IsZero() {
		if youngRule {
			return CheckHealth{Class: HealthPending, Reason: "the check has not run yet"}
		}
		return CheckHealth{Class: HealthStalled, Reason: "the check has never run"}
	}
	if lastCompletedAt.Before(stalledSince) && !youngRule {
		return CheckHealth{
			Class:  HealthStalled,
			Reason: fmt.Sprintf("no execution completed in the last %s", stallWindow.Round(time.Minute)),
		}
	}
	return CheckHealth{}
}

// isJudgedVerdict reports whether a status is a conclusion about the data. ERROR,
// SKIPPED and NOT_EVALUATED are monitoring states, never verdicts.
func isJudgedVerdict(status storepb.QualityStatus) bool {
	switch status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL:
		return true
	default:
		return false
	}
}
