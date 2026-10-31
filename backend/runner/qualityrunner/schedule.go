package qualityrunner

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// scanDue plans one internal batch per instance for the targets whose schedule is
// due and hands each instance's plans to a scheduled worker. The scan never
// executes a batch itself, so the runner loop stays available for manual requests
// while the workers run; a full scheduled queue leaves the rest of the pass due for
// the next tick. A due target may exceed the per-batch limit: its checks are
// grouped with the target, chunked into batches of at most maxExecutionsPerBatch,
// and one instance's batches execute serially while at most two instances execute
// at once.
//
// The store's due query selects whole targets in first-due order until the work
// budget is met, so a target's enabled checks never straddle two scheduler ticks
// just because the budget fell inside its group.
func (r *Runner) scanDue(ctx context.Context) {
	// The planning cost is settings refresh, the quota check, the due-target query
	// and the chunking. Executing the batches belongs to the workers, and including
	// it would turn this histogram into a copy of batch duration.
	planningStartedAt := time.Now()
	observePlanning := func() { r.metrics.ObserveQualityDueScan(time.Since(planningStartedAt)) }

	now := time.Now().UTC()

	r.refreshSettings(ctx)
	if r.overDailyExecutionQuota(ctx, now) {
		observePlanning()
		return
	}
	targets, err := r.stores.FindDueQualityTargets(ctx, now, dueRuleScanLimit, dueTargetScanLimit, maxScheduledRuleAttempts)
	if err != nil {
		observePlanning()
		slog.Error("failed to list due quality targets", log.WithError(err))
		return
	}
	dueRules := 0
	for _, target := range targets {
		dueRules += len(target.Rules)
	}
	r.metrics.ObserveQualityDueRules(dueRules)
	if len(targets) == 0 {
		observePlanning()
		return
	}

	// A target with no pending rule has a settled cycle: every enabled check has a
	// completed attempt or its retry budget is used. Advance its schedule instead of
	// creating an empty batch.
	pendingTargets := make([]*store.DueQualityTarget, 0, len(targets))
	for _, target := range targets {
		if len(target.Rules) == 0 {
			r.advanceSettledTarget(ctx, target)
			continue
		}
		pendingTargets = append(pendingTargets, target)
	}
	if len(pendingTargets) == 0 {
		observePlanning()
		return
	}

	// A target whose instance still has a queued or running group is planned after
	// that group finishes, so an instance never executes two batches at once.
	pendingTargets = r.dueTargetsWithoutBusyInstance(pendingTargets)
	if len(pendingTargets) == 0 {
		observePlanning()
		return
	}

	groups := groupDueBatchPlans(planDueBatches(pendingTargets, maxExecutionsPerBatch))
	observePlanning()
	for _, group := range groups {
		if !r.submitScheduledGroup(group) {
			return
		}
	}
}

// dueTargetInstance attributes a due target to its instance the same way the
// planner groups plans; a malformed GUID belongs to the empty instance.
func dueTargetInstance(target *store.DueQualityTarget) string {
	if target == nil {
		return ""
	}
	instance, ok := common.GetInstanceFromGUID(target.TargetGUID)
	if !ok {
		return ""
	}
	return instance
}

// dueTargetsWithoutBusyInstance drops targets whose instance already has a queued
// or running scheduled group. They stay due and are planned after that group
// finishes.
func (r *Runner) dueTargetsWithoutBusyInstance(targets []*store.DueQualityTarget) []*store.DueQualityTarget {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.scheduledInstances) == 0 {
		return targets
	}
	ready := make([]*store.DueQualityTarget, 0, len(targets))
	for _, target := range targets {
		if _, busy := r.scheduledInstances[dueTargetInstance(target)]; busy {
			continue
		}
		ready = append(ready, target)
	}
	return ready
}

// groupDueBatchPlans folds the planner's consecutive per-instance plans into the
// groups a worker executes serially. planDueBatches already orders plans by
// instance, so this only splits where the instance changes.
func groupDueBatchPlans(plans []dueBatchPlan) []scheduledGroup {
	groups := make([]scheduledGroup, 0, len(plans))
	for _, plan := range plans {
		if last := len(groups) - 1; last >= 0 && groups[last].instanceResourceID == plan.instanceResourceID {
			groups[last].plans = append(groups[last].plans, plan)
			continue
		}
		groups = append(groups, scheduledGroup{
			instanceResourceID: plan.instanceResourceID,
			plans:              []dueBatchPlan{plan},
		})
	}
	return groups
}

