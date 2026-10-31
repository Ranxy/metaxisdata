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
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

func qualityRuleStub(id int64, targetGUID string) *store.QualityRuleMessage {
	return &store.QualityRuleMessage{
		ID:         id,
		TargetGUID: targetGUID,
		TargetType: storepb.MetaType_TABLE,
	}
}

// dueTargetStub builds one due target with its enabled checks; the due scan
// returns targets this way, so the planner never has to regroup rules by target.
func dueTargetStub(targetGUID string, ruleIDs ...int64) *store.DueQualityTarget {
	rules := make([]*store.DueQualityRule, 0, len(ruleIDs))
	for _, id := range ruleIDs {
		rules = append(rules, &store.DueQualityRule{Rule: qualityRuleStub(id, targetGUID), Attempt: 1})
	}
	return &store.DueQualityTarget{
		TargetGUID:       targetGUID,
		TargetType:       storepb.MetaType_TABLE,
		ScheduleInterval: time.Hour,
		Rules:            rules,
	}
}

func qualityRuleIDs(rules []*store.DueQualityRule) []int64 {
	ids := make([]int64, 0, len(rules))
	for _, rule := range rules {
		ids = append(ids, rule.Rule.ID)
	}
	return ids
}

func qualityExecutionStub(status storepb.QualityStatus) *store.QualityExecutionMessage {
	return &store.QualityExecutionMessage{Status: status}
}

func TestPlanDueBatchesGroupsByInstanceAndOneTargetPerBatch(t *testing.T) {
	t.Parallel()

	targets := []*store.DueQualityTarget{
		dueTargetStub("inst-a;db;public;t1", 1, 2),
		dueTargetStub("inst-b;db;public;t2", 3, 4),
		dueTargetStub("inst-a;db;public;t3", 5),
	}

	plans := planDueBatches(targets, 10)
	require.Len(t, plans, 3, "each due target is its own batch")
	require.Equal(t, "inst-a", plans[0].instanceResourceID)
	require.Equal(t, []int64{1, 2}, qualityRuleIDs(plans[0].rules))
	require.Equal(t, "inst-a", plans[1].instanceResourceID)
	require.Equal(t, []int64{5}, qualityRuleIDs(plans[1].rules))
	require.Equal(t, "inst-b", plans[2].instanceResourceID)
	require.Equal(t, []int64{3, 4}, qualityRuleIDs(plans[2].rules))
	for _, plan := range plans {
		require.NotNil(t, plan.target, "a batch always carries the target it belongs to")
	}
}

func TestPlanDueBatchesNeverPacksTwoTargetsTogether(t *testing.T) {
	t.Parallel()

	targets := []*store.DueQualityTarget{
		dueTargetStub("inst;db;public;orders", 1),
		dueTargetStub("inst;db;public;customers", 2),
	}
	plans := planDueBatches(targets, 50)
	require.Len(t, plans, 2)
	require.Equal(t, []int64{1}, qualityRuleIDs(plans[0].rules))
	require.Equal(t, "inst;db;public;orders", plans[0].target.TargetGUID)
	require.Equal(t, []int64{2}, qualityRuleIDs(plans[1].rules))
	require.Equal(t, "inst;db;public;customers", plans[1].target.TargetGUID)
}

func TestPlanDueBatchesSplitsOnlyAnOversizedTarget(t *testing.T) {
	t.Parallel()

	orders := dueTargetStub("inst-a;db;public;orders", 1, 2, 3, 4, 5)
	customers := dueTargetStub("inst-a;db;public;customers", 6, 7)
	plans := planDueBatches([]*store.DueQualityTarget{orders, customers}, 2)
	require.Len(t, plans, 4)
	require.Equal(t, []int64{1, 2}, qualityRuleIDs(plans[0].rules))
	require.Equal(t, []int64{3, 4}, qualityRuleIDs(plans[1].rules))
	require.Equal(t, []int64{5}, qualityRuleIDs(plans[2].rules))
	require.Equal(t, []int64{6, 7}, qualityRuleIDs(plans[3].rules),
		"a target over the cap is the only one that spans batches")

	// Every plan names the target it belongs to; schedule advancement happens when
	// the cycle settles, not from a plan.
	require.Equal(t, "inst-a;db;public;orders", plans[1].target.TargetGUID)
	require.Equal(t, "inst-a;db;public;orders", plans[2].target.TargetGUID)
	require.Equal(t, "inst-a;db;public;customers", plans[3].target.TargetGUID)
}

func TestPlanDueBatchesOrdersTargetsAsGiven(t *testing.T) {
	t.Parallel()

	targets := []*store.DueQualityTarget{
		dueTargetStub("inst-a;db;public;customers", 1, 3),
		dueTargetStub("inst-a;db;public;orders", 2),
	}
	plans := planDueBatches(targets, 50)
	require.Len(t, plans, 2)
	require.Equal(t, []int64{1, 3}, qualityRuleIDs(plans[0].rules))
	require.Equal(t, []int64{2}, qualityRuleIDs(plans[1].rules),
		"the oldest due target keeps running first instead of a GUID reorder")
}

