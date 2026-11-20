package v1

import (
	"errors"
	"fmt"
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

// The owner-requested tool must reject a target whose database left the agent's
// scope; the same predicate is used for the explicit named-draft branch.
func TestScopeCoversTarget(t *testing.T) {
	t.Parallel()

	scope := []string{"instances/i-1/databases/db"}
	require.True(t, scopeCoversTarget(scope, "i-1;db;;orders"))
	require.False(t, scopeCoversTarget(scope, "i-1;other;;orders"))
	require.False(t, scopeCoversTarget(scope, ""))
	require.False(t, scopeCoversTarget(nil, "i-1;db;;orders"))
}

// A wide adoption call keeps its counts truthful and caps the per-draft detail so
// the tool result stays inside its byte budget.
func TestAdoptionReportCapsItemsAndKeepsCounts(t *testing.T) {
	t.Parallel()

	var result adoptionPassResult
	for index := 0; index < 25; index++ {
		result.recordItem(&store.QualitySuggestionMessage{
			ResourceID: fmt.Sprintf("draft-%d", index),
			Payload: &storepb.QualitySuggestionPayload{Payload: &storepb.QualitySuggestionPayload_Rule{
				Rule: &storepb.QualityRuleDraftConfig{Title: "a draft"},
			}},
		}, "skipped", "the target is outside the agent's database scope")
		result.Skipped++
	}
	result.RemainingKnown = true
	report := result.report()
	require.Equal(t, 25, report.Skipped)
	require.Len(t, report.Items, 20)
	require.True(t, report.ItemsTruncated)
	require.True(t, report.RemainingKnown)
	require.Equal(t, 20, len(report.Items), "the report detail is bounded")
}
