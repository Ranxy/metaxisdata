package agentpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
)

// The budget is the ceiling that stops "repair, freeze, reopen, repair" from looping on the agent's
// own. A zero on the agent row means the server default, and a refusal has to say what still works:
// a bare "no" is what makes a model retry the same verb or park the session.
func TestWriteBudgetRefusesAtTheCeilingAndNamesWhatStillWorks(t *testing.T) {
	t.Parallel()

	configured := WriteBudget{RepairsPerDay: 4, RunsPerRulePerDay: 2}
	for spent := int32(0); spent < 4; spent++ {
		reason, refused := configured.RepairRefusal(spent)
		require.False(t, refused, "spent %d", spent)
		require.Empty(t, reason)
	}
	reason, refused := configured.RepairRefusal(4)
	require.True(t, refused)
	require.Contains(t, reason, "4 writes to checks")
	require.Contains(t, reason, "proposing a change")
	require.Contains(t, reason, "resets tomorrow")

	for spent := int32(0); spent < 2; spent++ {
		_, refused := configured.RunRefusal(spent)
		require.False(t, refused, "spent %d", spent)
	}
	reason, refused = configured.RunRefusal(2)
	require.True(t, refused)
	require.Contains(t, reason, "2 times today")
	require.Contains(t, reason, "escalating still work")
}

// An agent row that leaves the slots at zero runs on the server defaults rather than on "no writes".
func TestWriteBudgetUnsetUsesTheServerDefault(t *testing.T) {
	t.Parallel()

	unset := WriteBudget{}
	require.Equal(t, int32(dataquality.DefaultAgentRepairsPerDay), unset.Effective().RepairsPerDay)
	require.Equal(t, int32(dataquality.DefaultAgentRunsPerRulePerDay), unset.Effective().RunsPerRulePerDay)
	_, refused := unset.RepairRefusal(dataquality.DefaultAgentRepairsPerDay - 1)
	require.False(t, refused)
	_, refused = unset.RepairRefusal(dataquality.DefaultAgentRepairsPerDay)
	require.True(t, refused)

	// One slot unset and the other configured resolves independently.
	partial := WriteBudget{RunsPerRulePerDay: 1}
	require.Equal(t, int32(dataquality.DefaultAgentRepairsPerDay), partial.Effective().RepairsPerDay)
	_, refused = partial.RunRefusal(1)
	require.True(t, refused)
}
