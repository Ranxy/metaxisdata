package qualitysessionrunner

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

// fakeStore is the in-memory slice of the store the runner tests need.
type fakeStore struct {
	agent           *store.QualityAgentMessage
	claimed         []*store.QualityAgentSessionMessage
	tokensSpent     int64
	wakesSpent      int64
	pendingInputSeq int64
	progress        []*store.QualityAgentSessionWakeResult
	states          []storepb.QualityAgentSessionState
	blockedCodes    []string
	nextWakes       []*time.Time
	releasedWakes   []*time.Time
	claimedLimit    int
}

func (*fakeStore) ResetStaleQualityAgentSessionLeases(context.Context, time.Time, int32) (int64, error) {
	return 0, nil
}

func (f *fakeStore) ClaimDueQualityAgentSessions(_ context.Context, _ time.Time, _ time.Duration, limit int) ([]*store.QualityAgentSessionMessage, error) {
	f.claimedLimit = limit
	return f.claimed, nil
}

func (f *fakeStore) GetQualityAgentByID(context.Context, int64) (*store.QualityAgentMessage, error) {
	return f.agent, nil
}

func (f *fakeStore) UpdateQualityAgentSessionProgress(_ context.Context, _ string, result *store.QualityAgentSessionWakeResult) (*store.QualityAgentSessionMessage, error) {
	f.progress = append(f.progress, result)
	return &store.QualityAgentSessionMessage{ResourceID: "session-1", Revision: 5}, nil
}

func (f *fakeStore) UpdateQualityAgentSessionState(_ context.Context, _ string, _ int32, state storepb.QualityAgentSessionState, code, _ string, nextWake *time.Time) (*store.QualityAgentSessionMessage, bool, error) {
	f.states = append(f.states, state)
	f.blockedCodes = append(f.blockedCodes, code)
	f.nextWakes = append(f.nextWakes, nextWake)
	return &store.QualityAgentSessionMessage{ResourceID: "session-1"}, true, nil
}

func (f *fakeStore) ReleaseQualityAgentSessionLease(_ context.Context, _ string, nextWake *time.Time) (bool, error) {
	f.releasedWakes = append(f.releasedWakes, nextWake)
	return true, nil
}

func (f *fakeStore) SumQualityAgentSessionTokensSince(context.Context, string, time.Time) (int64, error) {
	return f.tokensSpent, nil
}

func (f *fakeStore) CountQualityAgentSessionWakesSince(context.Context, string, time.Time) (int64, error) {
	return f.wakesSpent, nil
}

func (f *fakeStore) MaxQualityAgentInputSeqAfter(context.Context, int64, int64) (int64, error) {
	return f.pendingInputSeq, nil
}

// fakeEngine reports one fixed outcome.
type fakeEngine struct {
	outcome agentrunner.Outcome
	err     error
	called  int
}

func (f *fakeEngine) Wake(context.Context, *store.QualityAgentSessionMessage, agentrunner.Options) (agentrunner.Outcome, error) {
	f.called++
	return f.outcome, f.err
}

func testSession() *store.QualityAgentSessionMessage {
	return &store.QualityAgentSessionMessage{
		ID:          1,
		ResourceID:  "session-1",
		WorkspaceID: "ws",
		TargetGUID:  "target-1",
		DatabaseRef: "instances/i1/databases/d1",
		AgentID:     3,
		Revision:    4,
		State:       storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED,
	}
}

func testSettings() dataquality.Settings {
	return dataquality.Settings{
		AgentRunnerEnabled:                 true,
		AgentAutonomyEnabled:               true,
		AgentRunnerDatabases:               []string{"instances/i1/databases/d1"},
		AgentRunnerTokensPerHour:           1_000_000,
		AgentRunnerWakesPerHour:            100,
		AgentRunnerMaxConcurrentSessions:   2,
		AgentRunnerIdleWakeSeconds:         3_600,
		AgentRunnerCompactionTriggerTokens: dataquality.DefaultAgentRunnerCompactionTriggerTokens,
	}
}

func testRunner(stores *fakeStore, engine Waker) *Runner {
	runner := &Runner{
		store:        stores,
		engine:       engine,
		loadSettings: func(context.Context) (dataquality.Settings, error) { return testSettings(), nil },
		now:          func() time.Time { return time.Unix(10_000, 0).UTC() },
		tick:         time.Second,
	}
	stores.agent = &store.QualityAgentMessage{ID: 3, Enabled: true}
	return runner
}

