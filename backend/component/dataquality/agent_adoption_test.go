package dataquality

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestAgentAdoptionReady(t *testing.T) {
	t.Parallel()

	active := func() *store.QualityAgentMessage {
		return &store.QualityAgentMessage{
			State:        store.QualityAgentStateActive,
			Capabilities: []string{agentcapability.Suggest, agentcapability.Adopt},
			AdoptionMode: store.QualityAgentAdoptionModeActive,
		}
	}

	cases := []struct {
		name  string
		agent *store.QualityAgentMessage
		want  bool
	}{
		{name: "nil", agent: nil, want: false},
		{name: "active with both capabilities", agent: active(), want: true},
		{name: "paused adoption still allows adoption", agent: func() *store.QualityAgentMessage {
			agent := active()
			agent.AdoptionMode = store.QualityAgentAdoptionModePaused
			return agent
		}(), want: true},
		{name: "off", agent: func() *store.QualityAgentMessage {
			agent := active()
			agent.AdoptionMode = store.QualityAgentAdoptionModeOff
			return agent
		}(), want: false},
		{name: "paused agent", agent: func() *store.QualityAgentMessage {
			agent := active()
			agent.State = store.QualityAgentStatePaused
			return agent
		}(), want: false},
		{name: "missing suggest", agent: func() *store.QualityAgentMessage {
			agent := active()
			agent.Capabilities = []string{agentcapability.Adopt}
			return agent
		}(), want: false},
		{name: "missing adopt", agent: func() *store.QualityAgentMessage {
			agent := active()
			agent.Capabilities = []string{agentcapability.Suggest}
			return agent
		}(), want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, AgentAdoptionReady(testCase.agent))
		})
	}
}

func TestRuleTypeForDraftConfig(t *testing.T) {
	t.Parallel()

	ruleType, ok := RuleTypeForDraftConfig(&storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "id"}},
	})
	require.True(t, ok)
	require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE, ruleType)

	_, ok = RuleTypeForDraftConfig(nil)
	require.False(t, ok)
}
