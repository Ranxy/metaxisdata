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
	"sync"

	"github.com/Ranxy/metaxisdata/backend/common/log"
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
func Go(ctx context.Context, wg *sync.WaitGroup, r Runner) {
	wg.Go(func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("background runner panicked",
					slog.String("runner", r.Name()),
					slog.Any("panic", recovered),
					log.Stack("panic-stack"))
			}
		}()
		r.Run(ctx)
	})
}
