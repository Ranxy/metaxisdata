// Package qualityrunner executes data-quality rules on their schedule and on
// request.
//
// A quality_run row is an internal dispatch batch that belongs to exactly one
// target: it admits one connection lease per instance, groups the target's due
// checks, aggregates a storm notification and cascades retention. It is never
// exposed through the API as a write path. The durable, user-facing unit is
// quality_execution: one rule checked once, planned PENDING by the scheduler (or
// by the API for a manual run), marked RUNNING per rule, and closed by
// FinishQualityExecution with the verdict and its evidence.
//
// This runner is the only component that opens a connection to the target
// database for a quality check: the rule engine compiles and judges, the API
// writes and reads, and this runner decides when a rule runs and records what
// happened. One batch covers one instance, so a batch's checks share the
// instance's connection budget and its failures are reported together. A batch
// is admitted against that budget as a whole and holds one lease while it
// executes; a batch that cannot get one waits, then is deferred or abandoned, and
// never records a capacity failure as a rule execution.
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
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

const (
	// tickInterval is how often the scheduler looks for due rules. It is not
	// jittered: the query is a single indexed read and the pass is cheap.
	tickInterval = 10 * time.Second
	// dueRuleScanLimit is the target-complete work budget for one tick, counted in
	// rules. The store takes whole due targets in first-due order until the budget
	// is met; a target's enabled rules are never cut by it.
	dueRuleScanLimit = 200
	// dueTargetScanLimit bounds how many targets one tick turns into batches. A
	// target is now its own batch, so without this cap a tick with many small
	// targets would create one batch per target and multiply admission and store
	// work; the rule budget alone does not bound that.
	dueTargetScanLimit = 50
	// maxExecutionsPerBatch bounds one internal batch. It matches the per-target
	// rule cap, so every legal target fits one batch: a batch covers one instance,
	// and a target larger than the cap can only come from data that bypassed the API
	// limit. Raising the cap trades that guarantee against the 15-minute execution
	// budget; manual target runs already plan up to the same rule count in one batch.
	maxExecutionsPerBatch = 100
	// maxConcurrentInstanceBatches is the per-instance/global concurrency ceiling
	// for scheduled batches: the design allows two instances to execute at once,
	// and one instance's batches always run serially.
	maxConcurrentInstanceBatches = 2
	// manualQueueCapacity is how many manual batches may wait for the runner. A
	// manual selection is split into one batch per target, so a 200-rule selection
	// can create up to 200 batches. The API turns a full queue into
	// ResourceExhausted instead of blocking a request.
	manualQueueCapacity = 512
	// scheduledAdmissionWait bounds how long one scheduled batch waits for the
	// instance's connection budget before the pass leaves its rules due for the
	// next tick. Waiting is what turns normal contention into backpressure rather
	// than a durable ERROR execution.
	scheduledAdmissionWait = 2 * time.Minute
	// manualAdmissionWait bounds how long a manual batch waits for the instance's
	// connection budget. The executions are visible as PENDING while it waits;
	// past the budget they are abandoned instead of being left for the restart
	// sweep.
	manualAdmissionWait = 5 * time.Minute
	// abandonWriteTimeout bounds the best-effort write that closes a batch which
	// never executed. It uses a detached context because the runner's own is
	// usually cancelled by the shutdown that ended the wait.
	abandonWriteTimeout = 10 * time.Second
	// staleExecutionCutoff is how long a PENDING/RUNNING batch or execution may
	// predate the process before the startup sweep abandons it. A restart does not
	// retry such work; the next scheduled pass runs the rules again.
	staleExecutionCutoff = 15 * time.Minute
	// batchExecutionTimeout bounds one batch, including every rule and every
	// query in it. It is the design's 15-minute execution budget.
	batchExecutionTimeout = 15 * time.Minute
	// ruleQueryTimeout bounds one rule's aggregate query. It is fixed: the rule
	// config has no per-rule timeout override.
	ruleQueryTimeout = 60 * time.Second
	// ruleResultMaxBytes bounds the aggregate row and the failure sample the
	// executor keeps in memory.
	ruleResultMaxBytes = 1 << 20
	// ruleSampleLimit is how many failing rows one execution keeps as evidence.
	ruleSampleLimit = 20
	// minRuleScheduleInterval is the runner's backstop for a rule whose interval is
	// missing or nonsense. The API validates 15m..7d at write time; this keeps a
	// bad row from becoming due on every tick.
	minRuleScheduleInterval = 15 * time.Minute
	// maxErrorMessageLength bounds the error text stored on an execution. Error
	// details are for an operator, not a log file.
	maxErrorMessageLength = 1024
	// maxBaselineAge bounds how old a row_count_change baseline may be. An older
	// observation describes a different data shape.
	maxBaselineAge = 7 * 24 * time.Hour
	// QualityStormThreshold is how many failing executions in one batch turn
	// per-rule notifications into a single summary. A batch that fails more rules
	// than this would otherwise write one inbox message per rule for a single
	// incident.
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
// executions today as its settings allow. The tally is a safety valve against a
// dense schedule, not a correctness boundary: when it trips, the due rules stay
// due and the next day resumes them.
func (r *Runner) overDailyExecutionQuota(ctx context.Context, now time.Time) bool {
	limit := r.settings.DailyExecutionLimit
	if limit <= 0 {
		return false
	}
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	count, err := r.stores.CountQualityExecutionsSince(ctx, dayStart)
	if err != nil {
		slog.Warn("failed to count today's quality executions", log.WithError(err))
		return false
	}
	if count < limit {
		return false
	}
	slog.Warn("daily quality execution quota reached; skipping the due scan",
		slog.Int("executions_today", count), slog.Int("limit", limit))
	return true
}

// executionTask identifies one planned quality_execution row a batch has to
// execute. ruleID is known for a scheduled plan, where the due scan read it; a
// queued manual batch carries the row ids the API planned and resolves each rule
// id from the row itself.
type executionTask struct {
	executionID int64
	ruleID      int64
}

// batchExecutionStore is the slice of the store that executing one dispatch
// batch needs. *store.Store implements it; the runner-level tests inject a fake
// so the incomplete-batch accounting can be exercised without a database.
type batchExecutionStore interface {
	MarkQualityRunRunning(ctx context.Context, batchID int64, at time.Time) error
	GetQualityExecution(ctx context.Context, id int64) (*store.QualityExecutionMessage, error)
	ListRecentQualityExecutions(ctx context.Context, ruleID int64, limit int) ([]*store.QualityExecutionMessage, error)
	MarkQualityExecutionRunning(ctx context.Context, id int64, at time.Time) error
	FinishQualityExecution(ctx context.Context, execution *store.QualityExecutionMessage) error
	AbandonQualityExecutions(ctx context.Context, batchID int64, at time.Time) (int64, error)
	FinishQualityRun(ctx context.Context, batchID int64, status storepb.QualityExecutionState, outcome storepb.QualityStatus, counts *storepb.QualityStatusCounts, at time.Time) error
}

// manualRun is one queued manual batch. executions is the exact selection the
// API planned: a manual batch must not silently widen to every enabled rule of
// the target. instanceResourceID is the instance the API grouped the batch on;
// the queue carries it because admission needs the instance's connection ceiling
// before the batch's rows are read.
type manualRun struct {
	batchID            int64
	instanceResourceID string
	executions         []executionTask
}

// dueBatchPlan is one internal batch the due scan is about to create: one
// instance and exactly one target, with at most maxExecutionsPerBatch rules. A
// target larger than the cap (only data that bypassed the API limit) spans
// several plans; only the plan holding its final chunk advances its schedule.
type dueBatchPlan struct {
	instanceResourceID string
	target             *store.DueQualityTarget
	rules              []*store.QualityRuleMessage
	advance            bool
}

// batchAdmission is what a batch holds while it executes: the instance whose
// connection budget it leased (empty when it needed none) and the priority it was
// admitted with. A rule whose target moved to another instance after admission
// cannot use it; see ruleConnectionLease.
type batchAdmission struct {
	instanceResourceID string
	priority           state.ConnectionPriority
}

// batchContractFilters caches one target's contract filter for the duration of a
// batch. Rules of the same table usually execute together, so the batch reads the
// contract once instead of once per execution. A metadata rule compiles no SQL, so
// it never asks for a filter: the filter would be carried but never rendered.
type batchContractFilters struct {
	load     func(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error)
	byTarget map[contractFilterKey]*storepb.QualityFilter
}

// contractFilterKey identifies one target: the GUID alone is not unique across
// object types.
type contractFilterKey struct {
	targetGUID string
	metaType   storepb.MetaType
}

// newBatchContractFilters starts an empty cache for one batch.
func newBatchContractFilters(load func(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error)) *batchContractFilters {
	return &batchContractFilters{
		load:     load,
		byTarget: make(map[contractFilterKey]*storepb.QualityFilter),
	}
}

// forRule returns the contract filter to AND into one rule's statement, reading it
// at most once per target per batch. A metadata rule (schema_drift) observes the
// registry and compiles no SQL, so it returns no filter without a store read.
func (f *batchContractFilters) forRule(ctx context.Context, ruleType storepb.QualityRuleType, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error) {
	if dataquality.IsMetadataRule(ruleType) {
		return nil, nil
	}
	key := contractFilterKey{targetGUID: targetGUID, metaType: metaType}
	if cached, ok := f.byTarget[key]; ok {
		return cached, nil
	}
	filter, err := f.load(ctx, targetGUID, metaType)
	if err != nil {
		return nil, err
	}
	f.byTarget[key] = filter
	return filter, nil
}

// ruleExecution is one execution's stored row plus the inputs the problem ledger
// and the notifications need after the row is written: the mutable head (owner,
// severity, mute, resource id), the immutable definition that was executed, and
// the rule's status before this execution.
type ruleExecution struct {
	result   *store.QualityExecutionMessage
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
	// notifier delivers the problem and execution notifications an execution
	// implies. It is best effort: a delivery failure is logged and never fails the
	// batch.
	// A nil notifier keeps the runner silent, which is what its tests want.
	notifier *notification.Service
	// metrics records execution and rule outcomes; a nil value disables
	// instrumentation.
	metrics *metrics.Metrics

	// settings is the workspace's data-quality policy, reloaded on every scan and
	// manual batch. It starts at the documented defaults so a fresh process with
	// an unreadable setting still behaves sanely.
	settings dataquality.Settings

	// manual carries the batches the API created. It is buffered so enqueueing
	// never blocks a request; a full queue is reported back as false.
	manual chan manualRun
	// inflight holds the rule ids currently being executed, so a second execution
	// that finds the same rule due records SKIPPED instead of executing it twice.
	inflight map[int64]struct{}
	mu       sync.Mutex
}

// NewRunner creates the data-quality runner. The executor is stateless, so one
// instance is shared by every batch. notifier reports opened and recovered
// problems and execution failures; it may be nil.
func NewRunner(stores *store.Store, dbFactory *dbfactory.DBFactory, stateCfg *state.State, notifier *notification.Service, m *metrics.Metrics) *Runner {
	exec := dataquality.NewExecutor()
	exec.Metrics = m
	return &Runner{
		stores:    stores,
		dbFactory: dbFactory,
		stateCfg:  stateCfg,
		exec:      exec,
		notifier:  notifier,
		manual:    make(chan manualRun, manualQueueCapacity),
		inflight:  make(map[int64]struct{}),
		settings:  dataquality.DefaultSettings(),
		metrics:   m,
	}
}

// Name identifies the runner in the server's lifecycle logs.
func (*Runner) Name() string { return "qualityrunner" }

// ManualQueueDepth reports how many manual batches are waiting for the runner. It
// is read by the metrics collector at scrape time.
func (r *Runner) ManualQueueDepth() int {
	return len(r.manual)
}

// EnqueueQualityExecutions offers a manual batch to the runner and returns false
// when the queue is full. instanceResourceID is the instance the API grouped the
// batch on, and executionIDs are the exact quality_execution rows the API planned
// and must have executed: the runner executes those rows and nothing else. The
// call does not block: the API maps false to ResourceExhausted and is responsible
// for abandoning the batch and its executions.
func (r *Runner) EnqueueQualityExecutions(batchID int64, instanceResourceID string, executionIDs []int64) bool {
	select {
	case r.manual <- manualRun{
		batchID:            batchID,
		instanceResourceID: instanceResourceID,
		executions:         manualExecutionTasks(executionIDs),
	}:
		return true
	default:
		r.metrics.CountQualityManualEnqueueRejected()
		return false
	}
}

// Run starts the quality runner and blocks until ctx is cancelled. Manual batches
// take precedence over the periodic due scan.
func (r *Runner) Run(ctx context.Context) {
	r.abandonStaleWork(ctx)
	r.refreshSettings(ctx)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		// Drain the manual queue before looking at the ticker, so a pending user
		// request is not delayed by a due scan that is ready at the same moment.
		select {
		case <-ctx.Done():
			return
		case manual := <-r.manual:
			r.executeManualBatch(ctx, manual)
			continue
		default:
		}

		select {
		case <-ctx.Done():
			return
		case manual := <-r.manual:
			r.executeManualBatch(ctx, manual)
		case <-ticker.C:
			r.scanDue(ctx)
		}
	}
}

