package quality

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type fakeAgentStore struct {
	suggestions []*store.QualitySuggestionMessage
	status      *store.QualityAgentAdoptionStatusMessage
}

type fakeAgentEvidence struct {
	executions []dataquality.ExecutionEvidence
	check      *dataquality.CheckEvidence
	err        error
	principal  int32
	target     string
	limit      int
	rule       string
}

func (f *fakeAgentEvidence) ReadTargetExecutions(_ context.Context, principalID int32, targetGUID string, limit int) ([]dataquality.ExecutionEvidence, error) {
	f.principal, f.target, f.limit = principalID, targetGUID, limit
	return f.executions, f.err
}

func (f *fakeAgentEvidence) ReadCheckEvidence(_ context.Context, principalID int32, targetGUID, ruleResourceID string, limit int) (*dataquality.CheckEvidence, error) {
	f.principal, f.target, f.rule, f.limit = principalID, targetGUID, ruleResourceID, limit
	return f.check, f.err
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

// The evidence tools are adapters over the application-layer read: they must pass the
// agent principal and the session target through, and marshal what the read returns.
// The projection itself is the application layer's job (and is tested there).
func TestEvidenceToolsReadThroughTheApplicationLayer(t *testing.T) {
	t.Parallel()

	evidence := &fakeAgentEvidence{
		executions: []dataquality.ExecutionEvidence{{
			Execution: "qualityExecutions/4339",
			Rule:      "qualityRules/r460",
			Status:    "ERROR",
			ErrorCode: "BAD_VALUE",
		}},
		check: &dataquality.CheckEvidence{
			Target: "target-1",
			Rule: dataquality.CheckDefinition{
				Name:     "qualityRules/r460",
				Type:     "RANGE",
				Revision: 2,
				Config:   `{"range":{"column":"performed_at","min":"now - 1 day"}}`,
			},
			Diagnosis: &dataquality.Diagnosis{
				Class:      dataquality.ClassConfigValueInvalid,
				Field:      "range.min",
				Repairable: true,
				Actions:    []dataquality.RepairAction{dataquality.ActionReviseField, dataquality.ActionEscalate},
			},
		},
	}
	tools := NewAgentTools(AgentDeps{Store: &fakeAgentStore{}, Evidence: evidence})
	session := AgentSession{
		WorkspaceID:      "ws",
		AgentPrincipalID: 110,
		TargetGUID:       "target-1",
		TargetType:       storepb.MetaType_TABLE,
	}

	executions, err := tools.ListRecentExecutions(context.Background(), session)
	require.NoError(t, err)
	require.Contains(t, executions, `"execution":"qualityExecutions/4339"`)
	require.Contains(t, executions, `"errorCode":"BAD_VALUE"`)
	require.Equal(t, int32(110), evidence.principal)
	require.Equal(t, "target-1", evidence.target)

	inspected, err := tools.InspectCheck(context.Background(), session, `{"rule":"qualityRules/r460","executions":3}`)
	require.NoError(t, err)
	require.Contains(t, inspected, `"name":"qualityRules/r460"`)
	require.Contains(t, inspected, `"revision":2`)
	require.Contains(t, inspected, `now - 1 day`)
	require.Contains(t, inspected, `"class":"CONFIG_VALUE_INVALID"`)
	require.Contains(t, inspected, `"field":"range.min"`)
	require.Equal(t, "qualityRules/r460", evidence.rule)
	require.Equal(t, 3, evidence.limit)
}

// Without the application-layer read the tool refuses instead of falling back to an
// unbounded store query.
func TestEvidenceToolsRequireTheApplicationLayer(t *testing.T) {
	t.Parallel()

	tools := NewAgentTools(AgentDeps{Store: &fakeAgentStore{}})
	_, err := tools.ListRecentExecutions(context.Background(), AgentSession{TargetGUID: "target-1"})
	require.ErrorContains(t, err, "not configured")
	_, err = tools.InspectCheck(context.Background(), AgentSession{TargetGUID: "target-1"}, "")
	require.ErrorContains(t, err, "not configured")
}
