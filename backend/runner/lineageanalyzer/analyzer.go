// Package lineageanalyzer is a runner that analyzes SQL definitions to produce column-level lineage.
package lineageanalyzer

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/runner"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	lineageAnalysisInterval = 1 * time.Hour
	analyzeCheckerInterval  = 10 * time.Second
	// MaxGoroutines is the max number of concurrent analysis jobs.
	MaxGoroutines = 10
	// maxAnalyzeBatch bounds how many queued objects one drain starts analyzing. A cold start
	// queues the whole registry, and draining all of it would keep the consumer busy for hours
	// while a freshly saved or reported statement waits behind the backlog. Keys over the cap
	// stay queued for the next tick: the batch is a rate limit, not a drop. A hundred objects
	// per worker slot bounds one batch to a hundred waves of per-object work, so event-driven
	// work waits minutes rather than an entire backfill.
	maxAnalyzeBatch = MaxGoroutines * 100
	// analyzeObjectTimeout bounds one object's analysis. The analyzers do no failing I/O of
	// their own beyond the type lookup batch, so a stuck object is a bug rather than a slow
	// database; the bound is generous enough that a large script is not cut off. An
	// interrupted analysis is retried, never recorded as a parse failure.
	analyzeObjectTimeout = 2 * time.Minute
	// queueAllTimeout bounds one full-scan pass. The pass issues four digest listings (one
	// per analyzable object type) plus four version listings; a database that stops answering
	// must not hold the scanner forever, or the hourly ticker would never fire again. Only the
	// scan is bounded this way: per-object analysis keeps analyzeObjectTimeout.
	queueAllTimeout = 5 * time.Minute
)

// Analyzer is the column-level lineage analysis runner.
type Analyzer struct {
	store *store.Store
	// lineage resolves a statement against the analyzers the process assembled.
	// The runner does not build or own them: which engines exist is a startup
	// decision, and a runner that looked them up from package state could
	// disagree with the AnalyzeSQL handler about what is registered.
	lineage *lineage.Analyzer
	// metrics records analysis outcomes; a nil value disables instrumentation.
	metrics *metrics.Metrics
	// analyzeMap holds the event-driven queue: an object whose definition just changed, was
	// just saved or was just reported. It is drained first, so a backfill cannot push that
	// work behind the whole registry. Values are the generation of the queued request.
	analyzeMap sync.Map // map[analyzeKey]uint64
	// scanMap holds the hourly full scan's backlog. It fills only the batch the event queue
	// did not use. Values are the generation of the queued request.
	scanMap sync.Map // map[analyzeKey]uint64
	// queueMu serializes enqueueing so the two queues stay disjoint: QueueAnalysis removes
	// the scan entry and queueScan skips a key the event queue already holds, and without a
	// shared lock an interleaving could store the same key in both and analyze it twice.
	queueMu sync.Mutex
	// generation is the source of the monotonically increasing value stored with every queue
	// entry. Comparing it tells the drain whether the entry it analyzed is still the current
	// request or has been superseded.
	generation atomic.Uint64
	// retryMap tracks how many times a failed analysis was retried and when it
	// is due again, so a transient failure does not wait for the hourly scan.
	retryMap sync.Map // map[analyzeKey]analysisRetry
}

// NewAnalyzer creates a new lineage Analyzer that resolves statements through
// lineage, the process's assembled set of engine analyzers.
func NewAnalyzer(stores *store.Store, lineageAnalyzer *lineage.Analyzer, m *metrics.Metrics) *Analyzer {
	return &Analyzer{
		store:   stores,
		lineage: lineageAnalyzer,
		metrics: m,
	}
}

// Name identifies the runner in the server's lifecycle logs.
func (*Analyzer) Name() string { return "lineageanalyzer" }

// Run starts the analyzer. It blocks until ctx is cancelled.
func (a *Analyzer) Run(ctx context.Context) {
	sp := pool.New()

	// Goroutine 1: periodic full scan.
	sp.Go(func() {
		slog.Debug("Lineage analyzer started",
			slog.Duration("scanInterval", lineageAnalysisInterval),
			slog.Int("maxBatch", maxAnalyzeBatch),
			slog.Int("workers", MaxGoroutines))
		a.queueAllBounded(ctx)
		// Jitter keeps the hourly scan from staying aligned with every other
		// process that restarted at the same moment.
		ticker := time.NewTicker(runner.Jitter(lineageAnalysisInterval))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.queueAllBounded(ctx)
			case <-ctx.Done():
				return
			}
		}
	})

	// Goroutine 2: queue consumer.
	sp.Go(func() {
		ticker := time.NewTicker(analyzeCheckerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.drainAndAnalyze(ctx)
			case <-ctx.Done():
				return
			}
		}
	})

	sp.Wait()
}

// queueAllBounded runs one full scan under queueAllTimeout. The scan reads four digest
// listings and four version listings; without a deadline a database that stops answering
// would hold this goroutine forever, so the hourly ticker would never fire again.
func (a *Analyzer) queueAllBounded(ctx context.Context) {
	scanCtx, cancel := context.WithTimeout(ctx, queueAllTimeout)
	defer cancel()
	a.queueAll(scanCtx)
}

// queueAll scans all lineage-analyzable objects and queues those whose metahash
// or analyzer version differs from the stored analysis state. Each object type
// costs two queries (objects and their analysis state) instead of one query per
// object.
func (a *Analyzer) queueAll(ctx context.Context) {
	viewType := storepb.MetaType_VIEW
	mvType := storepb.MetaType_MATERIALIZED_VIEW
	manualSQLType := storepb.MetaType_MANUAL_SQL
	applicationSQLType := storepb.MetaType_APPLICATION_SQL

	for _, objType := range []storepb.MetaType{viewType, mvType, manualSQLType, applicationSQLType} {
		t := objType
		list, err := a.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{
			ObjectType: &t,
		})
		if err != nil {
			slog.Error("Lineage analyzer failed to list meta registry", slog.String("type", t.String()), log.WithError(err))
			continue
		}
		versions, err := a.store.ListColumnLineageVersions(ctx, t)
		if err != nil {
			slog.Error("Lineage analyzer failed to list analysis versions", slog.String("type", t.String()), log.WithError(err))
			continue
		}
		for _, res := range list {
			if needsAnalysis(res, versions[res.GUID]) {
				a.queueScan(res.GUID, res.ObjectType)
			}
		}
	}
}