func TestPlanDueBatchesAttributesMalformedTargetToNoInstance(t *testing.T) {
	t.Parallel()

	plans := planDueBatches([]*store.DueQualityTarget{nil, dueTargetStub("not-a-guid", 7)}, 50)
	require.Len(t, plans, 1)
	require.Empty(t, plans[0].instanceResourceID)
	require.Equal(t, []int64{7}, qualityRuleIDs(plans[0].rules))
}

func TestPlanDueBatchesRejectsNonPositiveBatchSize(t *testing.T) {
	t.Parallel()

	require.Empty(t, planDueBatches([]*store.DueQualityTarget{dueTargetStub("inst;db;public;t", 1)}, 0))
}

// A target at the API's per-table rule cap fits exactly one scheduled batch;
// only data that bypassed that cap can split a target.
func TestScheduledBatchCapacityFitsTheLargestLegalTarget(t *testing.T) {
	t.Parallel()

	ids := make([]int64, 0, 100)
	for id := int64(1); id <= 100; id++ {
		ids = append(ids, id)
	}
	plans := planDueBatches([]*store.DueQualityTarget{dueTargetStub("inst;db;public;orders", ids...)}, maxExecutionsPerBatch)
	require.Len(t, plans, 1, "the largest legal target stays in one batch")
	require.Len(t, plans[0].rules, 100)

	ids = append(ids, 101)
	plans = planDueBatches([]*store.DueQualityTarget{dueTargetStub("inst;db;public;orders", ids...)}, maxExecutionsPerBatch)
	require.Len(t, plans, 2, "only an over-cap target can span batches")
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

// A batch that could not execute a rule must not look like a batch whose rule
// found bad data: ERROR outranks FAIL in the rollup.
func TestCountsAndOutcomeSeparatesErrorFromFail(t *testing.T) {
	t.Parallel()

	counts, outcome := dataquality.CountResults([]storepb.QualityStatus{
		storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
		storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
	})
	require.Equal(t, int32(4), counts.GetTotal())
	require.Equal(t, int32(1), counts.GetPass())
	require.Equal(t, int32(1), counts.GetFail())
	require.Equal(t, int32(1), counts.GetError())
	require.Equal(t, int32(1), counts.GetSkipped())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, outcome)

	counts, outcome = dataquality.CountResults([]storepb.QualityStatus{
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL,
	})
	require.Equal(t, int32(2), counts.GetTotal())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, outcome)

	// A batch with no executions judged nothing; that is not a pass.
	counts, outcome = dataquality.CountResults(nil)
	require.Equal(t, int32(0), counts.GetTotal())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, outcome)
}

