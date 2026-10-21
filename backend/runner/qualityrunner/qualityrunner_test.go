package qualityrunner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func qualityRuleStub(id int64, targetGUID string) *store.QualityRuleMessage {
	return &store.QualityRuleMessage{
		ID:               id,
		TargetGUID:       targetGUID,
		ScheduleInterval: time.Hour,
	}
}

func qualityRuleIDs(rules []*store.QualityRuleMessage) []int64 {
	ids := make([]int64, 0, len(rules))
	for _, rule := range rules {
		ids = append(ids, rule.ID)
	}
	return ids
}

func qualityResultStub(status storepb.QualityStatus) *store.QualityRuleResultMessage {
	return &store.QualityRuleResultMessage{Status: status}
}

func TestPlanDueRunsGroupsByInstanceAndChunks(t *testing.T) {
	t.Parallel()

	rules := []*store.QualityRuleMessage{
		qualityRuleStub(1, "inst-a;db;public;t1"),
		qualityRuleStub(2, "inst-b;db;public;t2"),
		qualityRuleStub(3, "inst-a;db;public;t3"),
		qualityRuleStub(4, "inst-b;db;public;t4"),
		qualityRuleStub(5, "inst-a;db;public;t5"),
	}

	plans := planDueRuns(rules, 2)
	require.Len(t, plans, 3)
	require.Equal(t, "inst-a", plans[0].instanceResourceID)
	require.Equal(t, []int64{1, 3}, qualityRuleIDs(plans[0].rules))
	require.Equal(t, "inst-a", plans[1].instanceResourceID)
	require.Equal(t, []int64{5}, qualityRuleIDs(plans[1].rules))
	require.Equal(t, "inst-b", plans[2].instanceResourceID)
	require.Equal(t, []int64{2, 4}, qualityRuleIDs(plans[2].rules))
}

func TestPlanDueRunsKeepsOrderForSameInstance(t *testing.T) {
	t.Parallel()

	rules := []*store.QualityRuleMessage{
		qualityRuleStub(3, "inst-a;db;public;t3"),
		qualityRuleStub(1, "inst-a;db;public;t1"),
		qualityRuleStub(2, "inst-a;db;public;t2"),
	}
	plans := planDueRuns(rules, 50)
	require.Len(t, plans, 1)
	// The due scan already ordered the rules; grouping must not reorder them.
	require.Equal(t, []int64{3, 1, 2}, qualityRuleIDs(plans[0].rules))
}

func TestPlanDueRunsAttributesMalformedTargetToNoInstance(t *testing.T) {
	t.Parallel()

	plans := planDueRuns([]*store.QualityRuleMessage{nil, qualityRuleStub(7, "not-a-guid")}, 50)
	require.Len(t, plans, 1)
	require.Empty(t, plans[0].instanceResourceID)
	require.Equal(t, []int64{7}, qualityRuleIDs(plans[0].rules))
}

func TestPlanDueRunsRejectsNonPositiveBatchSize(t *testing.T) {
	t.Parallel()

	require.Empty(t, planDueRuns([]*store.QualityRuleMessage{qualityRuleStub(1, "inst;db;public;t")}, 0))
}

func TestNextRunAtUsesTheIntervalButNeverLoopsEveryTick(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 9, 0, 0, 0, time.UTC)
	require.Equal(t, now.Add(2*time.Hour), nextRunAt(now, 2*time.Hour))
	// A missing or too-short interval is the API's validation job; the runner's
	// backstop keeps such a rule from becoming due on every ten-second tick.
	require.Equal(t, now.Add(minRuleScheduleInterval), nextRunAt(now, 0))
	require.Equal(t, now.Add(minRuleScheduleInterval), nextRunAt(now, time.Minute))
}

