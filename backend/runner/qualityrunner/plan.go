package qualityrunner

import (
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// plannedExecution is the PENDING row the scheduler plans before executing a due
// rule. The revision, target snapshot and instance are the due scan's view; the
// runner may execute a newer revision and FinishQualityExecution then records the
// revision it really judged.
func plannedExecution(batchID int64, instanceResourceID string, dueRule *store.DueQualityRule, at time.Time) *store.QualityExecutionMessage {
	rule := dueRule.Rule
	return &store.QualityExecutionMessage{
		RunID:              batchID,
		RuleID:             rule.ID,
		RuleRevision:       rule.Revision,
		RuleEngineVersion:  dataquality.EngineVersion,
		TargetGUID:         rule.TargetGUID,
		TargetType:         rule.TargetType,
		TargetPath:         rule.TargetPath,
		InstanceResourceID: instanceResourceID,
		State:              storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING,
		Trigger:            storepb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
		Attempt:            dueRule.Attempt,
		StartedAt:          at,
	}
}

// plannedTasksFromMessages builds the task list from the planned rows. The
// execution id is the public resource identity, and PlanQualityExecutions fills it
// in on the messages it inserted.
func plannedTasksFromMessages(planned []*store.QualityExecutionMessage) []executionTask {
	tasks := make([]executionTask, 0, len(planned))
	for _, execution := range planned {
		if execution == nil || execution.ID <= 0 {
			continue
		}
		tasks = append(tasks, executionTask{executionID: execution.ID, ruleID: execution.RuleID})
	}
	return tasks
}

// nextRunAt is when the scheduler considers the target next. It is the target's
// interval from now, not from its previous due time: a late execution must not
// queue up catch-up runs.
func nextRunAt(now time.Time, interval time.Duration) time.Time {
	if interval < minRuleScheduleInterval {
		// The API validates the interval at write time; this backstop keeps a bad
		// row (for example an interval of zero) from becoming due on every tick.
		interval = minRuleScheduleInterval
	}
	return now.Add(interval)
}

// planDueBatches groups the due targets by instance and turns each target into
// its own batch (a target larger than the cap is split into several batches).
// Instance order is the order the targets became due in, so the oldest target is
// handled first and the resulting plans keep each instance's batches adjacent for
// serial execution. A target is never packed with another target: the batch's
// identity is the target.
func planDueBatches(targets []*store.DueQualityTarget, maxExecutionsPerBatch int) []dueBatchPlan {
	if maxExecutionsPerBatch <= 0 {
		return nil
	}
	instanceOrder := make([]string, 0, len(targets))
	byInstance := make(map[string][]*store.DueQualityTarget, len(targets))
	for _, target := range targets {
		if target == nil {
			continue
		}
		instance, ok := common.GetInstanceFromGUID(target.TargetGUID)
		if !ok {
			// A malformed target GUID is still executed (it resolves to ORPHANED),
			// but it cannot be attributed to an instance.
			instance = ""
		}
		if _, seen := byInstance[instance]; !seen {
			instanceOrder = append(instanceOrder, instance)
		}
		byInstance[instance] = append(byInstance[instance], target)
	}

	plans := make([]dueBatchPlan, 0, len(targets))
	for _, instance := range instanceOrder {
		plans = append(plans, planInstanceDueBatches(instance, byInstance[instance], maxExecutionsPerBatch)...)
	}
	return plans
}

// planInstanceDueBatches turns one instance's due targets into one batch each. A
// target larger than the cap is split because its checks cannot fit one batch;
// the target schedule is advanced only when the whole cycle is settled, so the
// plan itself never advances it. Every normal target (at most the per-table rule
// limit) is exactly one batch.
func planInstanceDueBatches(instance string, targets []*store.DueQualityTarget, maxExecutionsPerBatch int) []dueBatchPlan {
	plans := make([]dueBatchPlan, 0, len(targets))
	for _, target := range targets {
		if target == nil || len(target.Rules) == 0 {
			continue
		}
		group := target.Rules
		for len(group) > maxExecutionsPerBatch {
			plans = append(plans, dueBatchPlan{
				instanceResourceID: instance,
				target:             target,
				rules:              group[:maxExecutionsPerBatch],
			})
			group = group[maxExecutionsPerBatch:]
		}
		plans = append(plans, dueBatchPlan{
			instanceResourceID: instance,
			target:             target,
			rules:              group,
		})
	}
	return plans
}

// manualExecutionTasks turns an API selection into the runner's per-execution
// task list, dropping invalid and duplicate ids so one batch never executes the
// same planned row twice.
func manualExecutionTasks(executionIDs []int64) []executionTask {
	tasks := make([]executionTask, 0, len(executionIDs))
	seen := make(map[int64]struct{}, len(executionIDs))
	for _, executionID := range executionIDs {
		if executionID <= 0 {
			continue
		}
		if _, ok := seen[executionID]; ok {
			continue
		}
		seen[executionID] = struct{}{}
		tasks = append(tasks, executionTask{executionID: executionID})
	}
	return tasks
}

// baselineFromExecutions picks the most recent evaluated observation to use as
// the row_count_change baseline. ERROR, SKIPPED and NOT_EVALUATED verdicts carry
// no usable measurement even if one slipped into the observed value.
func baselineFromExecutions(executions []*store.QualityExecutionMessage, now time.Time, interval time.Duration) *float64 {
	for _, execution := range executions {
		if execution == nil || execution.ObservedValue == nil {
			continue
		}
		switch execution.Status {
		case storepb.QualityStatus_QUALITY_STATUS_PASS,
			storepb.QualityStatus_QUALITY_STATUS_WARN,
			storepb.QualityStatus_QUALITY_STATUS_FAIL:
			if !baselineIsRecent(execution.StartedAt, now, interval) {
				return nil
			}
			return execution.ObservedValue
		case storepb.QualityStatus_QUALITY_STATUS_ERROR,
			storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
			storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
			storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
			continue
		default:
			continue
		}
	}
	return nil
}

// baselineIsRecent reports whether an observation is still a fair denominator for
// a row-count comparison: no older than a week, and no more than two intervals
// behind. An older one describes a different data shape and would make the change
// percentage meaningless.
func baselineIsRecent(startedAt, now time.Time, interval time.Duration) bool {
	if startedAt.IsZero() {
		return false
	}
	age := now.Sub(startedAt)
	if age < 0 {
		return true
	}
	if age > maxBaselineAge {
		return false
	}
	if interval > 0 && age > 2*interval {
		return false
	}
	return true
}