func TestManualExecutionTasksDeduplicatesAndDropsInvalidIDs(t *testing.T) {
	t.Parallel()

	tasks := manualExecutionTasks([]int64{5, 0, -2, 5, 7, 7})
	require.Len(t, tasks, 2)
	require.Equal(t, int64(5), tasks[0].executionID)
	require.Equal(t, int64(7), tasks[1].executionID)
	require.Zero(t, tasks[0].ruleID)
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

// A due scan hands one group per instance to a worker; the group keeps the
// planner's order, so an instance's batches run serially.
func TestGroupDueBatchPlansKeepsInstanceOrder(t *testing.T) {
	t.Parallel()

	plans := planDueBatches([]*store.DueQualityTarget{
		dueTargetStub("inst-a;db;public;t1", 1, 2),
		dueTargetStub("inst-b;db;public;t2", 3),
		dueTargetStub("inst-a;db;public;t3", 4),
	}, 10)
	groups := groupDueBatchPlans(plans)

	require.Len(t, groups, 2)
	require.Equal(t, "inst-a", groups[0].instanceResourceID)
	require.Len(t, groups[0].plans, 2, "an instance's plans stay one serial group")
	require.Equal(t, "inst-b", groups[1].instanceResourceID)
	require.Empty(t, groupDueBatchPlans(nil))
}

// An instance whose group is queued or running must not be planned again while it
// waits, or two of its batches could execute concurrently.
func TestDueTargetsSkipAnInstanceThatAlreadyHasAGroup(t *testing.T) {
	t.Parallel()

	runner := &Runner{scheduledInstances: map[string]struct{}{"inst-a": {}}}
	targets := []*store.DueQualityTarget{
		dueTargetStub("inst-a;db;public;t1", 1),
		dueTargetStub("inst-b;db;public;t2", 2),
		dueTargetStub("not-a-guid", 3),
	}
	ready := runner.dueTargetsWithoutBusyInstance(targets)
	require.Len(t, ready, 2)
	require.Equal(t, "inst-b;db;public;t2", ready[0].TargetGUID)
	require.Equal(t, "not-a-guid", ready[1].TargetGUID,
		"an unattributable target belongs to the empty instance")

	// The malformed target's empty instance can be busy too.
	runner = &Runner{scheduledInstances: map[string]struct{}{"": {}}}
	require.Equal(t, []string{"inst-a;db;public;t1", "inst-b;db;public;t2"},
		func() []string {
			guids := make([]string, 0, 2)
			for _, target := range runner.dueTargetsWithoutBusyInstance(targets) {
				guids = append(guids, target.TargetGUID)
			}
			return guids
		}())
}

// Submitting reserves the instance; releasing it is what lets the next tick plan
// its remaining due targets. A full queue must not lose the reservation.
func TestSubmitScheduledGroupReservesAndReleasesTheInstance(t *testing.T) {
	t.Parallel()

	runner := &Runner{
		scheduled:          make(chan scheduledGroup, 1),
		scheduledInstances: make(map[string]struct{}),
	}
	group := scheduledGroup{instanceResourceID: "inst-a", plans: []dueBatchPlan{{instanceResourceID: "inst-a"}}}
	require.True(t, runner.submitScheduledGroup(group))
	require.False(t, runner.submitScheduledGroup(scheduledGroup{instanceResourceID: "inst-a"}),
		"an instance with a queued group is not queued twice")
	require.False(t, runner.submitScheduledGroup(scheduledGroup{instanceResourceID: "inst-b"}),
		"a full queue reports failure")
	require.Len(t, runner.scheduled, 1)

	<-runner.scheduled
	runner.releaseScheduledInstance("inst-a")
	require.True(t, runner.submitScheduledGroup(scheduledGroup{instanceResourceID: "inst-a"}))
}

func TestHasQualityExecutionQueueRoom(t *testing.T) {
	t.Parallel()

	runner := &Runner{manual: make(chan manualRun, 2)}
	require.True(t, runner.HasQualityExecutionQueueRoom(2))
	runner.manual <- manualRun{}
	require.False(t, runner.HasQualityExecutionQueueRoom(2))
	require.True(t, runner.HasQualityExecutionQueueRoom(1))
	require.True(t, runner.HasQualityExecutionQueueRoom(0), "nothing to queue is always fine")
}

func TestEnqueueQualityExecutionsReportsAFullQueue(t *testing.T) {
	t.Parallel()

	runner := &Runner{manual: make(chan manualRun, 1)}
	require.True(t, runner.EnqueueQualityExecutions(1, "inst-1", []int64{3}))

	// The queue is full, so the API must be told instead of blocking the request.
	require.False(t, runner.EnqueueQualityExecutions(2, "inst-1", nil))

	queued := <-runner.manual
	require.Equal(t, int64(1), queued.batchID)
	require.Equal(t, "inst-1", queued.instanceResourceID)
	require.Len(t, queued.executions, 1)
	require.Equal(t, int64(3), queued.executions[0].executionID)
	require.True(t, runner.EnqueueQualityExecutions(3, "inst-1", nil))
}

// The planned row is what a scheduled tick inserts before execution: the due
// scan's revision and target snapshot, PENDING, and the trigger that explains it.
func TestPlannedExecutionCarriesTheDueScanSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 19, 9, 0, 0, 0, time.UTC)
	path := &storepb.QualityTargetPath{Instance: "inst1", Database: "db1", Object: "orders"}
	rule := &store.QualityRuleMessage{
		ID:         7,
		Revision:   3,
		TargetGUID: "inst1;db1;public;orders",
		TargetType: storepb.MetaType_TABLE,
		TargetPath: path,
	}
	dueRule := &store.DueQualityRule{Rule: rule, Attempt: 2}
	planned := plannedExecution(11, "inst1", dueRule, now)

	require.Equal(t, int64(11), planned.RunID)
	require.Equal(t, int64(7), planned.RuleID)
	require.Equal(t, int32(3), planned.RuleRevision)
	require.Equal(t, dataquality.EngineVersion, planned.RuleEngineVersion)
	require.Equal(t, "inst1;db1;public;orders", planned.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, planned.TargetType)
	require.Same(t, path, planned.TargetPath)
	require.Equal(t, "inst1", planned.InstanceResourceID)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING, planned.State)
	require.Equal(t, storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED, planned.Trigger)
	require.Equal(t, int32(2), planned.Attempt)
	require.Equal(t, now, planned.StartedAt)
}

