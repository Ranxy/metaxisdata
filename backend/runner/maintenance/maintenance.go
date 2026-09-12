// Package maintenance runs the periodic housekeeping pass: it prunes rows that
// are already invisible (expired caches) or that a retention window explicitly
// bounds (the LLM debug log, OpenLineage runs).
package maintenance

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/config"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// interval is how often the maintenance pass runs.
	interval = 6 * time.Hour

	// llmDebugLogRetention bounds the debug log. It stores full request and
	// response bodies, so it is not kept indefinitely; only enabled debug mode
	// writes to it at all.
	llmDebugLogRetention = 7 * 24 * time.Hour
)

// Runner prunes data with a retention window.
type Runner struct {
	store   *store.Store
	profile *config.Profile
}

// NewRunner creates a maintenance runner.
func NewRunner(stores *store.Store, profile *config.Profile) *Runner {
	return &Runner{store: stores, profile: profile}
}

// Run blocks until ctx is cancelled, then signals wg.Done().
func (r *Runner) Run(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	r.runOnce(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.runOnce(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	now := time.Now().UTC()

	if deleted, err := r.store.DeleteExpiredExplainSQLCache(ctx, now.Add(-store.ExplainSQLCacheTTL)); err != nil {
		slog.Error("Failed to prune the explain SQL cache", log.WithError(err))
	} else if deleted > 0 {
		slog.Info("Pruned expired explain SQL cache entries", slog.Int64("count", deleted))
	}

	if deleted, err := r.store.DeleteExpiredLLMDebugLog(ctx, now.Add(-llmDebugLogRetention)); err != nil {
		slog.Error("Failed to prune the LLM debug log", log.WithError(err))
	} else if deleted > 0 {
		slog.Info("Pruned expired LLM debug log entries", slog.Int64("count", deleted))
	}

	// OpenLineage runs are audit data and are kept forever unless the operator
	// opts into a retention window.
	days := r.profile.OpenLineageRetentionDays
	if days <= 0 {
		return
	}
	if deleted, err := r.store.DeleteOpenLineageRunsBefore(ctx, now.AddDate(0, 0, -days)); err != nil {
		slog.Error("Failed to prune OpenLineage runs", log.WithError(err))
	} else if deleted > 0 {
		slog.Info("Pruned expired OpenLineage runs", slog.Int64("count", deleted), slog.Int("retentionDays", days))
	}
}
