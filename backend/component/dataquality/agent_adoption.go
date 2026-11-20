package dataquality

import (
	"slices"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// AgentAdoptionRequest is one owner-requested adoption pass for a quality agent.
// It is the in-process contract between the agent runtime's adopt tool and the
// service that owns the rule-creation path; it is not an RPC message.
type AgentAdoptionRequest struct {
	// Session is the target work session or the owner conversation that made the
	// request. A target session scopes the pass to its target; a conversation may
	// name a target or adopt every pending draft of the agent.
	Session *store.QualityAgentSessionMessage
	// TargetGUID optionally narrows the pass to one target. A target session leaves
	// it empty (its own target is implicit); a conversation may set it.
	TargetGUID string
	// DraftIDs names the drafts the owner explicitly approved. Empty means "apply the
	// configured adoption guardrails to every pending draft in scope".
	DraftIDs []string
	// Wake is the session wake that made the request, for the audit correlation.
	Wake int32
}

// AgentAdoptionItem is one draft's outcome in an adoption report.
type AgentAdoptionItem struct {
	ResourceID string `json:"id"`
	Title      string `json:"title"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
}

// AgentAdoptionReport is what one adoption pass did. The agent runtime renders it
// back to the model, so the counts are authoritative for the reply.
type AgentAdoptionReport struct {
	Adopted   int                 `json:"adopted"`
	Skipped   int                 `json:"skipped"`
	Failed    int                 `json:"failed"`
	Remaining int                 `json:"remaining"`
	Items     []AgentAdoptionItem `json:"items"`
}

// AgentAdoptionReady reports whether an agent can adopt at all: it is active, its
// adoption mode is not off, and it holds both the suggest and adopt capabilities.
// The same predicate gates the automatic pass, the explicit retry and the model
// tool, so the prompt and the executable capability cannot disagree.
func AgentAdoptionReady(agent *store.QualityAgentMessage) bool {
	return agent != nil &&
		agent.State == store.QualityAgentStateActive &&
		(agent.AdoptionMode == store.QualityAgentAdoptionModeActive ||
			agent.AdoptionMode == store.QualityAgentAdoptionModePaused) &&
		slices.Contains(agent.Capabilities, agentcapability.Adopt) &&
		slices.Contains(agent.Capabilities, agentcapability.Suggest)
}