// A batch holds one lease for its whole life, so a second batch on the same
// instance waits for it instead of being admitted above the connection budget.
func TestAdmitBatchHoldsTheInstanceBudgetForTheWholeBatch(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New(nil)
	require.NoError(t, err)
	runner := &Runner{stateCfg: stateCfg}
	instance := &store.InstanceMessage{
		ResourceID: "inst-1",
		Metadata:   &storepb.Instance{MaximumConnections: 1},
	}

	hold, _, admitted := runner.admitBatch(context.Background(), instance, state.ConnectionPriorityBackground, time.Second)
	require.True(t, admitted)

	type admission struct {
		release func()
		ok      bool
	}
	second := make(chan admission, 1)
	go func() {
		release, _, ok := runner.admitBatch(context.Background(), instance, state.ConnectionPriorityBackground, time.Second)
		second <- admission{release: release, ok: ok}
	}()
	select {
	case result := <-second:
		t.Fatalf("a second batch was admitted above the instance budget: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}

	hold()
	select {
	case result := <-second:
		require.True(t, result.ok)
		result.release()
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting batch was never admitted after the budget was released")
	}
}

// A batch whose budget never frees up is deferred, not failed: the caller gets
// false and the rules stay due for the next tick.
func TestAdmitBatchReportsWhenTheBudgetStaysBusy(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New(nil)
	require.NoError(t, err)
	runner := &Runner{stateCfg: stateCfg}
	instance := &store.InstanceMessage{
		ResourceID: "inst-1",
		Metadata:   &storepb.Instance{MaximumConnections: 1},
	}

	hold, _, admitted := runner.admitBatch(context.Background(), instance, state.ConnectionPriorityBackground, time.Second)
	require.True(t, admitted)
	defer hold()

	_, _, admitted = runner.admitBatch(context.Background(), instance, state.ConnectionPriorityBackground, 10*time.Millisecond)
	require.False(t, admitted)
}

// A batch whose instance row is gone needs no lease: target resolution records
// ORPHANED for its rules before a driver is opened.
func TestAdmitBatchDoesNotLeaseAMissingInstance(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New(nil)
	require.NoError(t, err)
	runner := &Runner{stateCfg: stateCfg}

	release, admission, admitted := runner.admitBatch(context.Background(), nil, state.ConnectionPriorityManual, time.Second)
	require.True(t, admitted)
	require.Empty(t, admission.instanceResourceID)
	require.NotNil(t, release)
	release()
}

// A rule retargeted to another instance after its batch was admitted cannot open
// a driver under the batch's lease; it leases the instance it actually resolves
// to, so no driver ever runs outside a leased budget.
func TestRuleConnectionLeaseCoversARetargetedRule(t *testing.T) {
	t.Parallel()

	stateCfg, err := state.New(nil)
	require.NoError(t, err)
	runner := &Runner{stateCfg: stateCfg}
	batchAdmittedForA := batchAdmission{instanceResourceID: "inst-a", priority: state.ConnectionPriorityBackground}
	instanceA := &store.InstanceMessage{ResourceID: "inst-a", Metadata: &storepb.Instance{}}

	release, ok := runner.ruleConnectionLease(instanceA, batchAdmittedForA)
	require.True(t, ok, "the batch's own instance is covered by its lease")
	release()

	instanceB := &store.InstanceMessage{ResourceID: "inst-b", Metadata: &storepb.Instance{MaximumConnections: 1}}
	hold, _, admitted := runner.admitBatch(context.Background(), instanceB, state.ConnectionPriorityBackground, time.Second)
	require.True(t, admitted)

	_, ok = runner.ruleConnectionLease(instanceB, batchAdmittedForA)
	require.False(t, ok, "a retargeted rule cannot take another instance's budget without a free slot")

	hold()
	release, ok = runner.ruleConnectionLease(instanceB, batchAdmittedForA)
	require.True(t, ok)
	release()

	// A batch admitted without a lease (its instance row was gone) still leases
	// the instance its rule really resolves to.
	release, ok = runner.ruleConnectionLease(instanceA, batchAdmission{})
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

func TestBuildExecutionMessageStoresTheVerdictAndItsEvidence(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 19, 9, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(1500 * time.Millisecond)
	planned := &store.QualityExecutionMessage{
		ID:                 11,
		RunID:              3,
		RuleID:             7,
		RuleRevision:       1,
		TargetGUID:         "inst;db;public;orders",
		TargetType:         storepb.MetaType_TABLE,
		TargetPath:         &storepb.QualityTargetPath{Instance: "inst", Object: "orders"},
		InstanceResourceID: "inst",
		Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
	}
	snapshot := &dataquality.RuleSnapshot{
		ID:       7,
		Revision: 4,
		Target: dataquality.Target{
			GUID:     "inst;db;public;orders",
			MetaType: storepb.MetaType_TABLE,
			Path:     &storepb.QualityTargetPath{Instance: "inst", Database: "db", Object: "orders"},
		},
	}
	execution := &dataquality.Execution{
		Samples:     &storepb.QualityFailureSamples{Samples: []*storepb.FailureSample{{}}},
		ExecutedSQL: "SELECT COUNT(*) FROM orders",
		Sampled:     true,
		Truncated:   true,
	}
	thresholds := &storepb.QualityThresholds{}
	result := buildExecutionMessage(planned, &ruleExecutionOutcome{
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

	require.Equal(t, int64(11), result.ID)
	require.Equal(t, int64(3), result.RunID)
	require.Equal(t, int64(7), result.RuleID)
	// The executed snapshot wins over the planned row: an edit never rewrites the
	// verdict's definition.
	require.Equal(t, int32(4), result.RuleRevision)
	require.Equal(t, dataquality.EngineVersion, result.RuleEngineVersion)
	require.Equal(t, "inst;db;public;orders", result.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, result.TargetType)
	require.Equal(t, "db", result.TargetPath.GetDatabase())
	require.Equal(t, "inst", result.InstanceResourceID)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, result.State)
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

// A verdict that failed before a snapshot was read still closes the row: the
// planned revision, target and engine stay on it, so the row explains what was
// attempted and never disappears.
func TestFailureExecutionMessageKeepsThePlannedSnapshot(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 19, 9, 0, 0, 0, time.UTC)
	planned := &store.QualityExecutionMessage{
		ID:           11,
		RunID:        3,
		RuleID:       7,
		RuleRevision: 2,
		TargetGUID:   "inst;db;public;orders",
		TargetType:   storepb.MetaType_TABLE,
		TargetPath:   &storepb.QualityTargetPath{Instance: "inst", Object: "orders"},
		Trigger:      storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_MANUAL,
	}
	long := make([]byte, maxErrorMessageLength+100)
	for i := range long {
		long[i] = 'x'
	}
	result := failureExecutionMessage(planned, nil, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
		dataquality.ErrCodeExecution, string(long))

	require.Equal(t, int64(11), result.ID)
	require.Equal(t, int64(3), result.RunID)
	require.Equal(t, int64(7), result.RuleID)
	require.Equal(t, int32(2), result.RuleRevision)
	require.Equal(t, "inst;db;public;orders", result.TargetGUID)
	require.Equal(t, storepb.MetaType_TABLE, result.TargetType)
	require.Equal(t, "orders", result.TargetPath.GetObject())
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, result.State)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, result.Status)
	require.Nil(t, result.ObservedValue)
	require.Len(t, result.ErrorMessage, maxErrorMessageLength)
	require.Equal(t, dataquality.ErrCodeExecution, result.ErrorCode)
	require.Equal(t, startedAt, result.StartedAt)
}

func TestObservedValueIsAbsentForUnjudgedExecutions(t *testing.T) {
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

func TestBaselineFromExecutionsSkipsUnjudgedVerdicts(t *testing.T) {
	t.Parallel()

	observed := 42.0
	// The newest execution is an ERROR; the last evaluated one is the baseline.
	executions := []*store.QualityExecutionMessage{
		{Status: storepb.QualityStatus_QUALITY_STATUS_ERROR, ObservedValue: &observed},
		{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, ObservedValue: &observed},
	}
	now := time.Now().UTC()
	executions[1].StartedAt = now.Add(-time.Minute)
	baseline := baselineFromExecutions(executions, now, time.Hour)
	require.NotNil(t, baseline)
	require.InDelta(t, 42, *baseline, 1e-9)

	require.Nil(t, baselineFromExecutions(nil, now, time.Hour))
	require.Nil(t, baselineFromExecutions([]*store.QualityExecutionMessage{
		{Status: storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED, ObservedValue: &observed},
	}, now, time.Hour))

	// A week-old observation, or one more than two intervals behind, is no
	// longer a fair denominator.
	stale := []*store.QualityExecutionMessage{
		{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, ObservedValue: &observed, StartedAt: now.Add(-8 * 24 * time.Hour)},
	}
	require.Nil(t, baselineFromExecutions(stale, now, 24*time.Hour))
	drifted := []*store.QualityExecutionMessage{
		{Status: storepb.QualityStatus_QUALITY_STATUS_PASS, ObservedValue: &observed, StartedAt: now.Add(-3 * time.Hour)},
	}
	require.Nil(t, baselineFromExecutions(drifted, now, time.Hour))
	require.NotNil(t, baselineFromExecutions(drifted, now, 4*time.Hour))
}

func TestErrorCodeAndMessageMapsRunnerLevelFailures(t *testing.T) {
	t.Parallel()

	code, _ := errorCodeAndMessage(dbfactory.ErrNoReadOnlyDataSource)
	require.Equal(t, dataquality.ErrCodeNoReadOnly, code)

	// Capacity contention is not a rule failure: the runner defers the batch
	// before it executes rather than turning the limiter's error into a QUOTA
	// verdict.
	code, _ = errorCodeAndMessage(state.ErrInstanceConnectionLimit)
	require.NotEqual(t, dataquality.ErrCodeQuota, code)

	code, message := errorCodeAndMessage(errors.New("the target refused the statement"))
	require.Equal(t, dataquality.ErrCodeExecution, code)
	require.Equal(t, "the target refused the statement", message)
}

func qualityPtr[T any](value T) *T {
	return &value
}

// ruleExecutionStub is one evaluated FAIL execution with every input the problem
// ledger and the notification builders read.
func ruleExecutionStub(status storepb.QualityStatus) *ruleExecution {
	observed := 7.0
	previous := storepb.QualityStatus_QUALITY_STATUS_FAIL
	return &ruleExecution{
		result: &store.QualityExecutionMessage{
			ID:            101,
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
		// the failed execution on the executions page, not in their inbox.
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

// Only FAIL executions make a storm: a batch that could not execute its rules is
// a different alert, and it must not merge into the data-quality summary.
func TestIsQualityStorm(t *testing.T) {
	t.Parallel()

	executions := func(statuses ...storepb.QualityStatus) []*ruleExecution {
		out := make([]*ruleExecution, 0, len(statuses))
		for _, status := range statuses {
			out = append(out, &ruleExecution{result: qualityExecutionStub(status)})
		}
		return out
	}

	atThreshold := make([]storepb.QualityStatus, 0, QualityStormThreshold)
	for range QualityStormThreshold {
		atThreshold = append(atThreshold, storepb.QualityStatus_QUALITY_STATUS_FAIL)
	}
	require.False(t, isQualityStorm(executions(atThreshold...)))

	overThreshold := make([]storepb.QualityStatus, 0, len(atThreshold)+1)
	overThreshold = append(overThreshold, atThreshold...)
	overThreshold = append(overThreshold, storepb.QualityStatus_QUALITY_STATUS_FAIL)
	require.True(t, isQualityStorm(executions(overThreshold...)))

	errors := make([]storepb.QualityStatus, 0, QualityStormThreshold+1)
	for range QualityStormThreshold + 1 {
		errors = append(errors, storepb.QualityStatus_QUALITY_STATUS_ERROR)
	}
	require.False(t, isQualityStorm(executions(errors...)))
}

func TestBuildDataQualityDetailForAnOpenedIssue(t *testing.T) {
	t.Parallel()

	execution := ruleExecutionStub(storepb.QualityStatus_QUALITY_STATUS_FAIL)
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     "ws1",
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		execution:       execution,
		issueResourceID: "issue1",
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
	require.Equal(t, "workspaces/ws1/qualityExecutions/101", detail.GetExecution())
	require.Empty(t, detail.GetErrorCode())
}

func TestBuildDataQualityDetailForARecovery(t *testing.T) {
	t.Parallel()

	execution := ruleExecutionStub(storepb.QualityStatus_QUALITY_STATUS_PASS)
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     "ws1",
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED,
		execution:       execution,
		issueResourceID: "issue1",
	})
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_FAIL, detail.GetFromStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_PASS, detail.GetToStatus())
}

func TestBuildDataQualityDetailForAnExecutionFailure(t *testing.T) {
	t.Parallel()

	execution := ruleExecutionStub(storepb.QualityStatus_QUALITY_STATUS_ERROR)
	execution.result.ObservedValue = nil
	execution.result.ErrorCode = dataquality.ErrCodeTimeout
	execution.previous = nil
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID: "ws1",
		kind:        storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		execution:   execution,
	})

	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, detail.GetFromStatus())
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_ERROR, detail.GetToStatus())
	require.Equal(t, dataquality.ErrCodeTimeout, detail.GetErrorCode())
	require.Zero(t, detail.GetObservedValue())
	require.Empty(t, detail.GetIssue())
	require.Equal(t, "workspaces/ws1/qualityExecutions/101", detail.GetExecution())
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
	// A storm does not use this helper: it is deduped by the internal batch id
	// through sendDataQualityNotificationFor, so no resource target is derived.
	require.Empty(t, qualityNotificationTarget(&storepb.DataQualityDetail{
		Kind:     storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY,
		Instance: "instances/inst1",
	}))
	require.Empty(t, qualityNotificationTarget(&storepb.DataQualityDetail{
		Kind: storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED,
	}))
}

