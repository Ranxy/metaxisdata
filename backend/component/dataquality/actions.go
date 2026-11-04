package dataquality

import "strings"

// AutonomyLevel is the WS2 action catalog's risk ladder. A0 reads and proposes,
// A1 acts reversibly on one event, A2 changes future judging, A3 changes promises,
// responsibility or irreversible state.
type AutonomyLevel int

const (
	AutonomyA0 AutonomyLevel = iota
	AutonomyA1
	AutonomyA2
	AutonomyA3
)

// ActorKind says who may ever perform an action.
type ActorKind int

const (
	ActorAgent ActorKind = iota + 1
	ActorHuman
)

// ActionID names one card of the catalog. The identifiers match WS2's
// action-inventory so the workshop, the code and any future UI share one name.
type ActionID string

const (
	ActionReadEvidence       ActionID = "A0-1"
	ActionPreviewProbe       ActionID = "A0-2"
	ActionWriteInvestigation ActionID = "A0-3"
	ActionProposePlanChange  ActionID = "A0-4"
	ActionWriteAssetFact     ActionID = "A0-5"
	ActionProduceReport      ActionID = "A0-6"
	ActionRequestEvidence    ActionID = "A0-7"
	ActionRerunCheck         ActionID = "A1-1"
	ActionMuteIssue          ActionID = "A1-2"
	ActionNotifyOwner        ActionID = "A1-3"
	ActionOpenTicket         ActionID = "A1-4"
	ActionMarkNoise          ActionID = "A1-5"
	ActionRecordDecisionWhy  ActionID = "A1-6"
	ActionCreateCheck        ActionID = "A2-1"
	ActionAdjustExpectation  ActionID = "A2-2"
	ActionReduceCadence      ActionID = "A2-3"
	ActionArchiveCheck       ActionID = "A2-4"
	ActionAcknowledgeSchema  ActionID = "A2-5"
	ActionPublishContract    ActionID = "A3-1"
	ActionCloseOrReassign    ActionID = "A3-2"
	ActionExpandAuthority    ActionID = "A3-3"
)

// ActionCard is one catalog entry.
//
// Implemented says a shipped server write path performs the action; AgentReachable
// says the shipped agent role and handlers actually expose it to an agent. The two
// differ on purpose: a check can be authored by a person while an agent may only
// propose it through a change request, and the catalog must not call that "allowed".
type ActionCard struct {
	ID             ActionID
	Title          string
	Level          AutonomyLevel
	Actor          ActorKind
	RedLine        bool
	Implemented    bool
	AgentReachable bool
}

