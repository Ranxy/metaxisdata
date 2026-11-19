package v1

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"connectrpc.com/connect"
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

func TestAdoptionFailureReasonKeepsTheCause(t *testing.T) {
	t.Parallel()

	require.Empty(t, adoptionFailureReason(nil))

	cause := connect.NewError(connect.CodeInvalidArgument, errors.New("quality rule dimension is required"))
	reason := adoptionFailureReason(cause)
	require.Contains(t, reason, "rule creation failed")
	require.Contains(t, reason, connect.CodeInvalidArgument.String())
	require.Contains(t, reason, "quality rule dimension is required")

	require.NotContains(t, reason, "invalid_argument: invalid_argument",
		"the connect code is not repeated inside the message")

	bounded := adoptionFailureReason(errors.New(strings.Repeat("x", 1000)))
	require.LessOrEqual(t, len(bounded), 240, "the ledger reason is bounded")

	boundedUnicode := adoptionFailureReason(errors.New(strings.Repeat("错", 300)))
	require.LessOrEqual(t, len(boundedUnicode), 240, "the ledger reason is bounded by bytes")
	require.True(t, utf8.ValidString(boundedUnicode), "the ledger reason stays valid UTF-8")
}