func TestBuildStormDetail(t *testing.T) {
	t.Parallel()

	detail := buildStormDetail("instances/inst1", "inst1;shop;public;orders", "prod.shop.public.orders", 12, 40)
	require.Equal(t, storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY, detail.GetKind())
	require.Equal(t, "instances/inst1", detail.GetInstance())
	require.Equal(t, "inst1;shop;public;orders", detail.GetTargetGuid())
	require.Equal(t, "prod.shop.public.orders", detail.GetTargetPath())
	require.Empty(t, detail.GetExecution())
	require.Equal(t, int32(12), detail.GetFailedRuleCount())
	require.Equal(t, int32(40), detail.GetTotalRuleCount())
}

// The runner formats execution names the way the API addresses a row: the last
// segment is the numeric execution id.
func TestFormatQualityExecutionName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "workspaces/ws1/qualityExecutions/101", formatQualityExecutionName("ws1", 101))
	require.Equal(t, "qualityExecutions/101", formatQualityExecutionName("", 101))
	require.Empty(t, formatQualityExecutionName("ws1", 0))
}

// Instance resource names are top-level; a storm summary names the instance the
// same way the API does.
func TestFormatInstanceResourceName(t *testing.T) {
	t.Parallel()

	require.Equal(t, "instances/inst1", formatInstanceResourceName("inst1"))
	require.Empty(t, formatInstanceResourceName(""))
}

