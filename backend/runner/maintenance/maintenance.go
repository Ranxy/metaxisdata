// Package maintenance runs the periodic housekeeping pass: it prunes rows that
// are already invisible (expired caches) or that a retention window explicitly
// bounds (the LLM debug log, OpenLineage runs).
package maintenance

import (
	"context"
	"log/slog"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// interval is how often the maintenance pass runs.
	interval = 6 * time.Hour
	// passTimeout bounds one pass. A wedged delete fails its remaining items and
	// the next tick retries them instead of a pass silently swallowing the ticks
	// that follow it. It is well above the usual pass so a large retention prune
	// still fits.
	passTimeout = time.Hour

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

// LineageRevalidator re-checks ingested lineage whose endpoints the metadata
// registry did not know when it was stored, and whose column claims a relation
// that appeared later contradicts.
type LineageRevalidator interface {
	RevalidateUnresolvedLineage(ctx context.Context, limit int) (revalidated, stillUnknown int, err error)
	RevalidateContradictedColumnClaims(ctx context.Context, limit int) (revalidated, stillUnknown int, err error)
}

// maintenanceStore is the slice of *store.Store the pass needs. Narrowing it here
// is what lets the pass be tested with a fake: the pass owns the order and error
// handling, the store owns the SQL.
type maintenanceStore interface {
	DeleteExpiredExplainSQLCache(ctx context.Context, olderThan time.Time) (int64, error)
	DeleteExpiredLLMDebugLog(ctx context.Context, olderThan time.Time) (int64, error)
	DeleteExpiredRevokedTokens(ctx context.Context, before time.Time) (int64, error)
	DeleteExpiredOAuthRefreshTokens(ctx context.Context, before time.Time) (int64, error)
	DeleteExpiredWebRefreshTokens(ctx context.Context, before time.Time) (int64, error)
	DeleteEmptyOpenLineageDatasets(ctx context.Context, olderThan time.Time) (int64, error)
	DeleteOpenLineageRunsBefore(ctx context.Context, cutoff time.Time) (int64, error)
	GetWorkspaceGeneralSetting(ctx context.Context) (*storepb.WorkspaceProfileSetting, error)
}

// Runner prunes data with a retention window.
type Runner struct {
	store       maintenanceStore
	revalidator LineageRevalidator
	// now is time.Now.UTC in production; a test fixes it so the retention and
	// grace cutoffs are exact.
	now func() time.Time
	// interval and passTimeout carry the production constants; a test overrides
	// them to keep the cadence and the pass bound observable without waiting.
	interval    time.Duration
	passTimeout time.Duration
}

// NewRunner creates a maintenance runner.
func NewRunner(stores *store.Store, revalidator LineageRevalidator) *Runner {
	return &Runner{
		store:       stores,
		revalidator: revalidator,
		now:         func() time.Time { return time.Now().UTC() },
		interval:    interval,
		passTimeout: passTimeout,
	}
}

// Name identifies the runner in the server's lifecycle logs.
func (*Runner) Name() string { return "maintenance" }

// Run blocks until ctx is cancelled.
func (r *Runner) Run(ctx context.Context) {
	r.runOnce(ctx)

	ticker := time.NewTicker(r.interval)
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

// maintenanceItem is one delete the pass runs. Its name keys the summary, and the
// messages are what an operator greps for.
type maintenanceItem struct {
	name    string
	failure string
	success string
	delete  func(context.Context) (int64, error)
}

// passSummary is what one pass deleted and what it could not do. It is returned
// for tests and logged as the pass' single summary line.
type passSummary struct {
	// counts holds what each item removed or revalidated, keyed by item name. It
	// is not only row deletions: the lineage items count the edges they re-checked.
	counts map[string]int64
	// failed lists the items that returned an error; the pass still ran the ones
	// after them.
	failed []string
	// retentionSkipped explains why the OpenLineage runs were not pruned, and is
	// empty when the retention item ran.
	retentionSkipped string
}

func (r *Runner) runOnce(ctx context.Context) *passSummary {
	started := r.now()
	// Every item sees the pass deadline, so a stuck delete cannot extend the pass
	// and the items after it fail fast instead of hanging.
	passCtx, cancel := context.WithTimeout(ctx, r.passTimeout)
	defer cancel()

	summary := &passSummary{counts: make(map[string]int64)}
	// The cutoffs share the pass' start time: they are the retention and grace
	// windows measured from when this pass began, not from each item's own clock.
	now := started

	items := []maintenanceItem{
		{
			name:    "explain_sql_cache",
			failure: "Failed to prune the explain SQL cache",
			success: "Pruned expired explain SQL cache entries",
			delete: func(ctx context.Context) (int64, error) {
				return r.store.DeleteExpiredExplainSQLCache(ctx, now.Add(-store.ExplainSQLCacheTTL))
			},
		},
		{
			name:    "llm_debug_log",
			failure: "Failed to prune the LLM debug log",
			success: "Pruned expired LLM debug log entries",
			delete: func(ctx context.Context) (int64, error) {
				return r.store.DeleteExpiredLLMDebugLog(ctx, now.Add(-llmDebugLogRetention))
			},
		},
		{
			// A revoked token is only kept until the token's own expiry: past that
			// its exp claim refuses it, so the record can refuse nothing.
			name:    "revoked_tokens",
			failure: "Failed to prune revoked access tokens",
			success: "Pruned expired revoked access token records",
			delete: func(ctx context.Context) (int64, error) {
				return r.store.DeleteExpiredRevokedTokens(ctx, now)
			},
		},
		{
			// An expired refresh token can issue nothing, and the rotate-on-use
			// path already drops the one it consumes, so what is left is a session
			// or an MCP connection nobody came back to. Two independent tables,
			// reported separately because a large count means different things for
			// each.
			name:    "oauth_refresh_tokens",
			failure: "Failed to prune OAuth refresh tokens",
			success: "Pruned expired OAuth refresh tokens",
			delete: func(ctx context.Context) (int64, error) {
				return r.store.DeleteExpiredOAuthRefreshTokens(ctx, now)
			},
		},
		{
			name:    "web_refresh_tokens",
			failure: "Failed to prune web refresh tokens",
			success: "Pruned expired web refresh tokens",
			delete: func(ctx context.Context) (int64, error) {
				return r.store.DeleteExpiredWebRefreshTokens(ctx, now)
			},
		},
	}
	for _, item := range items {
		pruneItem(passCtx, summary, item)
	}

	r.revalidateIngestedLineage(passCtx, summary)

	// A run deleted outside the store's own paths - a hand-written DELETE, or a
	// cleanup someone ran against the ledger - takes its references with it but not
	// the dataset aggregates the ingest built from them, and the pages would keep
	// offering datasets nothing references. The retention prune reconciles the
	// datasets it prunes; this sweeps what every other deletion left behind. It is
	// not a retention policy, so it runs whether or not a window is set, and before
	// the retention item whose setting read may fail.
	pruneItem(passCtx, summary, maintenanceItem{
		name:    "empty_openlineage_datasets",
		failure: "Failed to sweep the empty OpenLineage datasets",
		success: "Swept OpenLineage datasets nothing references",
		delete: func(ctx context.Context) (int64, error) {
			return r.store.DeleteEmptyOpenLineageDatasets(ctx, now.Add(-openLineageDatasetSweepGrace))
		},
	})

	// OpenLineage runs are audit data and are kept forever unless an admin sets a
	// retention window in the workspace profile setting. The setting read is a step
	// of its own: its failure skips only this retention item, never the items above.
	r.pruneOpenLineageRuns(passCtx, now, summary)

	slog.Info("Maintenance pass finished",
		slog.Duration("duration", r.now().Sub(started)),
		slog.Any("counts", summary.counts),
		slog.Any("failed", summary.failed),
		slog.String("retentionSkipped", summary.retentionSkipped))
	return summary
}

// pruneItem runs one delete and records its outcome. A failure is logged and the
// pass moves on: one broken table must not hold back the rest of the housekeeping
// for another tick.
func pruneItem(ctx context.Context, summary *passSummary, item maintenanceItem) {
	deleted, err := item.delete(ctx)
	if err != nil {
		summary.failed = append(summary.failed, item.name)
		slog.Error(item.failure, log.WithError(err))
		return
	}
	summary.counts[item.name] = deleted
	if deleted > 0 {
		slog.Info(item.success, slog.Int64("count", deleted))
	}
}

// revalidateIngestedLineage re-checks the ingested edges the registry can now
// judge. It runs before the retention item, so a setting read failure cannot skip
// it.
func (r *Runner) revalidateIngestedLineage(ctx context.Context, summary *passSummary) {
	if r.revalidator == nil {
		return
	}

	// Ingested lineage that named a relation the registry did not have is
	// re-checked here: once the relation has been synced its columns can be
	// validated, and what is still unknown is counted rather than kept silent.
	revalidated, stillUnknown, err := r.revalidator.RevalidateUnresolvedLineage(ctx, 0)
	if err != nil {
		summary.failed = append(summary.failed, "unresolved_lineage")
		slog.Error("Failed to revalidate lineage with an unknown endpoint", log.WithError(err))
	} else {
		summary.counts["unresolved_lineage"] = int64(revalidated)
		if revalidated > 0 || stillUnknown > 0 {
			slog.Info("Revalidated lineage with an unknown endpoint",
				slog.Int("edges", revalidated),
				slog.Int("objectsStillUnknown", stillUnknown))
		}
	}

	// A relation that has appeared since the pass above takes its edges out of
	// that sweep - they no longer have an unknown endpoint - so the claims they
	// could not be checked against are swept on their own.
	revalidated, _, err = r.revalidator.RevalidateContradictedColumnClaims(ctx, 0)
	if err != nil {
		summary.failed = append(summary.failed, "contradicted_column_claims")
		slog.Error("Failed to revalidate lineage with a contradicted column claim", log.WithError(err))
	} else {
		summary.counts["contradicted_column_claims"] = int64(revalidated)
		if revalidated > 0 {
			slog.Info("Revalidated lineage with a contradicted column claim", slog.Int("edges", revalidated))
		}
	}
}

// pruneOpenLineageRuns applies the workspace retention window to OpenLineage runs.
// It is separate from the other items because its setting read can fail, and that
// failure must skip this item only.
func (r *Runner) pruneOpenLineageRuns(ctx context.Context, now time.Time, summary *passSummary) {
	setting, err := r.store.GetWorkspaceGeneralSetting(ctx)
	if err != nil {
		summary.retentionSkipped = "workspace setting unavailable"
		slog.Error("Failed to load the OpenLineage retention setting, skipping the retention prune", log.WithError(err))
		return
	}
	days := int(setting.GetOpenlineageRetentionDays())
	if days <= 0 {
		summary.retentionSkipped = "no retention window set"
		slog.Info("Skipping the OpenLineage retention prune: no retention window is set")
		return
	}

	deleted, err := r.store.DeleteOpenLineageRunsBefore(ctx, now.AddDate(0, 0, -days))
	if err != nil {
		summary.failed = append(summary.failed, "openlineage_runs")
		slog.Error("Failed to prune OpenLineage runs", log.WithError(err))
		return
	}
	summary.counts["openlineage_runs"] = deleted
	if deleted > 0 {
		slog.Info("Pruned expired OpenLineage runs", slog.Int64("count", deleted), slog.Int("retentionDays", days))
	}
}
