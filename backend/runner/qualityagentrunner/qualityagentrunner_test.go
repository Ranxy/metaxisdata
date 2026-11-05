package qualityagentrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality/agentrunner"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

type fakeGoalStore struct {
	reset       int64
	resetCalls  int
	claimed     []*store.QualityAgentGoalMessage
	claimLimit  int
	steps       []*store.QualityAgentGoalStepMessage
	released    map[string]time.Time
	releaseMiss bool
	blocked     map[string]string
}

func (f *fakeGoalStore) ResetStaleQualityAgentGoalLeases(_ context.Context, _ time.Time, _ int32) (int64, error) {
	f.resetCalls++
	return f.reset, nil
}

func (f *fakeGoalStore) ClaimDueQualityAgentGoals(_ context.Context, _ time.Time, _ time.Duration, limit int) ([]*store.QualityAgentGoalMessage, error) {
	f.claimLimit = limit
	return f.claimed, nil
}

func (f *fakeGoalStore) AppendQualityAgentGoalStep(_ context.Context, step *store.QualityAgentGoalStepMessage) (*store.QualityAgentGoalStepMessage, error) {
	f.steps = append(f.steps, step)
	return step, nil
}

func (*fakeGoalStore) UpdateQualityAgentGoalProgress(_ context.Context, resourceID string, roundsStarted int32, _ int64, _ *storepb.QualityAgentGoalCheckpoint) (*store.QualityAgentGoalMessage, error) {
	return &store.QualityAgentGoalMessage{ResourceID: resourceID, Revision: 2, RoundsStarted: roundsStarted}, nil
}

func (f *fakeGoalStore) UpdateQualityAgentGoalState(_ context.Context, resourceID string, _ int32, phase storepb.QualityAgentGoalPhase, blockedCode, blockedReason string, _ *time.Time) (*store.QualityAgentGoalMessage, bool, error) {
	if f.blocked == nil {
		f.blocked = map[string]string{}
	}
	f.blocked[resourceID] = blockedCode + ": " + blockedReason
	return &store.QualityAgentGoalMessage{ResourceID: resourceID, Phase: phase}, true, nil
}

func (*fakeGoalStore) GetQualityAgentByID(_ context.Context, id int64) (*store.QualityAgentMessage, error) {
	return &store.QualityAgentMessage{ID: id, Enabled: true}, nil
}

func (f *fakeGoalStore) ReleaseQualityAgentGoalLease(_ context.Context, resourceID string, nextWakeAt *time.Time) (bool, error) {
	if f.releaseMiss {
		return false, nil
	}
	if f.released == nil {
		f.released = map[string]time.Time{}
	}
	if nextWakeAt != nil {
		f.released[resourceID] = *nextWakeAt
	}
	return true, nil
}

type fakeEngine struct {
	outcome agentrunner.Outcome
	err     error
	calls   int
}

func (f *fakeEngine) Run(_ context.Context, _ *store.QualityAgentGoalMessage) (agentrunner.Outcome, error) {
	f.calls++
	return f.outcome, f.err
}

func newTestRunner(store GoalStore, settings dataquality.Settings, now time.Time) *Runner {
	return &Runner{
		store: store,
		engine: &fakeEngine{outcome: agentrunner.Outcome{
			Phase:      storepb.QualityAgentGoalPhase_QUALITY_AGENT_GOAL_PHASE_ACTIVE,
			NextWakeAt: func() *time.Time { value := now.Add(time.Hour); return &value }(),
		}},
		loadSettings: func(context.Context) (dataquality.Settings, error) {
			return settings, nil
		},
		now:  func() time.Time { return now },
		tick: tickInterval,
	}
}

// The switch is the first gate: a disabled runner claims nothing, and the stale-lease
// sweep still runs once after a restart.
func TestRunnerRespectsTheSwitch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeGoalStore{reset: 2, claimed: []*store.QualityAgentGoalMessage{{
		ID: 1, ResourceID: "g1", DatabaseRef: "instances/i1/databases/d1", AgentID: 1,
	}}}
	runner := newTestRunner(store, dataquality.Settings{AgentRunnerEnabled: false}, now)
	runner.runOnce(context.Background())

	require.Equal(t, 1, store.resetCalls, "the restart sweep runs once")
	require.Empty(t, store.steps, "a disabled runner does not host a goal")

	runner.runOnce(context.Background())
	require.Equal(t, 1, store.resetCalls, "the sweep runs once per process")
	require.Empty(t, store.steps)
}