// abandonStaleWork closes the batches and executions a previous process left
// PENDING or RUNNING. It is a startup-only sweep: work whose process is gone is
// not retried, and its results are not invented; the rules run again on their
// next schedule. Executions are swept explicitly as well as through their batch,
// so a row whose batch write was lost still reaches a terminal state.
func (r *Runner) abandonStaleWork(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-staleExecutionCutoff)
	abandonedExecutions, err := r.stores.AbandonStaleQualityExecutions(ctx, cutoff)
	if err != nil {
		slog.Error("failed to abandon stale quality executions", log.WithError(err))
	} else if abandonedExecutions > 0 {
		for range abandonedExecutions {
			r.metrics.CountQualityExecutionAbandoned(metrics.QualityAbandonedStaleSweep)
		}
		slog.Warn("abandoned quality executions left behind by a previous process",
			slog.Int64("count", abandonedExecutions),
			slog.Time("started_before", cutoff))
	}

	abandonedBatches, err := r.stores.AbandonStaleQualityRuns(ctx, cutoff)
	if err != nil {
		slog.Error("failed to abandon stale quality batches", log.WithError(err))
		return
	}
	if abandonedBatches > 0 {
		slog.Warn("abandoned internal quality batches left behind by a previous process",
			slog.Int64("count", abandonedBatches),
			slog.Time("started_before", cutoff))
	}
}

