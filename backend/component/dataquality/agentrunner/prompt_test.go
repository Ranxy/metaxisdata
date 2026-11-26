package agentrunner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
)

// The adoption gate reaches the model as configuration, not as folklore: the
// eligible types, the confidence floor, the daily ceiling and the critical switch
// are all rendered, and so is the consequence of drafting outside them, which a
// wake otherwise learns only by watching an adoption be skipped.
func TestAdoptionPolicySectionRendersTheConfiguredGate(t *testing.T) {
	t.Parallel()

	agent := capabilityTestAgent(agentcapability.Suggest, agentcapability.Adopt)
	section := adoptionPolicySection(agent)
	require.Contains(t, section, "mode ACTIVE")
	require.Contains(t, section, "eligible check types: NULL_PERCENT")
	require.Contains(t, section, "minimum model confidence: 0.70")
	require.Contains(t, section, "daily ceiling: 20")
	require.Contains(t, section, "critical assets: not allowed")
	require.Contains(t, section, "waits for a person")

	// The defaults are stated as numbers, because the model reasons about them.
	agent.AdoptionRuleTypes = nil
	agent.AdoptionConfidenceThreshold = 0
	agent.AdoptionMaxPerDay = 0
	agent.AdoptionAllowCritical = true
	section = adoptionPolicySection(agent)
	require.Contains(t, section, "the default low-risk check types")
	require.Contains(t, section, "minimum model confidence: 0.70")
	require.Contains(t, section, "daily ceiling: 10")
	require.Contains(t, section, "critical assets: allowed")

	// An agent that cannot adopt is told so instead of being shown a policy it
	// cannot use.
	require.Contains(t, adoptionPolicySection(capabilityTestAgent(agentcapability.Suggest)), "Adoption is unavailable")
}
