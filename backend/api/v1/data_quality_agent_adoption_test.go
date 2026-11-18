package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentcapability"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestRuleTypeForDraftConfig(t *testing.T) {
	t.Parallel()

	ruleType, ok := ruleTypeForDraftConfig(&storepb.QualityRuleConfig{
		Config: &storepb.QualityRuleConfig_RowCount{RowCount: &storepb.RowCountConfig{}},
	})
	require.True(t, ok)
	require.Equal(t, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, ruleType)

	_, ok = ruleTypeForDraftConfig(nil)
	require.False(t, ok)
}

func TestAdoptionAllowsRuleType(t *testing.T) {
	t.Parallel()

	agent := &store.QualityAgentMessage{}
	require.True(t, adoptionAllowsRuleType(agent, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))
	require.False(t, adoptionAllowsRuleType(agent, storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL))
	require.True(t, agentcapability.IsLowRiskRuleType(storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))

	agent.AdoptionRuleTypes = []int16{int16(storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL)}
	require.True(t, adoptionAllowsRuleType(agent, storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL))
	require.False(t, adoptionAllowsRuleType(agent, storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT))
}