// scanDue creates and executes one internal batch per instance for the targets
// whose schedule is due. A due target may exceed the per-batch limit or span
// several instances: its checks are grouped with the target, chunked into batches
// of at most maxExecutionsPerBatch, and one instance's batches execute serially
// while at most two instances execute at once.
//
// The store's due query selects whole targets in first-due order until the work
// budget is met, so a target's enabled checks never straddle two scheduler ticks
// just because the budget fell inside its group.
func (r *Runner) scanDue(ctx context.Context) {
	// The planning cost is settings refresh, the quota check, the due-target query,
	// the chunking, the batch row and the planned execution rows. It deliberately
	// stops before wp.Wait(): the scans below execute the batches, and including
	// them would turn this histogram into a copy of batch duration.
	planningStartedAt := time.Now()
	observePlanning := func() { r.metrics.ObserveQualityDueScan(time.Since(planningStartedAt)) }

	now := time.Now().UTC()

	r.refreshSettings(ctx)
	if r.overDailyExecutionQuota(ctx, now) {
		observePlanning()
		return
	}
	targets, err := r.stores.FindDueQualityTargets(ctx, now, dueRuleScanLimit, dueTargetScanLimit)
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

	plans := planDueBatches(targets, maxExecutionsPerBatch)
	observePlanning()
	if len(plans) == 0 {
		return
	}

	wp := pool.New().WithMaxGoroutines(maxConcurrentInstanceBatches)
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
				r.startScheduledBatch(ctx, plan)
			}
		})
		start = end
	}
	wp.Wait()
}

// startScheduledBatch creates one internal batch and its planned execution rows,
// then executes them. The batch row exists before any rule runs, so an execution
// always names a batch; the execution rows themselves are the visible plan.
func (r *Runner) startScheduledBatch(ctx context.Context, plan dueBatchPlan) {
	rules := make([]*store.QualityRuleMessage, 0, len(plan.rules))
	for _, rule := range plan.rules {
		if rule != nil {
			rules = append(rules, rule)
		}
	}
	if len(rules) == 0 {
		return
	}

	instance, err := r.instanceForAdmission(ctx, plan.instanceResourceID)
	if err != nil {
		r.metrics.CountQualityExecutionsDeferred(metrics.QualityDeferralInstanceReadError)
		slog.Warn("failed to read the instance of a scheduled quality batch; leaving its rules due",
			slog.String("instance", plan.instanceResourceID),
			log.WithError(err))
		return
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
		return
	}
	// The release is idempotent and executeBatch releases it before the store work,
	// so this defer only covers the returns before executeBatch is reached.
	defer release()

	now := time.Now().UTC()
	resourceID, err := common.RandomString(12)
	if err != nil {
		slog.Error("failed to generate an internal quality batch resource id", log.WithError(err))
		return
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
		return
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
		return
	}
	for range planned {
		r.metrics.CountQualityExecutionCreated(metrics.QualityTriggerScheduled)
	}
	// Advance the schedule only after the rows are planned: the tick that picks a
	// target up must not pick it up again, and a failed plan leaves the target due
	// for the next pass. A target split across chunks is advanced by the plan that
	// holds its final chunk.
	if plan.advance {
		r.advanceScheduledTarget(ctx, plan.target)
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
		return
	}
	r.executeBatch(ctx, batch.ID, plan.instanceResourceID, tasks, admission, release)
}

// plannedExecution is the PENDING row the scheduler plans before executing a due
// rule. The revision, target snapshot and instance are the due scan's view; the
// runner may execute a newer revision and FinishQualityExecution then records the
// revision it really judged.
func plannedExecution(batchID int64, instanceResourceID string, rule *store.QualityRuleMessage, at time.Time) *store.QualityExecutionMessage {
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

// instanceForAdmission loads the instance a batch targets so the batch can be
// admitted against its connection ceiling. A nil instance means the row is gone:
// the executions still run because target resolution records ORPHANED for their
// rules without opening a connection. A read error is transient and defers the
// batch, because executing it could open a connection outside the budget.
func (r *Runner) instanceForAdmission(ctx context.Context, resourceID string) (*store.InstanceMessage, error) {
	if resourceID == "" {
		// A rule whose target GUID could not be attributed to an instance cannot
		// open a connection either; its execution records SKIPPED(ORPHANED).
		return nil, nil
	}
	return r.stores.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &resourceID})
}

