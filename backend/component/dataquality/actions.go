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

// ActionCard is one catalog entry. Implemented marks the actions this codebase can
// actually perform today; the rest are the design's commitments and are refused
// with a readable reason rather than pretending to work.
type ActionCard struct {
	ID      ActionID
	Title   string
	Level   AutonomyLevel
	Actor   ActorKind
	RedLine bool
	// Implemented is true when a shipped write path performs this action.
	Implemented bool
}

var actionCatalog = []ActionCard{
	{ActionReadEvidence, "Read evidence", AutonomyA0, ActorAgent, false, true},
	{ActionPreviewProbe, "Run a preview probe", AutonomyA0, ActorAgent, false, true},
	{ActionWriteInvestigation, "Write an investigation", AutonomyA0, ActorAgent, false, false},
	{ActionProposePlanChange, "Propose a plan change", AutonomyA0, ActorAgent, false, true},
	{ActionWriteAssetFact, "Record an asset fact", AutonomyA0, ActorAgent, false, false},
	{ActionProduceReport, "Produce a coverage or trust report", AutonomyA0, ActorAgent, false, true},
	{ActionRequestEvidence, "Request more evidence", AutonomyA0, ActorAgent, false, true},
	{ActionRerunCheck, "Re-run one check", AutonomyA1, ActorAgent, false, true},
	{ActionMuteIssue, "Mute an issue", AutonomyA1, ActorHuman, true, true},
	{ActionNotifyOwner, "Notify the owner", AutonomyA1, ActorAgent, false, true},
	{ActionOpenTicket, "Open a repair ticket", AutonomyA1, ActorAgent, false, false},
	{ActionMarkNoise, "Mark an event as noise", AutonomyA1, ActorAgent, false, false},
	{ActionRecordDecisionWhy, "Record the reason behind a decision", AutonomyA1, ActorAgent, false, true},
	{ActionCreateCheck, "Create a low-risk check", AutonomyA2, ActorAgent, false, false},
	{ActionAdjustExpectation, "Adjust a check's expectation", AutonomyA2, ActorAgent, false, false},
	{ActionReduceCadence, "Reduce a check's cadence", AutonomyA2, ActorAgent, false, false},
	{ActionArchiveCheck, "Archive a check", AutonomyA2, ActorHuman, true, true},
	{ActionAcknowledgeSchema, "Acknowledge a schema change", AutonomyA2, ActorHuman, true, true},
	{ActionPublishContract, "Publish a contract version", AutonomyA3, ActorHuman, true, true},
	{ActionCloseOrReassign, "Close an issue, change its owner or criticality", AutonomyA3, ActorHuman, true, true},
	{ActionExpandAuthority, "Expand authority: agents, cross-target plans, large batches", AutonomyA3, ActorHuman, false, true},
}

// ActionCatalog returns a copy of the catalog, so a caller cannot reorder the
// policy matrix by accident.
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
	if input.Actor == ActorAgent {
		if !input.AutonomyEnabled {
			return PolicyDecision{Code: DecisionDeniedAutonomyOff,
				Reason: "agent autonomy is disabled in this workspace"}
		}
		if !input.AgentEnabled {
			return PolicyDecision{Code: DecisionDeniedAutonomyOff,
				Reason: "this agent is disabled"}
		}
	}
	// The amplifier: a critical asset never lowers the requirement. An action above
	// A0, or a batch of proposals, is handed to a person instead of being performed.
	if input.Criticality >= CriticalityCritical && input.Actor == ActorAgent {
		if card.Level >= AutonomyA1 || input.ItemCount > 1 {
			return PolicyDecision{RequiresApproval: true, Code: DecisionRequiresApproval,
				Reason: "a critical asset's changes are decided by a person, one item at a time"}
		}
	}
	// A person performing an A2/A3 action is the approval.
	requiresApproval := card.Level >= AutonomyA2 && input.Actor == ActorAgent
	return PolicyDecision{
		Allowed:          true,
		RequiresApproval: requiresApproval,
		Code:             DecisionAllowed,
		Reason:           "allowed by the autonomy policy",
	}
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