// A run that could not execute a rule must not look like a run whose rule found
// bad data: ERROR outranks FAIL in the rollup.
func TestCountsAndOutcomeSeparatesErrorFromFail(t *testing.T) {
	t.Parallel()

	counts, outcome := countsAndOutcome([]*store.QualityRuleResultMessage{
		qualityResultStub(storepb.QualityStatus_QUALITY_STATUS_PASS),
		qualityResultStub(storepb.QualityStatus_QUALITY_STATUS_FAIL),
		qualityResultStub(storepb.QualityStatus_QUALITY_STATUS_ERROR),
		qualityResultStub(storepb.QualityStatus_QUALITY_STATUS_SKIPPED),
	})
	require.Equal(t, int32(4), counts.GetTotal())
	require.Equal(t, int32(1), counts.GetPass())
	require.Equal(t, int32(1), counts.GetFail())
	require.Equal(t, int32(1), counts.GetError())
	require.Equal(t, int32(1), counts.GetSkipped())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, outcome)

	counts, outcome = countsAndOutcome([]*store.QualityRuleResultMessage{
		qualityResultStub(storepb.QualityStatus_QUALITY_STATUS_WARN),
		qualityResultStub(storepb.QualityStatus_QUALITY_STATUS_FAIL),
	})
	require.Equal(t, int32(2), counts.GetTotal())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, outcome)

	// A run with no results judged nothing; that is not a pass.
	counts, outcome = countsAndOutcome(nil)
	require.Equal(t, int32(0), counts.GetTotal())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, outcome)
}

func TestManualRuleTasksDeduplicatesAndDropsInvalidIDs(t *testing.T) {
	t.Parallel()

	tasks := manualRuleTasks([]int64{5, 0, -2, 5, 7, 7})
	require.Len(t, tasks, 2)
	require.Equal(t, int64(5), tasks[0].ruleID)
	require.Equal(t, int64(7), tasks[1].ruleID)
	require.Nil(t, tasks[0].head)
}

func TestInflightGuardIsExclusivePerRule(t *testing.T) {
	t.Parallel()

	runner := &Runner{inflight: make(map[int64]struct{})}
	require.True(t, runner.acquireInflight(5))
	require.False(t, runner.acquireInflight(5))
	// A different rule is independent.
	require.True(t, runner.acquireInflight(6))
	runner.releaseInflight(5)
	require.True(t, runner.acquireInflight(5))
}

func TestEnqueueQualityRunReportsAFullQueue(t *testing.T) {
	t.Parallel()

	runner := &Runner{manual: make(chan manualRun, 1)}
	require.True(t, runner.EnqueueQualityRun(1, "inst-1", []int64{3}))

	// The queue is full, so the API must be told instead of blocking the request.
	require.False(t, runner.EnqueueQualityRun(2, "inst-1", nil))

	queued := <-runner.manual
	require.Equal(t, int64(1), queued.runID)
	require.Equal(t, "inst-1", queued.instanceResourceID)
	require.Equal(t, []int64{3}, queued.ruleIDs)
	require.True(t, runner.EnqueueQualityRun(3, "inst-1", nil))
}

