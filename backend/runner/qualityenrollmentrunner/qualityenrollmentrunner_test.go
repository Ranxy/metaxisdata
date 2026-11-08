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

func TestAdmissionSkipsTriageWithoutAModel(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = &store.QualityAgentMessage{ID: 7, Enabled: true}
	fake.candidates = []*store.QualityAgentEnrollmentMessage{{
		ResourceID:  "enr-1",
		TargetGUID:  "i;d;public;t",
		TargetType:  storepb.MetaType_TABLE,
		DatabaseRef: "instances/i/databases/d",
		Mode:        storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_TRIAGE,
	}}
	runner := testRunner(fake)
	runner.llmConfigured = func(context.Context) bool { return false }

	runner.admit(context.Background(), "ws", testSettings(), time.Unix(10_000, 0).UTC())

	require.Empty(t, fake.created)
	require.Equal(t, []string{"enr-1"}, fake.blockedReasons)
	require.Equal(t, []string{"no-model"}, fake.blockedReasonValues)
}

func TestAdmissionCreatesAutomaticSession(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = &store.QualityAgentMessage{ID: 7, Enabled: true}
	fake.candidates = []*store.QualityAgentEnrollmentMessage{{
		ResourceID:  "enr-1",
		TargetGUID:  "i;d;public;t",
		TargetType:  storepb.MetaType_TABLE,
		DatabaseRef: "instances/i/databases/d",
		Mode:        storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR,
	}}
	fake.sessionsByTarget = map[string]*store.QualityAgentSessionMessage{"i;d;public;t": nil}
	runner := testRunner(fake)
	runner.llmConfigured = func(context.Context) bool { return true }

	runner.admit(context.Background(), "ws", testSettings(), time.Unix(10_000, 0).UTC())

	require.Len(t, fake.created, 1)
	require.Equal(t, storepb.QualityAgentSessionSource_QUALITY_AGENT_SESSION_SOURCE_AUTO, fake.created[0].Source)
	require.Equal(t, storepb.QualityAgentSessionMode_QUALITY_AGENT_SESSION_MODE_MONITOR, fake.created[0].Mode)
	require.Nil(t, fake.created[0].CreatedBy)
	require.NotNil(t, fake.created[0].NextWakeAt)
	require.Len(t, fake.linked, 1)
}

func TestAdmissionStopsAtTheHourlyCap(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = &store.QualityAgentMessage{ID: 7, Enabled: true}
	fake.createdLastHour = testSettings().AgentRunnerMaxNewSessionsPerHour
	fake.candidates = []*store.QualityAgentEnrollmentMessage{{ResourceID: "enr-1", TargetGUID: "i;d;public;t"}}
	runner := testRunner(fake)

	runner.admit(context.Background(), "ws", testSettings(), time.Unix(10_000, 0).UTC())

	require.Empty(t, fake.created)
	require.Equal(t, []string{"admission-limit"}, fake.blockedReasonValues)
}

func TestAdmissionStopsAtTheActiveCap(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = &store.QualityAgentMessage{ID: 7, Enabled: true}
	fake.activeEnrollments = testSettings().AgentRunnerMaxActiveSessions
	fake.candidates = []*store.QualityAgentEnrollmentMessage{{ResourceID: "enr-1", TargetGUID: "i;d;public;t"}}
	runner := testRunner(fake)

	runner.admit(context.Background(), "ws", testSettings(), time.Unix(10_000, 0).UTC())

	require.Empty(t, fake.created)
	require.Equal(t, []string{"active-limit"}, fake.blockedReasonValues)
}

func TestRequeueDueReviews(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.dueDormant = []*store.QualityAgentEnrollmentMessage{
		{ResourceID: "enr-1"}, {ResourceID: "enr-2"},
	}
	runner := testRunner(fake)
	runner.requeueDueReviews(context.Background(), "ws", time.Unix(20_000, 0).UTC())

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
	fake.agent = &store.QualityAgentMessage{ID: 7, Enabled: true}
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
	runner.recoverBlocked(context.Background(), "ws", testSettings())

	require.Equal(t, []string{"session-1"}, fake.enabledSessions)
}

