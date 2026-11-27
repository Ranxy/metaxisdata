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
	agent                *store.QualityAgentMessage
	claimed              []*store.QualityAgentSessionMessage
	tokensSpent          int64
	wakesSpent           int64
	pendingInputSeq      int64
	progress             []*store.QualityAgentSessionWakeResult
	states               []storepb.QualityAgentSessionState
	blockedCodes         []string
	nextWakes            []*time.Time
	releasedWakes        []*time.Time
	claimedLimit         int
	databases            []string
	caseAction           *time.Time
	enrollment           *store.QualityAgentEnrollmentMessage
	parkedEnrollment     string
	enrollmentNextReview time.Time
	blockedSessions      []*store.QualityAgentSessionMessage
}

func (f *fakeStore) ListQualityAgentSessionsByBlockedCode(context.Context, string, int) ([]*store.QualityAgentSessionMessage, error) {
	return f.blockedSessions, nil
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

func (*fakeStore) RecordQualityAgentReview(context.Context, string, time.Time, time.Time) (bool, error) {
	return true, nil
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

func (f *fakeStore) SumQualityAgentSessionTokensSince(context.Context, string, int64, time.Time) (int64, error) {
	return f.tokensSpent, nil
}

func (f *fakeStore) CountQualityAgentSessionWakesSince(context.Context, string, int64, time.Time) (int64, error) {
	return f.wakesSpent, nil
}

func (f *fakeStore) ListQualityAgentDatabases(context.Context, int64) ([]string, error) {
	return f.databases, nil
}

// caseAction is the earliest next look among the agent's open cases.
func (f *fakeStore) EarliestOpenQualityAgentCaseActionAt(context.Context, int64) (*time.Time, error) {
	return f.caseAction, nil
}

func (*fakeStore) DeferQualityAgentCases(context.Context, int64, time.Time, time.Time) (int64, error) {
	return 0, nil
}

func (*fakeStore) ClearQualityAgentSessionManualResume(context.Context, string) error {
	return nil
}

func (f *fakeStore) MaxQualityAgentInputSeqAfter(context.Context, int64, int64) (int64, error) {
	return f.pendingInputSeq, nil
}

func (f *fakeStore) GetQualityAgentEnrollmentByTarget(context.Context, string, int64, string) (*store.QualityAgentEnrollmentMessage, error) {
	return f.enrollment, nil
}

func (f *fakeStore) UpdateQualityAgentEnrollmentAfterTriage(_ context.Context, resourceID string, nextReviewAt time.Time) (*store.QualityAgentEnrollmentMessage, bool, error) {
	f.parkedEnrollment = resourceID
	f.enrollmentNextReview = nextReviewAt
	if f.enrollment == nil {
		return nil, false, nil
	}
	return f.enrollment, true, nil
}

// fakeEngine reports one fixed outcome.
type fakeEngine struct {
	outcome    agentrunner.Outcome
	err        error
	called     int
	skipModel  bool
	reviewed   int
	reviewWake *time.Time
}

func (f *fakeEngine) Wake(context.Context, *store.QualityAgentSessionMessage, agentrunner.Options) (agentrunner.Outcome, error) {
	f.called++
	return f.outcome, f.err
}

func (f *fakeEngine) NeedsModel(context.Context, *store.QualityAgentSessionMessage) (bool, error) {
	return !f.skipModel, nil
}

func (f *fakeEngine) Review(context.Context, *store.QualityAgentSessionMessage) agentrunner.Outcome {
	f.reviewed++
	return agentrunner.Outcome{NextWakeAt: f.reviewWake}
}

func testSession() *store.QualityAgentSessionMessage {
	return &store.QualityAgentSessionMessage{
		ID:          1,
		ResourceID:  "session-1",
		WorkspaceID: "ws",
		TargetGUID:  "target-1",
		DatabaseRef: "instances/i1/databases/d1",
		AgentID:     3,
		Kind:        1,
		Revision:    4,
		State:       storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED,
	}
}

func testSettings() dataquality.Settings {
	return dataquality.Settings{AgentRunnerEnabled: true}
}

func testRunner(stores *fakeStore, engine Waker) *Runner {
	runner := &Runner{
		store:        stores,
		engine:       engine,
		loadSettings: func(context.Context) (dataquality.Settings, error) { return testSettings(), nil },
		now:          func() time.Time { return time.Unix(10_000, 0).UTC() },
		tick:         time.Second,
	}
	stores.agent = &store.QualityAgentMessage{
		ID:                    3,
		State:                 store.QualityAgentStateActive,
		TokensPerHour:         1_000_000,
		WakesPerHour:          100,
		MaxConcurrentSessions: 2,
	}
	stores.databases = []string{"instances/i1/databases/d1"}
	return runner
}

// A finished triage parks the queue row until its review, so the target does not wake
// and re-triage every idle interval.
func TestHostParksTheEnrollmentAfterTriage(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{enrollment: &store.QualityAgentEnrollmentMessage{ResourceID: "enr-1"}}
	engine := &fakeEngine{outcome: agentrunner.Outcome{TriageComplete: true}}
	runner := testRunner(stores, engine)
	session := testSession()
	session.Mode = storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE

	runner.host(context.Background(), session)

	require.Equal(t, "enr-1", stores.parkedEnrollment)
	require.WithinDuration(t,
		time.Unix(10_000, 0).Add(dataquality.DefaultAgentRunnerDormantReviewDays*24*time.Hour),
		stores.enrollmentNextReview, time.Second)
}

// Before spending a wake the runner checks that the session may still run: the agent
// must be active, the database still in scope, and both hourly budgets unspent. A
// refusal parks the session with its stable code and consumes no wake.
func TestHostParksASessionItMayNotRun(t *testing.T) {
	t.Parallel()

	nextHour := time.Unix(10_000, 0).UTC().Truncate(time.Hour).Add(time.Hour)
	for _, tc := range []struct {
		name     string
		prepare  func(*fakeStore)
		wantCode string
		wantWake *time.Time
	}{
		{
			name:     "the agent is not active",
			prepare:  func(f *fakeStore) { f.agent.State = store.QualityAgentStatePaused },
			wantCode: "agent-disabled",
		},
		{
			name:     "the database left the agent's scope",
			prepare:  func(f *fakeStore) { f.databases = []string{"instances/other/databases/d9"} },
			wantCode: "out-of-scope",
		},
		{
			name:     "the hourly token budget is spent",
			prepare:  func(f *fakeStore) { f.tokensSpent = 2_000_000 },
			wantCode: "token-budget",
			wantWake: &nextHour,
		},
		{
			name:     "the hourly wake budget is spent",
			prepare:  func(f *fakeStore) { f.wakesSpent = int64(f.agent.WakesPerHour) },
			wantCode: "wake-budget",
			wantWake: &nextHour,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stores := &fakeStore{}
			engine := &fakeEngine{}
			runner := testRunner(stores, engine)
			tc.prepare(stores)

			runner.host(context.Background(), testSession())

			require.Zero(t, engine.called)
			require.Equal(t, []storepb.QualityAgentSessionState{storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED}, stores.states)
			require.Equal(t, []string{tc.wantCode}, stores.blockedCodes)
			if tc.wantWake != nil {
				require.WithinDuration(t, *tc.wantWake, *stores.nextWakes[0], time.Second)
				return
			}
			require.Nil(t, stores.nextWakes[0])
		})
	}
}

// A finished wake records its progress and releases the lease on the idle cadence,
// unless an event that arrived while the wake ran makes the session due again
// immediately.
func TestHostSchedulesTheNextWake(t *testing.T) {
	t.Parallel()

	now := time.Unix(10_000, 0).UTC()
	for _, tc := range []struct {
		name       string
		pendingSeq int64
		wantWake   time.Time
	}{
		{
			name:     "nothing new wakes on the idle cadence",
			wantWake: now.Add(dataquality.DefaultAgentRunnerIdleWakeSeconds * time.Second),
		},
		{
			name:       "an event that arrived during the wake makes it due now",
			pendingSeq: 9,
			wantWake:   now,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stores := &fakeStore{pendingInputSeq: tc.pendingSeq}
			engine := &fakeEngine{outcome: agentrunner.Outcome{ProcessedSeq: 4, EngineState: &storepb.QualityAgentSessionEngineState{}}}
			runner := testRunner(stores, engine)

			runner.host(context.Background(), testSession())

			require.Equal(t, 1, engine.called)
			require.Len(t, stores.progress, 1)
			require.Equal(t, int32(1), stores.progress[0].WakeCountIncrement)
			require.Empty(t, stores.states)
			require.Len(t, stores.releasedWakes, 1)
			require.WithinDuration(t, tc.wantWake, *stores.releasedWakes[0], time.Second)
		})
	}
}

