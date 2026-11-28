package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// A draft nobody may retract is the one another principal proposed: a human or external
// suggestion (no agent id) and another agent's draft are both refused. The author check is
// what keeps the withdrawal from becoming an agent-side rejection of somebody else's queue.
func TestRequireWithdrawableDraftRefusesProposalsTheAgentDidNotMake(t *testing.T) {
	t.Parallel()

	agent := &store.QualityAgentMessage{ID: 7}
	own := int64(7)
	other := int64(8)

	require.NoError(t, requireWithdrawableDraft(agent, &store.QualitySuggestionMessage{
		AgentID: &own, Kind: storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_RULE_DRAFT,
	}))
	require.ErrorContains(t,
		requireWithdrawableDraft(agent, &store.QualitySuggestionMessage{AgentID: &other}),
		"another principal")
	require.ErrorContains(t,
		requireWithdrawableDraft(agent, &store.QualitySuggestionMessage{}),
		"another principal", "a human proposal has no agent and is never the agent's to retract")
	require.ErrorContains(t,
		requireWithdrawableDraft(nil, &store.QualitySuggestionMessage{AgentID: &own}),
		"another principal")
	require.ErrorContains(t,
		requireWithdrawableDraft(agent, &store.QualitySuggestionMessage{
			AgentID: &own, Kind: storepb.QualitySuggestionKind_QUALITY_SUGGESTION_KIND_LABEL,
		}),
		"only a check draft", "a label proposal is not a check draft and has no approval ask")
}