func TestRunOnceReconcilesChangedScope(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	runner := testRunner(fake)
	settings := testSettings()

	runner.runOnce(context.Background())
	require.Equal(t, 1, fake.reconcileCalls)
	firstFingerprint := runner.scopeFingerprint

	runner.runOnce(context.Background())
	require.Equal(t, 1, fake.reconcileCalls, "an unchanged scope is not reconciled again")

	settings.AgentRunnerDatabases = []string{"instances/other/databases/d"}
	runner.loadSettings = func(context.Context) (dataquality.Settings, error) { return settings, nil }
	runner.runOnce(context.Background())
	require.Equal(t, 2, fake.reconcileCalls)
	require.NotEqual(t, firstFingerprint, runner.scopeFingerprint)
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

	require.NoError(t, runner.scanDatabase(context.Background(), "ws", "instances/i/databases/d", time.Unix(10_000, 0).UTC()))
	require.Len(t, fake.upserted, 2)
	require.Equal(t, "instances/i/databases/d", fake.upserted[0].DatabaseRef)
	require.Equal(t, []string{"instances/i/databases/d"}, fake.markedGone)
	require.True(t, fake.prioritiesUpdated)
}

func TestAdmissionRespectsTheEventBudgetReserve(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = &store.QualityAgentMessage{ID: 7, Enabled: true}
	fake.wakesLastHour = 50
	fake.candidates = []*store.QualityAgentEnrollmentMessage{{ResourceID: "enr-1", TargetGUID: "i;d;public;t"}}
	runner := testRunner(fake)

	runner.admit(context.Background(), "ws", testSettings(), time.Unix(10_000, 0).UTC())

	require.Empty(t, fake.created)
	require.Equal(t, []string{"event-budget"}, fake.blockedReasonValues)
}

func TestAdmissionClampsToTheRemainingActiveSlots(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = &store.QualityAgentMessage{ID: 7, Enabled: true}
	fake.activeEnrollments = testSettings().AgentRunnerMaxActiveSessions - 1
	fake.candidates = []*store.QualityAgentEnrollmentMessage{
		{ResourceID: "enr-1", TargetGUID: "i;d;public;a", DatabaseRef: "instances/i/databases/d"},
		{ResourceID: "enr-2", TargetGUID: "i;d;public;b", DatabaseRef: "instances/i/databases/d"},
	}
	runner := testRunner(fake)

	runner.admit(context.Background(), "ws", testSettings(), time.Unix(10_000, 0).UTC())

	require.Len(t, fake.created, 1, "one free active slot admits exactly one session")
}

func TestAdmissionStopsWhenAutonomyIsOff(t *testing.T) {
	t.Parallel()

	fake := newFakeStore()
	fake.agent = &store.QualityAgentMessage{ID: 7, Enabled: true}
	fake.candidates = []*store.QualityAgentEnrollmentMessage{{ResourceID: "enr-1", TargetGUID: "i;d;public;t"}}
	runner := testRunner(fake)
	settings := testSettings()
	settings.AgentAutonomyEnabled = false

	runner.admit(context.Background(), "ws", settings, time.Unix(10_000, 0).UTC())

	require.Empty(t, fake.created)
	require.Equal(t, []string{"autonomy-off"}, fake.blockedReasonValues)
}

func testRunner(stores Store) *Runner {
	return &Runner{
		store: stores,
		loadSettings: func(context.Context) (dataquality.Settings, error) {
			return testSettings(), nil
		},
		llmConfigured: func(context.Context) bool { return true },
		now:           func() time.Time { return time.Unix(10_000, 0).UTC() },
		tick:          time.Second,
		scanInterval:  time.Hour,
		lastScan:      map[string]time.Time{},
	}
}

func testSettings() dataquality.Settings {
	return dataquality.Settings{
		AgentRunnerEnabled:               true,
		AgentAutonomyEnabled:             true,
		AgentRunnerAutoEnroll:            true,
		AgentRunnerDatabases:             []string{"instances/i/databases/d"},
		AgentRunnerTokensPerHour:         1_000_000,
		AgentRunnerWakesPerHour:          100,
		AgentRunnerMaxConcurrentSessions: 2,
		AgentRunnerIdleWakeSeconds:       3_600,
		AgentRunnerMaxNewSessionsPerHour: 10,
		AgentRunnerMaxActiveSessions:     100,
		AgentRunnerDormantReviewDays:     30,
		AgentRunnerEventBudgetPercent:    50,
	}
}

