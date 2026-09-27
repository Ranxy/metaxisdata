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
	"sync"
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
	RevalidateContradictedColumnClaims(ctx context.Context, limit int) (revalidated, stillUnknown int, err error)
}

// Runner revalidates ingested lineage after a schema sync commits.
type Runner struct {
	revalidator Revalidator
	// settle is settleDelay in production; a test shortens it.
	settle time.Duration
	// trigger holds at most one pending request: a signal that arrives while a
	// pass is pending or running is folded into the pending one, because a single
	// pass covers every pending edge anyway.
	trigger chan struct{}
}

// NewRunner creates a revalidation runner.
func NewRunner(revalidator Revalidator) *Runner {
	return &Runner{
		revalidator: revalidator,
		settle:      settleDelay,
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

// Run blocks until ctx is cancelled, then signals wg.Done().
func (r *Runner) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	if r.revalidator == nil {
		// Nothing to run: a server that wired no revalidator must not spin.
		<-ctx.Done()
		return
	}

	for {
		select {
		case <-r.trigger:
			select {
			case <-time.After(r.settle):
			case <-ctx.Done():
				return
			}
			r.runOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// runOnce revalidates every claim the registry now contradicts. A failure is
// logged and dropped: the next sync or the maintenance pass retries the same
// edges, and a half-finished pass would leave a run's edges replaced in part.
func (r *Runner) runOnce(ctx context.Context) {
	passCtx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()

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