// A run holds one lease for its whole life, so a second run on the same instance
// waits for it instead of being admitted above the connection budget.
func TestAdmitRunHoldsTheInstanceBudgetForTheWholeRun(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	runner := &Runner{stateCfg: stateCfg}
	instance := &store.InstanceMessage{
		ResourceID: "inst-1",
		Metadata:   &storepb.Instance{MaximumConnections: 1},
	}

	hold, _, admitted := runner.admitRun(context.Background(), instance, state.ConnectionPriorityBackground, time.Second)
	require.True(t, admitted)

	type admission struct {
		release func()
		ok      bool
	}
	second := make(chan admission, 1)
	go func() {
		release, _, ok := runner.admitRun(context.Background(), instance, state.ConnectionPriorityBackground, time.Second)
		second <- admission{release: release, ok: ok}
	}()
	select {
	case result := <-second:
		t.Fatalf("a second run was admitted above the instance budget: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}

	hold()
	select {
	case result := <-second:
		require.True(t, result.ok)
		result.release()
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting run was never admitted after the budget was released")
	}
}

// A run whose budget never frees up is deferred, not failed: the caller gets
// false and the rules stay due for the next tick.
func TestAdmitRunReportsWhenTheBudgetStaysBusy(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	runner := &Runner{stateCfg: stateCfg}
	instance := &store.InstanceMessage{
		ResourceID: "inst-1",
		Metadata:   &storepb.Instance{MaximumConnections: 1},
	}

	hold, _, admitted := runner.admitRun(context.Background(), instance, state.ConnectionPriorityBackground, time.Second)
	require.True(t, admitted)
	defer hold()

	_, _, admitted = runner.admitRun(context.Background(), instance, state.ConnectionPriorityBackground, 10*time.Millisecond)
	require.False(t, admitted)
}

// A run whose instance row is gone needs no lease: target resolution records
// ORPHANED for its rules before a driver is opened.
func TestAdmitRunDoesNotLeaseAMissingInstance(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	runner := &Runner{stateCfg: stateCfg}

	release, admission, admitted := runner.admitRun(context.Background(), nil, state.ConnectionPriorityManual, time.Second)
	require.True(t, admitted)
	require.Empty(t, admission.instanceResourceID)
	require.NotNil(t, release)
	release()
}

// A rule retargeted to another instance after its run was admitted cannot open a
// driver under the run's lease; it leases the instance it actually resolves to, so
// no driver ever runs outside a leased budget.
func TestRuleConnectionLeaseCoversARetargetedRule(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New()
	require.NoError(t, err)
	runner := &Runner{stateCfg: stateCfg}
	runAdmittedForA := runAdmission{instanceResourceID: "inst-a", priority: state.ConnectionPriorityBackground}
	instanceA := &store.InstanceMessage{ResourceID: "inst-a", Metadata: &storepb.Instance{}}

	release, ok := runner.ruleConnectionLease(instanceA, runAdmittedForA)
	require.True(t, ok, "the run's own instance is covered by its lease")
	release()

	instanceB := &store.InstanceMessage{ResourceID: "inst-b", Metadata: &storepb.Instance{MaximumConnections: 1}}
	hold, _, admitted := runner.admitRun(context.Background(), instanceB, state.ConnectionPriorityBackground, time.Second)
	require.True(t, admitted)

	_, ok = runner.ruleConnectionLease(instanceB, runAdmittedForA)
	require.False(t, ok, "a retargeted rule cannot take another instance's budget without a free slot")

	hold()
	release, ok = runner.ruleConnectionLease(instanceB, runAdmittedForA)
	require.True(t, ok)
	release()

	// A run admitted without a lease (its instance row was gone) still leases the
	// instance its rule really resolves to.
	release, ok = runner.ruleConnectionLease(instanceA, runAdmission{})
	require.True(t, ok)
	release()
}

// A non-positive ceiling means "use the deployment default", never "unlimited".
func TestInstanceConnectionLimitUsesTheDeploymentDefault(t *testing.T) {
	t.Parallel()

	require.Equal(t, 4, instanceConnectionLimit(&store.InstanceMessage{Metadata: &storepb.Instance{MaximumConnections: 4}}))
	require.Equal(t, 10, instanceConnectionLimit(&store.InstanceMessage{Metadata: &storepb.Instance{}}))
	require.Equal(t, 10, instanceConnectionLimit(&store.InstanceMessage{}))
}

func TestBuildRuleResultStoresTheVerdictAndItsEvidence(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 19, 9, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(1500 * time.Millisecond)
	snapshot := &dataquality.RuleSnapshot{
		ID:       7,
		Revision: 4,
		Target: dataquality.Target{
			GUID:     "inst;db;public;orders",
			MetaType: storepb.MetaType_TABLE,
		},
	}
	execution := &dataquality.Execution{
		Samples:     &storepb.QualityFailureSamples{Samples: []*storepb.FailureSample{{}}},
		ExecutedSQL: "SELECT COUNT(*) FROM orders",
		Sampled:     true,
		Truncated:   true,
	}
	thresholds := &storepb.QualityThresholds{}
	result := buildRuleResult(11, &ruleExecutionOutcome{
		ruleID:     7,
		snapshot:   snapshot,
		startedAt:  startedAt,
		finishedAt: finishedAt,
		observation: dataquality.Observation{
			Observed:     12,
			ObservedKind: storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
			FailedRows:   12,
			TotalRows:    50,
			Sampled:      true,
			SampleRows:   500,
		},
		execution: execution,
		state:     dataquality.ResultState{Status: storepb.QualityStatus_QUALITY_STATUS_FAIL, Thresholds: thresholds},
	})

	require.Equal(t, int64(11), result.RunID)
	require.Equal(t, int64(7), result.RuleID)
	require.Equal(t, int32(4), result.RuleRevision)
	require.Equal(t, dataquality.EngineVersion, result.RuleEngineVersion)
	require.Equal(t, "inst;db;public;orders", result.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, result.TargetType)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, result.Status)
	require.NotNil(t, result.ObservedValue)
	require.InDelta(t, 12, *result.ObservedValue, 1e-9)
	require.Equal(t, storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT, result.ObservedKind)
	require.Same(t, thresholds, result.Thresholds)
	require.Equal(t, int64(12), result.FailedRows)
	require.Equal(t, int64(50), result.TotalRows)
	require.True(t, result.Sampled)
	require.Equal(t, int64(500), result.SampleRows)
	require.True(t, result.Truncated)
	require.NotNil(t, result.Sample)
	require.Equal(t, "SELECT COUNT(*) FROM orders", result.ExecutedSQL)
	require.Equal(t, int32(1500), result.DurationMS)
	require.Equal(t, startedAt, result.StartedAt)
	require.Equal(t, finishedAt, result.FinishedAt)
	require.Empty(t, result.ErrorCode)
}

func TestBuildRuleResultKeepsErrorMessageBounded(t *testing.T) {
	t.Parallel()

	long := make([]byte, maxErrorMessageLength+100)
	for i := range long {
		long[i] = 'x'
	}
	result := failureRuleResult(1, 2, nil, time.Now().UTC(), storepb.QualityStatus_QUALITY_STATUS_ERROR,
		dataquality.ErrCodeExecution, string(long))
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, result.Status)
	require.Nil(t, result.ObservedValue)
	require.Len(t, result.ErrorMessage, maxErrorMessageLength)
	require.Equal(t, dataquality.ErrCodeExecution, result.ErrorCode)
	require.Zero(t, result.RuleRevision)
}

