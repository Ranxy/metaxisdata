// Package runner holds the shared lifecycle of the server's background runners.
//
// A runner is a long-lived loop that the server starts after its store and
// subsystems are ready and stops by cancelling the runner context. The server
// starts every runner through Go, which gives each one a name for logs and a
// panic boundary: a bug in one background loop is logged with a stack instead of
// taking the process (and every other runner) down with it.
package runner

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// Runner is one background loop. Run blocks until ctx is cancelled.
type Runner interface {
	// Name identifies the runner in logs and is stable across releases
	// (for example "schemasync").
	Name() string
	// Run executes the loop until ctx is cancelled. Implementations should
	// return when ctx is done instead of running a final unbounded pass.
	Run(ctx context.Context)
}

// Go starts r in its own goroutine and adds it to the server's runner
// WaitGroup, with a panic boundary: a bug in one background loop is logged with
// its name and stack instead of taking the process (and the other runners) down.
//
// It also owns the runner's lifecycle log line: one when the loop starts and one
// when it stops with the name and how long it ran, so an operator can tell from
// the log whether a runner ever started and when it last stopped.
func Go(ctx context.Context, wg *sync.WaitGroup, r Runner, m *metrics.Metrics) {
	wg.Go(func() {
		started := time.Now()
		slog.Info("background runner started", slog.String("runner", r.Name()))
		m.RunnerStarted(r.Name())
		defer func() {
			if recovered := recover(); recovered != nil {
				m.RunnerPanicked(r.Name())
				slog.Error("background runner panicked",
					slog.String("runner", r.Name()),
					slog.Any("panic", recovered),
					log.Stack("panic-stack"))
			}
			m.RunnerStopped(r.Name())
			slog.Info("background runner stopped",
				slog.String("runner", r.Name()),
				slog.Duration("duration", time.Since(started)))
		}()
		r.Run(ctx)
	})
}

// Jitter returns interval plus a random 0–10%. Restarting a process starts every
// runner at once, and their fixed intervals would otherwise keep them aligned
// forever: jitter spreads the periodic passes (and the load they put on the
// targets and the store) instead of synchronizing them.
func Jitter(interval time.Duration) time.Duration {
	spread := interval / 10
	if spread <= 0 {
		return interval
	}
	return interval + rand.N(spread)
}