// A case is due work: the next wake is the soonest of the engine's answer and the agent's
// open case, with a floor so a past-due case cannot make the three-second tick spin.
func TestHostSchedulesTheNextWakeFromAnOpenCase(t *testing.T) {
	t.Parallel()

	now := time.Unix(10_000, 0).UTC()
	for _, tc := range []struct {
		name       string
		caseAction *time.Time
		wantWake   time.Time
	}{
		{
			name:       "a due case wakes sooner than the idle cadence",
			caseAction: ptrTime(now.Add(5 * time.Minute)),
			wantWake:   now.Add(5 * time.Minute),
		},
		{
			name:       "a past-due case is clamped to the floor",
			caseAction: ptrTime(now.Add(-time.Hour)),
			wantWake:   now.Add(time.Minute),
		},
		{
			name:       "a case later than the idle cadence changes nothing",
			caseAction: ptrTime(now.Add(48 * time.Hour)),
			wantWake:   now.Add(dataquality.DefaultAgentRunnerIdleWakeSeconds * time.Second),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stores := &fakeStore{caseAction: tc.caseAction}
			runner := testRunner(stores, &fakeEngine{outcome: agentrunner.Outcome{ProcessedSeq: 4, EngineState: &storepb.QualityAgentSessionEngineState{}}})

			runner.host(context.Background(), testSession())

			require.Len(t, stores.releasedWakes, 1)
			require.WithinDuration(t, tc.wantWake, *stores.releasedWakes[0], time.Second)
		})
	}
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

	runner.host(context.Background(), testSession())

	require.Equal(t, []storepb.QualityAgentSessionState{storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED}, stores.states)
	require.Equal(t, []string{"model-reported"}, stores.blockedCodes)
	require.Nil(t, stores.nextWakes[0])
	require.Empty(t, stores.releasedWakes)
}

