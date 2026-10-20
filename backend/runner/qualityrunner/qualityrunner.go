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
	"strings"
	"sync"
	"time"

	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
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
	// maxBaselineAge bounds how old a row_count_change baseline may be. An older
	// observation describes a different data shape.
	maxBaselineAge = 7 * 24 * time.Hour
	// QualityStormThreshold is how many failing rules in one run turn per-rule
	// notifications into a single summary. A run that fails more rules than this
	// would otherwise write one inbox message per rule for a single incident.
	QualityStormThreshold = 10
	// qualityIssueResourceIDLength is the length of a generated issue resource id,
	// matching the other data-quality resource ids.
	qualityIssueResourceIDLength = 12
)

// execOptions is the execution budget for one rule. StoreSamples asks the
// executor for the failing rows that make a FAIL result explainable; the
// workspace settings decide whether they are kept and how many.
func (r *Runner) execOptions() dataquality.ExecOptions {
	settings := r.settings
	sampleLimit := settings.FailureSampleLimit
	if sampleLimit <= 0 {
		sampleLimit = ruleSampleLimit
	}
	return dataquality.ExecOptions{
		QueryTimeout: ruleQueryTimeout,
		MaxRows:      1,
		MaxBytes:     ruleResultMaxBytes,
		SampleLimit:  sampleLimit,
		StoreSamples: settings.StoreFailureSamples,
	}
}

// refreshSettings reloads the workspace data-quality settings. A read failure
// keeps the previous value: settings are a policy, and a transient store error
// must not silently switch the sample policy or the daily ceiling.
func (r *Runner) refreshSettings(ctx context.Context) {
	settings, err := dataquality.LoadSettings(ctx, r.stores)
	if err != nil {
		slog.Warn("failed to load the data quality settings", log.WithError(err))
		return
	}
	r.settings = settings
}

