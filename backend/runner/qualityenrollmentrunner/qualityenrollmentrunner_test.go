package qualityenrollmentrunner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestComputePriorityOrdersByValue(t *testing.T) {
	t.Parallel()

	monitored, reason := computePriority(prioritySignals{monitored: true, failing: true, consumers: 12, criticality: dataquality.CriticalityCritical, schemaChanged: true})
	require.Equal(t, int32(1000), monitored, "the score is capped")
	require.Contains(t, reason, "rules")
	require.Contains(t, reason, "failing")
	require.Contains(t, reason, "critical")

	unmonitored, reason := computePriority(prioritySignals{})
	require.Equal(t, int32(0), unmonitored)
	require.Equal(t, "unmonitored", reason)

	capped, _ := computePriority(prioritySignals{
		monitored: true, failing: true, consumers: 100,
		criticality: dataquality.CriticalityCritical, schemaChanged: true,
		recentlyDiscovered: true, columnCount: 1000,
	})
	require.Equal(t, int32(1000), capped, "the score is capped")
}

// A published contract is coverage: the target starts in MONITOR instead of spending
// a triage call and waiting weeks to be promoted.
func TestPublishedContractIsCoverageForPriority(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = testAgent()
	fake.enrollments = []*store.QualityAgentEnrollmentMessage{{
		ResourceID: "enr-1", TargetGUID: "i;d;public;t", TargetType: storepb.MetaType_TABLE,
		DatabaseRef: "instances/i/databases/d",
	}}
	fake.contracts = []*store.QualityContractMessage{{
		TargetGUID: "i;d;public;t", TargetType: storepb.MetaType_TABLE, PublishedVersion: 1,
	}}
	runner := testRunner(fake)

	runner.recomputePriorities(context.Background(), "ws", 7,
		"instances/i/databases/d", "i", "d", time.Unix(10_000, 0).UTC())

	require.Len(t, fake.priorityUpdates, 1)
	require.Equal(t, storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR, fake.priorityUpdates[0].Mode)
}

// Materialization creates a subagent for every pending target, without an existence
// budget: a covered target becomes an active monitor, an uncovered one waits in the
// triage queue with its session already created.
func TestMaterializeCreatesASubagentForEveryPendingTarget(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = testAgent()
	fake.unmaterialized = []*store.QualityAgentEnrollmentMessage{
		{ResourceID: "enr-monitor", TargetGUID: "i;d;public;covered", TargetType: storepb.MetaType_TABLE,
			DatabaseRef: "instances/i/databases/d", Mode: storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR},
		{ResourceID: "enr-triage", TargetGUID: "i;d;public;uncovered", TargetType: storepb.MetaType_TABLE,
			DatabaseRef: "instances/i/databases/d", Mode: storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE},
	}
	runner := testRunner(fake)

	runner.materialize(context.Background(), "ws", testActiveAgent(fake), time.Unix(10_000, 0).UTC())

	require.Len(t, fake.created, 2, "every pending target gets its fixed subagent")
	for _, created := range fake.created {
		require.Equal(t, storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR, created.Mode,
			"a waiting triage starts in monitor so it cannot model wake before admission")
		require.NotNil(t, created.NextWakeAt)
	}
	require.ElementsMatch(t, []string{"enr-monitor", "enr-triage"}, fake.attached)
	require.Equal(t, []string{"enr-monitor"}, fake.linked, "only the covered target is admitted immediately")
}

// A pending target outside the agent's current scope is not materialized.
func TestMaterializeSkipsAnOutOfScopeTarget(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = testAgent()
	fake.unmaterialized = []*store.QualityAgentEnrollmentMessage{{
		ResourceID: "enr-1", TargetGUID: "i;d;public;t", DatabaseRef: "instances/other/databases/d",
	}}
	runner := testRunner(fake)

	runner.materialize(context.Background(), "ws", testActiveAgent(fake), time.Unix(10_000, 0).UTC())

	require.Empty(t, fake.created)
	require.Equal(t, []string{"out-of-scope"}, fake.blockedReasonValues)
}

