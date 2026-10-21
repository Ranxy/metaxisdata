// Package lineagevalidation re-checks ingested lineage whose endpoints the
// metadata registry did not know when it was stored.
//
// Ingestion keeps such an edge because it cannot tell a table created after the
// last schema sync from one that never existed, so the edge is stored unchecked.
// A schema sync is what changes that: the relation the edge named may exist now,
// and waiting for the maintenance pass' six-hour interval is a poor bound on
// noticing. The syncer signals this runner when it has committed metadata, and
// the runner coalesces the signals into one pass, so a round of database syncs
// costs one revalidation.
package lineagevalidation

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/metrics"
)

const (
	// settleDelay coalesces the signals one sync round produces: an instance scan
	// queues every database of every instance, and each commit would otherwise
	// start its own pass over the same pending edges.
	settleDelay = 5 * time.Second
	// passTimeout bounds one pass so a stuck pass cannot wedge the runner. The
	// maintenance pass keeps its own cadence as the safety net.
	passTimeout = 5 * time.Minute
	// maxRevalidationBatches bounds how many default batches one pass drains. A
	// revalidation leaves an edge whose relation is still unknown in place, so that
	// edge stays eligible for the same query: a backlog at or above one batch would
	// otherwise be re-read and rewritten until the deadline. The budget ends the
	// pass after a bounded amount of work; the next trigger resumes from the oldest
	// rows.
	maxRevalidationBatches = 10
)

// Revalidator re-checks ingested lineage whose column claims a relation that
// appeared after ingestion contradicts, exactly as the maintenance pass does on
// its interval.
type Revalidator interface {
	// RevalidateContradictedColumnClaims revalidates one bounded batch of
	// contradicted edges and reports whether the batch was full, so the caller
	// can keep draining a larger backlog. limit 0 means the implementation's
	// default batch (revalidationBatch for the OpenLineage revalidator), not
	// "everything".
	RevalidateContradictedColumnClaims(ctx context.Context, limit int) (revalidated, stillUnknown int, hasMore bool, err error)
}

// Runner revalidates ingested lineage after a schema sync commits.
type Runner struct {
	revalidator Revalidator
	// settle is settleDelay in production; a test shortens it.
	settle time.Duration
	// passTimeout is the package constant in production; a test shortens it to
	// observe the pass deadline without waiting.
	passTimeout time.Duration
	// settleTimer waits out the settle window; production uses time.After, and a
	// test replaces it to hold the window open instead of racing a clock.
	settleTimer func(time.Duration) <-chan time.Time
	// metrics records trigger and pass outcomes; a nil value disables
	// instrumentation.
	metrics *metrics.Metrics
	// trigger holds at most one pending request. A signal that arrives during
	// settle is folded into the pass about to run, because that pass covers the
	// same pending edges; a signal that arrives while a pass is running stays
	// queued and earns one more pass, because it may describe metadata the running
	// pass did not see.
	trigger chan struct{}
}

// NewRunner creates a revalidation runner.
func NewRunner(revalidator Revalidator, m *metrics.Metrics) *Runner {
	return &Runner{
		revalidator: revalidator,
		settle:      settleDelay,
		passTimeout: passTimeout,
		settleTimer: time.After,
		trigger:     make(chan struct{}, 1),
		metrics:     m,
	}
}

// Trigger requests a pass once the current sync round has settled. It never
// blocks: its caller is a schema sync, which must not wait for lineage work.
// "Queued" means the signal was accepted, not that it earns its own pass: a
// signal that arrives during the settle window is folded into the pass about to
// run (see discardPendingTrigger).
func (r *Runner) Trigger() {
	if r == nil || r.revalidator == nil {
		return
	}
	select {
	case r.trigger <- struct{}{}:
		r.metrics.CountLineageValidationTrigger(metrics.ValidationTriggerQueued)
	default:
		// A signal that arrives while one is already pending is folded into the pass
		// that signal will run, so it earns no pass of its own.
		r.metrics.CountLineageValidationTrigger(metrics.ValidationTriggerCoalesced)
	}
}

// Name identifies the runner in the server's lifecycle logs.
func (*Runner) Name() string { return "lineagevalidation" }

// Run blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	if r.revalidator == nil {
		// Nothing to run: a server that wired no revalidator must not spin.
		<-ctx.Done()
		return
	}

	// A restart leaves the edges ingested while the process was down unchecked
	// until a sync commits, which may be hours away. One pass at startup closes
	// that window, and it goes through the trigger channel so a sync that arrives
	// at the same moment is folded into the same settle and pass.
	r.Trigger()

	for {
		select {
		case <-r.trigger:
			select {
			case <-r.settleTimer(r.settle):
			case <-ctx.Done():
				return
			}
			// A signal that arrived during settle names the same pending edges as the
			// pass about to run, so it is folded into it rather than costing a second
			// settle and pass. A signal that arrives once the pass is running is not
			// folded: it stays queued and earns its own pass.
			r.discardPendingTrigger()
			r.runOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// discardPendingTrigger folds a signal that arrived during settle into the pass
// about to run. Trigger keeps at most one token, so one non-blocking receive is
// the whole drain.
func (r *Runner) discardPendingTrigger() {
	select {
	case <-r.trigger:
	default:
	}
}

// runOnce revalidates the claims the registry now contradicts, draining batches
// until the backlog is worked off, the pass deadline ends, or the batch budget is
// spent. It returns what the whole pass revalidated, summed over its batches, for
// tests. A failure is logged and dropped: the next sync or the maintenance pass
// retries the same edges, and a half-finished pass would leave a run's edges
// replaced in part.
func (r *Runner) runOnce(ctx context.Context) (revalidated, stillUnknown int) {
	started := time.Now()
	passCtx, cancel := context.WithTimeout(ctx, r.passTimeout)
	defer cancel()

	// The pass outcome is only known at the end, so the observation is deferred;
	// an error still reports the edges the earlier batches handled.
	outcome := metrics.ValidationPassOK
	defer func() {
		r.metrics.ObserveLineageValidationPass(outcome, time.Since(started), revalidated, stillUnknown)
	}()

	// limit 0 asks for the implementation's default batch, and hasMore says
	// whether another batch is waiting. Draining the backlog here means one trigger
	// catches up a backlog larger than a batch, while the pass deadline and the
	// batch budget keep this loop bounded.
	backlogRemaining := false
	for batches := 0; passCtx.Err() == nil && batches < maxRevalidationBatches; batches++ {
		batchRevalidated, batchStillUnknown, hasMore, err := r.revalidator.RevalidateContradictedColumnClaims(passCtx, 0)
		if err != nil {
			slog.Error("Failed to revalidate lineage after a schema sync", log.WithError(err))
			outcome = metrics.ValidationPassError
			return revalidated, stillUnknown
		}
		revalidated += batchRevalidated
		stillUnknown += batchStillUnknown
		backlogRemaining = hasMore
		if !hasMore {
			break
		}
	}
	if revalidated > 0 || stillUnknown > 0 {
		slog.Info("Revalidated lineage after a schema sync",
			slog.Int("edges", revalidated),
			slog.Int("objectsStillUnknown", stillUnknown))
	}
	if backlogRemaining {
		// The batch budget, the deadline, or shutdown ended the pass with batches
		// still pending; the next trigger continues where this one stopped.
		outcome = metrics.ValidationPassBacklogRemaining
		slog.Warn("Revalidation pass ended with a backlog remaining",
			slog.Int("edges", revalidated),
			slog.Int("objectsStillUnknown", stillUnknown),
			slog.Bool("deadlineReached", passCtx.Err() != nil))
	}
	return revalidated, stillUnknown
}