var actionCatalog = []ActionCard{
	{ID: ActionReadEvidence, Title: "Read evidence", Level: AutonomyA0, Actor: ActorAgent, Implemented: true},
	{ID: ActionPreviewProbe, Title: "Run a preview probe", Level: AutonomyA0, Actor: ActorAgent, Implemented: true},
	{ID: ActionWriteInvestigation, Title: "Write an investigation", Level: AutonomyA0, Actor: ActorAgent},
	{ID: ActionProposePlanChange, Title: "Propose a plan change", Level: AutonomyA0, Actor: ActorAgent, Implemented: true, AgentReachable: true},
	{ID: ActionWriteAssetFact, Title: "Record an asset fact", Level: AutonomyA0, Actor: ActorAgent},
	{ID: ActionProduceReport, Title: "Produce a coverage or trust report", Level: AutonomyA0, Actor: ActorAgent, Implemented: true, AgentReachable: true},
	{ID: ActionRequestEvidence, Title: "Request more evidence", Level: AutonomyA0, Actor: ActorAgent, Implemented: true},
	{ID: ActionRerunCheck, Title: "Re-run one check", Level: AutonomyA1, Actor: ActorAgent, Implemented: true},
	{ID: ActionMuteIssue, Title: "Mute an issue", Level: AutonomyA1, Actor: ActorHuman, RedLine: true, Implemented: true},
	{ID: ActionNotifyOwner, Title: "Notify the owner", Level: AutonomyA1, Actor: ActorAgent},
	{ID: ActionOpenTicket, Title: "Open a repair ticket", Level: AutonomyA1, Actor: ActorAgent},
	{ID: ActionMarkNoise, Title: "Mark an event as noise", Level: AutonomyA1, Actor: ActorAgent},
	{ID: ActionRecordDecisionWhy, Title: "Record the reason behind a decision", Level: AutonomyA1, Actor: ActorAgent, Implemented: true},
	{ID: ActionCreateCheck, Title: "Create a low-risk check", Level: AutonomyA2, Actor: ActorAgent, Implemented: true},
	{ID: ActionAdjustExpectation, Title: "Adjust a check's expectation", Level: AutonomyA2, Actor: ActorAgent, Implemented: true},
	{ID: ActionReduceCadence, Title: "Reduce a check's cadence", Level: AutonomyA2, Actor: ActorAgent, Implemented: true},
	{ID: ActionArchiveCheck, Title: "Archive a check", Level: AutonomyA2, Actor: ActorHuman, RedLine: true, Implemented: true},
	{ID: ActionAcknowledgeSchema, Title: "Acknowledge a schema change", Level: AutonomyA2, Actor: ActorHuman, RedLine: true, Implemented: true},
	{ID: ActionPublishContract, Title: "Publish a contract version", Level: AutonomyA3, Actor: ActorHuman, RedLine: true, Implemented: true},
	{ID: ActionCloseOrReassign, Title: "Close an issue, change its owner or criticality", Level: AutonomyA3, Actor: ActorHuman, RedLine: true, Implemented: true},
	{ID: ActionExpandAuthority, Title: "Expand authority: agents, cross-target plans, large batches", Level: AutonomyA3, Actor: ActorHuman, RedLine: true, Implemented: true},
}

// ActionCatalog returns a copy of the catalog, so a caller cannot mutate the
// policy matrix through the returned slice.
func ActionCatalog() []ActionCard {
	out := make([]ActionCard, len(actionCatalog))
	copy(out, actionCatalog)
	return out
}

// ActionByID returns one card.
func ActionByID(id ActionID) (ActionCard, bool) {
	for _, card := range actionCatalog {
		if card.ID == id {
			return card, true
		}
	}
	return ActionCard{}, false
}

// DecisionCode is the machine-readable outcome of the policy check.
type DecisionCode int

const (
	DecisionAllowed DecisionCode = iota + 1
	DecisionDeniedAutonomyOff
	DecisionDeniedHumanOnly
	DecisionDeniedRedLine
	DecisionDeniedNotImplemented
	// DecisionDeniedNotExposed means the server has the write but the agent role or
	// a handler does not expose it to agents; it is not "not implemented".
	DecisionDeniedNotExposed
	// DecisionDeniedCritical means the criticality amplifier hands the action to a
	// person instead of letting an agent perform it.
	DecisionDeniedCritical
	// DecisionDeniedUnknownActor means the policy input did not say whether the actor
	// is an agent or a person; the policy fails closed.
	DecisionDeniedUnknownActor
	// DecisionRequiresApproval is a verdict for an allowed action that a person must
	// still approve before it runs. No handler performs it on its own.
	DecisionRequiresApproval
)

// PolicyInput is the live state a policy decision reads. Criticality is the
// amplifier: it never makes an action cheaper.
type PolicyInput struct {
	Action      ActionID
	Actor       ActorKind
	Criticality AssetCriticality
	// AutonomyEnabled is the workspace kill switch (agents only).
	AutonomyEnabled bool
	// AgentEnabled is the agent principal's own state (agents only).
	AgentEnabled bool
	// ItemCount is the blast radius of a batch (0 for single actions).
	ItemCount int
}

// PolicyDecision is a policy check's verdict. Allowed never means "silently":
// RequiresApproval says a person must still decide.
type PolicyDecision struct {
	Allowed          bool
	RequiresApproval bool
	Code             DecisionCode
	Reason           string
}

