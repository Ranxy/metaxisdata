package agentrunner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type fakeStore struct {
	contract       *store.QualityContractMessage
	rules          []*store.QualityRuleMessage
	execution      *store.QualityExecutionMessage
	investigations []*store.CreateQualityInvestigationInput
	memories       []*store.CreateQualityMemoryInput
	steps          []*store.QualityAgentGoalStepMessage
}

func (f *fakeStore) GetQualityContract(context.Context, string, storepb.MetaType) (*store.QualityContractMessage, error) {
	return f.contract, nil
}

func (f *fakeStore) ListQualityRules(context.Context, *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error) {
	return f.rules, nil
}

func (f *fakeStore) GetQualityExecution(context.Context, int64) (*store.QualityExecutionMessage, error) {
	return f.execution, nil
}

func (f *fakeStore) CreateQualityInvestigation(_ context.Context, input *store.CreateQualityInvestigationInput) (*store.QualityInvestigationMessage, bool, error) {
	f.investigations = append(f.investigations, input)
	return &store.QualityInvestigationMessage{ResourceID: input.ResourceID}, true, nil
}

func (f *fakeStore) CreateQualityMemory(_ context.Context, input *store.CreateQualityMemoryInput) (*store.QualityMemoryMessage, error) {
	f.memories = append(f.memories, input)
	return &store.QualityMemoryMessage{ResourceID: input.ResourceID}, nil
}

func (f *fakeStore) AppendQualityAgentGoalStep(_ context.Context, step *store.QualityAgentGoalStepMessage) (*store.QualityAgentGoalStepMessage, error) {
	f.steps = append(f.steps, step)
	return step, nil
}

func maintainGoal() *store.QualityAgentGoalMessage {
	return &store.QualityAgentGoalMessage{
		ID: 1, ResourceID: "g1", WorkspaceID: "ws1",
		Kind:       storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_MAINTAIN_TARGET,
		TargetGUID: "inst;db;;orders", TargetType: storepb.MetaType_TABLE,
		DatabaseRef: "instances/inst/databases/db",
	}
}

// Without any contract the plan is unplanned and there is no gap to record.
func TestMaintainWithoutPlanRests(t *testing.T) {
	t.Parallel()

	fake := &fakeStore{}
	engine := NewEngine(fake)
	outcome, err := engine.Run(context.Background(), maintainGoal())
	require.NoError(t, err)
	require.Equal(t, storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED, outcome.Phase)
	require.Equal(t, "no-work", outcome.BlockedCode)
	require.NotNil(t, outcome.NextWakeAt)
	require.Empty(t, fake.investigations)
	require.Empty(t, fake.memories)
}

// A planned important target with no rule covering the template promises records one
// investigation per uncovered property and exactly one asset fact, each journaled
// before and after the effect.
func TestMaintainRecordsGapsAndFact(t *testing.T) {
	t.Parallel()

	fake := &fakeStore{contract: &store.QualityContractMessage{
		TargetGUID: "inst;db;;orders", TargetType: storepb.MetaType_TABLE,
		Settings: &storepb.QualityContractSettings{Intent: &storepb.QualityContractIntent{
			Criticality: storepb.QualityContractCriticality_QUALITY_CONTRACT_CRITICALITY_IMPORTANT,
		}},
	}}
	engine := NewEngine(fake)
	goal := maintainGoal()
	outcome, err := engine.Run(context.Background(), goal)
	require.NoError(t, err)
	require.Equal(t, storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED, outcome.Phase)
	require.Equal(t, "no-work", outcome.BlockedCode)
	require.Len(t, fake.investigations, 2, "volume and schema stability are uncovered")
	require.Len(t, fake.memories, 1)
	require.Equal(t, storepb.QualityMemoryKind_QUALITY_MEMORY_KIND_ASSET_FACT, fake.memories[0].Kind)
	require.Equal(t, storepb.QualityMemorySource_QUALITY_MEMORY_SOURCE_AGENT, fake.memories[0].Source)
	require.Equal(t, storepb.QualityMemoryStatus_QUALITY_MEMORY_STATUS_CONFIRMED, fake.memories[0].Status)
	require.Contains(t, fake.memories[0].Statement, "volume")
	require.Len(t, fake.steps, 6, "intent and result for two investigations and one fact")

	// The checkpoint returned by the turn makes the next turn skip the same work.
	goal.Checkpoint = outcome.Checkpoint
	investigationsBefore := len(fake.investigations)
	memoriesBefore := len(fake.memories)
	_, err = engine.Run(context.Background(), goal)
	require.NoError(t, err)
	require.Len(t, fake.investigations, investigationsBefore, "a recorded gap is not recorded twice")
	require.Len(t, fake.memories, memoriesBefore, "the fact is not written twice")
}

// An event goal turns the failed execution into one inconclusive investigation and
// completes.
func TestInvestigateEvent(t *testing.T) {
	t.Parallel()

	fake := &fakeStore{execution: &store.QualityExecutionMessage{
		ID: 7, TargetGUID: "inst;db;;orders", TargetType: storepb.MetaType_TABLE,
		Status: storepb.QualityStatus_QUALITY_STATUS_FAIL, FailedRows: 12,
	}}
	engine := NewEngine(fake)
	outcome, err := engine.Run(context.Background(), &store.QualityAgentGoalMessage{
		ID: 2, ResourceID: "g2", WorkspaceID: "ws1",
		Kind:       storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_INVESTIGATE_EVENT,
		TargetGUID: "inst;db;;orders", TargetType: storepb.MetaType_TABLE,
		TriggerRef: "qualityExecutions/7",
	})
	require.NoError(t, err)
	require.Equal(t, storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_COMPLETE, outcome.Phase)
	require.Len(t, fake.investigations, 1)
	require.Equal(t, storepb.QualityInvestigationVerdict_QUALITY_INVESTIGATION_VERDICT_INCONCLUSIVE, fake.investigations[0].Verdict)
	require.Contains(t, fake.investigations[0].Summary, "12 failed rows")
	require.Equal(t, "qualityExecutions/7", fake.investigations[0].Evidence.Evidence[0].Reference)
	require.Empty(t, fake.memories, "an event investigation writes no memory")
}

// A trigger reference that is not an execution is parked for a person, not guessed at.
func TestInvestigateEventBadReference(t *testing.T) {
	t.Parallel()

	engine := NewEngine(&fakeStore{})
	outcome, err := engine.Run(context.Background(), &store.QualityAgentGoalMessage{
		ID: 3, ResourceID: "g3", WorkspaceID: "ws1",
		Kind:       storepb.QualityAgentGoalKind_QUALITY_AGENT_GOAL_KIND_INVESTIGATE_EVENT,
		TriggerRef: "qualityRules/9",
	})
	require.NoError(t, err)
	require.Equal(t, storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_BLOCKED, outcome.Phase)
	require.Equal(t, "needs-human", outcome.BlockedCode)
}