// Triage has its own concurrency and hourly rate. A missing model or capability parks
// the target with a reason instead of creating an unbounded model queue.
func TestAdmitTriageRespectsItsOwnBudget(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		setup      func(*fakeStore, *Runner)
		wantReason string
	}{
		{
			name: "without a model",
			setup: func(_ *fakeStore, runner *Runner) {
				runner.llmConfigured = func(context.Context, *store.QualityAgentMessage) bool { return false }
			},
			wantReason: "no-model",
		},
		{
			name: "without the suggest capability",
			setup: func(fake *fakeStore, _ *Runner) {
				fake.agent.Capabilities = []string{"patrol"}
			},
			wantReason: "capability-suggest-disabled",
		},
		{
			name: "at the concurrent triage budget",
			setup: func(fake *fakeStore, _ *Runner) {
				fake.activeTriage = dataquality.DefaultAgentRunnerMaxTriageSessions
			},
			wantReason: "triage-limit",
		},
		{
			name: "at the hourly triage rate",
			setup: func(fake *fakeStore, _ *Runner) {
				fake.triageStartsLastHour = dataquality.DefaultAgentRunnerMaxNewTriageSessionsPerHour
			},
			wantReason: "triage-limit",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeStore()
			fake.agent = testAgent()
			fake.pendingTriage = []*store.QualityAgentEnrollmentMessage{{ResourceID: "enr-1", TargetGUID: "i;d;public;t"}}
			runner := testRunner(fake)
			tc.setup(fake, runner)

			runner.admitTriage(context.Background(), "ws", testActiveAgent(fake), time.Unix(10_000, 0).UTC())

			require.Empty(t, fake.linked)
			require.Contains(t, fake.blockedReasonValues, tc.wantReason)
		})
	}
}

// Admitting a triage target switches its session out of the waiting monitor mode,
// links the queue row and makes the wake due now.
func TestAdmitTriageStartsTheFirstAnalysis(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = testAgent()
	fake.pendingTriage = []*store.QualityAgentEnrollmentMessage{{
		ResourceID: "enr-1", TargetGUID: "i;d;public;t", DatabaseRef: "instances/i/databases/d",
		Mode: storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE,
	}}
	fake.sessionsByTarget["i;d;public;t"] = &store.QualityAgentSessionMessage{
		ID: 3, ResourceID: "session-3", TargetGUID: "i;d;public;t",
	}
	runner := testRunner(fake)

	runner.admitTriage(context.Background(), "ws", testActiveAgent(fake), time.Unix(10_000, 0).UTC())

	require.Equal(t, []string{"enr-1"}, fake.linked)
	require.Equal(t, []string{"session-3"}, fake.sessionModesSet)
	require.Equal(t, []string{"session-3"}, fake.advancedWakes)
}

func TestRequeueDueReviews(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.dueDormant = []*store.QualityAgentEnrollmentMessage{
		{ResourceID: "enr-1"}, {ResourceID: "enr-2"},
	}
	runner := testRunner(fake)
	runner.requeueDueReviews(context.Background(), "ws", 7, time.Unix(20_000, 0).UTC())

	require.Equal(t, []string{"enr-1", "enr-2"}, fake.requeued)
}

func TestPromoteCoveredWakesSession(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.sessionsByTarget = map[string]*store.QualityAgentSessionMessage{
		"i;d;public;t": {
			ResourceID: "session-1",
			TargetGUID: "i;d;public;t",
			State:      storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_ENABLED,
			Revision:   3,
		},
	}
	runner := testRunner(fake)
	enrollment := &store.QualityAgentEnrollmentMessage{ResourceID: "enr-1", TargetGUID: "i;d;public;t"}

	require.NoError(t, runner.promote(context.Background(), "ws", enrollment, time.Unix(10_000, 0).UTC()))
	require.Equal(t, []string{"session-1"}, fake.sessionModesSet)
	require.Equal(t, []string{"session-1"}, fake.advancedWakes)
	require.Equal(t, []string{"enr-1"}, fake.promoted)
}

func TestRecoverBlockedReenablesScopeSession(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = testAgent()
	fake.recoverable = []*store.QualityAgentEnrollmentMessage{{
		ResourceID: "enr-1", TargetGUID: "i;d;public;t", DatabaseRef: "instances/i/databases/d",
	}}
	fake.sessionsByTarget = map[string]*store.QualityAgentSessionMessage{
		"i;d;public;t": {
			ResourceID:  "session-1",
			TargetGUID:  "i;d;public;t",
			DatabaseRef: "instances/i/databases/d",
			State:       storepb.QualityAgentSessionState_QUALITY_AGENT_SESSION_STATE_BLOCKED,
			BlockedCode: "out-of-scope",
			Revision:    2,
		},
	}
	runner := testRunner(fake)
	runner.recoverBlocked(context.Background(), "ws", testActiveAgent(fake))

	require.Equal(t, []string{"session-1"}, fake.enabledSessions)
}

