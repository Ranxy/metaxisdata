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
	rules       []*store.QualityRuleMessage
	executions  []*store.QualityExecutionMessage
}

func (f *fakeAgentStore) ListQualityRules(context.Context, *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error) {
	return f.rules, nil
}

func (*fakeAgentStore) GetQualityContract(context.Context, string, storepb.MetaType) (*store.QualityContractMessage, error) {
	return nil, nil
}

func (f *fakeAgentStore) ListQualityExecutions(context.Context, *store.FindQualityExecutionMessage) ([]*store.QualityExecutionMessage, error) {
	return f.executions, nil
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

// A failure list without the check it belongs to and the engine's own failure
// reason is unactionable: the sample that motivated this projection held a rule
// whose bound the database rejected, and the agent could only report that "the
// evaluation pipeline seems broken".
func TestListRecentExecutionsNamesTheCheckAndTheFailure(t *testing.T) {
	t.Parallel()

	finished := time.Date(2026, 11, 26, 6, 8, 55, 0, time.UTC)
	store := &fakeAgentStore{
		rules: []*store.QualityRuleMessage{{
			ID:         460,
			ResourceID: "r460",
			Title:      "performed_at must be recent and not in the future",
		}},
		executions: []*store.QualityExecutionMessage{{
			ID:           4339,
			RuleID:       460,
			RuleRevision: 1,
			Status:       storepb.QualityStatus_QUALITY_STATUS_ERROR,
			ErrorCode:    "BAD_VALUE",
			ErrorMessage: `invalid input syntax for type timestamp with time zone: "now - 1 day"`,
			Trigger:      storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
			Attempt:      1,
			StartedAt:    finished.Add(-12 * time.Millisecond),
			FinishedAt:   finished,
			DurationMS:   12,
		}},
	}
	tools := NewAgentTools(AgentDeps{Store: store})
	result, err := tools.ListRecentExecutions(context.Background(), AgentSession{
		WorkspaceID: "ws",
		TargetGUID:  "target-1",
		TargetType:  storepb.MetaType_TABLE,
	})
	require.NoError(t, err)
	require.Contains(t, result, `"execution":"qualityExecutions/4339"`)
	require.Contains(t, result, `"rule":"qualityRules/r460"`)
	require.Contains(t, result, `"title":"performed_at must be recent and not in the future"`)
	require.Contains(t, result, `"status":"ERROR"`)
	require.Contains(t, result, `"errorCode":"BAD_VALUE"`)
	require.Contains(t, result, `now - 1 day`)
}
