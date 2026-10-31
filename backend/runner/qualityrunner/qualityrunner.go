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
	"log/slog"
	"sync"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"
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
	// limit. It is a composition bound, not a time bound: a small target can still
	// time out, which the target cycle retries below.
	maxExecutionsPerBatch = 100
	// maxScheduledRuleAttempts is the initial attempt plus at most two retries for
	// one check in one target due cycle. A timed-out or abandoned attempt is retried
	// on the next tick until this budget is used; then the target cycle is allowed
	// to settle so a permanently failing chunk cannot retry forever.
	maxScheduledRuleAttempts = 3
	// maxConcurrentInstanceBatches is the per-instance/global concurrency ceiling
	// for scheduled batches: the design allows two instances to execute at once,
	// and one instance's batches always run serially.
	maxConcurrentInstanceBatches = 2
	// scheduledQueueCapacity bounds the instance groups waiting for a scheduled
	// worker. One due scan hands over at most one group per instance (at most
	// dueTargetScanLimit); a full queue leaves the rest of the pass due for the next
	// tick instead of blocking the runner loop, which also drains manual batches.
	scheduledQueueCapacity = 64
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
	settings := r.currentSettings()
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

// currentSettings returns the settings snapshot the runner last loaded. The
// scheduled workers and the manual lane run concurrently, so the snapshot is
// guarded instead of being read from the field directly.
func (r *Runner) currentSettings() dataquality.Settings {
	r.settingsMu.RLock()
	defer r.settingsMu.RUnlock()
	return r.settings
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
	r.settingsMu.Lock()
	r.settings = settings
	r.settingsMu.Unlock()
}

// overDailyExecutionQuota reports whether the workspace already started as many
// executions today as its settings allow. The tally is a safety valve against a
// dense schedule, not a correctness boundary: when it trips, the due rules stay
// due and the next day resumes them.
func (r *Runner) overDailyExecutionQuota(ctx context.Context, now time.Time) bool {
	limit := r.currentSettings().DailyExecutionLimit
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
	// settingsMu guards settings: the manual lane reloads it while the scheduled
	// workers read it.
	settingsMu sync.RWMutex

	// manual carries the batches the API created. It is buffered so enqueueing
	// never blocks a request; a full queue is reported back as false.
	manual chan manualRun
	// scheduled carries the due scan's instance groups to the scheduled workers, so
	// the runner loop that drains manual batches never executes a scheduled batch
	// itself. A full queue leaves the remaining due targets for the next tick.
	scheduled chan scheduledGroup
	// inflight holds the rule ids currently being executed, so a second execution
	// that finds the same rule due records SKIPPED instead of executing it twice.
	inflight map[int64]struct{}
	// scheduledInstances holds the instances whose queued or running group owns their
	// scheduled execution slot. The due scan skips them, keeping one instance's
	// batches serial across ticks as well as within a pass.
	scheduledInstances map[string]struct{}
	mu                 sync.Mutex
}

// NewRunner creates the data-quality runner. The executor is stateless, so one
// instance is shared by every batch. notifier reports opened and recovered
// problems and execution failures; it may be nil.
func NewRunner(stores *store.Store, dbFactory *dbfactory.DBFactory, stateCfg *state.State, notifier *notification.Service, m *metrics.Metrics) *Runner {
	exec := dataquality.NewExecutor()
	exec.Metrics = m
	return &Runner{
		stores:             stores,
		dbFactory:          dbFactory,
		stateCfg:           stateCfg,
		exec:               exec,
		notifier:           notifier,
		manual:             make(chan manualRun, manualQueueCapacity),
		scheduled:          make(chan scheduledGroup, scheduledQueueCapacity),
		inflight:           make(map[int64]struct{}),
		scheduledInstances: make(map[string]struct{}),
		settings:           dataquality.DefaultSettings(),
		metrics:            m,
	}
}

// Name identifies the runner in the server's lifecycle logs.
func (*Runner) Name() string { return "qualityrunner" }

// ManualQueueDepth reports how many manual batches are waiting for the runner. It
// is read by the metrics collector at scrape time.
func (r *Runner) ManualQueueDepth() int {
	return len(r.manual)
}

// HasQualityExecutionQueueRoom reports whether n more manual batches fit in the
// queue. The API checks a whole multi-target selection before planning any batch,
// so a failure cannot leave earlier targets already queued; enqueueing still
// re-checks, so a race at worst returns ResourceExhausted.
func (r *Runner) HasQualityExecutionQueueRoom(n int) bool {
	if n <= 0 {
		return true
	}
	return len(r.manual)+n <= cap(r.manual)
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
// take precedence over the periodic due scan: the scan only plans and hands its
// batches to the scheduled workers, so a long scheduled batch never delays a
// manual request.
func (r *Runner) Run(ctx context.Context) {
	r.abandonStaleWork(ctx)
	r.refreshSettings(ctx)

	var workers sync.WaitGroup
	workers.Add(maxConcurrentInstanceBatches)
	for range maxConcurrentInstanceBatches {
		go func() {
			defer workers.Done()
			r.runScheduledWorker(ctx)
		}()
	}
	defer workers.Wait()

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

// runScheduledWorker executes one instance group at a time. A worker is the only
// place a scheduled batch runs, which keeps the runner loop free for manual work.
func (r *Runner) runScheduledWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case group := <-r.scheduled:
			r.guardScheduledGroup(group.instanceResourceID, func() {
				r.runScheduledGroup(ctx, group)
			})
		}
	}
}

// guardScheduledGroup keeps a panic in one instance's scheduled batch from
// escaping the worker goroutine. The workers are started outside runner.Go, so
// they sit outside the panic boundary that protects the process; an uncaught
// panic here would take down the API and every other runner with it. The
// group's own deferred release still runs while the panic unwinds, and the
// remaining targets stay due for a later tick, so the worker can continue with
// the next group.
func (r *Runner) guardScheduledGroup(instanceResourceID string, run func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			r.metrics.RunnerPanicked(r.Name())
			slog.Error("scheduled quality batch panicked",
				slog.String("instance", instanceResourceID),
				slog.Any("panic", recovered),
				log.Stack("panic-stack"))
		}
	}()
	run()
}

// runScheduledGroup executes one instance's plans in order and then releases the
// instance's slot. A deferral or store failure stops the group: the remaining
// targets stay due and are retried on a later tick.
func (r *Runner) runScheduledGroup(ctx context.Context, group scheduledGroup) {
	defer r.releaseScheduledInstance(group.instanceResourceID)
	for _, plan := range group.plans {
		if ctx.Err() != nil {
			return
		}
		if !r.startScheduledBatch(ctx, plan) {
			return
		}
	}
}

// abandonStaleWork closes the batches and executions a previous process left
// PENDING or RUNNING. It is a startup-only sweep: the abandoned attempts are not
// resumed, and their results are not invented. A scheduled check counts the
// abandoned attempt toward its cycle budget and is retried on a later tick; a
// manual execution is not retried. Executions are swept explicitly as well as
// through their batch, so a row whose batch write was lost still reaches a
// terminal state.
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
