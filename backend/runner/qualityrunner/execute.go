package qualityrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// executeManualBatch executes a batch the API created for an explicit execution
// selection. The selection is fully planned by the API; the runner executes
// exactly those execution rows and never widens to every enabled rule of the
// instance.
func (r *Runner) executeManualBatch(ctx context.Context, manual manualRun) {
	// A manual request is the moment to pick up a settings change: the user could
	// have turned failure samples off just before asking for the batch.
	r.refreshSettings(ctx)

	instance, err := r.instanceForAdmission(ctx, manual.instanceResourceID)
	if err != nil {
		slog.Warn("failed to read the instance of a manual quality batch; abandoning it",
			slog.Int64("batch", manual.batchID),
			log.WithError(err))
		r.abandonUnadmittedBatch(ctx, manual.batchID, len(manual.executions), metrics.QualityAbandonedInstanceReadError)
		return
	}
	release, admission, admitted := r.admitBatch(ctx, instance, state.ConnectionPriorityManual, manualAdmissionWait)
	r.metrics.CountQualityExecutionAdmission(metrics.QualityTriggerManual, admitted)
	if !admitted {
		slog.Warn("abandoned a manual quality batch: the instance's connection budget stayed busy",
			slog.Int64("batch", manual.batchID),
			slog.String("instance", manual.instanceResourceID),
			slog.Int("executions", len(manual.executions)))
		r.abandonUnadmittedBatch(ctx, manual.batchID, len(manual.executions), metrics.QualityAbandonedAdmissionTimeout)
		return
	}
	// Idempotent, and executeBatch releases it before the store work; this covers
	// any return a later edit adds between admission and executeBatch.
	defer release()
	r.executeBatch(ctx, manual.batchID, manual.instanceResourceID, manual.executions, admission, release)
}

// executeBatch executes one batch's planned executions sequentially, writes each
// verdict through FinishQualityExecution, and then applies the problem ledger and
// the notifications the verdicts imply. Each execution row is its own commit
// point: a write failure stops the batch and abandons the rows that never got a
// verdict instead of leaving them RUNNING.
func (r *Runner) executeBatch(ctx context.Context, batchID int64, instanceResourceID string, tasks []executionTask, admission batchAdmission, release func()) {
	r.executeBatchWith(ctx, r.stores, batchID, instanceResourceID, tasks, admission, release)
}

