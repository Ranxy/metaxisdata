// Package qualityrunner executes data-quality rules on their schedule and on
// request.
//
// It is the only component that opens a connection to the target database for a
// quality check: the rule engine compiles and judges, the API writes and reads,
// and this runner decides when a rule runs and records what happened. One run
// covers one instance, so a run's rules share the instance's connection budget
// and its failures are reported together.
package qualityrunner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

const (
	// tickInterval is how often the scheduler looks for due rules. It is not
	// jittered: the query is a single indexed read and the pass is cheap.
	tickInterval = 10 * time.Second
	// dueScanLimit bounds one tick's due-rule query, matching the design's "at
	// most 200 rules per pass" budget.
	dueScanLimit = 200
	// maxRulesPerRun bounds one run. A run covers one instance, so this is also
	// the most rules the runner executes against one target without creating the
	// next run.
	maxRulesPerRun = 50
	// maxConcurrentInstanceRuns is the per-instance/global concurrency ceiling for
	// scheduled runs: the design allows two instances to execute at once, and one
	// instance's runs always run serially.
	maxConcurrentInstanceRuns = 2
	// manualQueueCapacity is how many manual runs may wait for the runner. The API
	// turns a full queue into ResourceExhausted instead of blocking a request.
	manualQueueCapacity = 64
	// staleRunCutoff is how long a PENDING/RUNNING run may predate the process
	// before the startup sweep abandons it. A restart does not retry such runs;
	// the next scheduled pass runs the rules again.
	staleRunCutoff = 15 * time.Minute
	// runExecutionTimeout bounds one run, including every rule and every query in
	// it. It is the design's 15-minute execution budget.
	runExecutionTimeout = 15 * time.Minute
	// ruleQueryTimeout bounds one rule's aggregate query. The rule config may
	// override it up to 300s; this is the default the executor receives.
	ruleQueryTimeout = 60 * time.Second
	// ruleResultMaxBytes bounds the aggregate row and the failure sample the
	// executor keeps in memory.
	ruleResultMaxBytes = 1 << 20
	// ruleSampleLimit is how many failing rows one result keeps as evidence.
	ruleSampleLimit = 20
	// minRuleScheduleInterval is the runner's backstop for a rule whose interval is
	// missing or nonsense. The API validates 15m..7d at write time; this keeps a
	// bad row from becoming due on every tick.
	minRuleScheduleInterval = 15 * time.Minute
	// maxErrorMessageLength bounds the error text stored on a result. Error details
	// are for an operator, not a log file.
	maxErrorMessageLength = 1024
)

// ruleExecOptions is the execution budget for one rule. StoreSamples asks the
// executor for the failing rows that make a FAIL result explainable.
//
// TODO(M2.4): the workspace data-quality settings will supply the query timeout,
// the sample limit, the sample switch and the daily execution quota (which is
// reported as ERROR(QUOTA) before a run starts). M1.5 uses the design defaults so
// the runner does not depend on the settings surface that does not exist yet.
var ruleExecOptions = dataquality.ExecOptions{
	QueryTimeout: ruleQueryTimeout,
	MaxRows:      1,
	MaxBytes:     ruleResultMaxBytes,
	SampleLimit:  ruleSampleLimit,
	StoreSamples: true,
}

// manualRun is one queued manual execution. ruleIDs is the exact selection the
// API made: a manual run must not silently widen to every enabled rule of the
// instance, and quality_run has no column that could persist the selection.
type manualRun struct {
	runID   int64
	ruleIDs []int64
}

// ruleTask is one rule a run has to execute. head is the rule row the due scan
// read; it is nil for a manual run, whose queue entry carries only rule ids.
type ruleTask struct {
	ruleID int64
	head   *store.QualityRuleMessage
}

// dueRunPlan is one run the due scan is about to create: one instance, at most
// maxRulesPerRun rules, already chunked.
type dueRunPlan struct {
	instanceResourceID string
	rules              []*store.QualityRuleMessage
}

// Runner is the data-quality background runner.
type Runner struct {
	stores    *store.Store
	dbFactory *dbfactory.DBFactory
	stateCfg  *state.State
	exec      *dataquality.Executor

	// manual carries the runs the API created. It is buffered so enqueueing never
	// blocks a request; a full queue is reported back as false.
	manual chan manualRun
	// inflight holds the rule ids currently being executed, so a second run that
	// finds the same rule due records SKIPPED instead of executing it twice.
	inflight map[int64]struct{}
	mu       sync.Mutex
}