// submitScheduledGroup reserves the group's instance and hands the group to a
// worker. It reports false when the queue is full, which leaves the rest of the
// pass due for the next tick.
func (r *Runner) submitScheduledGroup(group scheduledGroup) bool {
	r.mu.Lock()
	if _, busy := r.scheduledInstances[group.instanceResourceID]; busy {
		r.mu.Unlock()
		return false
	}
	r.scheduledInstances[group.instanceResourceID] = struct{}{}
	r.mu.Unlock()

	select {
	case r.scheduled <- group:
		return true
	default:
		r.releaseScheduledInstance(group.instanceResourceID)
		return false
	}
}

// releaseScheduledInstance frees an instance after its group finished or could not
// be queued, so the next tick may plan its remaining due targets.
func (r *Runner) releaseScheduledInstance(instanceResourceID string) {
	r.mu.Lock()
	delete(r.scheduledInstances, instanceResourceID)
	r.mu.Unlock()
}

// startScheduledBatch creates one internal batch and its planned execution rows,
// then executes them. The batch row exists before any rule runs, so an execution
// always names a batch; the execution rows themselves are the visible plan.
//
// It reports whether the caller may continue with the instance's remaining plans
// in the group. A deferral or a store failure returns false because the instance
// is not making progress: trying the next due target on the same instance would
// only multiply the admission wait (up to scheduledAdmissionWait per target). The
// skipped targets stay due and are retried on a later tick. A plan whose target is
// no longer due (its cycle settled or the user postponed it while the plan waited
// for a worker) is skipped without executing.
func (r *Runner) startScheduledBatch(ctx context.Context, plan dueBatchPlan) bool {
	rules := make([]*store.DueQualityRule, 0, len(plan.rules))
	for _, rule := range plan.rules {
		if rule != nil && rule.Rule != nil {
			rules = append(rules, rule)
		}
	}
	if len(rules) == 0 || plan.target == nil {
		return true
	}

	// The plan may have waited in the scheduled queue while another group finished
	// its cycle and advanced this target's schedule, or while the user postponed it.
	// The due query's snapshot is stale by then; executing it would duplicate a
	// completed cycle or ignore the edit.
	schedule, err := r.stores.GetQualityTargetSchedule(ctx, plan.target.TargetGUID, plan.target.TargetType)
	if err != nil {
		slog.Warn("failed to re-read a due target's schedule; leaving its rules due",
			slog.String("target", plan.target.TargetGUID),
			log.WithError(err))
		return false
	}
	dueAt := time.Now().UTC()
	if schedule == nil || schedule.NextRunAt == nil || schedule.NextRunAt.After(dueAt) {
		slog.Debug("skipped a scheduled quality batch: its target is no longer due",
			slog.String("target", plan.target.TargetGUID))
		return true
	}

	instance, err := r.instanceForAdmission(ctx, plan.instanceResourceID)
	if err != nil {
		r.metrics.CountQualityExecutionsDeferred(metrics.QualityDeferralInstanceReadError)
		slog.Warn("failed to read the instance of a scheduled quality batch; leaving its rules due",
			slog.String("instance", plan.instanceResourceID),
			log.WithError(err))
		return false
	}
	release, admission, admitted := r.admitBatch(ctx, instance, state.ConnectionPriorityBackground, scheduledAdmissionWait)
	r.metrics.CountQualityExecutionAdmission(metrics.QualityTriggerScheduled, admitted)
	if !admitted {
		// The batch was never created and no rule's schedule was advanced, so the
		// next tick retries the same rules. That is the scheduler's queue: the
		// work is healthy, the instance is busy.
		r.metrics.CountQualityExecutionsDeferred(metrics.QualityDeferralAdmissionTimeout)
		slog.Debug("deferred a scheduled quality batch: the instance has no free connection slot",
			slog.String("instance", plan.instanceResourceID),
			slog.Int("rules", len(rules)))
		return false
	}
	// The release is idempotent and executeBatch releases it before the store work,
	// so this defer only covers the returns before executeBatch is reached.
	defer release()

	now := time.Now().UTC()
	resourceID, err := common.RandomString(12)
	if err != nil {
		slog.Error("failed to generate an internal quality batch resource id", log.WithError(err))
		return false
	}
	targetPath := plan.target.TargetPath
	if targetPath == nil {
		targetPath = &storepb.QualityTargetPath{}
	}
	batch, err := r.stores.CreateQualityRun(ctx, &store.QualityRunMessage{
		ResourceID:         resourceID,
		InstanceResourceID: plan.instanceResourceID,
		TargetGUID:         plan.target.TargetGUID,
		TargetType:         plan.target.TargetType,
		TargetPath:         targetPath,
		Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
		Status:             storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
	})
	if err != nil {
		slog.Error("failed to create a scheduled quality batch",
			slog.String("instance", plan.instanceResourceID),
			log.WithError(err))
		return false
	}

	planned := make([]*store.QualityExecutionMessage, 0, len(rules))
	for _, rule := range rules {
		planned = append(planned, plannedExecution(batch.ID, plan.instanceResourceID, rule, now))
	}
	if err := r.stores.PlanQualityExecutions(ctx, planned); err != nil {
		slog.Error("failed to plan a scheduled quality batch's executions",
			slog.Int64("batch", batch.ID),
			slog.Int("executions", len(planned)),
			log.WithError(err))
		for range planned {
			r.metrics.CountQualityExecutionAbandoned(metrics.QualityAbandonedPlanFailed)
		}
		r.abandonEmptyBatch(ctx, batch.ID)
		return false
	}
	for range planned {
		r.metrics.CountQualityExecutionCreated(metrics.QualityTriggerScheduled)
	}

	// PlanQualityExecutions fills in each planned row's identity, so the tasks come
	// from the plan itself without another read.
	tasks := plannedTasksFromMessages(planned)
	if len(tasks) != len(planned) {
		slog.Error("a planned quality execution came back without a row id",
			slog.Int64("batch", batch.ID),
			slog.Int("planned", len(planned)),
			slog.Int("identified", len(tasks)))
		r.abandonBatch(ctx, r.stores, batch.ID, len(planned), metrics.QualityAbandonedPlanFailed)
		return false
	}
	r.executeBatch(ctx, batch.ID, plan.instanceResourceID, tasks, admission, release)
	// A timed-out/incomplete batch leaves ABANDONED executions whose retry budget is
	// not used yet, so the cycle stays pending and the next tick retries only those
	// checks. A normal batch settles the cycle here and advances the target now.
	r.advanceTargetIfCycleComplete(ctx, plan.target)
	return true
}