func TestRunOnceReconcilesChangedScope(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	runner := testRunner(fake)

	fake.agents = []*store.QualityAgentMessage{testAgent()}
	fake.scopes = map[int64][]string{7: {"instances/i/databases/d"}}

	runner.runOnce(context.Background())
	require.Equal(t, 1, fake.reconcileCalls)

	runner.runOnce(context.Background())
	require.Equal(t, 1, fake.reconcileCalls, "an unchanged scope is not reconciled again")

	fake.scopes[7] = []string{"instances/other/databases/d"}
	runner.runOnce(context.Background())
	require.Equal(t, 2, fake.reconcileCalls, "a changed scope is reconciled again")
}

func TestScanDatabaseUpsertsAndMarksGone(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.digest = []*store.MetaRegistryResource{
		{GUID: "i;d;public;a", ObjectType: storepb.MetaType_TABLE},
		{GUID: "i;d;public;b", ObjectType: storepb.MetaType_VIEW},
	}
	fake.enrollments = []*store.QualityAgentEnrollmentMessage{
		{ResourceID: "enr-a", TargetGUID: "i;d;public;a", TargetType: storepb.MetaType_TABLE},
		{ResourceID: "enr-b", TargetGUID: "i;d;public;b", TargetType: storepb.MetaType_VIEW},
	}
	runner := testRunner(fake)
	runner.scanInterval = 0

	require.NoError(t, runner.scanDatabase(context.Background(), "ws", 7, "instances/i/databases/d", time.Unix(10_000, 0).UTC()))
	require.Len(t, fake.upserted, 2)
	require.Equal(t, "instances/i/databases/d", fake.upserted[0].DatabaseRef)
	require.Equal(t, []string{"instances/i/databases/d"}, fake.markedGone)
	require.True(t, fake.prioritiesUpdated)
}

// testAgent is one ACTIVE agent with its merged runtime configuration and budgets.
func testAgent() *store.QualityAgentMessage {
	return &store.QualityAgentMessage{
		ID:                    7,
		State:                 store.QualityAgentStateActive,
		Capabilities:          []string{"suggest", "patrol"},
		WakesPerHour:          100,
		MaxConcurrentSessions: 2,
		MaxNewSessionsPerHour: 10,
		MaxActiveSessions:     100,
	}
}

func testActiveAgent(fake *fakeStore) *activeAgent {
	return &activeAgent{
		agent:     fake.agent,
		databases: []string{"instances/i/databases/d"},
	}
}

func testRunner(stores Store) *Runner {
	return &Runner{
		store: stores,
		loadSettings: func(context.Context) (dataquality.Settings, error) {
			return testSettings(), nil
		},
		llmConfigured: func(context.Context, *store.QualityAgentMessage) bool { return true },
		now:           func() time.Time { return time.Unix(10_000, 0).UTC() },
		tick:          time.Second,
		scanInterval:  time.Hour,
		lastScan:      map[string]time.Time{},
	}
}

func testSettings() dataquality.Settings {
	return dataquality.Settings{AgentRunnerEnabled: true}
}

// fakeStore is a configurable in-memory Store. Only the calls each test inspects are
// recorded; everything else returns an empty success.
type fakeStore struct {
	workspaceID string

	digest             []*store.MetaRegistryResource
	agent              *store.QualityAgentMessage
	agents             []*store.QualityAgentMessage
	scopes             map[int64][]string
	enrollments        []*store.QualityAgentEnrollmentMessage
	unmaterialized     []*store.QualityAgentEnrollmentMessage
	pendingTriage      []*store.QualityAgentEnrollmentMessage
	dueDormant         []*store.QualityAgentEnrollmentMessage
	covered            []*store.QualityAgentEnrollmentMessage
	recoverable        []*store.QualityAgentEnrollmentMessage
	enrollmentByTarget map[string]*store.QualityAgentEnrollmentMessage

	sessionsByTarget     map[string]*store.QualityAgentSessionMessage
	created              []*store.CreateQualityAgentSessionInput
	activeTriage         int
	triageStartsLastHour int
	wakesLastHour        int64
	databaseTime         time.Time
	contracts            []*store.QualityContractMessage
	rules                []*store.QualityRuleMessage

	upserted            []*store.QualityAgentEnrollmentUpsert
	markedGone          []string
	prioritiesUpdated   bool
	priorityUpdates     []*store.QualityAgentEnrollmentPriorityUpdate
	reconcileCalls      int
	requeued            []string
	attached            []string
	linked              []string
	promoted            []string
	sessionModesSet     []string
	advancedWakes       []string
	enabledSessions     []string
	blockedReasonValues []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		workspaceID:        "ws",
		enrollmentByTarget: map[string]*store.QualityAgentEnrollmentMessage{},
		sessionsByTarget:   map[string]*store.QualityAgentSessionMessage{},
	}
}