// NewRunner creates the data-quality runner. The executor is stateless, so one
// instance is shared by every run.
func NewRunner(stores *store.Store, dbFactory *dbfactory.DBFactory, stateCfg *state.State) *Runner {
	return &Runner{
		stores:    stores,
		dbFactory: dbFactory,
		stateCfg:  stateCfg,
		exec:      dataquality.NewExecutor(),
		manual:    make(chan manualRun, manualQueueCapacity),
		inflight:  make(map[int64]struct{}),
	}
}

// Name identifies the runner in the server's lifecycle logs.
func (*Runner) Name() string { return "qualityrunner" }

// EnqueueQualityRun offers a manual run to the runner and returns false when the
// queue is full. ruleIDs is the exact rule selection the API made for the run;
// the runner executes those rules and nothing else. The call does not block: the
// API maps false to ResourceExhausted and is responsible for closing the run.
func (r *Runner) EnqueueQualityRun(runID int64, ruleIDs []int64) bool {
	select {
	case r.manual <- manualRun{runID: runID, ruleIDs: ruleIDs}:
		return true
	default:
		return false
	}
}

// Run starts the quality runner and blocks until ctx is cancelled. Manual runs
// take precedence over the periodic due scan.
func (r *Runner) Run(ctx context.Context) {
	r.abandonStaleRuns(ctx)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		// Drain the manual queue before looking at the ticker, so a pending user
		// request is not delayed by a due scan that is ready at the same moment.
		select {
		case <-ctx.Done():
			return
		case run := <-r.manual:
			r.executeManualRun(ctx, run)
			continue
		default:
		}

		select {
		case <-ctx.Done():
			return
		case run := <-r.manual:
			r.executeManualRun(ctx, run)
		case <-ticker.C:
			r.scanDue(ctx)
		}
	}
}

// abandonStaleRuns closes the runs a previous process left PENDING or RUNNING.
// It is a startup-only sweep: a run whose process is gone is not retried, and
// its results are not invented; the rules run again on their next schedule.
func (r *Runner) abandonStaleRuns(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-staleRunCutoff)
	abandoned, err := r.stores.AbandonStaleQualityRuns(ctx, cutoff)
	if err != nil {
		slog.Error("failed to abandon stale quality runs", log.WithError(err))
		return
	}
	if abandoned > 0 {
		slog.Warn("abandoned quality runs left behind by a previous process",
			slog.Int64("count", abandoned),
			slog.Time("started_before", cutoff))
	}
}

// scanDue creates and executes one run per instance for the rules that are due.
// A due batch may exceed the per-run limit or span several instances: the rules
// are grouped by instance, chunked into runs of at most maxRulesPerRun, and the
// runs of one instance execute serially while at most two instances execute at
// once.
func (r *Runner) scanDue(ctx context.Context) {
	now := time.Now().UTC()
	rules, err := r.stores.FindDueQualityRules(ctx, now, dueScanLimit)
	if err != nil {
		slog.Error("failed to list due quality rules", log.WithError(err))
		return
	}
	if len(rules) == 0 {
		return
	}

	plans := planDueRuns(rules, maxRulesPerRun)
	if len(plans) == 0 {
		return
	}

	wp := pool.New().WithMaxGoroutines(maxConcurrentInstanceRuns)
	for start := 0; start < len(plans); {
		end := start
		for end < len(plans) && plans[end].instanceResourceID == plans[start].instanceResourceID {
			end++
		}
		// The slice is re-bound on every iteration so each goroutine sees its own
		// instance's plans; one instance's plans run in order in that goroutine.
		instancePlans := plans[start:end]
		wp.Go(func() {
			for _, plan := range instancePlans {
				r.startScheduledRun(ctx, plan)
			}
		})
		start = end
	}
	wp.Wait()
}