// An admin who turns autonomy off does not spend model tokens: the session parks on a
// retryable blocker instead of waking.
func TestHostBlocksWhenAgentAutonomyIsOff(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{}
	engine := &fakeEngine{}
	runner := testRunner(stores, engine)
	settings := testSettings()
	settings.AgentAutonomyEnabled = false

	runner.host(context.Background(), settings, testSession())

	require.Zero(t, engine.called)
	require.Equal(t, []storepb.QualityAgentSessionState{storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED}, stores.states)
	require.Equal(t, []string{"autonomy-off"}, stores.blockedCodes)
	require.NotNil(t, stores.nextWakes[0])
	require.WithinDuration(t, time.Unix(10_000, 0).Add(5*time.Minute), *stores.nextWakes[0], time.Second)
}

// The hourly token budget stops model spend before the wake starts.
func TestHostBlocksOnTheTokenBudget(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{tokensSpent: 2_000_000}
	engine := &fakeEngine{}
	runner := testRunner(stores, engine)

	runner.host(context.Background(), testSettings(), testSession())

	require.Zero(t, engine.called)
	require.Equal(t, []string{"token-budget"}, stores.blockedCodes)
	require.WithinDuration(t, time.Unix(10_000, 0).Truncate(time.Hour).Add(time.Hour), *stores.nextWakes[0], time.Second)
}

// A normal wake records its progress, then releases the lease on the idle cadence.
func TestHostSchedulesTheIdleWake(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{}
	engine := &fakeEngine{outcome: agentrunner.Outcome{ProcessedSeq: 4, TokensUsed: 120, ContextTokens: 500, EngineState: &storepb.QualityAgentSessionEngineState{}}}
	runner := testRunner(stores, engine)

	runner.host(context.Background(), testSettings(), testSession())

	require.Equal(t, 1, engine.called)
	require.Len(t, stores.progress, 1)
	require.Equal(t, int32(1), stores.progress[0].WakeCountIncrement)
	require.Equal(t, int64(120), stores.progress[0].TokensUsed)
	require.Empty(t, stores.states)
	require.Len(t, stores.releasedWakes, 1)
	require.WithinDuration(t, time.Unix(10_000, 0).Add(time.Hour), *stores.releasedWakes[0], time.Second)
}

// An event that arrived while the wake ran is visible as a newer message seq, so the
// session becomes due again immediately instead of sleeping for the idle cadence.
func TestHostWakesAgainWhenAnEventArrived(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{pendingInputSeq: 9}
	engine := &fakeEngine{outcome: agentrunner.Outcome{ProcessedSeq: 4, EngineState: &storepb.QualityAgentSessionEngineState{}}}
	runner := testRunner(stores, engine)

	runner.host(context.Background(), testSettings(), testSession())

	require.Len(t, stores.releasedWakes, 1)
	require.WithinDuration(t, time.Unix(10_000, 0), *stores.releasedWakes[0], time.Second)
}

// A blocked outcome parks the session with its stable code and no next wake.
func TestHostParksABlockedSession(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{}
	engine := &fakeEngine{outcome: agentrunner.Outcome{
		Blocked:       true,
		BlockedCode:   "model-reported",
		BlockedReason: "the source table is empty",
		ProcessedSeq:  3,
		EngineState:   &storepb.QualityAgentSessionEngineState{},
	}}
	runner := testRunner(stores, engine)

	runner.host(context.Background(), testSettings(), testSession())

	require.Equal(t, []storepb.QualityAgentSessionState{storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED}, stores.states)
	require.Equal(t, []string{"model-reported"}, stores.blockedCodes)
	require.Nil(t, stores.nextWakes[0])
	require.Empty(t, stores.releasedWakes)
}

// A removed database parks the session instead of running a wake the allowlist forbids.
func TestHostBlocksASessionOutsideTheAllowlist(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{}
	engine := &fakeEngine{}
	runner := testRunner(stores, engine)
	settings := testSettings()
	settings.AgentRunnerDatabases = []string{"instances/other/databases/d9"}

	runner.host(context.Background(), settings, testSession())

	require.Zero(t, engine.called)
	require.Equal(t, []string{"needs-human"}, stores.blockedCodes)
	require.Nil(t, stores.nextWakes[0])
}
