package agentpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// A field rewrite is the first write an agent gets, so every refusal has to be pinned: the
// order is pins, case state, diagnosis, field, participation. A hole here is an agent editing
// a check it may not edit.
func TestDecideReviseRefusesInPipelineOrder(t *testing.T) {
	t.Parallel()

	valueInvalid := &dataquality.Diagnosis{
		Class:   dataquality.ClassConfigValueInvalid,
		Field:   "range.min",
		Actions: []dataquality.RepairAction{dataquality.ActionReviseField, dataquality.ActionRunNow},
	}
	drift := &dataquality.Diagnosis{
		Class:   dataquality.ClassStructureDrift,
		Field:   "",
		Actions: []dataquality.RepairAction{dataquality.ActionReviseField, dataquality.ActionAckStructure},
	}
	emptyTarget := &dataquality.Diagnosis{
		Class:   dataquality.ClassNoVerdictEmpty,
		Actions: []dataquality.RepairAction{dataquality.ActionResolveEmpty},
	}
	diagnosed := store.QualityAgentCaseStateDiagnosed

	t.Run("a manual lock outranks the delegation", func(t *testing.T) {
		t.Parallel()
		policy := Policy{Participation: ParticipationTune}
		verdict := policy.DecideRevise(CaseFacts{
			State: diagnosed, Diagnosis: drift, ManualLock: true,
		}, FieldChange{})
		require.False(t, verdict.Allowed)
		require.Contains(t, verdict.Reason, "a person manages this check")
	})

	t.Run("a pinned field is refused, and a parent pin covers its children", func(t *testing.T) {
		t.Parallel()
		policy := Policy{Participation: ParticipationTune}
		for _, pin := range []string{"range.min", "range"} {
			verdict := policy.DecideRevise(CaseFacts{
				State: diagnosed, Diagnosis: valueInvalid, PinnedFields: []string{pin},
			}, FieldChange{Field: "range.min"})
			require.False(t, verdict.Allowed, pin)
			require.Contains(t, verdict.Reason, "pinned")
		}
		// A pin on a sibling path does not cover this field.
		verdict := policy.DecideRevise(CaseFacts{
			State: diagnosed, Diagnosis: valueInvalid, PinnedFields: []string{"range.max"},
		}, FieldChange{Field: "range.min"})
		require.False(t, verdict.Allowed)
		require.NotContains(t, verdict.Reason, "pinned")
	})

	t.Run("only a diagnosed case may be revised", func(t *testing.T) {
		t.Parallel()
		policy := Policy{Participation: ParticipationTune}
		for _, state := range []store.QualityAgentCaseState{
			store.QualityAgentCaseStateOpen,
			store.QualityAgentCaseStateActed,
			store.QualityAgentCaseStateAwaiting,
			store.QualityAgentCaseStateResolved,
			store.QualityAgentCaseStateFrozen,
		} {
			verdict := policy.DecideRevise(CaseFacts{State: state, Diagnosis: drift}, FieldChange{})
			require.False(t, verdict.Allowed, state.Name())
		}
	})

	t.Run("a failure with no diagnosis names no field", func(t *testing.T) {
		t.Parallel()
		verdict := (Policy{Participation: ParticipationTune}).DecideRevise(CaseFacts{State: diagnosed}, FieldChange{})
		require.False(t, verdict.Allowed)
		require.Contains(t, verdict.Reason, "no diagnosis")
	})

	t.Run("a class without a field repair is refused", func(t *testing.T) {
		t.Parallel()
		verdict := (Policy{Participation: ParticipationTune}).DecideRevise(
			CaseFacts{State: diagnosed, Diagnosis: emptyTarget}, FieldChange{Field: "range.min"})
		require.False(t, verdict.Allowed)
		require.Contains(t, verdict.Reason, "cannot be repaired by changing one field")
	})

	t.Run("the field must be the one the diagnosis named", func(t *testing.T) {
		t.Parallel()
		verdict := (Policy{Participation: ParticipationTune}).DecideRevise(
			CaseFacts{State: diagnosed, Diagnosis: valueInvalid}, FieldChange{Field: "range.max"})
		require.False(t, verdict.Allowed)
		require.Contains(t, verdict.Reason, "range.min")
	})

	t.Run("rewriting a refused literal establishes a criterion, so repair may not", func(t *testing.T) {
		t.Parallel()
		verdict := (Policy{Participation: ParticipationRepair}).DecideRevise(
			CaseFacts{State: diagnosed, Diagnosis: valueInvalid}, FieldChange{Field: "range.min"})
		require.False(t, verdict.Allowed)
		require.Contains(t, verdict.Reason, "tune-level")
	})

	t.Run("a criterion change needs both tune and a replay", func(t *testing.T) {
		t.Parallel()
		tuned := Policy{Participation: ParticipationTune}
		withoutReplay := tuned.DecideRevise(CaseFacts{State: diagnosed, Diagnosis: valueInvalid}, FieldChange{Field: "range.min"})
		require.False(t, withoutReplay.Allowed)
		require.Contains(t, withoutReplay.Reason, "cannot be replayed")

		withReplay := tuned.DecideRevise(
			CaseFacts{State: diagnosed, Diagnosis: valueInvalid, ReplayAvailable: true},
			FieldChange{Field: "range.min"})
		require.True(t, withReplay.Allowed)
	})

	t.Run("a structure snapshot is a repair-level change", func(t *testing.T) {
		t.Parallel()
		verdict := (Policy{Participation: ParticipationRepair}).DecideRevise(
			CaseFacts{State: diagnosed, Diagnosis: drift}, FieldChange{Field: ""})
		require.True(t, verdict.Allowed, verdict.Reason)
	})

	t.Run("observe may not revise even a snapshot", func(t *testing.T) {
		t.Parallel()
		verdict := (Policy{Participation: ParticipationObserve}).DecideRevise(
			CaseFacts{State: diagnosed, Diagnosis: drift}, FieldChange{})
		require.False(t, verdict.Allowed)
		require.Contains(t, verdict.Reason, "repair-level")
	})
}

// The effect is server-derived from the class: a model that could label its own change
// "executability" would hold the authority the table withholds.
func TestEffectOfComesFromTheClass(t *testing.T) {
	t.Parallel()

	require.Equal(t, EffectExecutability, EffectOf(&dataquality.Diagnosis{Class: dataquality.ClassStructureDrift}))
	require.Equal(t, EffectCriterion, EffectOf(&dataquality.Diagnosis{Class: dataquality.ClassConfigValueInvalid}))
	require.Equal(t, EffectSubject, EffectOf(&dataquality.Diagnosis{Class: dataquality.ClassNoVerdictEmpty}))
	require.Equal(t, EffectSubject, EffectOf(nil))
}