// advanceSettledTarget moves a settled target's next run forward. The write only
// applies while the schedule is still due, so a concurrent interval edit wins. A
// failure is logged, not fatal: the target remains due and the next tick retries
// the same settled cycle.
func (r *Runner) advanceSettledTarget(ctx context.Context, target *store.DueQualityTarget) {
	if target == nil {
		return
	}
	now := time.Now().UTC()
	next := nextRunAt(now, target.ScheduleInterval)
	advanced, err := r.stores.AdvanceDueQualityTargetNextRun(ctx, target.TargetGUID, target.TargetType, next, now)
	if err != nil {
		slog.Warn("failed to advance a quality target's next schedule",
			slog.String("target", target.TargetGUID),
			log.WithError(err))
		return
	}
	if advanced {
		slog.Debug("advanced a settled quality target cycle",
			slog.String("target", target.TargetGUID),
			slog.Int("pending_rules", 0))
	}
}

// advanceTargetIfCycleComplete advances a target when none of its enabled rules
// still needs an attempt in the current cycle. A batch that timed out leaves
// ABANDONED executions, which count as attempts until the retry budget is used;
// the target then stays due and the next tick retries only the unfinished checks.
// A target with a remaining chunk (a >100-rule target) also stays due.
func (r *Runner) advanceTargetIfCycleComplete(ctx context.Context, target *store.DueQualityTarget) {
	if target == nil {
		return
	}
	pending, err := r.stores.CountPendingQualityTargetRules(ctx, target.TargetGUID, target.TargetType, maxScheduledRuleAttempts)
	if err != nil {
		slog.Warn("failed to count a quality target's pending rules",
			slog.String("target", target.TargetGUID),
			log.WithError(err))
		return
	}
	if pending > 0 {
		return
	}
	r.advanceSettledTarget(ctx, target)
}

// targetScheduleInterval returns the interval a row_count_change baseline must
// still fit inside. The baseline window follows the target's cadence, because
// scheduling is per target; a missing schedule falls back to the runner's
// minimum so a malformed row cannot make every historical execution acceptable.
func (r *Runner) targetScheduleInterval(ctx context.Context, targetGUID string, metaType storepb.MetaType) time.Duration {
	schedule, err := r.stores.GetQualityTargetSchedule(ctx, targetGUID, metaType)
	if err != nil {
		slog.Warn("failed to read a quality target's schedule for a baseline",
			slog.String("target", targetGUID),
			log.WithError(err))
		return minRuleScheduleInterval
	}
	if schedule == nil || schedule.ScheduleInterval <= 0 {
		return minRuleScheduleInterval
	}
	return schedule.ScheduleInterval
}
