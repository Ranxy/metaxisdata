// Package agentpolicy owns how much authority one quality agent has over one target and
// which actions that authority permits. It is the single place the delegation is decided:
// the prompt renders it, the wake's verbs enforce it, and a startup check refuses a tool
// whose declared permission the agent principal could not hold.
//
// The package is deliberately free of IO: a caller passes the participation levels it read,
// and the policy answers questions about them.
package agentpolicy

import (
	"strings"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
)

// Participation is how far one agent may go on one target. Zero means "inherit" in the
// database and target slots, and "unset" on the agent row, where the delegation default
// applies.
type Participation int16

const (
	ParticipationUnset Participation = 0
	// ParticipationObserve may read, record facts, run a check and escalate, and nothing
	// else.
	ParticipationObserve Participation = 1
	// ParticipationRepair may also fix what makes a check unable to run and stop the noise of
	// one that cannot conclude. It may not change what the check judges.
	ParticipationRepair Participation = 2
	// ParticipationTune may also change a criterion, but only through the replay gate.
	ParticipationTune Participation = 3
)

// DefaultParticipation is the level a delegated target starts at: the agent maintains the
// target, while changing a criterion is a decision the operator opts into.
const DefaultParticipation = ParticipationRepair

// Name is the stable upper-case label the API, the settings dialog and the prompt render.
func (p Participation) Name() string {
	switch p {
	case ParticipationObserve:
		return "OBSERVE"
	case ParticipationRepair:
		return "REPAIR"
	case ParticipationTune:
		return "TUNE"
	default:
		return ""
	}
}

// ParseParticipation reads the stored or wire label.
func ParseParticipation(value string) (Participation, bool) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "OBSERVE":
		return ParticipationObserve, true
	case "REPAIR":
		return ParticipationRepair, true
	case "TUNE":
		return ParticipationTune, true
	default:
		return ParticipationUnset, false
	}
}

// AtLeast reports whether p grants at least the level of other.
func (p Participation) AtLeast(other Participation) bool {
	return p >= other
}

// Effective resolves the level a target runs at: the most specific row wins, and an unset
// slot inherits. The agent row is the delegation, the database row is the operator's answer
// at the moment they delegated, and the target row is an explicit exception.
func Effective(agent Participation, database, target *Participation) Participation {
	level := agent
	if level == ParticipationUnset {
		level = DefaultParticipation
	}
	if database != nil && *database != ParticipationUnset {
		level = *database
	}
	if target != nil && *target != ParticipationUnset {
		level = *target
	}
	return level
}

// Effect is what an action touches. The same verb means different things depending on it:
// rewriting a bound whose literal the engine refused establishes a criterion as much as
// rewriting a threshold does, and only one of those may be a `repair`-level act.
type Effect int16

const (
	// EffectNone is an action that changes no definition: run, record, propose, escalate.
	EffectNone Effect = 0
	// EffectExecutability is a value the server can compute from the registry: a structure
	// snapshot, a literal form that must bind.
	EffectExecutability Effect = 1
	// EffectCriterion changes what counts as acceptable: thresholds, bounds, expected values,
	// on_empty, sampling, a filter's scope.
	EffectCriterion Effect = 2
	// EffectSubject changes what is judged: the rule type, the target, the checked columns.
	EffectSubject Effect = 3
	// EffectState stops or restarts a check without changing its definition.
	EffectState Effect = 4
	// EffectRemoval retires a check. Only its history survives, so it is the strictest write.
	EffectRemoval Effect = 5
)

// Name is the stable upper-case label the patch, the report and the prompt render. It is the
// label a person reads in "a criterion change", so it must not be the Go constant's spelling.
func (e Effect) Name() string {
	switch e {
	case EffectNone:
		return ""
	case EffectExecutability:
		return "EXECUTABILITY"
	case EffectCriterion:
		return "CRITERION"
	case EffectSubject:
		return "SUBJECT"
	case EffectState:
		return "STATE"
	case EffectRemoval:
		return "REMOVAL"
	default:
		return ""
	}
}

// Decision is one requested action reduced to what the policy decides on.
type Decision struct {
	Action dataquality.RepairAction
	Effect Effect
}

// Verdict is the policy's answer. A refusal always carries the next step, because a bare
// "not allowed" is what makes an agent repeat itself or park the session.
type Verdict struct {
	Allowed bool
	// Reason is empty when the action is allowed, and the refusal plus the next step when it
	// is not.
	Reason string
}