func (f *fakeStore) GetWorkspaceID(context.Context) (string, error) { return f.workspaceID, nil }

func (f *fakeStore) UpsertQualityAgentEnrollments(_ context.Context, _ string, updates []*store.QualityAgentEnrollmentUpsert) (int64, error) {
	f.upserted = append(f.upserted, updates...)
	return int64(len(updates)), nil
}
func (f *fakeStore) MarkQualityAgentEnrollmentsGone(_ context.Context, _ string, _ int64, databaseRef string, _ time.Time) (int64, error) {
	f.markedGone = append(f.markedGone, databaseRef)
	return 1, nil
}
func (f *fakeStore) ReconcileQualityAgentEnrollmentScope(context.Context, string, int64, []string) (int64, int64, error) {
	f.reconcileCalls++
	return 0, 0, nil
}
func (f *fakeStore) ListQualityAgentEnrollments(context.Context, *store.FindQualityAgentEnrollmentMessage) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.enrollments, nil
}
func (f *fakeStore) ListUnmaterializedQualityAgentEnrollments(context.Context, string, int64, int) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.unmaterialized, nil
}
func (f *fakeStore) AttachQualityAgentEnrollmentSession(_ context.Context, resourceID string, _ int64, _ storepb.QualityAgentSessionMode) (*store.QualityAgentEnrollmentMessage, bool, error) {
	f.attached = append(f.attached, resourceID)
	return &store.QualityAgentEnrollmentMessage{ResourceID: resourceID}, true, nil
}
func (f *fakeStore) ListPendingTriageQualityAgentEnrollments(context.Context, string, int64, int) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.pendingTriage, nil
}
func (f *fakeStore) CountActiveTriageQualityAgentEnrollments(context.Context, string, int64) (int, error) {
	return f.activeTriage, nil
}
func (f *fakeStore) CountQualityAgentTriageStartsSince(context.Context, int64, time.Time) (int, error) {
	return f.triageStartsLastHour, nil
}
func (f *fakeStore) ListDueDormantQualityAgentEnrollments(context.Context, string, int64, time.Time, int) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.dueDormant, nil
}
func (f *fakeStore) ListCoveredDormantQualityAgentEnrollments(context.Context, string, int64, int) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.covered, nil
}
func (f *fakeStore) ListRecoverableQualityAgentEnrollments(context.Context, string, int64, int) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.recoverable, nil
}
func (f *fakeStore) RequeueQualityAgentEnrollments(_ context.Context, resourceIDs []string, _ string) (int64, error) {
	f.requeued = append(f.requeued, resourceIDs...)
	return int64(len(resourceIDs)), nil
}
func (*fakeStore) UpdateQualityAgentEnrollmentAfterTriage(context.Context, string, time.Time) (*store.QualityAgentEnrollmentMessage, bool, error) {
	return &store.QualityAgentEnrollmentMessage{}, true, nil
}
func (f *fakeStore) PromoteQualityAgentEnrollment(_ context.Context, resourceID string, _ storepb.QualityAgentSessionMode) (*store.QualityAgentEnrollmentMessage, bool, error) {
	f.promoted = append(f.promoted, resourceID)
	return &store.QualityAgentEnrollmentMessage{ResourceID: resourceID}, true, nil
}
func (f *fakeStore) LinkQualityAgentEnrollmentSession(_ context.Context, resourceID string, _ int64, _ storepb.QualityAgentSessionMode) (*store.QualityAgentEnrollmentMessage, bool, error) {
	f.linked = append(f.linked, resourceID)
	return &store.QualityAgentEnrollmentMessage{ResourceID: resourceID}, true, nil
}
func (f *fakeStore) AdmitQualityAgentTriage(_ context.Context, enrollmentResourceID, sessionResourceID string, _ time.Time) (bool, error) {
	f.linked = append(f.linked, enrollmentResourceID)
	f.sessionModesSet = append(f.sessionModesSet, sessionResourceID)
	f.advancedWakes = append(f.advancedWakes, sessionResourceID)
	return true, nil
}
func (f *fakeStore) SetQualityAgentEnrollmentBlockedReason(_ context.Context, _ string, reason string) error {
	f.blockedReasonValues = append(f.blockedReasonValues, reason)
	return nil
}
func (f *fakeStore) UpdateQualityAgentEnrollmentPriority(_ context.Context, updates []*store.QualityAgentEnrollmentPriorityUpdate) error {
	f.prioritiesUpdated = true
	f.priorityUpdates = append(f.priorityUpdates, updates...)
	return nil
}
func (f *fakeStore) GetQualityAgentSessionByTarget(_ context.Context, _ string, _ int64, targetGUID string) (*store.QualityAgentSessionMessage, error) {
	return f.sessionsByTarget[targetGUID], nil
}
func (f *fakeStore) CreateQualityAgentSession(_ context.Context, input *store.CreateQualityAgentSessionInput) (*store.QualityAgentSessionMessage, error) {
	f.created = append(f.created, input)
	return &store.QualityAgentSessionMessage{ID: int64(len(f.created)), ResourceID: fmt.Sprintf("session-%d", len(f.created)), TargetGUID: input.TargetGUID}, nil
}
func (f *fakeStore) UpdateQualityAgentSessionState(_ context.Context, resourceID string, _ int32, state storepb.QualityAgentSessionState, _, _ string, _ *time.Time) (*store.QualityAgentSessionMessage, bool, error) {
	_ = state
	f.enabledSessions = append(f.enabledSessions, resourceID)
	return &store.QualityAgentSessionMessage{ResourceID: resourceID}, true, nil
}
func (f *fakeStore) UpdateQualityAgentSessionMode(_ context.Context, resourceID string, _ storepb.QualityAgentSessionMode) (bool, error) {
	f.sessionModesSet = append(f.sessionModesSet, resourceID)
	return true, nil
}
func (f *fakeStore) AdvanceQualityAgentSessionWake(_ context.Context, resourceID string, _ time.Time) (bool, error) {
	f.advancedWakes = append(f.advancedWakes, resourceID)
	return true, nil
}
func (f *fakeStore) CountQualityAgentSessionWakesSince(context.Context, string, int64, time.Time) (int64, error) {
	return f.wakesLastHour, nil
}

