package qualityagentrunner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
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

func newTestRunner(store GoalStore, settings dataquality.Settings, now time.Time) *Runner {
	return &Runner{
		store: store,
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
	store := &fakeGoalStore{reset: 2, claimed: []*store.QualityAgentGoalMessage{{ID: 1, ResourceID: "g1"}}}
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
		{ID: 1, ResourceID: "g1", WorkspaceID: "ws1"},
		{ID: 2, ResourceID: "g2", WorkspaceID: "ws1"},
	}}
	runner := newTestRunner(store, dataquality.Settings{
		AgentRunnerEnabled:            true,
		AgentRunnerMaxConcurrentGoals: 2,
	}, now)
	runner.runOnce(context.Background())

	require.Equal(t, 2, store.claimLimit)
	require.Len(t, store.steps, 2)
	for i, step := range store.steps {
		require.Equal(t, storepb.QualityAgentGoalStepKind_QUALITY_AGENT_GOAL_STEP_KIND_GOAL_CHANGE, step.Kind)
		require.Equal(t, store.claimed[i].ID, step.GoalID)
		require.Equal(t, store.claimed[i].WorkspaceID, step.WorkspaceID)
	}
	require.Equal(t, now.Add(placeholderWake), store.released["g1"])
	require.Equal(t, now.Add(placeholderWake), store.released["g2"])
}

// A goal paused while the turn was being hosted is not rescheduled.
func TestRunnerLeavesNonLeasedGoalAlone(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &fakeGoalStore{
		claimed:     []*store.QualityAgentGoalMessage{{ID: 1, ResourceID: "g1"}},
		releaseMiss: true,
	}
	runner := newTestRunner(store, dataquality.Settings{AgentRunnerEnabled: true}, now)
	runner.runOnce(context.Background())

	require.Len(t, store.steps, 1, "the claim is journaled even if the goal was paused")
}