// startScheduledRun creates one run for a plan's rules and executes it. The run
// row exists before any rule runs, so a rule's result always names a run that is
// observable through the API even while it executes.
func (r *Runner) startScheduledRun(ctx context.Context, plan dueRunPlan) {
	tasks := make([]ruleTask, 0, len(plan.rules))
	for _, rule := range plan.rules {
		if rule == nil {
			continue
		}
		tasks = append(tasks, ruleTask{ruleID: rule.ID, head: rule})
	}
	if len(tasks) == 0 {
		return
	}

	resourceID, err := common.RandomString(12)
	if err != nil {
		slog.Error("failed to generate a quality run resource id", log.WithError(err))
		return
	}
	run, err := r.stores.CreateQualityRun(ctx, &store.QualityRunMessage{
		ResourceID:         resourceID,
		InstanceResourceID: plan.instanceResourceID,
		Trigger:            storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_SCHEDULED,
		Status:             storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING,
	})
	if err != nil {
		slog.Error("failed to create a scheduled quality run",
			slog.String("instance", plan.instanceResourceID),
			log.WithError(err))
		return
	}
	r.executeRun(ctx, run.ID, tasks)
}

// executeManualRun executes a run the API created for an explicit rule
// selection. The selection is already resolved to rule ids; the run carries no
// persisted rule list, and it must not fall back to "all enabled rules of the
// instance", which could execute a rule the user did not ask for.
func (r *Runner) executeManualRun(ctx context.Context, manual manualRun) {
	r.executeRun(ctx, manual.runID, manualRuleTasks(manual.ruleIDs))
}

// executeRun executes one run's rules sequentially and writes the whole run in
// one result batch.
func (r *Runner) executeRun(ctx context.Context, runID int64, tasks []ruleTask) {
	runCtx, cancel := context.WithTimeout(ctx, runExecutionTimeout)
	defer cancel()

	if err := r.stores.MarkQualityRunRunning(runCtx, runID, time.Now().UTC()); err != nil {
		// A run that is already terminal (abandoned at startup, or finished by a
		// duplicate enqueue) is not an error worth retrying; just log it.
		slog.Warn("quality run cannot be started",
			slog.Int64("run", runID),
			slog.Int("rules", len(tasks)),
			log.WithError(err))
		return
	}

	results := make([]*store.QualityRuleResultMessage, 0, len(tasks))
	for _, task := range tasks {
		results = append(results, r.executeOneSafe(runCtx, runID, task))
	}

	if err := r.persistResults(runCtx, results); err != nil {
		slog.Error("failed to persist quality rule results",
			slog.Int64("run", runID),
			slog.Int("results", len(results)),
			log.WithError(err))
		// The rules were already rescheduled, so this pass is not retried. Close
		// the run instead of leaving it RUNNING for the restart sweep: ABANDONED
		// says the run produced no trustworthy result.
		// TODO(M2.1): the issue ledger is not updated by this runner yet; issue
		// transitions are applied where results are committed, which is here.
		_ = r.stores.FinishQualityRun(ctx, runID, storepb.QualityRunStatus_QUALITY_RUN_STATUS_ABANDONED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, nil, time.Now().UTC())
		return
	}

	// TODO(M2.1/M2.2): after the run commits, migrate quality issues from the
	// result statuses and emit notifications. Both are deliberately out of M1.5.
	counts, outcome := countsAndOutcome(results)
	if err := r.stores.FinishQualityRun(ctx, runID, storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED, outcome, counts, time.Now().UTC()); err != nil {
		slog.Error("failed to finish a quality run",
			slog.Int64("run", runID),
			slog.Int("results", len(results)),
			log.WithError(err))
	}
}

// executeOneSafe isolates one rule's execution: a panic in the rule engine or a
// driver turns into an ERROR result for that rule instead of killing the run (or
// the runner, which would lose every remaining rule in the batch).
func (r *Runner) executeOneSafe(ctx context.Context, runID int64, task ruleTask) (result *store.QualityRuleResultMessage) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("quality rule execution panicked",
				slog.Int64("run", runID),
				slog.Int64("rule", task.ruleID),
				slog.Any("panic", recovered),
				log.Stack("panic-stack"))
			result = failureRuleResult(runID, task.ruleID, nil, time.Now().UTC(), storepb.QualityStatus_QUALITY_STATUS_ERROR,
				dataquality.ErrCodeExecution, fmt.Sprintf("quality rule execution panicked: %v", recovered))
		}
	}()
	return r.executeOne(ctx, runID, task)
}

// persistResults writes one run's results. Results are insert-only, so no
// conflict handling is needed; an empty batch never touches the database.
func (r *Runner) persistResults(ctx context.Context, results []*store.QualityRuleResultMessage) error {
	if len(results) == 0 {
		return nil
	}
	return r.stores.BatchCreateQualityRuleResults(ctx, results)
}