// Policy is one agent's resolved authority over one target.
type Policy struct {
	// Participation is the effective level for the target.
	Participation Participation
	// Capabilities is the agent's selected capability set. It does not change what a level
	// permits, but an action whose tool the agent lacks is not executable; the tool set, not
	// this value, is the final word.
	Capabilities []string
}

// Allows answers whether this policy permits one action. The rules are a table, not a
// judgement call:
//
//	                           observe  repair  tune
//	RUN_NOW / RECORD / PROPOSE    yes     yes    yes
//	ESCALATE                      yes     yes    yes
//	REVISE_FIELD executability     no     yes    yes
//	REVISE_FIELD criterion         no      no    yes
//	CONTROL state (pause/resume)   no     yes    yes
//	CONTROL removal (archive)      no      no    yes
//	anything touching subject      no      no     no
func (p Policy) Allows(decision Decision) Verdict {
	switch decision.Action {
	case dataquality.ActionRunNow, dataquality.ActionRecord,
		dataquality.ActionProposeRevision, dataquality.ActionEscalate:
		// Reading evidence and asking a person are always permitted: they are how an
		// observe-level agent is useful at all.
		return Verdict{Allowed: true}
	case dataquality.ActionReviseField:
		return p.allowsFieldWrite(decision.Effect)
	case dataquality.ActionControl:
		return p.allowsStateWrite(decision.Effect)
	case dataquality.ActionResolveEmpty, dataquality.ActionAckStructure:
		// The server performs these from a deterministic diagnosis; no model verb exists for
		// them, so a request for one is a defect rather than an authorization question.
		return Verdict{Reason: "the server performs this action from a diagnosis; it is not a model verb"}
	default:
		return Verdict{Reason: "the action is not one this agent may take on a target"}
	}
}

// allowsFieldWrite is the REVISE_FIELD row of the table.
func (p Policy) allowsFieldWrite(effect Effect) Verdict {
	level := p.level()
	switch effect {
	case EffectExecutability:
		if level.AtLeast(ParticipationRepair) {
			return Verdict{Allowed: true}
		}
		return refuse(level, "rewriting a value needs a repair-level delegation")
	case EffectCriterion:
		if level.AtLeast(ParticipationTune) {
			return Verdict{Allowed: true}
		}
		return refuse(level, "changing what counts as acceptable needs a tune-level delegation; propose the change instead")
	case EffectSubject, EffectState, EffectRemoval, EffectNone:
		return Verdict{Reason: "a field rewrite cannot change what is judged, stop a check or retire it"}
	default:
		return Verdict{Reason: "the change has no effect class this build understands"}
	}
}

// allowsStateWrite is the CONTROL row of the table.
func (p Policy) allowsStateWrite(effect Effect) Verdict {
	level := p.level()
	switch effect {
	case EffectState:
		if level.AtLeast(ParticipationRepair) {
			return Verdict{Allowed: true}
		}
		return refuse(level, "pausing or resuming a check needs a repair-level delegation")
	case EffectRemoval:
		if level.AtLeast(ParticipationTune) {
			return Verdict{Allowed: true}
		}
		return refuse(level, "retiring a check needs a tune-level delegation; propose it instead")
	case EffectCriterion, EffectSubject, EffectExecutability, EffectNone:
		return Verdict{Reason: "a control action is a state change, not a definition change"}
	default:
		return Verdict{Reason: "the change has no effect class this build understands"}
	}
}

// level resolves the unset default so every table row reads the same value.
func (p Policy) level() Participation {
	if p.Participation == ParticipationUnset {
		return DefaultParticipation
	}
	return p.Participation
}

// refuse builds a refusal that names the level the operator has to raise.
func refuse(level Participation, step string) Verdict {
	return Verdict{Reason: step + " (current participation: " + level.Name() + ")"}
}

// EffectiveLevel is the participation the policy runs at, with the unset default resolved. A
// refusal names it, so the message a model reads and the metric a dashboard counts agree.
func (p Policy) EffectiveLevel() Participation { return p.level() }

// CanTune reports whether the policy may change a criterion at all. The prompt and the case
// view use it to say what is possible without enumerating verbs.
func (p Policy) CanTune() bool {
	return p.level().AtLeast(ParticipationTune)
}
