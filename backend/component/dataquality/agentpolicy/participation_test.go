package agentpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
)

// The delegation is a cascade: the agent row is the default, the database row is what the
// operator chose when they delegated, and a target row is an explicit exception. An unset
// slot inherits rather than resetting, which is what makes "delegate this database, but
// leave that table on observe" expressible.
func TestEffectiveParticipationCascades(t *testing.T) {
	t.Parallel()

	observe, repair, tune := ParticipationObserve, ParticipationRepair, ParticipationTune
	unset := ParticipationUnset
	cases := []struct {
		name     string
		agent    Participation
		database *Participation
		target   *Participation
		want     Participation
	}{
		{name: "an unset agent row takes the delegation default", want: DefaultParticipation},
		{name: "the agent row is the base", agent: ParticipationObserve, want: ParticipationObserve},
		{name: "the database row overrides the agent", agent: observer(), database: &repair, want: ParticipationRepair},
		{name: "an unset database row inherits", agent: ParticipationTune, database: &unset, want: ParticipationTune},
		{name: "the target row overrides the database", agent: ParticipationTune, database: &tune, target: &observe, want: ParticipationObserve},
		{name: "an unset target row inherits the database", agent: ParticipationRepair, database: &tune, target: &unset, want: ParticipationTune},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.want, Effective(test.agent, test.database, test.target))
		})
	}
}

// observer is a helper so the table above reads as prose.
func observer() Participation { return ParticipationObserve }

func TestParticipationNamesAndParsing(t *testing.T) {
	t.Parallel()

	for _, level := range []Participation{ParticipationObserve, ParticipationRepair, ParticipationTune} {
		parsed, ok := ParseParticipation(level.Name())
		require.True(t, ok, level.Name())
		require.Equal(t, level, parsed)
	}
	parsed, ok := ParseParticipation(" tune ")
	require.True(t, ok)
	require.Equal(t, ParticipationTune, parsed)
	_, ok = ParseParticipation("OWNER")
	require.False(t, ok)
	require.Empty(t, ParticipationUnset.Name())
}

// The action table is the whole authority model, so every cell is pinned. A change that lets
// a repair-level agent rewrite a threshold, or a tune-level agent change what is judged, has
// to break this test.
func TestPolicyAllowsIsTheActionTable(t *testing.T) {
	t.Parallel()

	type cell struct {
		level  Participation
		action dataquality.RepairAction
		effect Effect
		want   bool
	}
	cells := []cell{
		// Reading and asking are always permitted.
		{ParticipationObserve, dataquality.ActionRunNow, EffectNone, true},
		{ParticipationObserve, dataquality.ActionRecord, EffectNone, true},
		{ParticipationObserve, dataquality.ActionProposeRevision, EffectNone, true},
		{ParticipationObserve, dataquality.ActionEscalate, EffectNone, true},

		// Fixing what the server can compute is a repair-level act.
		{ParticipationObserve, dataquality.ActionReviseField, EffectExecutability, false},
		{ParticipationRepair, dataquality.ActionReviseField, EffectExecutability, true},
		{ParticipationTune, dataquality.ActionReviseField, EffectExecutability, true},

		// Changing what counts as acceptable is a tune-level act.
		{ParticipationRepair, dataquality.ActionReviseField, EffectCriterion, false},
		{ParticipationTune, dataquality.ActionReviseField, EffectCriterion, true},

		// Nothing may change what is judged.
		{ParticipationTune, dataquality.ActionReviseField, EffectSubject, false},
		{ParticipationTune, dataquality.ActionReviseField, EffectRemoval, false},

		// Pausing is reversible and belongs to repair; archiving does not.
		{ParticipationObserve, dataquality.ActionControl, EffectState, false},
		{ParticipationRepair, dataquality.ActionControl, EffectState, true},
		{ParticipationTune, dataquality.ActionControl, EffectState, true},
		{ParticipationRepair, dataquality.ActionControl, EffectRemoval, false},
		{ParticipationTune, dataquality.ActionControl, EffectRemoval, true},

		// The server performs these from a diagnosis; no model verb exists.
		{ParticipationTune, dataquality.ActionResolveEmpty, EffectNone, false},
		{ParticipationTune, dataquality.ActionAckStructure, EffectNone, false},
	}
	for _, test := range cells {
		policy := Policy{Participation: test.level}
		verdict := policy.Allows(Decision{Action: test.action, Effect: test.effect})
		require.Equal(t, test.want, verdict.Allowed,
			"%s %s on %s", test.level.Name(), test.action, effectName(test.effect))
		if test.want {
			require.Empty(t, verdict.Reason)
			continue
		}
		require.NotEmpty(t, verdict.Reason, "a refusal names the next step")
	}
}

// A refusal tells the agent what to do instead, and names the level the operator would have
// to raise. A bare "denied" is what made the agent repeat itself or park the session.
func TestPolicyRefusalNamesTheNextStep(t *testing.T) {
	t.Parallel()

	verdict := (Policy{Participation: ParticipationRepair}).
		Allows(Decision{Action: dataquality.ActionReviseField, Effect: EffectCriterion})
	require.False(t, verdict.Allowed)
	require.Contains(t, verdict.Reason, "tune-level")
	require.Contains(t, verdict.Reason, "propose")
	require.Contains(t, verdict.Reason, "REPAIR")
}

// An unset level behaves like the delegation default rather than like "no authority".
func TestPolicyUnsetUsesTheDefault(t *testing.T) {
	t.Parallel()

	var policy Policy
	require.True(t, policy.Allows(Decision{Action: dataquality.ActionReviseField, Effect: EffectExecutability}).Allowed)
	require.False(t, policy.Allows(Decision{Action: dataquality.ActionReviseField, Effect: EffectCriterion}).Allowed)
	require.False(t, policy.CanTune())
	require.True(t, (Policy{Participation: ParticipationTune}).CanTune())
}

// effectName keeps the table's failure message readable.
func effectName(effect Effect) string {
	switch effect {
	case EffectNone:
		return "no effect"
	case EffectExecutability:
		return "executability"
	case EffectCriterion:
		return "criterion"
	case EffectSubject:
		return "subject"
	case EffectState:
		return "state"
	case EffectRemoval:
		return "removal"
	default:
		return "unknown effect"
	}
}