// executeOne resolves, validates and executes one rule, returning the result row
// it produced. It never returns nil: every failure path is an ERROR or SKIPPED
// result, because a run has to account for every rule it picked up.
func (r *Runner) executeOne(ctx context.Context, runID int64, task ruleTask) *store.QualityRuleResultMessage {
	startedAt := time.Now().UTC()
	if !r.acquireInflight(task.ruleID) {
		return failureRuleResult(runID, task.ruleID, nil, startedAt, storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
			"", "another run is already executing this rule")
	}
	defer r.releaseInflight(task.ruleID)

	// Advance the schedule before executing, as the design requires: the tick
	// that picks a rule up must not pick it up again, and a crash mid-run leaves
	// the rule scheduled for its next interval instead of immediately due. A
	// manual run has no head row and does not move the schedule.
	r.advanceScheduledRule(ctx, task.head)

	snapshot, _, err := r.loadRuleSnapshot(ctx, task)
	if err != nil {
		if errors.Is(err, errRuleRetired) {
			return failureRuleResult(runID, task.ruleID, nil, startedAt, storepb.QualityStatus_QUALITY_STATUS_SKIPPED, "", err.Error())
		}
		return failureRuleResult(runID, task.ruleID, nil, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
			dataquality.ErrCodeExecution, err.Error())
	}

	resolution, err := dataquality.ResolveTarget(ctx, r.stores, snapshot.Target.GUID, snapshot.Target.MetaType)
	if err != nil {
		if errors.Is(err, dataquality.ErrOrphaned) {
			// The target is gone: this is not a failed check, it is a rule that no
			// longer has anything to watch. SKIPPED(ORPHANED) and no notification.
			return failureRuleResult(runID, task.ruleID, snapshot, startedAt, storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
				dataquality.ErrCodeOrphaned, err.Error())
		}
		return failureRuleResult(runID, task.ruleID, snapshot, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
			dataquality.ErrCodeExecution, err.Error())
	}
	if err := dataquality.RequireTarget(resolution, snapshot.Type); err != nil {
		return failureRuleResult(runID, task.ruleID, snapshot, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
			dataquality.ErrCodeUnsupported, err.Error())
	}
	if err := dataquality.ValidateConfig(resolution.Target, snapshot.Type, snapshot.Config); err != nil {
		// The definition no longer describes the current metadata (for example a
		// named column was dropped). A saved rule is not re-validated on schema
		// change, so execution is where this surfaces.
		return failureRuleResult(runID, task.ruleID, snapshot, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
			dataquality.ErrCodeUnsupported, err.Error())
	}

	release, err := r.acquireInstanceConnection(resolution.Instance)
	if err != nil {
		return failureRuleResult(runID, task.ruleID, snapshot, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
			dataquality.ErrCodeQuota, err.Error())
	}
	defer release()

	driver, err := r.dbFactory.GetReadOnlyDatabaseDriver(ctx, resolution.Instance, resolution.Database)
	if err != nil {
		code, message := errorCodeAndMessage(err)
		return failureRuleResult(runID, task.ruleID, snapshot, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR, code, message)
	}
	defer driver.Close(ctx)

	queryer, ok := driver.(db.ReadOnlyQueryer)
	if !ok {
		return failureRuleResult(runID, task.ruleID, snapshot, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
			dataquality.ErrCodeUnsupported, "this database engine does not support read-only query execution")
	}

	execution, err := r.exec.Execute(ctx, queryer, resolution, *snapshot, ruleExecOptions)
	if err != nil {
		// A stored rule this build cannot compile (an engine whose dialect was
		// removed, or a definition the current metadata no longer satisfies) is an
		// execution failure, not a quality verdict: ERROR(UNSUPPORTED), the code
		// the save/preview path already uses for the same class of defect.
		if common.ErrorCode(err) == common.Invalid {
			return failureRuleResult(runID, task.ruleID, snapshot, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
				dataquality.ErrCodeUnsupported, err.Error())
		}
		code, message := errorCodeAndMessage(err)
		return failureRuleResult(runID, task.ruleID, snapshot, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR, code, message)
	}

	observation := execution.Observation
	if snapshot.Type == storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT_CHANGE {
		// The executor measures the current row count; the baseline is this rule's
		// last evaluated observation, which only the store can supply.
		observation.Baseline = r.ruleBaseline(ctx, snapshot.ID)
	}
	state := dataquality.Evaluate(*snapshot, observation, time.Now().UTC())
	return buildRuleResult(runID, &ruleExecutionOutcome{
		ruleID:      task.ruleID,
		snapshot:    snapshot,
		startedAt:   startedAt,
		finishedAt:  time.Now().UTC(),
		observation: observation,
		execution:   execution,
		state:       state,
	})
}

