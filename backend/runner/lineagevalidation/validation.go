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
)

const (
	// settleDelay coalesces the signals one sync round produces: an instance scan
	// queues every database of every instance, and each commit would otherwise
	// start its own pass over the same pending edges.
	settleDelay = 5 * time.Second
	// passTimeout bounds one pass so a stuck pass cannot wedge the runner. The
	// maintenance pass keeps its own cadence as the safety net.
	passTimeout = 5 * time.Minute
)

// Revalidator re-checks ingested lineage whose column claims a relation that
// appeared after ingestion contradicts, exactly as the maintenance pass does on
// its interval.
type Revalidator interface {
	// RevalidateContradictedColumnClaims revalidates one bounded batch of
	// contradicted edges. limit 0 means the implementation's default batch
	// (revalidationBatch for the OpenLineage revalidator), not "everything": a
	// backlog larger than that batch is left for the next pass, so a caller must
	// not assume one call drains it.
	RevalidateContradictedColumnClaims(ctx context.Context, limit int) (revalidated, stillUnknown int, err error)
}

// Runner revalidates ingested lineage after a schema sync commits.
type Runner struct {
	revalidator Revalidator
	// settle is settleDelay in production; a test shortens it.
	settle time.Duration
	// settleTimer waits out the settle window; production uses time.After, and a
	// test replaces it to hold the window open instead of racing a clock.
	settleTimer func(time.Duration) <-chan time.Time
	// trigger holds at most one pending request. A signal that arrives during
	// settle is folded into the pass about to run, because that pass covers the
	// same pending edges; a signal that arrives while a pass is running stays
	// queued and earns one more pass, because it may describe metadata the running
	// pass did not see.
	trigger chan struct{}
}

// NewRunner creates a revalidation runner.
func NewRunner(revalidator Revalidator) *Runner {
	return &Runner{
		revalidator: revalidator,
		settle:      settleDelay,
		settleTimer: time.After,
		trigger:     make(chan struct{}, 1),
	}
}

// Trigger requests a pass once the current sync round has settled. It never
// blocks: its caller is a schema sync, which must not wait for lineage work.
func (r *Runner) Trigger() {
	if r == nil || r.revalidator == nil {
		return
	}
	select {
	case r.trigger <- struct{}{}:
	default:
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

// runOnce revalidates the claims the registry now contradicts, at most one
// default batch of them. A failure is logged and dropped: the next sync or the
// maintenance pass retries the same edges, and a half-finished pass would leave a
// run's edges replaced in part.
func (r *Runner) runOnce(ctx context.Context) {
	passCtx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()

	// limit 0 asks for the implementation's default batch: one trigger therefore
	// revalidates a bounded slice even when the backlog is larger. Draining the
	// backlog is the maintenance pass' job, not a reason for this loop to spin.
	revalidated, stillUnknown, err := r.revalidator.RevalidateContradictedColumnClaims(passCtx, 0)
	if err != nil {
		slog.Error("Failed to revalidate lineage after a schema sync", log.WithError(err))
		return
	}
	if revalidated > 0 || stillUnknown > 0 {
		slog.Info("Revalidated lineage after a schema sync",
			slog.Int("edges", revalidated),
			slog.Int("objectsStillUnknown", stillUnknown))
	}
}