func TestObservedValueIsAbsentForUnjudgedResults(t *testing.T) {
	t.Parallel()

	require.NotNil(t, observedValue(storepb.QualityStatus_QUALITY_STATUS_PASS, 0))
	require.NotNil(t, observedValue(storepb.QualityStatus_QUALITY_STATUS_FAIL, 3))
	require.Nil(t, observedValue(storepb.QualityStatus_QUALITY_STATUS_ERROR, 3))
	require.Nil(t, observedValue(storepb.QualityStatus_QUALITY_STATUS_SKIPPED, 3))
	require.Nil(t, observedValue(storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, 3))
	require.Nil(t, observedValue(storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, 3))
}

func TestDurationMSClampsNegativeAndOverflow(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	require.Equal(t, int32(0), durationMS(now, now.Add(-time.Second)))
	require.Equal(t, int32(2000), durationMS(now, now.Add(2*time.Second)))
	require.Equal(t, int32(^uint32(0)>>1), durationMS(now.Add(-20*24*time.Hour), now.Add(20*24*time.Hour)))
}

func TestBaselineFromResultsSkipsUnjudgedResults(t *testing.T) {
	t.Parallel()

	observed := 42.0
	// The newest result is an ERROR; the last evaluated one is the baseline.
	results := []*store.QualityRuleResultMessage{
		{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR, ObservedValue: &observed},
		{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, ObservedValue: &observed},
	}
	now := time.Now().UTC()
	results[1].StartedAt = now.Add(-time.Minute)
	baseline := baselineFromResults(results, now, time.Hour)
	require.NotNil(t, baseline)
	require.InDelta(t, 42, *baseline, 1e-9)

	require.Nil(t, baselineFromResults(nil, now, time.Hour))
	require.Nil(t, baselineFromResults([]*store.QualityRuleResultMessage{
		{Status: storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, ObservedValue: &observed},
	}, now, time.Hour))

	// A week-old observation, or one more than two intervals behind, is no
	// longer a fair denominator.
	stale := []*store.QualityRuleResultMessage{
		{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, ObservedValue: &observed, StartedAt: now.Add(-8 * 24 * time.Hour)},
	}
	require.Nil(t, baselineFromResults(stale, now, 24*time.Hour))
	drifted := []*store.QualityRuleResultMessage{
		{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, ObservedValue: &observed, StartedAt: now.Add(-3 * time.Hour)},
	}
	require.Nil(t, baselineFromResults(drifted, now, time.Hour))
	require.NotNil(t, baselineFromResults(drifted, now, 4*time.Hour))
}

func TestErrorCodeAndMessageMapsRunnerLevelFailures(t *testing.T) {
	t.Parallel()

	code, _ := errorCodeAndMessage(dbfactory.ErrNoReadOnlyDataSource)
	require.Equal(t, dataquality.ErrCodeNoReadOnly, code)

	// Capacity contention is not a rule failure: the runner defers the run before
	// it executes rather than turning the limiter's error into a QUOTA result.
	code, _ = errorCodeAndMessage(state.ErrInstanceConnectionLimit)
	require.NotEqual(t, dataquality.ErrCodeQuota, code)

	code, message := errorCodeAndMessage(errors.New("the target refused the statement"))
	require.Equal(t, dataquality.ErrCodeExecution, code)
	require.Equal(t, "the target refused the statement", message)
}