// advanceScheduledRule moves one scheduled rule's next_run_at forward. The due
// scan already returned the rule, so this is a scheduling bookkeeping step: a
// failure is logged, not fatal, because executing and recording the rule is
// still more useful than dropping the pass.
func (r *Runner) advanceScheduledRule(ctx context.Context, head *store.QualityRuleMessage) {
	if head == nil {
		return
	}
	if err := r.stores.AdvanceQualityRuleNextRun(ctx, head.ID, nextRunAt(time.Now().UTC(), head.ScheduleInterval)); err != nil {
		slog.Warn("failed to advance a quality rule's next run",
			slog.Int64("rule", head.ID),
			log.WithError(err))
	}
}

// loadRuleSnapshot loads the immutable definition a result is judged with. A
// rule edited between the due scan and this read is executed with its newest
// revision, never with the stale head the scan held.
func (r *Runner) loadRuleSnapshot(ctx context.Context, task ruleTask) (*dataquality.RuleSnapshot, *store.QualityRuleMessage, error) {
	head := task.head
	limit := 1
	revisions, err := r.stores.ListQualityRuleRevisions(ctx, task.ruleID, &limit, nil)
	if err != nil {
		return nil, head, err
	}
	if len(revisions) == 0 {
		return nil, head, common.Errorf(common.NotFound, "quality rule %d has no revision", task.ruleID)
	}

	if head != nil {
		if head.Deleted || head.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED {
			return nil, head, errRuleRetired
		}
		if revisions[0].Revision != head.Revision {
			// The head and its revision row are written in one transaction, so
			// reading the head again gives the revision to execute.
			refreshed, err := r.stores.GetQualityRule(ctx, &store.FindQualityRuleMessage{ResourceID: &head.ResourceID})
			if err != nil {
				return nil, head, err
			}
			if refreshed == nil || refreshed.Deleted || refreshed.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED {
				return nil, head, errRuleRetired
			}
			head = refreshed
			if refreshed.Revision != revisions[0].Revision {
				revisions, err = r.stores.ListQualityRuleRevisions(ctx, task.ruleID, &limit, nil)
				if err != nil {
					return nil, head, err
				}
				if len(revisions) == 0 {
					return nil, head, common.Errorf(common.NotFound, "quality rule %d has no revision", task.ruleID)
				}
			}
		}
	}

	revision := revisions[0]
	return &dataquality.RuleSnapshot{
		ID:        revision.RuleID,
		Revision:  revision.Revision,
		Type:      revision.RuleType,
		Dimension: revision.Dimension,
		Severity:  revision.Severity,
		Target: dataquality.Target{
			GUID:     revision.TargetGUID,
			MetaType: revision.TargetType,
			Path:     revision.TargetPath,
		},
		Config: revision.Config,
	}, head, nil
}

// acquireInstanceConnection reserves one of the instance's outstanding
// connection slots, the same budget the schema syncer uses, so a quality run
// cannot push a target past its configured ceiling.
func (r *Runner) acquireInstanceConnection(instance *store.InstanceMessage) (func(), error) {
	maximumConnections := int(instance.Metadata.GetMaximumConnections())
	if maximumConnections <= 0 {
		maximumConnections = common.DefaultInstanceMaximumConnections
	}
	return r.stateCfg.AcquireInstanceConnection(instance.ResourceID, maximumConnections)
}

// ruleBaseline returns the rule's last evaluated observation, the denominator
// row_count_change compares the current count against. An unreadable baseline is
// not fatal: the rule then reports NOT_EVALUATED, which is the correct "no
// baseline" verdict rather than a made-up comparison.
func (r *Runner) ruleBaseline(ctx context.Context, ruleID int64) *float64 {
	results, err := r.stores.ListRecentQualityRuleResults(ctx, ruleID, 1)
	if err != nil {
		slog.Warn("failed to read a quality rule baseline",
			slog.Int64("rule", ruleID),
			log.WithError(err))
		return nil
	}
	return baselineFromResults(results)
}

