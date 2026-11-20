package quality

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type fakeAgentStore struct {
	suggestions []*store.QualitySuggestionMessage
	status      *store.QualityAgentAdoptionStatusMessage
}

func (*fakeAgentStore) ListQualityRules(context.Context, *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error) {
	return nil, nil
}

func (*fakeAgentStore) GetQualityContract(context.Context, string, storepb.MetaType) (*store.QualityContractMessage, error) {
	return nil, nil
}

func (*fakeAgentStore) ListQualityExecutions(context.Context, *store.FindQualityExecutionMessage) ([]*store.QualityExecutionMessage, error) {
	return nil, nil
}

func (*fakeAgentStore) ListQualityMemories(context.Context, *store.FindQualityMemoryMessage) ([]*store.QualityMemoryMessage, error) {
	return nil, nil
}

func (*fakeAgentStore) CreateQualityMemory(context.Context, *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error) {
	return nil, nil
}

func (f *fakeAgentStore) ListQualitySuggestions(context.Context, *store.FindQualitySuggestionMessage) ([]*store.QualitySuggestionMessage, error) {
	return f.suggestions, nil
}

func (f *fakeAgentStore) GetQualityAgentAdoptionStatus(context.Context, int64, time.Time) (*store.QualityAgentAdoptionStatusMessage, error) {
	return f.status, nil
}

// The target summary is the model's evidence for attribution: it must carry the
// agent's adoption outcome and the pending drafts with the ids the adoption tool
// takes, not only the rule count.
func TestGetTargetSummaryCarriesAdoptionAndPendingDrafts(t *testing.T) {
	t.Parallel()

	store := &fakeAgentStore{
		status: &store.QualityAgentAdoptionStatusMessage{
			AdoptedLast24h:    5,
			FailedLast24h:     0,
			PendingDraftCount: 2,
		},
		suggestions: []*store.QualitySuggestionMessage{{
			ResourceID: "s1",
			TargetGUID: "target-1",
			TargetType: storepb.MetaType_TABLE,
			Confidence: 0.9,
			Payload: &storepb.QualitySuggestionPayload{
				Payload: &storepb.QualitySuggestionPayload_Rule{Rule: &storepb.QualityRuleDraftConfig{
					Title:      "id must be unique",
					Confidence: 0.9,
					Config: &storepb.QualityRuleConfig{
						Config: &storepb.QualityRuleConfig_Unique{Unique: &storepb.UniqueConfig{Column: "id"}},
					},
				}},
			},
		}},
	}
	tools := NewAgentTools(AgentDeps{Store: store})
	result, err := tools.GetTargetSummary(context.Background(), AgentSession{
		WorkspaceID: "ws",
		AgentID:     1,
		TargetGUID:  "target-1",
		TargetType:  storepb.MetaType_TABLE,
	})
	require.NoError(t, err)
	require.Contains(t, result, `"adoptedLast24h":5`)
	require.Contains(t, result, `"pendingDraftCount":1`)
	require.Contains(t, result, `"id":"workspaces/ws/qualitySuggestions/s1"`)
	require.Contains(t, result, `"ruleType":"UNIQUE"`)
	require.Contains(t, result, `"title":"id must be unique"`)
}