func qualityPtr[T any](value T) *T {
	return &value
}

// qualityExecutionStub is one evaluated FAIL execution with every input the
// problem ledger and the notification builders read.
func qualityExecutionStub(status storepb.QualityStatus) *ruleExecution {
	observed := 7.0
	previous := storepb.QualityStatus_QUALITY_STATUS_FAIL
	return &ruleExecution{
		result: &store.QualityRuleResultMessage{
			RunID:         11,
			RuleID:        7,
			RuleRevision:  2,
			TargetGUID:    "inst;db;public;orders",
			TargetType:    storepb.MetaType_TABLE,
			Status:        status,
			ObservedValue: &observed,
			ObservedKind:  storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
			FailedRows:    7,
			TotalRows:     20,
		},
		head: &store.QualityRuleMessage{
			ID:         7,
			ResourceID: "rule1",
			Title:      "orders status must not be null",
			Severity:   storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			OwnerID:    qualityPtr(int32(9)),
		},
		snapshot: &dataquality.RuleSnapshot{
			ID:       7,
			Revision: 2,
			Severity: storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
			Target: dataquality.Target{
				GUID:     "inst;db;public;orders",
				MetaType: storepb.MetaType_TABLE,
				Path: &storepb.QualityTargetPath{
					Instance: "inst1",
					Database: "db1",
					Object:   "orders",
					Column:   "status",
				},
			},
		},
		previous: &previous,
	}
}

// A problem transition goes to the owner that must act on it, falling back to
// the administrators; an execution failure and a storm summary go to the
// administrators only (design §8.2).
func TestQualityNotificationRecipient(t *testing.T) {
	t.Parallel()

	opened := storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED
	recovered := storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED
	executionFailed := storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED
	storm := storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY

	for _, kind := range []storepb.DataQualityNotificationKind{opened, recovered} {
		recipientID, toAdmins := qualityNotificationRecipient(kind, qualityPtr(int32(9)))
		require.False(t, toAdmins)
		require.Equal(t, int32(9), recipientID)

		recipientID, toAdmins = qualityNotificationRecipient(kind, nil)
		require.True(t, toAdmins)
		require.Zero(t, recipientID)

		recipientID, toAdmins = qualityNotificationRecipient(kind, qualityPtr(int32(0)))
		require.True(t, toAdmins)
		require.Zero(t, recipientID)
	}

	for _, kind := range []storepb.DataQualityNotificationKind{executionFailed, storm} {
		// The rule's owner still gets an administrator-only alert: the owner sees
		// the failed execution on the run page, not in their inbox.
		recipientID, toAdmins := qualityNotificationRecipient(kind, qualityPtr(int32(9)))
		require.True(t, toAdmins)
		require.Zero(t, recipientID)

		recipientID, toAdmins = qualityNotificationRecipient(kind, nil)
		require.True(t, toAdmins)
		require.Zero(t, recipientID)
	}

	recipientID, toAdmins := qualityNotificationRecipient(
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED, qualityPtr(int32(9)))
	require.True(t, toAdmins)
	require.Zero(t, recipientID)
}

func TestQualityRuleMutedOnlyWhileTheWindowIsOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 10, 0, 0, 0, time.UTC)
	require.False(t, qualityRuleMuted(nil, now))
	require.False(t, qualityRuleMuted(&store.QualityRuleMessage{}, now))
	require.True(t, qualityRuleMuted(&store.QualityRuleMessage{MuteUntil: qualityPtr(now.Add(time.Hour))}, now))
	require.False(t, qualityRuleMuted(&store.QualityRuleMessage{MuteUntil: qualityPtr(now.Add(-time.Hour))}, now))
	// A mute that expires exactly now is over.
	require.False(t, qualityRuleMuted(&store.QualityRuleMessage{MuteUntil: &now}, now))
}

func TestQualityIssueMutedOnlyWhileTheWindowIsOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 10, 0, 0, 0, time.UTC)
	require.False(t, qualityIssueMuted(nil, now))
	require.True(t, qualityIssueMuted(&store.QualityIssueMessage{MutedUntil: qualityPtr(now.Add(time.Hour))}, now))
	require.False(t, qualityIssueMuted(&store.QualityIssueMessage{MutedUntil: qualityPtr(now.Add(-time.Hour))}, now))
}

