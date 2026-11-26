package agentpolicy

import (
	"fmt"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
)

// WriteBudget is how many writes one agent may make in one rolling day: definition and state writes
// across its whole scope, and immediate runs for one check. A zero field means the server default,
// the same convention the agent row stores every other budget with.
//
// The two ceilings answer different questions. Repairs are per agent because "how much churn of
// checks will I tolerate" is a property of the delegation; runs are per check because a run is the
// agent's diagnostic, and running one broken check ten times an hour is noise while running ten
// different checks once is normal.
type WriteBudget struct {
	RepairsPerDay     int32
	RunsPerRulePerDay int32
}

// Effective resolves the server defaults for the unset slots.
func (b WriteBudget) Effective() WriteBudget {
	if b.RepairsPerDay <= 0 {
		b.RepairsPerDay = dataquality.DefaultAgentRepairsPerDay
	}
	if b.RunsPerRulePerDay <= 0 {
		b.RunsPerRulePerDay = dataquality.DefaultAgentRunsPerRulePerDay
	}
	return b
}

// RepairRefusal reports whether one more write to a check may be made, given how many the agent has
// already spent today. The refusal names the ceiling and what still works, because a budget that
// reads as "not allowed" invites the model to retry or to park the session.
func (b WriteBudget) RepairRefusal(spent int32) (string, bool) {
	limit := b.Effective().RepairsPerDay
	if spent < limit {
		return "", false
	}
	return fmt.Sprintf("the agent has spent its %d writes to checks for today; proposing a change, recording a fact "+
		"and escalating still work, and the budget resets tomorrow", limit), true
}

// RunRefusal reports whether one more immediate run of a check may be requested, given how many the
// agent has already spent on it today.
func (b WriteBudget) RunRefusal(spentForRule int32) (string, bool) {
	limit := b.Effective().RunsPerRulePerDay
	if spentForRule < limit {
		return "", false
	}
	return fmt.Sprintf("the agent has already run this check %d times today, which is its daily limit; "+
		"inspecting it and escalating still work, and the budget resets tomorrow", limit), true
}