func (f *fakeStore) CurrentDatabaseTime(context.Context) (time.Time, error) {
	if f.databaseTime.IsZero() {
		return time.Unix(10_000, 0).UTC(), nil
	}
	return f.databaseTime, nil
}
func (f *fakeStore) ListQualityAgentDatabasesByAgent(context.Context) (map[int64][]string, error) {
	return f.scopes, nil
}

func (f *fakeStore) ListQualityAgents(context.Context) ([]*store.QualityAgentMessage, error) {
	agents := append([]*store.QualityAgentMessage(nil), f.agents...)
	if f.agent != nil {
		agents = append(agents, f.agent)
	}
	return agents, nil
}
func (f *fakeStore) ListQualityRules(context.Context, *store.FindQualityRuleMessage) ([]*store.QualityRuleMessage, error) {
	return f.rules, nil
}
func (f *fakeStore) ListQualityContracts(context.Context) ([]*store.QualityContractMessage, error) {
	return f.contracts, nil
}
func (*fakeStore) ListQualityTargetFailureEvidence(context.Context, []store.MetaGUIDKey, time.Time, time.Time, int) (map[store.MetaGUIDKey]*store.QualityFailureEvidence, error) {
	return nil, nil
}
func (*fakeStore) CountColumnLineageByGUID(context.Context, []string) (map[string]*store.ColumnLineageCount, error) {
	return nil, nil
}
func (*fakeStore) ListMetaRegistryTargetColumns(context.Context, []store.MetaGUIDKey) (map[store.MetaGUIDKey]map[string]struct{}, error) {
	return nil, nil
}
func (*fakeStore) ListMetaRegistrySchemaChangeTimes(context.Context, []store.MetaGUIDKey, time.Time, time.Time) (map[store.MetaGUIDKey]time.Time, error) {
	return nil, nil
}
func (f *fakeStore) ListMetaRegistryResourceDigest(_ context.Context, find *store.FindMetaRegistryResourceMessage) ([]*store.MetaRegistryResource, error) {
	if find.Offset != nil && *find.Offset > 0 {
		return nil, nil
	}
	return f.digest, nil
}