func TestQualityNotificationSeverityMapping(t *testing.T) {
	t.Parallel()

	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		qualityNotificationSeverity(storepb.QualitySeverity_QUALITY_SEVERITY_WARNING))
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		qualityNotificationSeverity(storepb.QualitySeverity_QUALITY_SEVERITY_ERROR))
	// The notification envelope has no CRITICAL level; critical failures are errors.
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		qualityNotificationSeverity(storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL))
	require.Equal(t, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING,
		qualityNotificationSeverity(storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED))
}

func TestQualityObservedUnit(t *testing.T) {
	t.Parallel()

	require.Equal(t, "rows", qualityObservedUnit(storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT))
	require.Equal(t, "%", qualityObservedUnit(storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO))
	require.Equal(t, "s", qualityObservedUnit(storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_SECONDS))
	require.Equal(t, "timestamp", qualityObservedUnit(storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_TIMESTAMP))
	require.Empty(t, qualityObservedUnit(storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER))
	require.Empty(t, qualityObservedUnit(storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_UNSPECIFIED))
}

func TestQualityTargetDisplayPath(t *testing.T) {
	t.Parallel()

	require.Empty(t, qualityTargetDisplayPath(nil))
	require.Equal(t, "inst1/db1/public/orders.status", qualityTargetDisplayPath(&storepb.QualityTargetPath{
		Instance: "inst1",
		Database: "db1",
		Schema:   "public",
		Object:   "orders",
		Column:   "status",
	}))
	// The MySQL family has no schema segment; it must not leave an empty part.
	require.Equal(t, "inst1/db1/orders", qualityTargetDisplayPath(&storepb.QualityTargetPath{
		Instance: "inst1",
		Database: "db1",
		Object:   "orders",
	}))
}

// Only FAIL results make a storm: a run that could not execute its rules is a
// different alert, and it must not merge into the data-quality summary.
func TestIsQualityRunStorm(t *testing.T) {
	t.Parallel()

	results := func(statuses ...storepb.QualityStatus) []*store.QualityRuleResultMessage {
		out := make([]*store.QualityRuleResultMessage, 0, len(statuses))
		for _, status := range statuses {
			out = append(out, qualityResultStub(status))
		}
		return out
	}

	atThreshold := make([]storepb.QualityStatus, 0, QualityStormThreshold)
	for range QualityStormThreshold {
		atThreshold = append(atThreshold, storepb.QualityStatus_QUALITY_STATUS_FAIL)
	}
	require.False(t, isQualityRunStorm(results(atThreshold...)))

	overThreshold := make([]storepb.QualityStatus, 0, len(atThreshold)+1)
	overThreshold = append(overThreshold, atThreshold...)
	overThreshold = append(overThreshold, storepb.QualityStatus_QUALITY_STATUS_FAIL)
	require.True(t, isQualityRunStorm(results(overThreshold...)))

	errors := make([]storepb.QualityStatus, 0, QualityStormThreshold+1)
	for range QualityStormThreshold + 1 {
		errors = append(errors, storepb.QualityStatus_QUALITY_STATUS_ERROR)
	}
	require.False(t, isQualityRunStorm(results(errors...)))

	mixed := make([]storepb.QualityStatus, 0, len(errors))
	mixed = append(mixed, errors[1:]...)
	mixed = append(mixed, storepb.QualityStatus_QUALITY_STATUS_FAIL)
	require.False(t, isQualityRunStorm(results(mixed...)))
}

