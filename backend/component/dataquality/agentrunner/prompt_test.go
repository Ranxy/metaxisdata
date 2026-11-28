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
	section := adoptionPolicySection(agent, conversationProduct)
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
	section = adoptionPolicySection(agent, conversationProduct)
	require.Contains(t, section, "the default low-risk check types")
	require.Contains(t, section, "minimum model confidence: 0.70")
	require.Contains(t, section, "daily ceiling: 10")
	require.Contains(t, section, "critical assets: allowed")

	// An agent that cannot adopt is told so instead of being shown a policy it
	// cannot use.
	require.Contains(t, adoptionPolicySection(capabilityTestAgent(agentcapability.Suggest), conversationProduct), "Adoption is unavailable")
}

// A monitoring wake must be told what a blocker is not. It once read "escalate a blocker instead of
// guessing", judged a persistent FAIL a blocker, and invented a case id out of the execution it had
// — while the issue ledger had already reported that failure to the owner.
func TestMonitorPromptSaysAFailingCheckIsNotAWorkItem(t *testing.T) {
	t.Parallel()

	profile, ok := profileFor(agentcapability.Patrol)
	require.True(t, ok, "the patrol capability has a profile")
	sections := profile.Prompt(monitorSpec())
	require.Len(t, sections, 1)
	require.Contains(t, sections[0].Text, "A failing check is a finding, not a work item")
	require.Contains(t, sections[0].Text, "issue ledger")
	require.NotContains(t, sections[0].Text, "Escalate a blocker that needs a person instead of guessing",
		"an unqualified escalation invitation is what produced a fabricated case id")
}

// A fact is durable knowledge, not a wake log. The dev agent stored 188 "facts" averaging 500
// characters, one per wake, because both the tool and the wake wording invited a summary of the
// reasoning; the guidance now says what a fact is, without constraining what the tool accepts.
func TestFactRecordingIsGuidedNotConstrained(t *testing.T) {
	t.Parallel()

	var recordSpec toolSpec
	for _, spec := range triageSpec().Tools {
		if spec.name == "record_asset_fact" {
			recordSpec = spec
		}
	}
	require.Equal(t, "record_asset_fact", recordSpec.name, "the fact tool is on the triage product")
	require.Contains(t, recordSpec.description, "durable fact")
	require.Contains(t, recordSpec.description, "reasoning in your message")
	require.Contains(t, recordSpec.description, "Record nothing when a wake concluded nothing new")

	profile, ok := profileFor(agentcapability.Patrol)
	require.True(t, ok)
	sections := profile.Prompt(monitorSpec())
	require.Contains(t, sections[0].Text, "one durable sentence per fact")
}