// PlanQualityExecutions fills in each planned row's id; the runner must derive
// tasks from that identity and drop a row the store did not identify.
func TestPlannedTasksFromMessages(t *testing.T) {
	t.Parallel()

	tasks := plannedTasksFromMessages([]*store.QualityExecutionMessage{
		{ID: 101, RuleID: 7},
		nil,
		{ID: 0, RuleID: 8},
		{ID: 103, RuleID: 9},
	})
	require.Len(t, tasks, 2)
	require.Equal(t, executionTask{executionID: 101, ruleID: 7}, tasks[0])
	require.Equal(t, executionTask{executionID: 103, ruleID: 9}, tasks[1])
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

// fakeBatchStore is the batchExecutionStore the accounting tests drive. It only
// implements what executeBatchWith touches when no rule is actually executed;
// that is exactly the incomplete and DB-error paths H1 is about.
type fakeBatchStore struct {
	markBatchErr       error
	planned            map[int64]*store.QualityExecutionMessage
	loadErr            map[int64]error
	markExecutionErr   map[int64]error
	finishExecutionErr error
	abandonErr         error
	abandonRows        int64
	abandonCalls       []int64
	finishBatchCalls   []fakeFinishBatchCall
}

type fakeFinishBatchCall struct {
	status  storepb.QualityExecutionState
	outcome storepb.QualityStatus
	counts  *storepb.QualityStatusCounts
}

func (f *fakeBatchStore) MarkQualityRunRunning(_ context.Context, _ int64, _ time.Time) error {
	return f.markBatchErr
}

func (f *fakeBatchStore) GetQualityExecution(_ context.Context, id int64) (*store.QualityExecutionMessage, error) {
	if err := f.loadErr[id]; err != nil {
		return nil, err
	}
	return f.planned[id], nil
}

func (*fakeBatchStore) ListRecentQualityExecutions(_ context.Context, _ int64, _ int) ([]*store.QualityExecutionMessage, error) {
	return nil, nil
}

func (f *fakeBatchStore) MarkQualityExecutionRunning(_ context.Context, id int64, _ time.Time) error {
	return f.markExecutionErr[id]
}

func (f *fakeBatchStore) FinishQualityExecution(_ context.Context, _ *store.QualityExecutionMessage) error {
	return f.finishExecutionErr
}

func (f *fakeBatchStore) AbandonQualityExecutions(_ context.Context, batchID int64, _ time.Time) (int64, error) {
	f.abandonCalls = append(f.abandonCalls, batchID)
	return f.abandonRows, f.abandonErr
}

func (f *fakeBatchStore) FinishQualityRun(_ context.Context, _ int64, status storepb.QualityExecutionState, outcome storepb.QualityStatus, counts *storepb.QualityStatusCounts, _ time.Time) error {
	f.finishBatchCalls = append(f.finishBatchCalls, fakeFinishBatchCall{status: status, outcome: outcome, counts: counts})
	return nil
}

func newAccountingRunner() *Runner {
	return &Runner{metrics: metrics.New()}
}

// TestExecuteBatchIncompleteAbandonsUnfinishedRows is the H1 regression test: a
// skipped task (unreadable row, or a row another writer already closed) must turn
// the whole batch ABANDONED, never a partial COMPLETED rollup, and the rows the
// batch still owns must be abandoned.
func TestExecuteBatchIncompleteAbandonsUnfinishedRows(t *testing.T) {
	t.Parallel()

	fake := &fakeBatchStore{
		planned: map[int64]*store.QualityExecutionMessage{
			12: {ID: 12, RunID: 5, RuleID: 7, State: storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING},
		},
		loadErr:          map[int64]error{11: errors.New("read failed")},
		markExecutionErr: map[int64]error{12: errors.New("already closed")},
		// Deliberately fewer rows than tasks: the batch total must come from the
		// plan, not from what the abandon UPDATE happened to change.
		abandonRows: 1,
	}
	tasks := []executionTask{{executionID: 11, ruleID: 7}, {executionID: 12, ruleID: 7}}

	newAccountingRunner().executeBatchWith(context.Background(), fake, 5, "inst-1", tasks, batchAdmission{}, func() {})

	require.Equal(t, []int64{5}, fake.abandonCalls, "the batch's unfinished rows are abandoned")
	require.Len(t, fake.finishBatchCalls, 1)
	finished := fake.finishBatchCalls[0]
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, finished.status,
		"an incomplete batch must never be reported COMPLETED")
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, finished.outcome,
		"an incomplete batch has no rollup outcome")
	require.Equal(t, int32(len(tasks)), finished.counts.GetTotal(), "the batch accounts for every planned execution")
}

