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
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// interval is how often the maintenance pass runs.
	interval = 6 * time.Hour

	// llmDebugLogRetention bounds the debug log. It stores full request and
	// response bodies, so it is not kept indefinitely; only enabled debug mode
	// writes to it at all.
	llmDebugLogRetention = 7 * 24 * time.Hour

	// openLineageDatasetSweepGrace is how long a dataset aggregate is left alone
	// before the sweep may decide it has no references. An ingest holds its rows
	// for the life of one transaction, so a row touched inside the grace period may
	// still have a writer that this pass cannot see.
	openLineageDatasetSweepGrace = time.Hour
)

// Runner prunes data with a retention window.
// LineageRevalidator re-checks ingested lineage whose endpoints the metadata
// registry did not know when it was stored, and whose column claims a relation
// that appeared later contradicts.
type LineageRevalidator interface {
	RevalidateUnresolvedLineage(ctx context.Context, limit int) (revalidated, stillUnknown int, err error)
	RevalidateContradictedColumnClaims(ctx context.Context, limit int) (revalidated, stillUnknown int, err error)
}

type Runner struct {
	store       *store.Store
	revalidator LineageRevalidator
}

// NewRunner creates a maintenance runner.
func NewRunner(stores *store.Store, revalidator LineageRevalidator) *Runner {
	return &Runner{store: stores, revalidator: revalidator}
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

	// A revoked token is only kept until the token's own expiry: past that its
	// exp claim refuses it, so the record can refuse nothing.
	if deleted, err := r.store.DeleteExpiredRevokedTokens(ctx, now); err != nil {
		slog.Error("Failed to prune revoked access tokens", log.WithError(err))
	} else if deleted > 0 {
		slog.Info("Pruned expired revoked access token records", slog.Int64("count", deleted))
	}

	// An expired refresh token can issue nothing, and the rotate-on-use path
	// already drops the one it consumes, so what is left is a session or an MCP
	// connection nobody came back to. Two independent tables, reported
	// separately because a large count means different things for each.
	if deleted, err := r.store.DeleteExpiredOAuthRefreshTokens(ctx, now); err != nil {
		slog.Error("Failed to prune OAuth refresh tokens", log.WithError(err))
	} else if deleted > 0 {
		slog.Info("Pruned expired OAuth refresh tokens", slog.Int64("count", deleted))
	}
	if deleted, err := r.store.DeleteExpiredWebRefreshTokens(ctx, now); err != nil {
		slog.Error("Failed to prune web refresh tokens", log.WithError(err))
	} else if deleted > 0 {
		slog.Info("Pruned expired web refresh tokens", slog.Int64("count", deleted))
	}

	// Ingested lineage that named a relation the registry did not have is
	// re-checked here: once the relation has been synced its columns can be
	// validated, and what is still unknown is counted rather than kept silent.
	// This runs before the retention window, which returns early when unset.
	if r.revalidator != nil {
		if revalidated, stillUnknown, err := r.revalidator.RevalidateUnresolvedLineage(ctx, 0); err != nil {
			slog.Error("Failed to revalidate lineage with an unknown endpoint", log.WithError(err))
		} else if revalidated > 0 || stillUnknown > 0 {
			slog.Info("Revalidated lineage with an unknown endpoint",
				slog.Int("edges", revalidated),
				slog.Int("objectsStillUnknown", stillUnknown))
		}

		// A relation that has appeared since the pass above takes its edges out of
		// that sweep - they no longer have an unknown endpoint - so the claims they
		// could not be checked against are swept on their own.
		if revalidated, _, err := r.revalidator.RevalidateContradictedColumnClaims(ctx, 0); err != nil {
			slog.Error("Failed to revalidate lineage with a contradicted column claim", log.WithError(err))
		} else if revalidated > 0 {
			slog.Info("Revalidated lineage with a contradicted column claim", slog.Int("edges", revalidated))
		}
	}

	// A run deleted outside the store's own paths — a hand-written DELETE, or a
	// cleanup someone ran against the ledger — takes its references with it but not
	// the dataset aggregates the ingest built from them, and the pages would keep
	// offering datasets nothing references. The retention prune reconciles the
	// datasets it prunes; this sweeps what every other deletion left behind. It is
	// not a retention policy, so it runs whether or not a window is set.
	if deleted, err := r.store.DeleteEmptyOpenLineageDatasets(ctx, now.Add(-openLineageDatasetSweepGrace)); err != nil {
		slog.Error("Failed to sweep the empty OpenLineage datasets", log.WithError(err))
	} else if deleted > 0 {
		slog.Info("Swept OpenLineage datasets nothing references", slog.Int64("count", deleted))
	}

	// OpenLineage runs are audit data and are kept forever unless an admin sets
	// a retention window in the workspace profile setting.
	setting, err := r.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		slog.Error("Failed to load the OpenLineage retention setting", log.WithError(err))
		return
	}
	days := int(setting.GetOpenlineageRetentionDays())
	if days <= 0 {
		return
	}
	if deleted, err := r.store.DeleteOpenLineageRunsBefore(ctx, now.AddDate(0, 0, -days)); err != nil {
		slog.Error("Failed to prune OpenLineage runs", log.WithError(err))
	} else if deleted > 0 {
		slog.Info("Pruned expired OpenLineage runs", slog.Int64("count", deleted), slog.Int("retentionDays", days))
	}
}