func TestBuildDataQualityDetailForAnOpenedIssue(t *testing.T) {
	t.Parallel()

	execution := qualityExecutionStub(storepb.QualityStatus_QUALITY_STATUS_FAIL)
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     "ws1",
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		execution:       execution,
		issueResourceID: "issue1",
		runResourceName: "workspaces/ws1/qualityRuns/run1",
	})

	require.Equal(t, storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED, detail.GetKind())
	require.Equal(t, "workspaces/ws1/qualityRules/rule1", detail.GetRule())
	require.Equal(t, "orders status must not be null", detail.GetRuleTitle())
	require.Equal(t, "inst;db;public;orders", detail.GetTargetGuid())
	require.Equal(t, "inst1/db1/orders.status", detail.GetTargetPath())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, detail.GetFromStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, detail.GetToStatus())
	require.Equal(t, storepb.QualitySeverity_QUALITY_SEVERITY_ERROR, detail.GetSeverity())
	require.InDelta(t, 7, detail.GetObservedValue(), 1e-9)
	require.Equal(t, "rows", detail.GetObservedUnit())
	require.Equal(t, int64(7), detail.GetFailedRows())
	require.Equal(t, int64(20), detail.GetTotalRows())
	require.Equal(t, "workspaces/ws1/qualityIssues/issue1", detail.GetIssue())
	require.Equal(t, "workspaces/ws1/qualityRuns/run1", detail.GetRun())
	require.Empty(t, detail.GetErrorCode())
}

func TestBuildDataQualityDetailForARecovery(t *testing.T) {
	t.Parallel()

	execution := qualityExecutionStub(storepb.QualityStatus_QUALITY_STATUS_PASS)
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     "ws1",
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED,
		execution:       execution,
		issueResourceID: "issue1",
		runResourceName: "workspaces/ws1/qualityRuns/run2",
	})
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, detail.GetFromStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, detail.GetToStatus())
}

func TestBuildDataQualityDetailForAnExecutionFailure(t *testing.T) {
	t.Parallel()

	execution := qualityExecutionStub(storepb.QualityStatus_QUALITY_STATUS_ERROR)
	execution.result.ObservedValue = nil
	execution.result.ErrorCode = dataquality.ErrCodeTimeout
	execution.previous = nil
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     "ws1",
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		execution:       execution,
		runResourceName: "workspaces/ws1/qualityRuns/run1",
	})

	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, detail.GetFromStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, detail.GetToStatus())
	require.Equal(t, dataquality.ErrCodeTimeout, detail.GetErrorCode())
	require.Zero(t, detail.GetObservedValue())
	require.Empty(t, detail.GetIssue())
}

func TestQualityNotificationTargetIsScopedToTheEvent(t *testing.T) {
	t.Parallel()

	require.Equal(t, "workspaces/ws/qualityIssues/issue1", qualityNotificationTarget(&storepb.DataQualityDetail{
		Kind:  storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		Rule:  "workspaces/ws/qualityRules/rule1",
		Issue: "workspaces/ws/qualityIssues/issue1",
	}))
	require.Equal(t, "workspaces/ws/qualityRules/rule1", qualityNotificationTarget(&storepb.DataQualityDetail{
		Kind: storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		Rule: "workspaces/ws/qualityRules/rule1",
	}))
	require.Equal(t, "workspaces/ws/qualityRuns/run1", qualityNotificationTarget(&storepb.DataQualityDetail{
		Kind: storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY,
		Run:  "workspaces/ws/qualityRuns/run1",
	}))
	require.Empty(t, qualityNotificationTarget(&storepb.DataQualityDetail{
		Kind: storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED,
	}))
}

func TestBuildStormDetail(t *testing.T) {
	t.Parallel()

	detail := buildStormDetail("workspaces/ws/qualityRuns/run1", 12, 40)
	require.Equal(t, storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY, detail.GetKind())
	require.Equal(t, "workspaces/ws/qualityRuns/run1", detail.GetRun())
	require.Equal(t, int32(12), detail.GetFailedRuleCount())
	require.Equal(t, int32(40), detail.GetTotalRuleCount())
}

// A metadata rule observes the registry, so it must run without a target
// connection; every query rule still needs the read-only data source.
func TestRequiresReadOnlySource(t *testing.T) {
	t.Parallel()

	require.False(t, requiresReadOnlySource(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT),
		"schema drift reads metadata, not the target database")
	require.True(t, requiresReadOnlySource(storepb.QualityRuleType_QUALITY_RULE_TYPE_GROUP_CHECK),
		"group check runs a GROUP BY query")
	for _, ruleType := range []storepb.QualityRuleType{
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_CUSTOM_SQL,
	} {
		require.True(t, requiresReadOnlySource(ruleType), "rule type %s", ruleType)
	}

	// The decision must agree with the component's own definition, so the runner
	// and the executor cannot disagree about which rules need a queryer.
	require.Equal(t, dataquality.IsMetadataRule(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT),
		!requiresReadOnlySource(storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT))
}