// TestExecuteBatchMarkRunningFailureAbandonsBatch covers the DB-error residual:
// when the batch cannot even be marked RUNNING, its planned rows must not linger
// as PENDING waiting for the next startup sweep.
func TestExecuteBatchMarkRunningFailureAbandonsBatch(t *testing.T) {
	t.Parallel()

	fake := &fakeBatchStore{
		markBatchErr: errors.New("batch already terminal"),
		abandonRows:  1,
	}
	tasks := []executionTask{{executionID: 21, ruleID: 7}}

	newAccountingRunner().executeBatchWith(context.Background(), fake, 6, "inst-1", tasks, batchAdmission{}, func() {})

	require.Equal(t, []int64{6}, fake.abandonCalls)
	require.Len(t, fake.finishBatchCalls, 1)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, fake.finishBatchCalls[0].status)
	require.Equal(t, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, fake.finishBatchCalls[0].outcome)
}

// TestExecuteBatchAbandonWriteFailureStillClosesBatch pins the second DB-error
// residual: a failed abandon UPDATE leaves the rows to the startup sweep, but the
// batch itself is still closed instead of staying RUNNING forever.
func TestExecuteBatchAbandonWriteFailureStillClosesBatch(t *testing.T) {
	t.Parallel()

	fake := &fakeBatchStore{
		loadErr:     map[int64]error{31: errors.New("read failed")},
		abandonErr:  errors.New("update failed"),
		abandonRows: 0,
	}
	tasks := []executionTask{{executionID: 31, ruleID: 7}}

	newAccountingRunner().executeBatchWith(context.Background(), fake, 7, "inst-1", tasks, batchAdmission{}, func() {})

	require.Equal(t, []int64{7}, fake.abandonCalls, "the abandon is still attempted")
	require.Len(t, fake.finishBatchCalls, 1)
	require.Equal(t, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, fake.finishBatchCalls[0].status,
		"the batch must be closed even when the row abandon failed")
}