// executeBatchWith is executeBatch with an injectable store so the batch
// accounting (including the incomplete path) is testable without PostgreSQL.
func (r *Runner) executeBatchWith(ctx context.Context, st batchExecutionStore, batchID int64, instanceResourceID string, tasks []executionTask, admission batchAdmission, release func()) {
	batchCtx, cancel := context.WithTimeout(ctx, batchExecutionTimeout)
	defer cancel()
	// The release is idempotent, so this net and the caller's defer compose.
	defer release()

	if err := st.MarkQualityRunRunning(batchCtx, batchID, time.Now().UTC()); err != nil {
		// A batch that is already terminal (abandoned at startup, or finished by a
		// duplicate enqueue) is not an error worth retrying. Close whatever rows it
		// still owned instead of leaving them PENDING for the next startup sweep.
		slog.Warn("quality batch cannot be started",
			slog.Int64("batch", batchID),
			slog.Int("executions", len(tasks)),
			log.WithError(err))
		r.abandonBatch(ctx, st, batchID, len(tasks), metrics.QualityAbandonedMarkRunningFailed)
		return
	}

	// The contract filter is read at execution time (a publish takes effect for the
	// next run) but cached for the batch, so a target with many rules costs one read
	// rather than one per execution.
	contracts := newBatchContractFilters(r.loadBatchContractFilter())

	executions := make([]*ruleExecution, 0, len(tasks))
	aborted := false

	for _, task := range tasks {
		planned, err := loadPlannedExecution(batchCtx, st, batchID, task)
		if err != nil {
			slog.Warn("failed to read a planned quality execution; skipping it",
				slog.Int64("batch", batchID),
				slog.Int64("execution", task.executionID),
				log.WithError(err))
			continue
		}
		if planned == nil {
			continue
		}

		// The previous status is read before this execution's row is written, so
		// the problem planner sees the transition the new verdict really is.
		previous := previousQualityStatus(batchCtx, st, planned.RuleID)

		startedAt := time.Now().UTC()
		if err := st.MarkQualityExecutionRunning(batchCtx, planned.ID, startedAt); err != nil {
			// An execution another process already closed (a stale sweep, a retried
			// dispatch) is not worth retrying. It stays unaccounted for, so the
			// post-loop accounting abandons the batch; the abandoned counter is
			// incremented there for the rows that really changed, never here.
			slog.Warn("quality execution cannot be started",
				slog.Int64("batch", batchID),
				slog.Int64("execution", planned.ID),
				log.WithError(err))
			continue
		}

		r.metrics.IncQualityExecutionsInProgress()
		execution := r.executeOneSafe(batchCtx, planned, startedAt, admission, contracts)
		if execution == nil {
			r.metrics.DecQualityExecutionsInProgress()
			continue
		}
		execution.previous = previous

		writeErr := st.FinishQualityExecution(batchCtx, execution.result)
		r.metrics.DecQualityExecutionsInProgress()
		if writeErr != nil {
			slog.Error("failed to write a quality execution verdict",
				slog.Int64("batch", batchID),
				slog.Int64("execution", planned.ID),
				slog.Int64("rule", planned.RuleID),
				log.WithError(writeErr))
			aborted = true
			r.abandonBatch(batchCtx, st, batchID, len(tasks), metrics.QualityAbandonedPersistFailed)
			break
		}
		r.observeRuleExecution(execution)
		r.metrics.CountQualityExecutionCompleted(execution.result.Status)
		executions = append(executions, execution)
	}

	// Every target query is done. The ledger, notifications and the batch rollup
	// are store work, so they must not hold a connection slot on the instance; the
	// release is idempotent and the deferred one is the panic net.
	release()

	now := time.Now().UTC()
	workspaceID := ""
	if r.notifier != nil {
		workspaceID = r.workspaceID(ctx)
	}
	// A task that never produced a written verdict (a read or mark failure, a
	// terminal row owned by another writer, or the batch deadline) must not leave
	// PENDING/RUNNING rows behind: abandon the batch's unfinished executions and
	// finish the batch ABANDONED instead of reporting a partial rollup.
	incomplete := !aborted && len(executions) != len(tasks)
	// Neither an aborted nor an incomplete batch sends a storm summary, so its
	// written verdicts keep their per-rule notifications instead of being
	// suppressed by a summary that will not arrive.
	storm := !aborted && !incomplete && isQualityStorm(executions)
	// Verdicts that were written are real rows even if a later write failed or the
	// batch ran out of time, so their problem transitions and notifications are
	// settled in both cases.
	r.settleQualityExecutions(ctx, workspaceID, executions, storm, now)

	if aborted || incomplete {
		if incomplete {
			r.abandonBatch(ctx, st, batchID, len(tasks), metrics.QualityAbandonedIncomplete)
		}
		return
	}

	statuses := make([]storepb.QualityStatus, 0, len(executions))
	for _, execution := range executions {
		statuses = append(statuses, execution.result.Status)
	}
	counts, outcome := dataquality.CountResults(statuses)
	if err := st.FinishQualityRun(ctx, batchID, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED, outcome, counts, now); err != nil {
		slog.Error("failed to finish an internal quality batch",
			slog.Int64("batch", batchID),
			slog.Int("executions", len(executions)),
			log.WithError(err))
		return
	}
	if storm {
		// The storm summary is sent once the batch is finished, so the executions it
		// merges are already terminal when the recipient opens one.
		r.reportQualityStorm(ctx, batchID, instanceResourceID, executions[0], counts.GetFail(), counts.GetTotal(), now)
	}
}