// fakeStore is a configurable in-memory Store. Only the calls each test inspects are
// recorded; everything else returns an empty success.
type fakeStore struct {
	workspaceID string

	digest             []*store.MetaRegistryResource
	agent              *store.QualityAgentMessage
	agents             []*store.QualityAgentMessage
	enrollments        []*store.QualityAgentEnrollmentMessage
	candidates         []*store.QualityAgentEnrollmentMessage
	dueDormant         []*store.QualityAgentEnrollmentMessage
	covered            []*store.QualityAgentEnrollmentMessage
	recoverable        []*store.QualityAgentEnrollmentMessage
	enrollmentByTarget map[string]*store.QualityAgentEnrollmentMessage

	sessionsByTarget  map[string]*store.QualityAgentSessionMessage
	created           []*store.CreateQualityAgentSessionInput
	createdLastHour   int
	activeEnrollments int
	wakesLastHour     int64
	databaseTime      time.Time
	contracts         []*store.QualityContractMessage
	rules             []*store.QualityRuleMessage

	upserted            []*store.QualityAgentEnrollmentUpsert
	markedGone          []string
	prioritiesUpdated   bool
	reconcileCalls      int
	requeued            []string
	linked              []string
	promoted            []string
	sessionModesSet     []string
	advancedWakes       []string
	enabledSessions     []string
	blockedReasons      []string
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
func (f *fakeStore) MarkQualityAgentEnrollmentsGone(_ context.Context, _, databaseRef string, _ time.Time) (int64, error) {
	f.markedGone = append(f.markedGone, databaseRef)
	return 1, nil
}
func (f *fakeStore) ReconcileQualityAgentEnrollmentScope(context.Context, string, []string) (int64, int64, error) {
	f.reconcileCalls++
	return 0, 0, nil
}
func (f *fakeStore) ListQualityAgentEnrollments(context.Context, *store.FindQualityAgentEnrollmentMessage) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.enrollments, nil
}
func (f *fakeStore) ListAdmissibleQualityAgentEnrollments(context.Context, string, int) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.candidates, nil
}
func (f *fakeStore) ListDueDormantQualityAgentEnrollments(context.Context, string, time.Time, int) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.dueDormant, nil
}
func (f *fakeStore) ListCoveredDormantQualityAgentEnrollments(context.Context, string, int) ([]*store.QualityAgentEnrollmentMessage, error) {
	return f.covered, nil
}
func (f *fakeStore) ListRecoverableQualityAgentEnrollments(context.Context, string, int) ([]*store.QualityAgentEnrollmentMessage, error) {
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
func (f *fakeStore) SetQualityAgentEnrollmentBlockedReason(_ context.Context, resourceID, reason string) error {
	f.blockedReasons = append(f.blockedReasons, resourceID)
	f.blockedReasonValues = append(f.blockedReasonValues, reason)
	return nil
}
func (f *fakeStore) UpdateQualityAgentEnrollmentPriority(context.Context, []*store.QualityAgentEnrollmentPriorityUpdate) error {
	f.prioritiesUpdated = true
	return nil
}
func (f *fakeStore) GetQualityAgentSessionByTarget(_ context.Context, _, targetGUID string) (*store.QualityAgentSessionMessage, error) {
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
func (f *fakeStore) CountActiveQualityAgentEnrollments(context.Context, string) (int, error) {
	return f.activeEnrollments, nil
}
func (f *fakeStore) CountQualityAgentSessionsCreatedSince(context.Context, string, storepb.QualityAgentSessionSource, time.Time) (int, error) {
	return f.createdLastHour, nil
}

func (f *fakeStore) CountQualityAgentSessionWakesSince(context.Context, string, time.Time) (int64, error) {
	return f.wakesLastHour, nil
}

func (f *fakeStore) CurrentDatabaseTime(context.Context) (time.Time, error) {
	if f.databaseTime.IsZero() {
		return time.Unix(10_000, 0).UTC(), nil
	}
	return f.databaseTime, nil
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