// TestBatchContractFiltersReadsEachTargetOnce pins the batch-level contract cache:
// rules of one target share a single read, a different object type (or target) is
// read separately, a metadata rule reads nothing (it compiles no SQL), and a failed
// read is returned without poisoning the cache.
func TestBatchContractFiltersReadsEachTargetOnce(t *testing.T) {
	t.Parallel()

	type readCall struct {
		guid     string
		metaType storepb.MetaType
	}
	var calls []readCall
	usersFilter := &storepb.QualityFilter{Clauses: []*storepb.QualityFilterClause{{
		Column: "id",
		Op:     storepb.QualityFilterOperator_QUALITY_FILTER_OPERATOR_GT,
		Values: []string{"0"},
	}}}
	contracts := newBatchContractFilters(func(_ context.Context, guid string, metaType storepb.MetaType) (*storepb.QualityFilter, error) {
		calls = append(calls, readCall{guid: guid, metaType: metaType})
		switch guid {
		case "users":
			return usersFilter, nil
		case "broken":
			return nil, errors.New("contract read failed")
		default:
			return nil, nil
		}
	})

	filter, err := contracts.forRule(context.Background(),
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "users", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, usersFilter, filter)
	require.Len(t, calls, 1)

	// A second rule on the same target reuses the cached filter.
	filter, err = contracts.forRule(context.Background(),
		storepb.QualityRuleType_QUALITY_RULE_TYPE_NULL_PERCENT, "users", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, usersFilter, filter)
	require.Len(t, calls, 1, "the same target is read once per batch")

	// A target without a contract row caches the absence of a filter.
	filter, err = contracts.forRule(context.Background(),
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "orders", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Nil(t, filter)
	require.Len(t, calls, 2)
	_, err = contracts.forRule(context.Background(),
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "orders", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Len(t, calls, 2)

	// The GUID alone is not the identity: a different object type is another target.
	_, err = contracts.forRule(context.Background(),
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "users", storepb.MetaType_VIEW)
	require.NoError(t, err)
	require.Len(t, calls, 3)

	// A metadata rule never reads the contract at all.
	filter, err = contracts.forRule(context.Background(),
		storepb.QualityRuleType_QUALITY_RULE_TYPE_SCHEMA_DRIFT, "users", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Nil(t, filter)
	require.Len(t, calls, 3, "schema_drift compiles no SQL, so it needs no contract filter")

	// A failed read is returned and retried rather than cached as "no filter".
	_, err = contracts.forRule(context.Background(),
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "broken", storepb.MetaType_TABLE)
	require.Error(t, err)
	_, err = contracts.forRule(context.Background(),
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT, "broken", storepb.MetaType_TABLE)
	require.Error(t, err)
	require.Len(t, calls, 5)
}

// TestLoadBatchContractFilterWithoutAStoreIsNoFilter keeps the accounting runner
// (which has no store) from panicking if a future test ever executes a rule.
func TestLoadBatchContractFilterWithoutAStoreIsNoFilter(t *testing.T) {
	t.Parallel()

	r := &Runner{}
	filter, err := r.loadBatchContractFilter()(context.Background(), "users", storepb.MetaType_TABLE)
	require.NoError(t, err)
	require.Nil(t, filter)
}