// acquireInflight marks one rule as being executed and reports whether this
// caller owns it. The guard is process-local, like the design's in-flight map: a
// second run of the same rule records SKIPPED instead of executing it twice.
func (r *Runner) acquireInflight(ruleID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.inflight[ruleID]; ok {
		return false
	}
	r.inflight[ruleID] = struct{}{}
	return true
}

// releaseInflight clears one rule's in-flight mark.
func (r *Runner) releaseInflight(ruleID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inflight, ruleID)
}

// errRuleRetired reports that a rule was archived or deleted after it was
// picked up; the run records SKIPPED rather than executing a retired definition.
var errRuleRetired = errors.New("quality rule is retired")

// ruleExecutionOutcome is everything one execution produced, in the shape the
// result row needs. Keeping the failure paths and the evaluated path in one
// struct makes buildRuleResult the single mapping from a verdict to a stored
// result.
type ruleExecutionOutcome struct {
	ruleID       int64
	snapshot     *dataquality.RuleSnapshot
	startedAt    time.Time
	finishedAt   time.Time
	observation  dataquality.Observation
	execution    *dataquality.Execution
	state        dataquality.ResultState
	errorCode    string
	errorMessage string
}

// buildRuleResult maps one execution outcome to the insert-only result row.
func buildRuleResult(runID int64, outcome *ruleExecutionOutcome) *store.QualityRuleResultMessage {
	result := &store.QualityRuleResultMessage{
		RunID:             runID,
		RuleID:            outcome.ruleID,
		RuleEngineVersion: dataquality.EngineVersion,
		Status:            outcome.state.Status,
		ObservedKind:      outcome.observation.ObservedKind,
		Thresholds:        outcome.state.Thresholds,
		FailedRows:        outcome.observation.FailedRows,
		TotalRows:         outcome.observation.TotalRows,
		Sampled:           outcome.observation.Sampled,
		SampleRows:        outcome.observation.SampleRows,
		Truncated:         outcome.observation.Truncated,
		ErrorCode:         outcome.errorCode,
		ErrorMessage:      truncateErrorMessage(outcome.errorMessage),
		DurationMS:        durationMS(outcome.startedAt, outcome.finishedAt),
		StartedAt:         outcome.startedAt,
		FinishedAt:        outcome.finishedAt,
	}
	if outcome.snapshot != nil {
		result.RuleRevision = outcome.snapshot.Revision
		result.TargetGUID = outcome.snapshot.Target.GUID
		result.TargetType = outcome.snapshot.Target.MetaType
	}
	if outcome.execution != nil {
		result.Sample = outcome.execution.Samples
		result.ExecutedSQL = outcome.execution.ExecutedSQL
		result.Sampled = outcome.execution.Sampled
		result.Truncated = outcome.execution.Truncated
	}
	result.ObservedValue = observedValue(outcome.state.Status, outcome.observation.Observed)
	return result
}

// failureRuleResult records a rule that could not be judged. ERROR and SKIPPED
// results carry no observation, and the thresholds snapshot is nil because no
// verdict was reached.
func failureRuleResult(runID int64, ruleID int64, snapshot *dataquality.RuleSnapshot, startedAt time.Time, status storepb.QualityStatus, code, message string) *store.QualityRuleResultMessage {
	return buildRuleResult(runID, &ruleExecutionOutcome{
		ruleID:       ruleID,
		snapshot:     snapshot,
		startedAt:    startedAt,
		finishedAt:   time.Now().UTC(),
		state:        dataquality.ResultState{Status: status},
		errorCode:    code,
		errorMessage: message,
	})
}

// observedValue decides whether a result stores a measured value. ERROR,
// SKIPPED and NOT_EVALUATED results have no measurement; storing zero for them
// would be indistinguishable from a real zero and would make a trend chart
// invent a value.
func observedValue(status storepb.QualityStatus, observed float64) *float64 {
	switch status {
	case storepb.QualityStatus_QUALITY_STATUS_PASS,
		storepb.QualityStatus_QUALITY_STATUS_WARN,
		storepb.QualityStatus_QUALITY_STATUS_FAIL:
		value := observed
		return &value
	case storepb.QualityStatus_QUALITY_STATUS_ERROR,
		storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		storepb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		return nil
	case storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED:
		return nil
	default:
		return nil
	}
}

// durationMS is how long one rule took, clamped to the INT4 column.
func durationMS(startedAt, finishedAt time.Time) int32 {
	elapsed := finishedAt.Sub(startedAt).Milliseconds()
	if elapsed < 0 {
		return 0
	}
	if elapsed > int64(^uint32(0)>>1) {
		return int32(^uint32(0) >> 1)
	}
	return int32(elapsed)
}