// admitBatch reserves the instance's connection budget for one batch and returns
// its lease plus the admission later rules are checked against. A batch holds one
// lease for its whole query phase: its rules execute serially against the same
// instance and need at most one session at a time, and taking the lease up front
// is what keeps a mid-batch admission from failing. A nil instance needs none: its
// rules are ORPHANED before a driver is opened.
func (r *Runner) admitBatch(ctx context.Context, instance *store.InstanceMessage, priority state.ConnectionPriority, wait time.Duration) (func(), batchAdmission, bool) {
	// A gone or metadata-less instance is ORPHANED by target resolution before a
	// driver is opened, so it needs no lease.
	if instance == nil || instance.Deleted || instance.Metadata == nil {
		return func() {}, batchAdmission{priority: priority}, true
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	release, err := r.stateCfg.AcquireInstanceConnection(waitCtx, state.InstanceConnectionRequest{
		InstanceID:         instance.ResourceID,
		MaximumConnections: instanceConnectionLimit(instance),
		Priority:           priority,
	})
	if err != nil {
		return nil, batchAdmission{}, false
	}
	return release, batchAdmission{instanceResourceID: instance.ResourceID, priority: priority}, true
}

// ruleConnectionLease returns the budget covering one rule's driver. The batch's
// own lease covers the normal case. A rule whose newest revision targets another
// instance than the batch was admitted for needs a lease on that instance, and a
// batch that needed no lease at all (its instance row was gone) must lease the
// instance the rule really resolves to before opening a driver.
//
// The fallback tries once instead of waiting: it runs while the batch already
// holds another instance's lease, and a blocking wait between two concurrent
// batches could each hold what the other wants until both deadlines. A rule that
// cannot be covered is SKIPPED and runs on its next schedule.
func (r *Runner) ruleConnectionLease(instance *store.InstanceMessage, admission batchAdmission) (func(), bool) {
	if instance == nil || instance.Deleted || instance.Metadata == nil {
		// Target resolution reports ORPHANED before a driver is opened.
		return func() {}, true
	}
	if admission.instanceResourceID != "" && admission.instanceResourceID == instance.ResourceID {
		return func() {}, true
	}
	return r.stateCfg.TryAcquireInstanceConnection(state.InstanceConnectionRequest{
		InstanceID:         instance.ResourceID,
		MaximumConnections: instanceConnectionLimit(instance),
		Priority:           admission.priority,
	})
}

// instanceConnectionLimit is the instance's configured connection ceiling, with
// the deployment default when the instance does not set one. A non-positive
// setting means "use the default", not "unlimited"; the schema syncer and the
// connection test read it the same way.
func instanceConnectionLimit(instance *store.InstanceMessage) int {
	maximumConnections := int(instance.Metadata.GetMaximumConnections())
	if maximumConnections <= 0 {
		maximumConnections = common.DefaultInstanceMaximumConnections
	}
	return maximumConnections
}

// abandonUnadmittedBatch closes a manual batch that never got the instance's
// connection budget. It executed nothing, so ABANDONED with no outcome is the
// honest terminal state; leaving it PENDING would defer it to the restart sweep
// and show executions that apparently never end.
func (r *Runner) abandonUnadmittedBatch(ctx context.Context, batchID int64, selected int, reason string) {
	// The runner's context is often already cancelled here (a shutdown ended the
	// wait), but the rows must still reach a terminal state: PENDING executions
	// with no queue entry would otherwise linger until a later startup sweep.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abandonWriteTimeout)
	defer cancel()
	abandoned, err := r.stores.AbandonQualityExecutions(writeCtx, batchID, time.Now().UTC())
	if err != nil {
		slog.Warn("failed to abandon a manual quality batch that never got a connection slot",
			slog.Int64("batch", batchID),
			log.WithError(err))
		// The rows are left to the startup sweep; count the planned work as
		// abandoned so the metric still reflects what the request lost.
		for range selected {
			r.metrics.CountQualityExecutionAbandoned(reason)
		}
		return
	}
	for range abandoned {
		r.metrics.CountQualityExecutionAbandoned(reason)
	}
	if err := r.stores.FinishQualityRun(writeCtx, batchID, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, &storepb.QualityStatusCounts{Total: int32(selected)}, time.Now().UTC()); err != nil {
		slog.Warn("failed to close a manual quality batch that never got a connection slot",
			slog.Int64("batch", batchID),
			log.WithError(err))
	}
}

// abandonEmptyBatch closes an internal batch no execution row was planned for.
func (r *Runner) abandonEmptyBatch(ctx context.Context, batchID int64) {
	if err := r.stores.FinishQualityRun(ctx, batchID, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, nil, time.Now().UTC()); err != nil {
		slog.Warn("failed to close an internal quality batch with no planned executions",
			slog.Int64("batch", batchID),
			log.WithError(err))
	}
}

// abandonBatch abandons a batch's remaining executions and then the batch. It is
// used whenever the batch cannot finish cleanly (a verdict write failed, a task
// was skipped, the batch deadline expired, or the batch could not be marked
// RUNNING): no execution may be left PENDING/RUNNING and the batch must not
// report a partial rollup.
func (r *Runner) abandonBatch(ctx context.Context, st batchExecutionStore, batchID int64, planned int, reason string) {
	// The batch context is usually alive here, but a cancelled one must not stop
	// the terminal writes.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abandonWriteTimeout)
	defer cancel()
	abandoned, err := st.AbandonQualityExecutions(writeCtx, batchID, time.Now().UTC())
	if err != nil {
		// The rows are left to the startup sweep, but the batch itself must still
		// reach a terminal state: an ABANDONED batch with orphaned rows is more
		// honest than a batch that stays RUNNING forever.
		slog.Error("failed to abandon the remaining executions of a quality batch",
			slog.Int64("batch", batchID),
			log.WithError(err))
	}
	for range abandoned {
		r.metrics.CountQualityExecutionAbandoned(reason)
	}
	counts := &storepb.QualityStatusCounts{Total: int32(planned)}
	if err := st.FinishQualityRun(writeCtx, batchID, storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_ABANDONED, storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED, counts, time.Now().UTC()); err != nil {
		slog.Error("failed to abandon a quality batch that could not finish cleanly",
			slog.Int64("batch", batchID),
			log.WithError(err))
	}
}

// loadBatchContractFilter reads one target's contract filter for the batch cache.
// A nil store (the accounting tests) reads as "no contract", and a target without a
// contract row does too.
func (r *Runner) loadBatchContractFilter() func(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error) {
	return func(ctx context.Context, targetGUID string, metaType storepb.MetaType) (*storepb.QualityFilter, error) {
		if r.stores == nil {
			return nil, nil
		}
		contract, err := r.stores.GetQualityContract(ctx, targetGUID, metaType)
		if err != nil || contract == nil {
			return nil, err
		}
		return contract.Settings.GetFilter(), nil
	}
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
		r.reportQualityStorm(ctx, batchID, instanceResourceID, counts.GetFail(), counts.GetTotal(), now)
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

// observeRuleExecution records the per-rule funnel exactly once, right where the
// batch collects each written execution. This loop is the only place that sees
// every verdict an execution attempt produces, so buildExecutionMessage (the pure
// row mapping) must not record it too.
func (r *Runner) observeRuleExecution(execution *ruleExecution) {
	if execution == nil || execution.result == nil {
		return
	}
	ruleType := ruleExecutionType(execution)
	r.metrics.ObserveQualityRuleExecution(ruleType, execution.result.Status, time.Duration(execution.result.DurationMS)*time.Millisecond)
	r.metrics.CountQualityRuleError(ruleType, execution.result.ErrorCode)
}

// ruleExecutionType derives the rule type a metric should label an execution
// with. A rule that failed before its snapshot was read (an inflight skip, a
// retired revision) still names its type on the due scan's head row; a manual
// execution has no head, so the last resort is "unspecified" rather than a wrong
// label.
func ruleExecutionType(execution *ruleExecution) storepb.QualityRuleType {
	if execution == nil {
		return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED
	}
	if execution.snapshot != nil {
		return execution.snapshot.Type
	}
	if execution.head != nil {
		return execution.head.RuleType
	}
	return storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED
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
				result: failureExecutionMessage(planned, startedAt, storepb.QualityStatus_QUALITY_STATUS_ERROR,
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
			result: failureExecutionMessage(planned, startedAt, storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
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
			result: failureExecutionMessage(planned, startedAt, status, code, err.Error()),
			head:   head,
		}
	}

	// fail records one execution the rule could not judge. The snapshot is
	// carried so the row still names the definition and target it was about.
	fail := func(status storepb.QualityStatus, code, message string) *ruleExecution {
		return &ruleExecution{
			result:   failureExecutionMessage(planned, startedAt, status, code, message),
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

// advanceScheduledTarget moves one due target's next run forward. The due scan
// already returned the target, so this is a scheduling bookkeeping step: a
// failure is logged, not fatal, because executing and recording its checks is
// still more useful than dropping the pass.
func (r *Runner) advanceScheduledTarget(ctx context.Context, target *store.DueQualityTarget) {
	if target == nil {
		return
	}
	next := nextRunAt(time.Now().UTC(), target.ScheduleInterval)
	if err := r.stores.AdvanceQualityTargetNextRun(ctx, target.TargetGUID, target.TargetType, next); err != nil {
		slog.Warn("failed to advance a quality target's next schedule",
			slog.String("target", target.TargetGUID),
			log.WithError(err))
	}
}

// loadRuleSnapshot loads the immutable definition an execution is judged with. A
// rule edited after its execution row was planned is executed with its newest
// revision, and FinishQualityExecution records the revision the verdict really
// judged; the planned row is never rewritten by an edit.
func (r *Runner) loadRuleSnapshot(ctx context.Context, ruleID int64) (*dataquality.RuleSnapshot, *store.QualityRuleMessage, error) {
	limit := 1
	revisions, err := r.stores.ListQualityRuleRevisions(ctx, ruleID, &limit, nil)
	if err != nil {
		return nil, nil, err
	}
	if len(revisions) == 0 {
		return nil, nil, common.Errorf(common.NotFound, "quality rule %d has no revision", ruleID)
	}

	revision := revisions[0]
	// The mutable head carries the owner, severity, mute and resource name the
	// problem ledger and the notifications need. It is read by internal id with no
	// lifecycle filter, so an archived or deleted rule still resolves and retires.
	head := r.resolveRuleHead(ctx, ruleID)
	if head != nil && (head.Deleted || head.State == storepb.QualityRuleState_QUALITY_RULE_STATE_ARCHIVED) {
		return nil, head, errRuleRetired
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

// ruleBaseline returns the rule's last evaluated observation, the denominator
// row_count_change compares the current count against. An unreadable baseline is
// not fatal: the rule then reports NOT_EVALUATED, which is the correct "no
// baseline" verdict rather than a made-up comparison.
func (r *Runner) ruleBaseline(ctx context.Context, ruleID int64, interval time.Duration) *float64 {
	executions, err := r.stores.ListRecentQualityExecutions(ctx, ruleID, 1)
	if err != nil {
		slog.Warn("failed to read a quality rule baseline",
			slog.Int64("rule", ruleID),
			log.WithError(err))
		return nil
	}
	return baselineFromExecutions(executions, time.Now().UTC(), interval)
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

// acquireInflight marks one rule as being executed and reports whether this
// caller owns it. The guard is process-local, like the design's in-flight map: a
// second execution of the same rule records SKIPPED instead of executing it
// twice.
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

// errRuleRetired reports that a rule was archived or deleted after it was picked
// up; the execution records SKIPPED rather than executing a retired definition.
var errRuleRetired = errors.New("quality rule is retired")

// ruleExecutionOutcome is everything one execution produced, in the shape the
// execution row needs. Keeping the failure paths and the evaluated path in one
// struct makes buildExecutionMessage the single mapping from a verdict to a
// stored execution.
type ruleExecutionOutcome struct {
	snapshot     *dataquality.RuleSnapshot
	startedAt    time.Time
	finishedAt   time.Time
	observation  dataquality.Observation
	execution    *dataquality.Execution
	state        dataquality.ResultState
	errorCode    string
	errorMessage string
}

// buildExecutionMessage maps one execution outcome onto the row
// FinishQualityExecution writes. The planned PENDING row supplies the identity
// (id, batch, rule, trigger, initiator) and its revision/target snapshot; a
// verdict that read a snapshot overwrites them with what was really judged, and a
// verdict that failed before it could read one keeps the planned values.
func buildExecutionMessage(planned *store.QualityExecutionMessage, outcome *ruleExecutionOutcome) *store.QualityExecutionMessage {
	execution := &store.QualityExecutionMessage{}
	if planned != nil {
		execution.ID = planned.ID
		execution.RunID = planned.RunID
		execution.RuleID = planned.RuleID
		execution.RuleRevision = planned.RuleRevision
		execution.TargetGUID = planned.TargetGUID
		execution.TargetType = planned.TargetType
		execution.TargetPath = planned.TargetPath
		execution.InstanceResourceID = planned.InstanceResourceID
		execution.Trigger = planned.Trigger
		execution.InitiatorID = planned.InitiatorID
	}
	execution.RuleEngineVersion = dataquality.EngineVersion
	execution.State = storepb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED
	execution.Status = outcome.state.Status
	execution.ObservedKind = outcome.observation.ObservedKind
	execution.Thresholds = outcome.state.Thresholds
	execution.FailedRows = outcome.observation.FailedRows
	execution.TotalRows = outcome.observation.TotalRows
	execution.Sampled = outcome.observation.Sampled
	execution.SampleRows = outcome.observation.SampleRows
	execution.Truncated = outcome.observation.Truncated
	execution.ErrorCode = outcome.errorCode
	execution.ErrorMessage = truncateErrorMessage(outcome.errorMessage)
	execution.DurationMS = durationMS(outcome.startedAt, outcome.finishedAt)
	execution.StartedAt = outcome.startedAt
	execution.FinishedAt = outcome.finishedAt
	if outcome.snapshot != nil {
		execution.RuleRevision = outcome.snapshot.Revision
		execution.TargetGUID = outcome.snapshot.Target.GUID
		execution.TargetType = outcome.snapshot.Target.MetaType
		execution.TargetPath = outcome.snapshot.Target.Path
	}
	if outcome.execution != nil {
		execution.Sample = outcome.execution.Samples
		execution.ExecutedSQL = outcome.execution.ExecutedSQL
		execution.Sampled = outcome.execution.Sampled
		execution.Truncated = outcome.execution.Truncated
	}
	execution.ObservedValue = observedValue(outcome.state.Status, outcome.observation.Observed)
	return execution
}

// failureExecutionMessage records an execution that could not be judged. ERROR and
// SKIPPED verdicts carry no observation, and the thresholds snapshot is nil
// because no verdict was reached.
func failureExecutionMessage(planned *store.QualityExecutionMessage, startedAt time.Time, status storepb.QualityStatus, code, message string) *store.QualityExecutionMessage {
	return buildExecutionMessage(planned, &ruleExecutionOutcome{
		startedAt:    startedAt,
		finishedAt:   time.Now().UTC(),
		state:        dataquality.ResultState{Status: status},
		errorCode:    code,
		errorMessage: message,
	})
}

// observedValue decides whether an execution stores a measured value. ERROR,
// SKIPPED and NOT_EVALUATED verdicts have no measurement; storing zero for them
// would be indistinguishable from a real zero and would make a trend chart invent
// a value.
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

// truncateErrorMessage bounds the stored error text so one broken query cannot put
// an unbounded blob in an execution row. It cuts on a rune boundary because the
// column is UTF-8 text.
func truncateErrorMessage(message string) string {
	return common.TruncateUTF8Bytes(message, maxErrorMessageLength)
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
// only the plan holding its final chunk advances its schedule, so a split target
// is rescheduled once. Every normal target (at most the per-table rule limit) is
// exactly one batch.
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
			advance:            true,
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

// errorCodeAndMessage classifies an error that happened before or during the
// read-only query. Every error goes through the engine's classifier: capacity
// contention is never one of them, because a batch is admitted against the
// instance's connection budget before it starts and holds the lease throughout.
func errorCodeAndMessage(err error) (code string, message string) {
	if errors.Is(err, dbfactory.ErrNoReadOnlyDataSource) {
		return dataquality.ErrCodeNoReadOnly, err.Error()
	}
	code, message = dataquality.ClassifyError(err)
	if message == "" {
		message = err.Error()
	}
	return code, message
}

// settleQualityExecutions applies a batch's committed verdicts to the problem
// ledger and emits the notifications their transitions imply. Both are best
// effort: the execution rows are already stored, so a lost issue write or a lost
// notification is logged and never turns a completed batch into a failure.
func (r *Runner) settleQualityExecutions(ctx context.Context, workspaceID string, executions []*ruleExecution, storm bool, now time.Time) {
	for _, execution := range executions {
		if execution == nil || execution.result == nil {
			continue
		}
		r.settleRuleExecution(ctx, workspaceID, execution, storm, now)
	}
}

// settleRuleExecution decides what one verdict means for the rule's open issue and
// its owner's inbox. The issue read is the only state the planner needs: the open
// issue row and the rule's previous status decide the transition together.
func (r *Runner) settleRuleExecution(ctx context.Context, workspaceID string, execution *ruleExecution, storm bool, now time.Time) {
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
			r.openQualityIssue(ctx, workspaceID, execution, storm, now)
		case dataquality.IssueActionTouch:
			if issue != nil {
				if err := r.stores.TouchQualityIssue(ctx, issue.ID, execution.executionID(), execution.severity(), now); err != nil {
					slog.Warn("failed to touch a quality issue",
						slog.Int64("issue", issue.ID),
						log.WithError(err))
				}
			}
		case dataquality.IssueActionResolve:
			if issue != nil {
				r.resolveQualityIssue(ctx, workspaceID, execution, issue, now)
			}
		case dataquality.IssueActionNone:
			// A passing execution without an issue, or a status that says nothing
			// about the problem: the ledger is already correct.
		default:
			// An action this build does not know is a defect, not a reason to stop
			// the batch; the verdict is stored and the next execution will retry the
			// ledger.
			slog.Warn("unknown quality issue action",
				slog.Int64("rule", result.RuleID),
				slog.Int("action", int(action)))
		}
	}

	// An execution failure is its own alert: it must reach someone even when the
	// rule is muted, because the mute is about the problem the rule watches, not
	// about the rule being broken. A storm merges these into one summary.
	if result.Status == storepb.QualityStatus_QUALITY_STATUS_ERROR && !storm {
		r.notifyExecutionFailed(ctx, workspaceID, execution, now)
	}
}

// openQualityIssue records a new open problem and, unless the batch is a storm or
// the rule is muted, announces it.
func (r *Runner) openQualityIssue(ctx context.Context, workspaceID string, execution *ruleExecution, storm bool, now time.Time) {
	resourceID, err := common.RandomString(qualityIssueResourceIDLength)
	if err != nil {
		slog.Warn("failed to generate a quality issue resource id", log.WithError(err))
		return
	}
	executionID := execution.executionID()
	created, err := r.stores.OpenQualityIssue(ctx, &store.QualityIssueMessage{
		ResourceID:           resourceID,
		RuleID:               execution.result.RuleID,
		TargetGUID:           execution.targetGUID(),
		TargetType:           execution.targetType(),
		Status:               storepb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
		Severity:             execution.severity(),
		FirstSeenExecutionID: &executionID,
		LastSeenExecutionID:  &executionID,
		FirstSeenAt:          now,
		LastSeenAt:           now,
		OccurrenceCount:      1,
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
	r.notifyIssueOpened(ctx, workspaceID, execution, created, now)
}

// resolveQualityIssue closes a recovered problem and tells its recipient, unless
// the rule or the issue is muted. A recovery is never suppressed by a storm: a
// storm is a burst of failures, and hiding the recoveries would leave the inbox
// with only the alarming half of the story.
func (r *Runner) resolveQualityIssue(ctx context.Context, workspaceID string, execution *ruleExecution, issue *store.QualityIssueMessage, now time.Time) {
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
	})
	r.sendDataQualityNotification(ctx, execution.ownerID(), detail, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_INFO, now)
}

// notifyIssueOpened announces a new open problem to its rule's owner, or to the
// workspace administrators when the rule has no owner.
func (r *Runner) notifyIssueOpened(ctx context.Context, workspaceID string, execution *ruleExecution, issue *store.QualityIssueMessage, now time.Time) {
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID:     workspaceID,
		kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_ISSUE_OPENED,
		execution:       execution,
		issueResourceID: issue.ResourceID,
	})
	r.sendDataQualityNotification(ctx, execution.ownerID(), detail, qualityNotificationSeverity(execution.severity()), now)
}

// notifyExecutionFailed announces a rule that could not be executed at all. The
// design makes this alert unmutable and addresses it to the workspace
// administrators only: a broken rule is an operational failure, not a problem the
// rule's owner or mute is meant to absorb.
func (r *Runner) notifyExecutionFailed(ctx context.Context, workspaceID string, execution *ruleExecution, now time.Time) {
	detail := buildDataQualityDetail(dataQualityDetailInput{
		workspaceID: workspaceID,
		kind:        storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED,
		execution:   execution,
	})
	r.sendDataQualityNotification(ctx, execution.ownerID(), detail, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR, now)
}

// sendDataQualityNotification addresses one message and hands it to the service.
// Delivery is best effort: the notification service logs its own failures, so a
// failed write only adds a log line here.
func (r *Runner) sendDataQualityNotification(ctx context.Context, ownerID *int32, detail *storepb.DataQualityDetail, severity storepb.NotificationSeverity, now time.Time) {
	r.sendDataQualityNotificationFor(ctx, ownerID, detail, severity, qualityNotificationTarget(detail), now)
}

// sendDataQualityNotificationFor delivers one message with an explicit dedupe
// target. It exists for the storm summary, whose event is one internal batch
// rather than one resource name.
func (r *Runner) sendDataQualityNotificationFor(ctx context.Context, ownerID *int32, detail *storepb.DataQualityDetail, severity storepb.NotificationSeverity, dedupeTarget string, now time.Time) {
	if r.notifier == nil || detail == nil {
		return
	}
	message := notification.DataQualityMessage(severity, detail)
	message.DedupeKey = notification.DataQualityDedupeKey(detail.GetKind(), dedupeTarget, now)
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
// addressed to the workspace administrators. It names the instance whose batch
// produced the burst, not an execution: the storm is about the instance's tick,
// and the dashboard links the executions. It is sent after the batch finished, so
// the executions are terminal when the recipient opens one.
func (r *Runner) reportQualityStorm(ctx context.Context, batchID int64, instanceResourceID string, failed, total int32, now time.Time) {
	if r.notifier == nil {
		return
	}
	detail := buildStormDetail(formatInstanceResourceName(instanceResourceID), failed, total)
	// The dedupe key is scoped to the internal batch, not the instance: one
	// scheduler tick can chunk an instance into several batches, and each storm is
	// its own event. The batch id is internal and never leaves the server.
	r.sendDataQualityNotificationFor(ctx, nil, detail, storepb.NotificationSeverity_NOTIFICATION_SEVERITY_ERROR,
		fmt.Sprintf("qualityBatch/%d", batchID), now)
}

// dataQualityDetailInput is everything a notification detail is built from. It is
// a struct so the mapping is one function instead of one per event kind.
type dataQualityDetailInput struct {
	workspaceID     string
	kind            storepb.DataQualityNotificationKind
	execution       *ruleExecution
	issueResourceID string
}

// buildDataQualityDetail maps one execution to the structured payload the client
// renders. It deliberately carries no sample rows or prose: the detail is a
// pointer into the product, and the notification table is kept forever.
func buildDataQualityDetail(in dataQualityDetailInput) *storepb.DataQualityDetail {
	execution := in.execution
	result := execution.result
	detail := &storepb.DataQualityDetail{
		Kind:       in.kind,
		Rule:       formatQualityRuleName(in.workspaceID, execution.ruleResourceID()),
		RuleTitle:  execution.ruleTitle(),
		TargetGuid: execution.targetGUID(),
		Execution:  formatQualityExecutionName(in.workspaceID, execution.executionID()),
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
		detail.Issue = formatQualityIssueName(in.workspaceID, in.issueResourceID)
	}
	if in.kind == storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_EXECUTION_FAILED {
		detail.ErrorCode = result.ErrorCode
	}
	return detail
}

// buildStormDetail is the summary one storm notification carries: how many
// executions failed out of how many the instance's batch ran, and the instance
// whose batch merged them.
func buildStormDetail(instanceResourceName string, failed, total int32) *storepb.DataQualityDetail {
	return &storepb.DataQualityDetail{
		Kind:            storepb.DataQualityNotificationKind_DATA_QUALITY_NOTIFICATION_KIND_STORM_SUMMARY,
		Instance:        instanceResourceName,
		Severity:        storepb.QualitySeverity_QUALITY_SEVERITY_ERROR,
		FailedRuleCount: failed,
		TotalRuleCount:  total,
	}
}

// isQualityStorm reports whether one batch failed enough rules to be summarized
// instead of announced rule by rule. Only FAIL verdicts count: a batch that could
// not execute its rules is a different alert, not a data-quality storm.
func isQualityStorm(executions []*ruleExecution) bool {
	failed := 0
	for _, execution := range executions {
		if execution != nil && execution.result != nil && execution.result.Status == storepb.QualityStatus_QUALITY_STATUS_FAIL {
			failed++
		}
	}
	return failed > QualityStormThreshold
}

// qualityNotificationTarget returns the resource the dedupe key is scoped to: the
// issue for a problem transition, the rule for an execution failure, the instance
// for a storm.
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
		// Unreachable in production: reportQualityStorm passes the internal batch
		// id to sendDataQualityNotificationFor. Kept explicit so the exhaustive
		// switch states the intent instead of falling through to the default.
		return ""
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
// only (design §8.2): the owner already sees the failed execution on the
// executions page, and paging them again for every broken rule would bury the
// problem alerts that actually need their attention.
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
// non-nullable enum. UNSPECIFIED means "there was no earlier execution", which the
// client renders as "first result" rather than as a transition from PASS.
func statusOrUnspecified(status *storepb.QualityStatus) storepb.QualityStatus {
	if status == nil {
		return storepb.QualityStatus_QUALITY_STATUS_UNSPECIFIED
	}
	return *status
}

// formatQualityRuleName formats a rule resource name. An empty workspace is the
// degraded form used when the workspace could not be read; it still keeps the
// detail parseable.
func formatQualityRuleName(workspaceID, resourceID string) string {
	if resourceID == "" {
		return ""
	}
	if workspaceID == "" {
		return "qualityRules/" + resourceID
	}
	return "workspaces/" + workspaceID + "/qualityRules/" + resourceID
}

// formatQualityIssueName formats an issue resource name.
func formatQualityIssueName(workspaceID, resourceID string) string {
	if resourceID == "" {
		return ""
	}
	if workspaceID == "" {
		return "qualityIssues/" + resourceID
	}
	return "workspaces/" + workspaceID + "/qualityIssues/" + resourceID
}

// formatQualityExecutionName formats an execution resource name. An execution is
// addressed by its numeric row id: the table is insert-only history, so it has no
// separate public identity column.
func formatQualityExecutionName(workspaceID string, executionID int64) string {
	if executionID <= 0 {
		return ""
	}
	name := fmt.Sprintf("qualityExecutions/%d", executionID)
	if workspaceID == "" {
		return name
	}
	return "workspaces/" + workspaceID + "/" + name
}

// formatInstanceResourceName formats the instance a storm summary is scoped to.
// Instance resource names are top-level, like every parent resource the API
// accepts, so they carry no workspace prefix.
func formatInstanceResourceName(instanceResourceID string) string {
	if instanceResourceID == "" {
		return ""
	}
	return common.InstanceNamePrefix + instanceResourceID
}

// previousQualityStatus returns the rule's last terminal verdict before this
// execution, which is what tells a recovery apart from a rule that never failed. A
// missing or unreadable history is treated as "no previous result": opening an
// issue is still correct and the partial unique index prevents a duplicate.
func previousQualityStatus(ctx context.Context, st batchExecutionStore, ruleID int64) *storepb.QualityStatus {
	executions, err := st.ListRecentQualityExecutions(ctx, ruleID, 1)
	if err != nil {
		slog.Warn("failed to read a quality rule's previous execution",
			slog.Int64("rule", ruleID),
			log.WithError(err))
		return nil
	}
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		status := execution.Status
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

// resolveRuleHead reads the mutable rule row of an execution by its internal id.
// The lookup has no lifecycle filter, so an archived or deleted rule still
// resolves and retires; a missing row stays nil and the execution record
// degrades gracefully.
func (r *Runner) resolveRuleHead(ctx context.Context, ruleID int64) *store.QualityRuleMessage {
	// ListQualityRulesByIDs has no lifecycle filter, so an archived or deleted
	// rule still resolves: loadRuleSnapshot must see it and report SKIPPED rather
	// than executing a retired definition.
	rules, err := r.stores.ListQualityRulesByIDs(ctx, []int64{ruleID})
	if err != nil {
		slog.Warn("failed to resolve a quality rule's head",
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

// executionID returns the numeric row id the execution resource name is built
// from, or 0 when the row is not available.
func (e *ruleExecution) executionID() int64 {
	if e.result == nil {
		return 0
	}
	return e.result.ID
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

// severity returns the rule's current severity. The immutable revision carries the
// severity the execution was judged with, but the notification and the issue row
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

// targetGUID returns the registry GUID the execution is about.
func (e *ruleExecution) targetGUID() string {
	if e.snapshot != nil && e.snapshot.Target.GUID != "" {
		return e.snapshot.Target.GUID
	}
	if e.result != nil {
		return e.result.TargetGUID
	}
	return ""
}

// targetType returns the registry object type the execution is about.
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
// registry instead, so they must run without a READ_ONLY data source; every query
// rule still requires one.
func requiresReadOnlySource(ruleType storepb.QualityRuleType) bool {
	return !dataquality.IsMetadataRule(ruleType)
}
