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
		ActionPublishContract: true, ActionCloseOrReassign: true, ActionExpandAuthority: true,
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
	require.Equal(t, DecisionDeniedRedLine, DecideAction(on(ActionExpandAuthority)).Code)
	require.Equal(t, DecisionDeniedNotImplemented, DecideAction(on(ActionNotifyOwner)).Code)
	// Asset facts and decision memories are implemented and reachable (A0-5/A1-6).
	require.Equal(t, DecisionAllowed, DecideAction(on(ActionWriteAssetFact)).Code)
	require.Equal(t, DecisionAllowed, DecideAction(on(ActionRecordDecisionWhy)).Code)
	// Writing an investigation is implemented and exposed to an agent (A0-3).
	require.Equal(t, DecisionAllowed, DecideAction(on(ActionWriteInvestigation)).Code)
	require.Equal(t, DecisionDeniedNotImplemented, DecideAction(PolicyInput{Action: "A9-9", Actor: ActorAgent, AutonomyEnabled: true, AgentEnabled: true}).Code)

	// An action the codebase implements but the agent role does not expose is refused
	// as not exposed: authoring a check is a person's write, an agent may only propose.
	require.Equal(t, DecisionDeniedNotExposed, DecideAction(on(ActionCreateCheck)).Code)
	require.Equal(t, DecisionDeniedNotExposed, DecideAction(on(ActionRerunCheck)).Code)
	require.Equal(t, DecisionAllowed, DecideAction(on(ActionProposePlanChange)).Code)
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
	require.Equal(t, DecisionDeniedCritical, decision.Code)

	// A single A0 proposal on a critical asset is still only a proposal.
	decision = DecideAction(PolicyInput{
		Action: ActionProposePlanChange, Actor: ActorAgent, AutonomyEnabled: true, AgentEnabled: true,
		Criticality: CriticalityCritical, ItemCount: 1,
	})
	require.True(t, decision.Allowed)

	// Important is not critical: no amplifier.
	decision = DecideAction(PolicyInput{
		Action: ActionProposePlanChange, Actor: ActorAgent, AutonomyEnabled: true, AgentEnabled: true,
		Criticality: CriticalityImportant, ItemCount: 3,
	})
	require.True(t, decision.Allowed)

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

// The policy fails closed on an actor it cannot classify: a forgotten actor must not
// skip the red-line and human-only checks.
func TestDecideActionUnknownActor(t *testing.T) {
	t.Parallel()

	decision := DecideAction(PolicyInput{Action: ActionPublishContract})
	require.False(t, decision.Allowed)
	require.Equal(t, DecisionDeniedUnknownActor, decision.Code)
}

// A person is never given an agent verdict: human A2/A3 actions are simply allowed,
// and performing them is the approval.
func TestDecideActionHuman(t *testing.T) {
	t.Parallel()

	create := DecideAction(PolicyInput{Action: ActionCreateCheck, Actor: ActorHuman})
	require.True(t, create.Allowed)
	require.False(t, create.RequiresApproval)

	publish := DecideAction(PolicyInput{Action: ActionPublishContract, Actor: ActorHuman})
	require.True(t, publish.Allowed)
	require.False(t, publish.RequiresApproval)
}