// The lease sweep's restart-limit parks are escalated to one owner attention per
// session through the existing sink.
func TestRunOnceEscalatesRestartLimitSessions(t *testing.T) {
	t.Parallel()

	blocked := &store.QualityAgentSessionMessage{
		ID:            2,
		ResourceID:    "session-2",
		BlockedCode:   "restart-limit",
		BlockedReason: "the process restarted while this session was running too many times",
	}
	stores := &fakeStore{blockedSessions: []*store.QualityAgentSessionMessage{blocked}}
	runner := testRunner(stores, &fakeEngine{})
	var escalated []string
	runner.SetAttentionSink(func(_ context.Context, session *store.QualityAgentSessionMessage, code, _ string) {
		escalated = append(escalated, session.ResourceID+":"+code)
	})

	runner.runOnce(context.Background())

	require.Equal(t, []string{"session-2:restart-limit"}, escalated)
}

// A session skipped because its agent is at the concurrent-session budget is handed
// back to the queue instead of being parked, so a claim batch larger than the budget
// cannot livelock every session in the batch.
func TestHostRequeuesASessionOverTheAgentConcurrencyBudget(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{}
	engine := &fakeEngine{}
	runner := testRunner(stores, engine)
	stores.agent.MaxConcurrentSessions = 1
	runner.agentSlots = map[int64]int{stores.agent.ID: 1}

	runner.host(context.Background(), testSession())

	require.Empty(t, stores.blockedCodes)
	require.Zero(t, engine.called)
	require.Len(t, stores.releasedWakes, 1)
	require.NotNil(t, stores.releasedWakes[0])
}

func TestHostRunsADeterministicReviewWithoutAWake(t *testing.T) {
	t.Parallel()

	stores := &fakeStore{}
	future := time.Unix(20_000, 0).UTC()
	engine := &fakeEngine{skipModel: true, reviewWake: &future}
	runner := testRunner(stores, engine)
	runner.host(context.Background(), testSession())
	require.Zero(t, engine.called, "a no-op review must not call the model wake")
	require.Equal(t, 1, engine.reviewed)
}
