package agentpolicy

import (
	"strings"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// FieldChange is one requested rewrite of one config field. The caller supplies the field the
// diagnosis named; it never supplies the effect, because a model that could declare its own
// change "executability" would have the authority the table is meant to withhold.
type FieldChange struct {
	// Field is the dotted config path the diagnosis named ("range.min").
	Field string
}

// CaseFacts is what a write verb knows about the case it acts on. The diagnosis is the
// server's own re-derivation, never the one the model quoted back.
type CaseFacts struct {
	State    store.QualityAgentCaseState
	Attempts int32
	// Diagnosis is the deterministic explanation of the case's execution. Nil means the server
	// could not explain the failure, and nothing may be written.
	Diagnosis *dataquality.Diagnosis
	// ManualLock is a person's decision to manage the check themselves.
	ManualLock bool
	// PinnedFields are the fields a person owns, in dotted form.
	PinnedFields []string
	// ReplayAvailable reports that the server can re-evaluate the recent executions under the
	// new definition. A criterion change without a replay is unverifiable, and an unverifiable
	// change is a proposal rather than a write.
	ReplayAvailable bool
}

// EffectOf classifies what rewriting the field a diagnosis named would touch. The class
// decides, not the field name: a value that never bound establishes a criterion when it is
// replaced, while a structure snapshot is a value the registry already holds.
func EffectOf(diagnosis *dataquality.Diagnosis) Effect {
	if diagnosis == nil {
		return EffectSubject
	}
	switch diagnosis.Class {
	case dataquality.ClassStructureDrift:
		return EffectExecutability
	case dataquality.ClassConfigValueInvalid:
		return EffectCriterion
	default:
		return EffectSubject
	}
}

// DecideRevise answers whether one field rewrite may be applied to one case. The order is the
// pipeline's order: the pins first (a person's decision outranks the delegation), then the
// case's state, then the diagnosis, then the field, then the participation level.
func (p Policy) DecideRevise(facts CaseFacts, change FieldChange) Verdict {
	field := strings.TrimSpace(change.Field)
	if facts.ManualLock {
		return Verdict{Reason: "a person manages this check; propose the change instead of applying it"}
	}
	if field != "" && pinsField(facts.PinnedFields, field) {
		return Verdict{Reason: "a person pinned " + field + "; propose the change instead of applying it"}
	}
	switch facts.State {
	case store.QualityAgentCaseStateDiagnosed:
	case store.QualityAgentCaseStateOpen:
		return Verdict{Reason: "the case has not been diagnosed yet; diagnose it before changing a field"}
	case store.QualityAgentCaseStateActed, store.QualityAgentCaseStateAwaiting:
		return Verdict{Reason: "a repair is already waiting for its verification run"}
	default:
		return Verdict{Reason: "the case is " + facts.State.Name() + " and cannot be revised"}
	}
	if facts.Diagnosis == nil {
		return Verdict{Reason: "the failure has no diagnosis, so no field can be named"}
	}
	if !hasAction(facts.Diagnosis.Actions, dataquality.ActionReviseField) {
		return Verdict{Reason: "this failure cannot be repaired by changing one field"}
	}
	if field != facts.Diagnosis.Field {
		named := facts.Diagnosis.Field
		if named == "" {
			named = "none"
		}
		return Verdict{Reason: "a field rewrite may only touch the field the diagnosis named (" + named + ")"}
	}
	effect := EffectOf(facts.Diagnosis)
	// A structure snapshot names no config field: rewriting it is the whole change. Any other
	// class must name the field it repairs, or there is nothing to write.
	if field == "" && effect != EffectExecutability {
		return Verdict{Reason: "the diagnosis could not name the field to repair; propose the change instead"}
	}
	verdict := p.Allows(Decision{Action: dataquality.ActionReviseField, Effect: effect})
	if !verdict.Allowed {
		return verdict
	}
	if effect == EffectCriterion && !facts.ReplayAvailable {
		return Verdict{Reason: "this change cannot be replayed against the recent executions, so it is unverifiable; propose it instead"}
	}
	return Verdict{Allowed: true}
}

// hasAction reports whether an action is in a diagnosis's list.
func hasAction(actions []dataquality.RepairAction, want dataquality.RepairAction) bool {
	for _, action := range actions {
		if action == want {
			return true
		}
	}
	return false
}

// pinsField reports whether a person pinned the field. A pin on a parent path pins everything
// below it, so pinning "range" owns "range.min" without listing every bound.
func pinsField(pinned []string, field string) bool {
	for _, pin := range pinned {
		pin = strings.TrimSpace(pin)
		if pin == "" {
			continue
		}
		if field == pin || strings.HasPrefix(field, pin+".") {
			return true
		}
	}
	return false
}

// ControlAction is one requested state change on a check.
type ControlAction string

const (
	// ControlPause stops a check that cannot produce a verdict, so its noise ends while a
	// person decides what to do with it.
	ControlPause ControlAction = "PAUSE"
	// ControlResume restarts a paused check. It is not a case verb: pausing escalates the case,
	// which releases the subject, and a paused check produces no new evidence to base a resume
	// on. A person resumes it.
	ControlResume ControlAction = "RESUME"
	// ControlArchive retires a check.
	ControlArchive ControlAction = "ARCHIVE"
)

// DecideControl answers whether one state change may be applied to one case. Pausing is only for
// a check that cannot produce a verdict: there is no red to hide, so stopping the noise is safe,
// while pausing a check that concludes would remove it from the score.
func (p Policy) DecideControl(facts CaseFacts, action ControlAction) Verdict {
	if facts.ManualLock {
		return Verdict{Reason: "a person manages this check"}
	}
	if facts.State != store.QualityAgentCaseStateDiagnosed {
		return Verdict{Reason: "only a diagnosed case may change a check's state"}
	}
	if facts.Diagnosis == nil {
		return Verdict{Reason: "the failure has no diagnosis, so there is nothing to pause"}
	}
	switch action {
	case ControlPause:
	case ControlResume:
		// The pause escalates the case and a terminal case releases the subject, so there is no
		// case left to resume from; a paused check also produces no new evidence.
		return Verdict{Reason: "resuming a paused check is a person's decision"}
	case ControlArchive:
		return Verdict{Reason: "retiring a check is a decision made on the check itself, not from a case"}
	default:
		return Verdict{Reason: "the control action is not one this build understands"}
	}
	switch facts.Diagnosis.Class {
	case dataquality.ClassConfigValueInvalid,
		dataquality.ClassConfigUnsupported,
		dataquality.ClassStructureDrift:
		// The check itself is the problem. Nothing that could have produced a verdict is being
		// hidden, because these classes cannot produce one.
	default:
		return Verdict{Reason: "only a check that cannot run may be paused; this failure is " + string(facts.Diagnosis.Class)}
	}
	return p.Allows(Decision{Action: dataquality.ActionControl, Effect: EffectState})
}
