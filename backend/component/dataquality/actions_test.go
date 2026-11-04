package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The catalog must stay the WS2 matrix: every card counted, no duplicate id, and the
// red lines exactly the ones the workshop froze.
func TestActionCatalog(t *testing.T) {
	t.Parallel()

	catalog := ActionCatalog()
	require.Len(t, catalog, 21)
	levels := map[AutonomyLevel]int{}
	ids := map[ActionID]bool{}
	redLines := map[ActionID]bool{}
	for _, card := range catalog {
		levels[card.Level]++
		require.False(t, ids[card.ID], "duplicate action id %s", card.ID)
		ids[card.ID] = true
		if card.RedLine {
			redLines[card.ID] = true
		}
	}
	require.Equal(t, map[AutonomyLevel]int{AutonomyA0: 7, AutonomyA1: 6, AutonomyA2: 5, AutonomyA3: 3}, levels)
	require.Equal(t, map[ActionID]bool{
		ActionMuteIssue: true, ActionArchiveCheck: true, ActionAcknowledgeSchema: true,
		ActionPublishContract: true, ActionCloseOrReassign: true,
	}, redLines)
}

// An agent may propose and read once autonomy is on; the kill switch and the agent's
// own state refuse everything before that.
func TestDecideActionForAgent(t *testing.T) {
	t.Parallel()

	on := PolicyInput{Action: ActionProposePlanChange, Actor: ActorAgent, AutonomyEnabled: true, AgentEnabled: true}
	decision := DecideAction(on)
	require.True(t, decision.Allowed)
	require.False(t, decision.RequiresApproval)
	require.Equal(t, DecisionAllowed, decision.Code)

	off := on
	off.AutonomyEnabled = false
	decision = DecideAction(off)
	require.False(t, decision.Allowed)
	require.Equal(t, DecisionDeniedAutonomyOff, decision.Code)

	disabled := on
	disabled.AgentEnabled = false
	require.Equal(t, DecisionDeniedAutonomyOff, DecideAction(disabled).Code)
}

// Red lines and human-only actions refuse an agent even with autonomy on; publishing
// and closing stay human, and an unimplemented action is refused as such.
func TestDecideActionRedLines(t *testing.T) {
	t.Parallel()

	on := func(action ActionID) PolicyInput {
		return PolicyInput{Action: action, Actor: ActorAgent, AutonomyEnabled: true, AgentEnabled: true}
	}
	require.Equal(t, DecisionDeniedRedLine, DecideAction(on(ActionMuteIssue)).Code)
	require.Equal(t, DecisionDeniedRedLine, DecideAction(on(ActionArchiveCheck)).Code)
	require.Equal(t, DecisionDeniedRedLine, DecideAction(on(ActionAcknowledgeSchema)).Code)
	require.Equal(t, DecisionDeniedRedLine, DecideAction(on(ActionPublishContract)).Code)
	require.Equal(t, DecisionDeniedRedLine, DecideAction(on(ActionCloseOrReassign)).Code)
	require.Equal(t, DecisionDeniedNotImplemented, DecideAction(on(ActionWriteInvestigation)).Code)
	require.Equal(t, DecisionDeniedNotImplemented, DecideAction(PolicyInput{Action: "A9-9", Actor: ActorAgent, AutonomyEnabled: true, AgentEnabled: true}).Code)

	// A2 changes future judging and has no agent write path yet, so it is refused as
	// not implemented rather than promised; the ACL is a separate, additional gate.
	require.Equal(t, DecisionDeniedNotImplemented, DecideAction(on(ActionCreateCheck)).Code)
	// A1 is implemented (re-running one check) and an enabled agent may do it.
	require.Equal(t, DecisionAllowed, DecideAction(on(ActionRerunCheck)).Code)
}

// A critical asset is the amplifier: the same action is handed to a person, and a
// batch of proposals is never auto-allowed there.
func TestDecideActionCriticalAmplifier(t *testing.T) {
	t.Parallel()

	decision := DecideAction(PolicyInput{
		Action: ActionProposePlanChange, Actor: ActorAgent, AutonomyEnabled: true, AgentEnabled: true,
		Criticality: CriticalityCritical, ItemCount: 2,
	})
	require.False(t, decision.Allowed)
	require.True(t, decision.RequiresApproval)
	require.Equal(t, DecisionRequiresApproval, decision.Code)

	decision = DecideAction(PolicyInput{
		Action: ActionRerunCheck, Actor: ActorAgent, AutonomyEnabled: true, AgentEnabled: true,
		Criticality: CriticalityCritical,
	})
	require.False(t, decision.Allowed)
	require.Equal(t, DecisionRequiresApproval, decision.Code)

	// A person performing the same action is the approval, not a pending one.
	human := DecideAction(PolicyInput{Action: ActionPublishContract, Actor: ActorHuman})
	require.True(t, human.Allowed)
	require.False(t, human.RequiresApproval)
}

func TestActionNames(t *testing.T) {
	t.Parallel()

	require.Equal(t, "A0", AutonomyLevelName(AutonomyA0))
	require.Equal(t, "A3", AutonomyLevelName(AutonomyA3))
	require.Equal(t, "AGENT", ActorKindName(ActorAgent))
	level, ok := ParseAutonomyLevel("a2")
	require.True(t, ok)
	require.Equal(t, AutonomyA2, level)
	_, ok = ParseAutonomyLevel("A9")
	require.False(t, ok)
}
