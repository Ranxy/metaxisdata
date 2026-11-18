// Package agentcapability owns the catalog of what a quality agent can do.
//
// Capabilities are data, not a proto enum: an administrator selects a set when
// creating or editing an agent, and the server validates the ids against this
// registry. Adding a capability is a registry entry plus the runner/product code
// that honors it; no schema migration is needed.
package agentcapability

import (
	"slices"

	"github.com/pkg/errors"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Stable capability ids. They are stored in quality_agent.capabilities
// and exposed by the API, so they are never renamed.
const (
	// Suggest lets an agent triage targets without coverage and draft checks,
	// plans and evidence-backed proposals. A person still decides them.
	Suggest = "suggest"
	// Adopt lets an agent automatically accept its own low-risk rule drafts
	// under the configured guardrails. It requires Suggest.
	Adopt = "adopt"
	// Patrol lets an agent monitor covered targets, investigate failures and
	// produce the scheduled patrol report.
	Patrol = "patrol"
)

// Capability is one catalog entry.
type Capability struct {
	// ID is the stable stored value.
	ID string
	// Title and Description are developer-facing English text; the UI owns the
	// localized copy keyed by ID.
	Title       string
	Description string
	// Requires lists capability ids that must also be selected.
	Requires []string
	// RequiresModel reports that the capability needs a configured LLM for its
	// primary work; Patrol degrades to a deterministic survey.
	RequiresModel bool
}

var catalog = []Capability{
	{
		ID:            Suggest,
		Title:         "Suggest",
		Description:   "Triage targets without coverage and draft checks, plans and evidence-backed proposals for a person to decide.",
		RequiresModel: true,
	},
	{
		ID:            Adopt,
		Title:         "Adopt",
		Description:   "Automatically accept this agent's own low-risk rule drafts under the configured guardrails; a person still publishes contracts and changes responsibility.",
		Requires:      []string{Suggest},
		RequiresModel: true,
	},
	{
		ID:          Patrol,
		Title:       "Patrol",
		Description: "Monitor covered targets, investigate failures, escalate to the owner and produce the scheduled patrol report.",
	},
}

// All returns a copy of the catalog so a caller cannot mutate the registry.
func All() []Capability {
	out := make([]Capability, len(catalog))
	copy(out, catalog)
	return out
}

// Lookup returns one capability.
func Lookup(id string) (Capability, bool) {
	for _, capability := range catalog {
		if capability.ID == id {
			return capability, true
		}
	}
	return Capability{}, false
}

// Validate checks a capability set: at least one, all known, no duplicates and
// every dependency selected.
func Validate(ids []string) error {
	if len(ids) == 0 {
		return errors.New("an agent needs at least one capability")
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		capability, ok := Lookup(id)
		if !ok {
			return errors.Errorf("unknown agent capability %q", id)
		}
		if seen[id] {
			return errors.Errorf("agent capability %q is selected twice", id)
		}
		seen[id] = true
		for _, required := range capability.Requires {
			if !slices.Contains(ids, required) {
				return errors.Errorf("agent capability %q requires %q", id, required)
			}
		}
	}
	return nil
}

// DefaultLowRiskRuleTypes is the conservative set of check types an agent may
// auto-adopt when the agent does not configure its own list. It deliberately
// excludes custom SQL, cross-table/cross-column, aggregate, CEL, segment,
// standard-code and schema-drift checks.
func DefaultLowRiskRuleTypes() []storepb.QualityRuleType {
	return []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ACCEPTED_VALUES,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_RANGE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_STRING_LENGTH,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_FRESHNESS,
	}
}

// IsLowRiskRuleType reports whether a rule type is in the default low-risk set.
func IsLowRiskRuleType(ruleType storepb.QualityRuleType) bool {
	return slices.Contains(DefaultLowRiskRuleTypes(), ruleType)
}