// An enabled runner claims up to the configured concurrency and journals each goal.
func TestRunnerHostsClaimedGoals(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeGoalStore{claimed: []*store.QualityAgentGoalMessage{
		{ID: 1, ResourceID: "g1", WorkspaceID: "ws1", DatabaseRef: "instances/i1/databases/d1", AgentID: 1},
		{ID: 2, ResourceID: "g2", WorkspaceID: "ws1", DatabaseRef: "instances/i1/databases/d1", AgentID: 1},
	}}
	runner := newTestRunner(store, dataquality.Settings{
		AgentRunnerEnabled:            true,
		AgentRunnerDatabases:          []string{"instances/i1/databases/d1"},
		AgentRunnerMaxConcurrentGoals: 2,
	}, now)
	runner.runOnce(context.Background())

	require.Equal(t, 2, store.claimLimit)
	require.Len(t, store.steps, 4, "each goal journals its turn reservation and its decision")
	for i, step := range store.steps {
		want := storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_TURN_RESERVED
		if i%2 == 1 {
			want = storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_GOAL_CHANGE
		}
		require.Equal(t, want, step.Kind)
		require.Equal(t, store.claimed[i/2].ID, step.GoalID)
		require.Equal(t, store.claimed[i/2].WorkspaceID, step.WorkspaceID)
	}
	require.Equal(t, now.Add(time.Hour), store.released["g1"])
	require.Equal(t, now.Add(time.Hour), store.released["g2"])
}

// A goal paused while the turn was being hosted is not rescheduled.
func TestRunnerLeavesNonLeasedGoalAlone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeGoalStore{
		claimed:     []*store.QualityAgentGoalMessage{{ID: 1, ResourceID: "g1", DatabaseRef: "instances/i1/databases/d1", AgentID: 1}},
		releaseMiss: true,
	}
	runner := newTestRunner(store, dataquality.Settings{
		AgentRunnerEnabled:   true,
		AgentRunnerDatabases: []string{"instances/i1/databases/d1"},
	}, now)
	runner.runOnce(context.Background())

	require.Len(t, store.steps, 2, "the claim and the decision are journaled even if the goal was paused")
}

// A goal whose database left the allowlist is parked for a person instead of being
// hosted; the run-time scope check is the second half of the creation check.
func TestRunnerBlocksOutOfScopeGoal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeGoalStore{claimed: []*store.QualityAgentGoalMessage{
		{ID: 1, ResourceID: "g1", DatabaseRef: "instances/i1/databases/d1", AgentID: 1, Revision: 4},
	}}
	runner := newTestRunner(store, dataquality.Settings{
		AgentRunnerEnabled:   true,
		AgentRunnerDatabases: []string{"instances/i1/databases/other"},
	}, now)
	runner.runOnce(context.Background())

	require.Equal(t, "needs-human: the goal's database is no longer in the runner's allowlist", store.blocked["g1"])
	require.Len(t, store.steps, 1, "the block is journaled")
	require.Equal(t, storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_STOP, store.steps[0].Kind)
}

// The round limit is the runner's first gate: a goal at its cap is parked, not run.
func TestRunnerStopsAtRoundLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeGoalStore{claimed: []*store.QualityAgentGoalMessage{{
		ID: 1, ResourceID: "g1", DatabaseRef: "instances/i1/databases/d1", AgentID: 1,
		RoundsStarted: 8, MaxRounds: 8,
	}}}
	engine := &fakeEngine{}
	runner := newTestRunner(store, dataquality.Settings{
		AgentRunnerEnabled:   true,
		AgentRunnerDatabases: []string{"instances/i1/databases/d1"},
	}, now)
	runner.engine = engine
	runner.runOnce(context.Background())

	require.Equal(t, 0, engine.calls)
	require.Equal(t, "round-limit: the goal reached its round limit", store.blocked["g1"])
}