// loadPlannedExecution reads the PENDING execution row a task names. A nil row is
// a row that is gone (retention), and a row that is no longer PENDING was already
// closed by another writer; both are skipped. A row from another batch is a
// dispatch defect and is refused rather than executed under the wrong batch.
func loadPlannedExecution(ctx context.Context, st batchExecutionStore, batchID int64, task executionTask) (*store.QualityExecutionMessage, error) {
	if task.executionID <= 0 {
		return nil, nil
	}
	planned, err := st.GetQualityExecution(ctx, task.executionID)
	if err != nil {
		return nil, err
	}
	if planned == nil {
		return nil, nil
	}
	if planned.RunID != batchID {
		slog.Warn("refusing a quality execution planned for another batch",
			slog.Int64("batch", batchID),
			slog.Int64("execution", planned.ID),
			slog.Int64("planned_batch", planned.RunID))
		return nil, nil
	}
	if planned.State != storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_PENDING {
		slog.Warn("skipping a quality execution that is no longer pending",
			slog.Int64("batch", batchID),
			slog.Int64("execution", planned.ID),
			slog.String("state", planned.State.String()))
		return nil, nil
	}
	return planned, nil
}

// executeOneSafe isolates one rule's execution: a panic in the rule engine or a
// driver turns into an ERROR verdict for that execution instead of killing the
// batch (or the runner, which would lose every remaining execution).
func (r *Runner) executeOneSafe(ctx context.Context, planned *store.QualityExecutionMessage, startedAt time.Time, admission batchAdmission, contracts *batchContractFilters) (execution *ruleExecution) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("quality rule execution panicked",
				slog.Int64("batch", planned.RunID),
				slog.Int64("execution", planned.ID),
				slog.Int64("rule", planned.RuleID),
				slog.Any("panic", recovered),
				log.Stack("panic-stack"))
			execution = &ruleExecution{
				result: failureExecutionMessage(planned, nil, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
					dataquality.ErrCodeExecution, fmt.Sprintf("quality rule execution panicked: %v", recovered)),
			}
		}
	}()
	return r.executeOne(ctx, planned, startedAt, admission, contracts)
}