// overDailyExecutionQuota reports whether the workspace already started as many
// runs today as its settings allow. The tally is a safety valve against a dense
// schedule, not a correctness boundary: when it trips, the due rules stay due and
// the next day resumes them.
func (r *Runner) overDailyExecutionQuota(ctx context.Context, now time.Time) bool {
	limit := r.settings.DailyExecutionLimit
	if limit <= 0 {
		return false
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	count, err := r.stores.CountQualityRunsSince(ctx, dayStart)
	if err != nil {
		slog.Warn("failed to count today's quality runs", log.WithError(err))
		return false
	}
	if count < limit {
		return false
	}
	slog.Warn("daily quality execution quota reached; skipping the due scan",
		slog.Int("runs_today", count), slog.Int("limit", limit))
	return true
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

// ruleExecution is one rule's result plus the inputs the problem ledger and the
// notifications need after the run commits: the mutable head (owner, severity,
// mute, resource id), the immutable definition that was executed, and the rule's
// status before this run.
type ruleExecution struct {
	result   *store.QualityRuleResultMessage
	head     *store.QualityRuleMessage
	snapshot *dataquality.RuleSnapshot
	previous *storepb.QualityStatus
}

// Runner is the data-quality background runner.
type Runner struct {
	stores    *store.Store
	dbFactory *dbfactory.DBFactory
	stateCfg  *state.State
	exec      *dataquality.Executor
	// notifier delivers the problem and execution notifications a run implies.
	// It is best effort: a delivery failure is logged and never fails the run.
	// A nil notifier keeps the runner silent, which is what its tests want.
	notifier *notification.Service

	// settings is the workspace's data-quality policy, reloaded on every scan and
	// manual run. It starts at the documented defaults so a fresh process with an
	// unreadable setting still behaves sanely.
	settings dataquality.Settings

	// manual carries the runs the API created. It is buffered so enqueueing never
	// blocks a request; a full queue is reported back as false.
	manual chan manualRun
	// inflight holds the rule ids currently being executed, so a second run that
	// finds the same rule due records SKIPPED instead of executing it twice.
	inflight map[int64]struct{}
	mu       sync.Mutex
}

// NewRunner creates the data-quality runner. The executor is stateless, so one
// instance is shared by every run. notifier reports opened and recovered problems
// and execution failures; it may be nil.
func NewRunner(stores *store.Store, dbFactory *dbfactory.DBFactory, stateCfg *state.State, notifier *notification.Service) *Runner {
	return &Runner{
		stores:    stores,
		dbFactory: dbFactory,
		stateCfg:  stateCfg,
		exec:      dataquality.NewExecutor(),
		notifier:  notifier,
		manual:    make(chan manualRun, manualQueueCapacity),
		inflight:  make(map[int64]struct{}),
		settings:  dataquality.DefaultSettings(),
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
	r.refreshSettings(ctx)

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
	r.refreshSettings(ctx)
	if r.overDailyExecutionQuota(ctx, now) {
		return
	}
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
	r.executeRun(ctx, run.ID, run.ResourceID, tasks)
}

// executeManualRun executes a run the API created for an explicit rule
// selection. The selection is already resolved to rule ids; the run carries no
// persisted rule list, and it must not fall back to "all enabled rules of the
// instance", which could execute a rule the user did not ask for.
func (r *Runner) executeManualRun(ctx context.Context, manual manualRun) {
	// A manual request is the moment to pick up a settings change: the user could
	// have turned failure samples off just before asking for the run.
	r.refreshSettings(ctx)
	r.executeRun(ctx, manual.runID, r.manualRunResourceID(ctx, manual.runID), manualRuleTasks(manual.ruleIDs))
}

// executeRun executes one run's rules sequentially, writes the whole run in one
// result batch, and then applies the problem ledger and the notifications the
// results imply. The result write is the commit point: nothing touches an issue
// or an inbox when it failed, because there is no result to explain.
func (r *Runner) executeRun(ctx context.Context, runID int64, runResourceID string, tasks []ruleTask) {
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

	executions := make([]*ruleExecution, 0, len(tasks))
	results := make([]*store.QualityRuleResultMessage, 0, len(tasks))
	for _, task := range tasks {
		// The previous status is read before this run's results exist, so the
		// problem planner sees the transition the new result really is.
		previous := r.previousQualityStatus(runCtx, task.ruleID)
		execution := r.executeOneSafe(runCtx, runID, task)
		execution.previous = previous
		executions = append(executions, execution)
		results = append(results, execution.result)
	}

	if err := r.persistResults(runCtx, results); err != nil {
		slog.Error("failed to persist quality rule results",
			slog.Int64("run", runID),
			slog.Int("results", len(results)),
			log.WithError(err))
		// The rules were already rescheduled, so this pass is not retried. Close
		// the run instead of leaving it RUNNING for the restart sweep: ABANDONED
		// says the run produced no trustworthy result.
		_ = r.stores.FinishQualityRun(ctx, runID, storepb.QualityRunStatus_QUALITY_RUN_STATUS_ABANDONED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, nil, time.Now().UTC())
		return
	}

	now := time.Now().UTC()
	workspaceID := ""
	if r.notifier != nil {
		workspaceID = r.workspaceID(ctx)
	}
	runResourceName := qualityRunResourceName(workspaceID, runResourceID)
	if runResourceName == "" {
		// A manual run's resource id could not be resolved (see
		// manualRunResourceID). The database id still gives the detail and the
		// dedupe key a unique target, even though it is not a usable resource name.
		runResourceName = fmt.Sprintf("qualityRuns/%d", runID)
	}
	storm := isQualityRunStorm(results)
	r.settleQualityResults(ctx, runID, runResourceName, workspaceID, executions, storm, now)

	counts, outcome := countsAndOutcome(results)
	if err := r.stores.FinishQualityRun(ctx, runID, storepb.QualityRunStatus_QUALITY_RUN_STATUS_COMPLETED, outcome, counts, now); err != nil {
		slog.Error("failed to finish a quality run",
			slog.Int64("run", runID),
			slog.Int("results", len(results)),
			log.WithError(err))
	}
	if storm {
		// The design sends the storm summary once the run is finished, so the run
		// it points at is already terminal when the recipient opens it.
		r.reportQualityStorm(ctx, runResourceName, counts.GetFail(), counts.GetTotal(), now)
	}
}

// executeOneSafe isolates one rule's execution: a panic in the rule engine or a
// driver turns into an ERROR result for that rule instead of killing the run (or
// the runner, which would lose every remaining rule in the batch).
func (r *Runner) executeOneSafe(ctx context.Context, runID int64, task ruleTask) (execution *ruleExecution) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("quality rule execution panicked",
				slog.Int64("run", runID),
				slog.Int64("rule", task.ruleID),
				slog.Any("panic", recovered),
				log.Stack("panic-stack"))
			execution = &ruleExecution{
				result: failureRuleResult(runID, task.ruleID, nil, time.Now().UTC(), storepb.QualityStatus_QUALITY_STATUS_ERROR,
					dataquality.ErrCodeExecution, fmt.Sprintf("quality rule execution panicked: %v", recovered)),
				head: task.head,
			}
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
func (r *Runner) executeOne(ctx context.Context, runID int64, task ruleTask) *ruleExecution {
	startedAt := time.Now().UTC()
	if !r.acquireInflight(task.ruleID) {
		return &ruleExecution{
			result: failureRuleResult(runID, task.ruleID, nil, startedAt, storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
				"", "another run is already executing this rule"),
			head: task.head,
		}
	}
	defer r.releaseInflight(task.ruleID)

	// Advance the schedule before executing, as the design requires: the tick
	// that picks a rule up must not pick it up again, and a crash mid-run leaves
	// the rule scheduled for its next interval instead of immediately due. A
	// manual run has no head row and does not move the schedule.
	r.advanceScheduledRule(ctx, task.head)

	snapshot, head, err := r.loadRuleSnapshot(ctx, task)
	if err != nil {
		status := storepb.QualityStatus_QUALITY_STATUS_ERROR
		code := dataquality.ErrCodeExecution
		if errors.Is(err, errRuleRetired) {
			status = storepb.QualityStatus_QUALITY_STATUS_SKIPPED
			code = ""
		}
		return &ruleExecution{
			result: failureRuleResult(runID, task.ruleID, nil, startedAt, status, code, err.Error()),
			head:   head,
		}
	}

	// fail records one rule this execution could not judge. The snapshot is
	// carried so a result row still names the definition and target it was about.
	fail := func(status storepb.QualityStatus, code, message string) *ruleExecution {
		return &ruleExecution{
			result:   failureRuleResult(runID, task.ruleID, snapshot, startedAt, status, code, message),
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
	// neither the instance's connection budget nor a READ_ONLY data source: it
	// must not fail because the target has no read-only account. The executor
	// receives a nil queryer for it.
	var queryer db.ReadOnlyQueryer
	if requiresReadOnlySource(snapshot.Type) {
		release, err := r.acquireInstanceConnection(resolution.Instance)
		if err != nil {
			return fail(storepb.QualityStatus_QUALITY_STATUS_ERROR, dataquality.ErrCodeQuota, err.Error())
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
		observation.Baseline = r.ruleBaseline(ctx, snapshot.ID, ruleIntervalForBaseline(head))
	}
	state := dataquality.Evaluate(*snapshot, observation, time.Now().UTC())
	return &ruleExecution{
		result: buildRuleResult(runID, &ruleExecutionOutcome{
			ruleID:      task.ruleID,
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
	if head == nil {
		// A manual run's queue entry carries only the rule id, and the store has no
		// lookup by internal id. The revision's target identifies the rule closely
		// enough to read its head, which the problem ledger and the notifications
		// need for the owner, severity, mute and resource name.
		head = r.resolveRuleHead(ctx, revision.RuleID, revision.TargetGUID)
		if head != nil && (head.Deleted || head.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED) {
			return nil, head, errRuleRetired
		}
	}
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
func (r *Runner) ruleBaseline(ctx context.Context, ruleID int64, interval time.Duration) *float64 {
	results, err := r.stores.ListRecentQualityRuleResults(ctx, ruleID, 1)
	if err != nil {
		slog.Warn("failed to read a quality rule baseline",
			slog.Int64("rule", ruleID),
			log.WithError(err))
		return nil
	}
	return baselineFromResults(results, time.Now().UTC(), interval)
}

// ruleIntervalForBaseline returns the interval a row_count_change baseline must
// still fit inside. A rule whose interval is missing falls back to the runner's
// minimum, so a malformed row cannot make every historical result acceptable.
func ruleIntervalForBaseline(head *store.QualityRuleMessage) time.Duration {
	if head != nil && head.ScheduleInterval > 0 {
		return head.ScheduleInterval
	}
	return minRuleScheduleInterval
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
func baselineFromResults(results []*store.QualityRuleResultMessage, now time.Time, interval time.Duration) *float64 {
	for _, result := range results {
		if result == nil || result.ObservedValue == nil {
			continue
		}
		switch result.Status {
		case storepb.QualityStatus_QUALITY_STATUS_PASS,
			storepb.QualityStatus_QUALITY_STATUS_WARN,
			storepb.QualityStatus_QUALITY_STATUS_FAIL:
			if !baselineIsRecent(result.StartedAt, now, interval) {
				return nil
			}
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

// baselineIsRecent reports whether an observation is still a fair denominator for
// a row-count comparison: no older than a week, and no more than two intervals
// behind. An older one describes a different data shape and would make the
// change percentage meaningless.
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

// settleQualityResults applies one run's committed results to the problem ledger
// and emits the notifications their transitions imply. Both are best effort: the
// results are already stored, so a lost issue write or a lost notification is
// logged and never turns a completed run into a failure.
func (r *Runner) settleQualityResults(ctx context.Context, runID int64, runResourceName, workspaceID string, executions []*ruleExecution, storm bool, now time.Time) {
	for _, execution := range executions {
		if execution == nil || execution.result == nil {
			continue
		}
		r.settleRuleExecution(ctx, runID, runResourceName, workspaceID, execution, storm, now)
	}
}

// settleRuleExecution decides what one result means for the rule's open issue and
// its owner's inbox. The issue read is the only state the planner needs: the
// open issue row and the rule's previous status decide the transition together.
func (r *Runner) settleRuleExecution(ctx context.Context, runID int64, runResourceName, workspaceID string, execution *ruleExecution, storm bool, now time.Time) {
	result := execution.result
	issue, err := r.stores.GetOpenQualityIssueForRule(ctx, result.RuleID)
	if err != nil {
		slog.Warn("failed to read the open quality issue of a rule",
			slog.Int64("rule", result.RuleID),
			log.WithError(err))
	} else {
		action := dataquality.PlanIssueTransition(execution.previous, result.Status, issue != nil)
		switch action {
		case dataquality.IssueActionOpen:
			r.openQualityIssue(ctx, runID, runResourceName, workspaceID, execution, storm, now)
		case dataquality.IssueActionTouch:
			if issue != nil {
				if err := r.stores.TouchQualityIssue(ctx, issue.ID, runID, execution.severity(), now); err != nil {
					slog.Warn("failed to touch a quality issue",
						slog.Int64("issue", issue.ID),
						log.WithError(err))
				}
			}
		case dataquality.IssueActionResolve:
			if issue != nil {
				r.resolveQualityIssue(ctx, runResourceName, workspaceID, execution, issue, now)
			}
		case dataquality.IssueActionNone:
			// A passing run without an issue, or a status that says nothing about
			// the problem: the ledger is already correct.
		default:
			// An action this build does not know is a defect, not a reason to stop
			// the run; the result is stored and the next run will retry the ledger.
			slog.Warn("unknown quality issue action",
				slog.Int64("rule", result.RuleID),
				slog.Int("action", int(action)))
		}
	}

	// An execution failure is its own alert: it must reach someone even when the
	// rule is muted, because the mute is about the problem the rule watches, not
	// about the rule being broken. A storm merges these into one summary.
	if result.Status == storepb.QualityStatus_QUALITY_STATUS_ERROR && !storm {
		r.notifyExecutionFailed(ctx, workspaceID, runResourceName, execution, now)
	}
}

// openQualityIssue records a new open problem and, unless the run is a storm or
// the rule is muted, announces it.
func (r *Runner) openQualityIssue(ctx context.Context, runID int64, runResourceName, workspaceID string, execution *ruleExecution, storm bool, now time.Time) {
	resourceID, err := common.RandomString(qualityIssueResourceIDLength)
	if err != nil {
		slog.Warn("failed to generate a quality issue resource id", log.WithError(err))
		return
	}
	created, err := r.stores.OpenQualityIssue(ctx, &store.QualityIssueMessage{
		ResourceID:      resourceID,
		RuleID:          execution.result.RuleID,
		TargetGUID:      execution.targetGUID(),
		TargetType:      execution.targetType(),
		Status:          storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		Severity:        execution.severity(),
		FirstSeenRunID:  &runID,
		LastSeenRunID:   &runID,
		FirstSeenAt:     now,
		LastSeenAt:      now,
		OccurrenceCount: 1,
	})
	if err != nil {
		slog.Warn("failed to open a quality issue",
			slog.Int64("rule", execution.result.RuleID),
			log.WithError(err))
		return
	}
	if created == nil {
		// The partial unique index refused the insert: another writer opened the
		// rule's issue between our read and this write. It is not a new problem, so
		// it is not announced again.
		return
	}
	if storm || qualityRuleMuted(execution.head, now) {
		return
	}
	r.notifyIssueOpened(ctx, workspaceID, runResourceName, execution, created, now)
}

// resolveQualityIssue closes a recovered problem and tells its recipient, unless
// the rule or the issue is muted. A recovery is never suppressed by a storm: a
// storm is a burst of failures, and hiding the recoveries would leave the inbox
// with only the alarming half of the story.
func (r *Runner) resolveQualityIssue(ctx context.Context, runResourceName, workspaceID string, execution *ruleExecution, issue *store.QualityIssueMessage, now time.Time) {
	if err := r.stores.ResolveQualityIssue(ctx, issue.ID, storepb.QualityResolutionReason_QUALITY_RESOLUTION_REASON_AUTO_RECOVERED, "", now); err != nil {
		slog.Warn("failed to resolve a quality issue",
			slog.Int64("issue", issue.ID),
			log.WithError(err))
		return
	}
	if qualityRuleMuted(execution.head, now) || qualityIssueMuted(issue, now) {
		return
	}
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     workspaceID,
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED,
		execution:       execution,
		issueResourceID: issue.ResourceID,
		runResourceName: runResourceName,
	})
	r.sendDataQualityNotification(ctx, execution.ownerID(), detail, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, now)
}

// notifyIssueOpened announces a new open problem to its rule's owner, or to the
// workspace administrators when the rule has no owner.
func (r *Runner) notifyIssueOpened(ctx context.Context, workspaceID, runResourceName string, execution *ruleExecution, issue *store.QualityIssueMessage, now time.Time) {
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     workspaceID,
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		execution:       execution,
		issueResourceID: issue.ResourceID,
		runResourceName: runResourceName,
	})
	r.sendDataQualityNotification(ctx, execution.ownerID(), detail, qualityNotificationSeverity(execution.severity()), now)
}

// notifyExecutionFailed announces a rule that could not be executed at all. The
// design makes this alert unmutable and addresses it to the workspace
// administrators only: a broken rule is an operational failure, not a problem the
// rule's owner or mute is meant to absorb.
func (r *Runner) notifyExecutionFailed(ctx context.Context, workspaceID, runResourceName string, execution *ruleExecution, now time.Time) {
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     workspaceID,
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		execution:       execution,
		runResourceName: runResourceName,
	})
	r.sendDataQualityNotification(ctx, execution.ownerID(), detail, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, now)
}

// sendDataQualityNotification addresses one message and hands it to the service.
// Delivery is best effort: the notification service logs its own failures, so a
// failed write only adds a log line here.
func (r *Runner) sendDataQualityNotification(ctx context.Context, ownerID *int32, detail *storepb.DataQualityDetail, severity storepb.NotificationSeverity, now time.Time) {
	if r.notifier == nil || detail == nil {
		return
	}
	message := notification.DataQualityMessage(severity, detail)
	message.DedupeKey = notification.DataQualityDedupeKey(detail.GetKind(), qualityNotificationTarget(detail), now)
	recipientID, toAdmins := qualityNotificationRecipient(detail.GetKind(), ownerID)
	if toAdmins {
		if err := r.notifier.SendToWorkspaceAdmins(ctx, message); err != nil {
			notification.LogFailure(err, slog.String("quality_event", detail.GetKind().String()))
		}
		return
	}
	message.RecipientId = recipientID
	if err := r.notifier.Send(ctx, message); err != nil {
		notification.LogFailure(err, slog.String("quality_event", detail.GetKind().String()))
	}
}

// reportQualityStorm replaces a burst of per-rule notifications with one summary
// addressed to the workspace administrators. It is sent after the run finished,
// so the run it points at is terminal when the recipient opens it.
func (r *Runner) reportQualityStorm(ctx context.Context, runResourceName string, failed, total int32, now time.Time) {
	if r.notifier == nil {
		return
	}
	detail := buildStormDetail(runResourceName, failed, total)
	r.sendDataQualityNotification(ctx, nil, detail, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, now)
}

// dataQualityDetailInput is everything a notification detail is built from. It is
// a struct so the mapping is one function instead of one per event kind.
type dataQualityDetailInput struct {
	workspaceID     string
	kind            storepb.DataQualityNotificationKind
	execution       *ruleExecution
	issueResourceID string
	runResourceName string
}

// buildDataQualityDetail maps one execution to the structured payload the client
// renders. It deliberately carries no sample rows or prose: the detail is a
// pointer into the product, and the notification table is kept forever.
func buildDataQualityDetail(in dataQualityDetailInput) *storepb.DataQualityDetail {
	execution := in.execution
	result := execution.result
	detail := &storepb.DataQualityDetail{
		Kind:       in.kind,
		Rule:       qualityRuleResourceName(in.workspaceID, execution.ruleResourceID()),
		RuleTitle:  execution.ruleTitle(),
		TargetGuid: execution.targetGUID(),
		Run:        in.runResourceName,
		Severity:   execution.severity(),
		FromStatus: statusOrUnspecified(execution.previous),
		ToStatus:   result.Status,
		FailedRows: result.FailedRows,
		TotalRows:  result.TotalRows,
	}
	if execution.snapshot != nil {
		detail.TargetPath = qualityTargetDisplayPath(execution.snapshot.Target.Path)
	}
	if result.ObservedValue != nil {
		detail.ObservedValue = *result.ObservedValue
		detail.ObservedUnit = qualityObservedUnit(result.ObservedKind)
	}
	if in.issueResourceID != "" {
		detail.Issue = qualityIssueResourceName(in.workspaceID, in.issueResourceID)
	}
	if in.kind == storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED {
		detail.ErrorCode = result.ErrorCode
	}
	return detail
}

// buildStormDetail is the summary one storm notification carries: how many rules
// failed out of how many the run executed, and the run that merged them.
func buildStormDetail(runResourceName string, failed, total int32) *storepb.DataQualityDetail {
	return &storepb.DataQualityDetail{
		Kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY,
		Run:             runResourceName,
		Severity:        storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		FailedRuleCount: failed,
		TotalRuleCount:  total,
	}
}

// isQualityRunStorm reports whether one run failed enough rules to be summarized
// instead of announced rule by rule. Only FAIL results count: a run that could
// not execute its rules is a different alert, not a data-quality storm.
func isQualityRunStorm(results []*store.QualityRuleResultMessage) bool {
	failed := 0
	for _, result := range results {
		if result != nil && result.Status == storepb.QualityStatus_QUALITY_STATUS_FAIL {
			failed++
		}
	}
	return failed > QualityStormThreshold
}

// qualityNotificationTarget returns the resource the dedupe key is scoped to: the
// issue for a problem transition, the rule for an execution failure, the run for
// a storm.
func qualityNotificationTarget(detail *storepb.DataQualityDetail) string {
	switch detail.GetKind() {
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED:
		if detail.GetIssue() != "" {
			return detail.GetIssue()
		}
		return detail.GetRule()
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED:
		return detail.GetRule()
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY:
		return detail.GetRun()
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

// qualityNotificationRecipient picks the inbox a data-quality message goes to.
// A problem transition goes to the rule's owner when it has one and to the
// workspace administrators otherwise, so whoever owns the rule is the one asked
// to look. An execution failure and a storm summary go to the administrators
// only (design §8.2): the owner already sees the failed execution on the run
// page, and paging them again for every broken rule would bury the problem
// alerts that actually need their attention.
func qualityNotificationRecipient(kind storepb.DataQualityNotificationKind, ownerID *int32) (recipientID int32, toAdmins bool) {
	switch kind {
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY:
		return 0, true
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_RECOVERED:
		if ownerID == nil || *ownerID <= 0 {
			return 0, true
		}
		return *ownerID, false
	case storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_UNSPECIFIED:
		return 0, true
	default:
		return 0, true
	}
}

// qualityNotificationSeverity maps the rule's severity onto the notification
// envelope's three levels.
func qualityNotificationSeverity(severity storepb.QualitySeverity) storepb.NotificationSeverity {
	switch severity {
	case storepb.QualitySeverity_QUALITY_SEVERITY_WARNING:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING
	case storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		storepb.QualitySeverity_QUALITY_SEVERITY_CRITICAL:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR
	case storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING
	default:
		return storepb.NotificationSeverity_NOTIFICATION_SEVERITY_WARNING
	}
}

// qualityRuleMuted reports whether the rule's mute window suppresses notifications
// about the problem it watches. It never suppresses execution-failure alerts.
func qualityRuleMuted(head *store.QualityRuleMessage, now time.Time) bool {
	if head == nil || head.MuteUntil == nil {
		return false
	}
	return now.Before(*head.MuteUntil)
}

// qualityIssueMuted reports whether one issue's own mute window suppresses its
// problem notifications.
func qualityIssueMuted(issue *store.QualityIssueMessage, now time.Time) bool {
	if issue == nil || issue.MutedUntil == nil {
		return false
	}
	return now.Before(*issue.MutedUntil)
}

// qualityObservedUnit is the display unit of an observation, matching the units
// the frontend labels: row counts, percentages, seconds and timestamps.
func qualityObservedUnit(kind storepb.QualityObservedKind) string {
	switch kind {
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT:
		return "rows"
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_RATIO:
		return "%"
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_SECONDS:
		return "s"
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_TIMESTAMP:
		return "timestamp"
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_NUMBER:
		return ""
	case storepb.QualityObservedKind_QUALITY_OBSERVED_KIND_UNSPECIFIED:
		return ""
	default:
		return ""
	}
}

// qualityTargetDisplayPath renders the target path snapshot as
// instance/database/schema/table[.column], skipping the containers an engine does
// not have. It is display text, not an identity.
func qualityTargetDisplayPath(path *storepb.QualityTargetPath) string {
	if path == nil {
		return ""
	}
	segments := make([]string, 0, 4)
	for _, segment := range []string{path.GetInstance(), path.GetDatabase(), path.GetSchema(), path.GetObject()} {
		if segment != "" {
			segments = append(segments, segment)
		}
	}
	display := strings.Join(segments, "/")
	if path.GetColumn() != "" {
		display += "." + path.GetColumn()
	}
	return display
}

// statusOrUnspecified turns the nullable "previous status" into the detail's
// non-nullable enum. UNSPECIFIED means "there was no earlier result", which the
// client renders as "first result" rather than as a transition from PASS.
func statusOrUnspecified(status *storepb.QualityStatus) storepb.QualityStatus {
	if status == nil {
		return storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED
	}
	return *status
}

// qualityRuleResourceName formats a rule resource name. An empty workspace is the
// degraded form used when the workspace could not be read; it still keeps the
// detail parseable.
func qualityRuleResourceName(workspaceID, resourceID string) string {
	if resourceID == "" {
		return ""
	}
	if workspaceID == "" {
		return "qualityRules/" + resourceID
	}
	return "workspaces/" + workspaceID + "/qualityRules/" + resourceID
}

// qualityIssueResourceName formats an issue resource name.
func qualityIssueResourceName(workspaceID, resourceID string) string {
	if resourceID == "" {
		return ""
	}
	if workspaceID == "" {
		return "qualityIssues/" + resourceID
	}
	return "workspaces/" + workspaceID + "/qualityIssues/" + resourceID
}

// qualityRunResourceName formats a run resource name. An empty resource id means
// the run row could not be resolved, and the caller substitutes the database id.
func qualityRunResourceName(workspaceID, resourceID string) string {
	if resourceID == "" {
		return ""
	}
	if workspaceID == "" {
		return "qualityRuns/" + resourceID
	}
	return "workspaces/" + workspaceID + "/qualityRuns/" + resourceID
}

// previousQualityStatus returns the rule's last result status before this run,
// which is what tells a recovery apart from a rule that never failed. A missing
// or unreadable history is treated as "no previous result": opening an issue is
// still correct and the partial unique index prevents a duplicate.
func (r *Runner) previousQualityStatus(ctx context.Context, ruleID int64) *storepb.QualityStatus {
	results, err := r.stores.ListRecentQualityRuleResults(ctx, ruleID, 1)
	if err != nil {
		slog.Warn("failed to read a quality rule's previous result",
			slog.Int64("rule", ruleID),
			log.WithError(err))
		return nil
	}
	for _, result := range results {
		if result == nil {
			continue
		}
		status := result.Status
		return &status
	}
	return nil
}

// workspaceID is the workspace every notification resource name is scoped to. A
// read failure leaves the name in its degraded form rather than dropping the
// message: a delivery is still useful without the workspace prefix.
func (r *Runner) workspaceID(ctx context.Context) string {
	workspaceID, err := r.stores.GetWorkspaceID(ctx)
	if err != nil {
		slog.Warn("failed to read the workspace id for a quality notification", log.WithError(err))
		return ""
	}
	return workspaceID
}

// resolveRuleHead reads the mutable rule row of a manual run's rule. The queue
// carries only the rule id and the store has no lookup by internal id, so the
// rule is found through its revision's target and matched by id; a target holds a
// handful of rules, so the read stays bounded.
func (r *Runner) resolveRuleHead(ctx context.Context, ruleID int64, targetGUID string) *store.QualityRuleMessage {
	if targetGUID == "" {
		return nil
	}
	rules, err := r.stores.ListQualityRules(ctx, &store.FindQualityRuleMessage{TargetGUID: &targetGUID})
	if err != nil {
		slog.Warn("failed to resolve a manual quality rule's head",
			slog.Int64("rule", ruleID),
			log.WithError(err))
		return nil
	}
	for _, rule := range rules {
		if rule.ID == ruleID {
			return rule
		}
	}
	return nil
}

// manualRunResourceID resolves a manual run's resource id for the notification
// detail. The queue entry carries only the database id, and the store has no
// lookup by id, so the run is found among the manual runs that are still
// waiting or executing. A miss is not fatal: the caller falls back to the
// database id in the detail.
func (r *Runner) manualRunResourceID(ctx context.Context, runID int64) string {
	statuses := []storepb.QualityRunStatus{
		storepb.QualityRunStatus_QUALITY_RUN_STATUS_PENDING,
		storepb.QualityRunStatus_QUALITY_RUN_STATUS_RUNNING,
	}
	trigger := storepb.QualityRunTrigger_QUALITY_RUN_TRIGGER_MANUAL
	limit := 200
	runs, err := r.stores.ListQualityRuns(ctx, &store.FindQualityRunMessage{
		Statuses: statuses,
		Trigger:  &trigger,
		Limit:    &limit,
	})
	if err != nil {
		slog.Warn("failed to resolve a manual quality run's resource id",
			slog.Int64("run", runID),
			log.WithError(err))
		return ""
	}
	for _, run := range runs {
		if run.ID == runID {
			return run.ResourceID
		}
	}
	return ""
}

// ruleResourceID returns the rule's public resource id, or "" when the mutable
// head could not be read.
func (e *ruleExecution) ruleResourceID() string {
	if e.head == nil {
		return ""
	}
	return e.head.ResourceID
}

// ruleTitle returns the rule's display title, or "" when the head is missing.
func (e *ruleExecution) ruleTitle() string {
	if e.head == nil {
		return ""
	}
	return e.head.Title
}

// ownerID returns the rule's owner principal id, or nil.
func (e *ruleExecution) ownerID() *int32 {
	if e.head == nil {
		return nil
	}
	return e.head.OwnerID
}

// severity returns the rule's current severity. The immutable revision carries
// the severity the result was judged with, but the notification and the issue row
// report the rule's current one; the head wins when it is available.
func (e *ruleExecution) severity() storepb.QualitySeverity {
	if e.head != nil {
		return e.head.Severity
	}
	if e.snapshot != nil {
		return e.snapshot.Severity
	}
	return storepb.QualitySeverity_QUALITY_SEVERITY_UNSPECIFIED
}

// targetGUID returns the registry GUID the result is about.
func (e *ruleExecution) targetGUID() string {
	if e.snapshot != nil && e.snapshot.Target.GUID != "" {
		return e.snapshot.Target.GUID
	}
	if e.result != nil {
		return e.result.TargetGUID
	}
	return ""
}

// targetType returns the registry object type the result is about.
func (e *ruleExecution) targetType() storepb.MetaType {
	if e.snapshot != nil {
		return e.snapshot.Target.MetaType
	}
	if e.result != nil {
		return e.result.TargetType
	}
	return storepb.MetaType_UNSPECIFIED
}

// requiresReadOnlySource reports whether executing this rule type needs a
// connection to the target database. Metadata rules (schema_drift) observe the
// registry instead, so they must run without a READ_ONLY data source; every
// query rule still requires one.
func requiresReadOnlySource(ruleType storepb.QualityRuleType) bool {
	return !dataquality.IsMetadataRule(ruleType)
}