// truncateErrorMessage bounds the stored error text so one broken query cannot
// put an unbounded blob in a result row. It cuts on a rune boundary because the
// column is UTF-8 text.
func truncateErrorMessage(message string) string {
	return common.TruncateUTF8Bytes(message, maxErrorMessageLength)
}

// nextRunAt is when the scheduler considers the rule next. It is the rule's
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

// planDueRuns groups the due rules by instance and chunks each instance's rules
// into runs of at most maxRulesPerRun. Instance order is the order the rules
// became due in, so the oldest due rule is handled first and the resulting plans
// keep each instance's chunks adjacent for serial execution.
func planDueRuns(rules []*store.QualityRuleMessage, maxRulesPerRun int) []dueRunPlan {
	if maxRulesPerRun <= 0 {
		return nil
	}
	instanceOrder := make([]string, 0, len(rules))
	byInstance := make(map[string][]*store.QualityRuleMessage, len(rules))
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		instance, ok := common.GetInstanceFromGUID(rule.TargetGUID)
		if !ok {
			// A malformed target GUID is still executed (it resolves to ORPHANED),
			// but it cannot be attributed to an instance.
			instance = ""
		}
		if _, seen := byInstance[instance]; !seen {
			instanceOrder = append(instanceOrder, instance)
		}
		byInstance[instance] = append(byInstance[instance], rule)
	}

	plans := make([]dueRunPlan, 0, len(rules)/maxRulesPerRun+1)
	for _, instance := range instanceOrder {
		instanceRules := byInstance[instance]
		for start := 0; start < len(instanceRules); start += maxRulesPerRun {
			end := min(start+maxRulesPerRun, len(instanceRules))
			plans = append(plans, dueRunPlan{
				instanceResourceID: instance,
				rules:              instanceRules[start:end],
			})
		}
	}
	return plans
}

// manualRuleTasks turns an API selection into the runner's per-rule task list,
// dropping invalid and duplicate ids so one run never executes the same rule
// twice.
func manualRuleTasks(ruleIDs []int64) []ruleTask {
	tasks := make([]ruleTask, 0, len(ruleIDs))
	seen := make(map[int64]struct{}, len(ruleIDs))
	for _, ruleID := range ruleIDs {
		if ruleID <= 0 {
			continue
		}
		if _, ok := seen[ruleID]; ok {
			continue
		}
		seen[ruleID] = struct{}{}
		tasks = append(tasks, ruleTask{ruleID: ruleID})
	}
	return tasks
}

// countsAndOutcome tallies one run's results into the counts JSONB column and
// the run's rollup outcome. ERROR outranks FAIL: a run that could not execute a
// rule is not the same thing as a run whose rule found bad data.
func countsAndOutcome(results []*store.QualityRuleResultMessage) (*storepb.QualityRunCounts, storepb.QualityStatus) {
	statuses := make([]storepb.QualityStatus, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		statuses = append(statuses, result.Status)
	}
	return dataquality.CountResults(statuses)
}

// baselineFromResults picks the most recent evaluated observation to use as the
// row_count_change baseline. ERROR, SKIPPED and NOT_EVALUATED results carry no
// usable measurement even if one slipped into the observed value.
func baselineFromResults(results []*store.QualityRuleResultMessage) *float64 {
	for _, result := range results {
		if result == nil || result.ObservedValue == nil {
			continue
		}
		switch result.Status {
		case storepb.QualityStatus_QUALITY_STATUS_PASS,
			storepb.QualityStatus_QUALITY_STATUS_WARN,
			storepb.QualityStatus_QUALITY_STATUS_FAIL:
			return result.ObservedValue
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

// errorCodeAndMessage classifies an error that happened before or during the
// read-only query. The two runner-level conditions have explicit codes; every
// other error goes through the engine's classifier.
func errorCodeAndMessage(err error) (code string, message string) {
	if errors.Is(err, dbfactory.ErrNoReadOnlyDataSource) {
		return dataquality.ErrCodeNoReadOnly, err.Error()
	}
	if errors.Is(err, state.ErrInstanceConnectionLimit) {
		return dataquality.ErrCodeQuota, err.Error()
	}
	code, message = dataquality.ClassifyError(err)
	if message == "" {
		message = err.Error()
	}
	return code, message
}