// executeOne resolves, validates and executes one rule, returning the completed
// execution row it produces. It never returns nil: every failure path is an ERROR
// or SKIPPED verdict, because a batch has to account for every execution it
// planned.
func (r *Runner) executeOne(ctx context.Context, planned *store.QualityExecutionMessage, startedAt time.Time, admission batchAdmission, contracts *batchContractFilters) *ruleExecution {
	if !r.acquireInflight(planned.RuleID) {
		return &ruleExecution{
			result: failureExecutionMessage(planned, nil, startedAt, storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
				"", "another batch is already executing this rule"),
		}
	}
	defer r.releaseInflight(planned.RuleID)

	snapshot, head, err := r.loadRuleSnapshot(ctx, planned.RuleID)
	if err != nil {
		status := storepb.QualityStatus_QUALITY_STATUS_ERROR
		code := dataquality.ErrCodeExecution
		if errors.Is(err, errRuleRetired) {
			status = storepb.QualityStatus_QUALITY_STATUS_SKIPPED
			code = ""
		}
		return &ruleExecution{
			result: failureExecutionMessage(planned, nil, startedAt, status, code, err.Error()),
			head:   head,
		}
	}

	// fail records one execution the rule could not judge. The snapshot is
	// carried so the row still names the definition and target it was about.
	fail := func(status storepb.QualityStatus, code, message string) *ruleExecution {
		return &ruleExecution{
			result:   failureExecutionMessage(planned, snapshot, startedAt, status, code, message),
			head:     head,
			snapshot: snapshot,
		}
	}

	resolution, err := dataquality.ResolveTarget(ctx, r.stores, snapshot.Target.GUID, snapshot.Target.MetaType)
	if err != nil {
		if errors.Is(err, dataquality.ErrOrphaned) {
			// The target is gone: this is not a failed check, it is a rule that no
			// longer has anything to watch. SKIPPED(ORPHANED) and no notification.
			return fail(storepb.QualityStatus_QUALITY_STATUS_SKIPPED, dataquality.ErrCodeOrphaned, err.Error())
		}
		return fail(storepb.QualityStatus_QUALITY_STATUS_ERROR, dataquality.ErrCodeExecution, err.Error())
	}
	// The target's contract filter is ANDed into every statement the rule compiles
	// to. It is read at execution time (so publishing a contract change takes
	// effect for the next run without rewriting any rule) and cached per batch, so a
	// target with many rules is read once. A target without a contract row reads as
	// no filter; a metadata rule skips the read entirely (it compiles no SQL).
	contractFilter, err := contracts.forRule(ctx, snapshot.Type, snapshot.Target.GUID, snapshot.Target.MetaType)
	if err != nil {
		return fail(storepb.QualityStatus_QUALITY_STATUS_ERROR, dataquality.ErrCodeExecution, err.Error())
	}
	snapshot.ContractFilter = contractFilter
	if err := dataquality.RequireTarget(resolution, snapshot.Type); err != nil {
		return fail(storepb.QualityStatus_QUALITY_STATUS_ERROR, dataquality.ErrCodeUnsupported, err.Error())
	}
	if err := dataquality.ValidateConfig(resolution.Target, snapshot.Type, snapshot.Config); err != nil {
		// The definition no longer describes the current metadata (for example a
		// named column was dropped). A saved rule is not re-validated on schema
		// change, so execution is where this surfaces.
		return fail(storepb.QualityStatus_QUALITY_STATUS_ERROR, dataquality.ErrCodeUnsupported, err.Error())
	}

	// A metadata rule observes the registry and compiles to no SQL, so it needs
	// neither a target connection nor a READ_ONLY data source: it must not fail
	// because the target has no read-only account. The executor receives a nil
	// queryer for it.
	//
	// The batch's lease covers the instance it was admitted for, so one query rule
	// at a time opens and closes a driver under it. A rule whose target changed to
	// a different instance after admission cannot use that lease;
	// ruleConnectionLease then leases the resolved instance for this rule alone, so
	// no driver is ever opened outside a leased budget.
	var queryer db.ReadOnlyQueryer
	if requiresReadOnlySource(snapshot.Type) {
		release, ok := r.ruleConnectionLease(resolution.Instance, admission)
		if !ok {
			return fail(storepb.QualityStatus_QUALITY_STATUS_SKIPPED, "",
				"the instance had no free connection slot when the rule needed one")
		}
		defer release()

		driver, err := r.dbFactory.GetReadOnlyDatabaseDriver(ctx, resolution.Instance, resolution.Database)
		if err != nil {
			code, message := errorCodeAndMessage(err)
			return fail(storepb.QualityStatus_QUALITY_STATUS_ERROR, code, message)
		}
		defer driver.Close(ctx)

		readOnly, ok := driver.(db.ReadOnlyQueryer)
		if !ok {
			return fail(storepb.QualityStatus_QUALITY_STATUS_ERROR, dataquality.ErrCodeUnsupported,
				"this database engine does not support read-only query execution")
		}
		queryer = readOnly
	}

	execution, err := r.exec.Execute(ctx, queryer, resolution, *snapshot, r.execOptions())
	if err != nil {
		// A stored rule this build cannot compile (an engine whose dialect was
		// removed, or a definition the current metadata no longer satisfies) is an
		// execution failure, not a quality verdict: ERROR(UNSUPPORTED), the code
		// the save/preview path already uses for the same class of defect.
		if common.ErrorCode(err) == common.Invalid {
			return fail(storepb.QualityStatus_QUALITY_STATUS_ERROR, dataquality.ErrCodeUnsupported, err.Error())
		}
		code, message := errorCodeAndMessage(err)
		return fail(storepb.QualityStatus_QUALITY_STATUS_ERROR, code, message)
	}

	observation := execution.Observation
	if snapshot.Type == storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE {
		// The executor measures the current row count; the baseline is this rule's
		// last evaluated observation, which only the store can supply.
		observation.Baseline = r.ruleBaseline(ctx, snapshot.ID,
			r.targetScheduleInterval(ctx, snapshot.Target.GUID, snapshot.Target.MetaType))
	}
	state := dataquality.Evaluate(*snapshot, observation, time.Now().UTC())
	return &ruleExecution{
		result: buildExecutionMessage(planned, &ruleExecutionOutcome{
			snapshot:    snapshot,
			startedAt:   startedAt,
			finishedAt:  time.Now().UTC(),
			observation: observation,
			execution:   execution,
			state:       state,
		}),
		head:     head,
		snapshot: snapshot,
	}
}