// DecideAction evaluates one action for one actor. Red lines and human-only
// actions are refused for agents even when autonomy is on; an unimplemented action
// is refused with its own reason so a caller cannot mistake "not yet" for "allowed".
func DecideAction(input PolicyInput) PolicyDecision {
	card, ok := ActionByID(input.Action)
	if !ok {
		return PolicyDecision{Code: DecisionDeniedNotImplemented, Reason: "unknown quality action"}
	}
	// Fail closed on an actor the policy cannot classify; a forgotten actor must not
	// skip the red-line and human-only checks.
	if input.Actor != ActorAgent && input.Actor != ActorHuman {
		return PolicyDecision{Code: DecisionDeniedUnknownActor,
			Reason: "the policy needs to know whether the actor is an agent or a person"}
	}
	// Static constraints come first: a red line is closed whatever the switches say,
	// and an action with no write path is refused as such instead of being promised.
	if card.Actor == ActorHuman && input.Actor == ActorAgent {
		if card.RedLine {
			return PolicyDecision{Code: DecisionDeniedRedLine,
				Reason: "this action is a red line and needs a person"}
		}
		return PolicyDecision{Code: DecisionDeniedHumanOnly,
			Reason: "this action is reserved for a person"}
	}
	if !card.Implemented {
		return PolicyDecision{Code: DecisionDeniedNotImplemented,
			Reason: "this quality action is not implemented yet"}
	}
	if input.Actor == ActorAgent && !card.AgentReachable {
		return PolicyDecision{Code: DecisionDeniedNotExposed,
			Reason: "the agent role does not expose this action; a person performs it"}
	}
	if input.Actor == ActorAgent {
		if !input.AutonomyEnabled {
			return PolicyDecision{Code: DecisionDeniedAutonomyOff,
				Reason: "agent autonomy is disabled in this workspace"}
		}
		if !input.AgentEnabled {
			return PolicyDecision{Code: DecisionDeniedAutonomyOff,
				Reason: "this agent is disabled; an administrator must create a new agent"}
		}
	}
	// The amplifier: a critical asset never lowers the requirement. An action above
	// A0, or a batch of proposals, is handed to a person instead of being performed.
	if input.Criticality >= CriticalityCritical && input.Actor == ActorAgent {
		if card.Level >= AutonomyA1 || input.ItemCount > 1 {
			return PolicyDecision{Allowed: false, RequiresApproval: true, Code: DecisionDeniedCritical,
				Reason: "a critical asset's changes are decided by a person, one item at a time"}
		}
	}
	// A person performing an A2/A3 action is the approval. An agent's A2+ action is
	// allowed only as a proposal that still needs a person.
	if input.Actor == ActorAgent && card.Level >= AutonomyA2 {
		return PolicyDecision{Allowed: true, RequiresApproval: true, Code: DecisionRequiresApproval,
			Reason: "allowed as a proposal; a person must still approve it"}
	}
	return PolicyDecision{Allowed: true, Code: DecisionAllowed,
		Reason: "allowed by the autonomy policy"}
}

// AutonomyLevelName is the stable name the API exposes for a level.
func AutonomyLevelName(level AutonomyLevel) string {
	switch level {
	case AutonomyA0:
		return "A0"
	case AutonomyA1:
		return "A1"
	case AutonomyA2:
		return "A2"
	case AutonomyA3:
		return "A3"
	default:
		return "UNKNOWN"
	}
}

// ActorKindName is the stable name the API exposes for an actor kind.
func ActorKindName(actor ActorKind) string {
	switch actor {
	case ActorAgent:
		return "AGENT"
	case ActorHuman:
		return "HUMAN"
	default:
		return "UNKNOWN"
	}
}

// ParseAutonomyLevel parses the API's level name.
func ParseAutonomyLevel(value string) (AutonomyLevel, bool) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "A0":
		return AutonomyA0, true
	case "A1":
		return AutonomyA1, true
	case "A2":
		return AutonomyA2, true
	case "A3":
		return AutonomyA3, true
	default:
		return 0, false
	}
}
