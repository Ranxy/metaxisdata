// Package lineageanalyzer is a runner that analyzes SQL definitions to produce column-level lineage.
package lineageanalyzer

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/plugin/dmltarget"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/model"
	"github.com/Ranxy/metaxisdata/backend/plugin/sqlsanitize"
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

// analyzeKey uniquely identifies an object to analyze.
type analyzeKey struct {
	MetaGUID string
	MetaType storepb.MetaType
}

// queuedAnalysis is one queue entry: the object, the generation stored when it was queued,
// and the queue it belongs to. The generation is what lets the drain remove exactly the
// entry it analyzed: a request that re-queued the object in the meantime stored a newer
// generation, so its entry survives.
type queuedAnalysis struct {
	key        analyzeKey
	generation uint64
	queue      *sync.Map
}

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

// analysisRetry is the backoff state of a failed analysis.
type analysisRetry struct {
	attempts int
	nextAt   time.Time
}

// maxAnalysisRetries bounds the retries before the object waits for the next
// full scan.
const maxAnalysisRetries = 3

// analysisRetryBackoff returns the delay before the next attempt.
func analysisRetryBackoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	default:
		return 5 * time.Minute
	}
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

// QueueAnalysis enqueues an object for analysis because its definition changed or was just
// reported. A request supersedes a pending backoff and a pending scan entry: the analysis
// runs now and reads the current definition, so the scan no longer has a reason to queue it.
// Storing a new generation also supersedes an analysis already running for the old request:
// that attempt's result is guarded by its own hash, and its failure path leaves this entry
// alone.
func (a *Analyzer) QueueAnalysis(metaGUID string, metaType storepb.MetaType) {
	key := analyzeKey{MetaGUID: metaGUID, MetaType: metaType}
	a.queueMu.Lock()
	defer a.queueMu.Unlock()
	a.retryMap.Delete(key)
	a.scanMap.Delete(key)
	a.analyzeMap.Store(key, a.nextGeneration())
}

// queueScan enqueues an object because the hourly scan found its stored result out of date.
// It is the backlog queue: an event that already queued the object wins, because the event
// analysis reads the same current definition and has to run anyway.
func (a *Analyzer) queueScan(metaGUID string, metaType storepb.MetaType) {
	key := analyzeKey{MetaGUID: metaGUID, MetaType: metaType}
	a.queueMu.Lock()
	defer a.queueMu.Unlock()
	if _, ok := a.analyzeMap.Load(key); ok {
		return
	}
	a.retryMap.Delete(key)
	a.scanMap.Store(key, a.nextGeneration())
}

// nextGeneration returns the next queue generation. It is strictly increasing for the life
// of the process, so two queued requests for the same object can never look alike.
func (a *Analyzer) nextGeneration() uint64 {
	return a.generation.Add(1)
}

// queued returns the entry currently stored for key, if any. The two queues are disjoint by
// construction: QueueAnalysis removes the scan entry and queueScan skips a key the event
// queue already holds.
func (a *Analyzer) queued(key analyzeKey) (queuedAnalysis, bool) {
	if v, ok := a.analyzeMap.Load(key); ok {
		generation, ok := v.(uint64)
		if !ok {
			return queuedAnalysis{}, false
		}
		return queuedAnalysis{key: key, generation: generation, queue: &a.analyzeMap}, true
	}
	if v, ok := a.scanMap.Load(key); ok {
		generation, ok := v.(uint64)
		if !ok {
			return queuedAnalysis{}, false
		}
		return queuedAnalysis{key: key, generation: generation, queue: &a.scanMap}, true
	}
	return queuedAnalysis{}, false
}

// consumeQueuedAnalysis removes the analyzed entry from its queue only if it still carries
// the generation the batch selected. A request that re-queued the object meanwhile stored a
// newer generation, so CompareAndDelete leaves it queued for the next tick.
func consumeQueuedAnalysis(item queuedAnalysis) bool {
	return item.queue.CompareAndDelete(item.key, item.generation)
}

// retryAfterFailure schedules the bounded backoff for a failed attempt, unless a newer
// request queued the object while the analysis ran. The queue entry carries the generation
// of the request the attempt served; once it is a different generation, the fresh request
// supersedes the old backoff and must not be delayed by it.
func (a *Analyzer) retryAfterFailure(item queuedAnalysis) {
	current, ok := a.queued(item.key)
	if !ok || current.generation != item.generation {
		return
	}
	if !consumeQueuedAnalysis(item) {
		// A request replaced the entry between the check and the delete; leave it be.
		return
	}
	a.scheduleRetry(item.key)
}

// scheduleRetry re-queues a failed analysis with a bounded backoff. After the
// last attempt the object is left to the next full scan.
func (a *Analyzer) scheduleRetry(key analyzeKey) {
	attempts := 1
	if v, ok := a.retryMap.Load(key); ok {
		if entry, ok := v.(analysisRetry); ok {
			attempts = entry.attempts + 1
		}
	}
	if attempts > maxAnalysisRetries {
		// Drop the retry state: leaving it queued would restart the backoff on
		// the very next tick, which is the hourly retry loop this replaces.
		a.retryMap.Delete(key)
		a.metrics.CountLineageAnalysisRetriesExhausted(key.MetaType)
		slog.Error("Lineage analysis gave up after repeated failures",
			slog.String("guid", key.MetaGUID),
			slog.String("type", key.MetaType.String()))
		return
	}
	retry := analysisRetry{attempts: attempts, nextAt: time.Now().Add(analysisRetryBackoff(attempts))}
	a.retryMap.Store(key, retry)
	// Add the key only if no fresh request queued it since this attempt consumed
	// its own entry. Re-queueing unconditionally would overwrite that request with
	// this attempt's older backoff.
	if _, loaded := a.analyzeMap.LoadOrStore(key, a.nextGeneration()); loaded {
		a.retryMap.CompareAndDelete(key, retry)
	}
}

// Name identifies the runner in the server's lifecycle logs.
func (*Analyzer) Name() string { return "lineageanalyzer" }

// QueueStats reports how many objects are waiting in each analysis queue. It is
// read by the metrics collector at scrape time: sync.Map has no Len, so both
// queues are walked. Objects waiting out a retry backoff are counted too.
func (a *Analyzer) QueueStats() (eventQueue, scanQueue int) {
	a.analyzeMap.Range(func(_, _ any) bool {
		eventQueue++
		return true
	})
	a.scanMap.Range(func(_, _ any) bool {
		scanQueue++
		return true
	})
	return eventQueue, scanQueue
}

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

// needsAnalysis reports whether an object must be (re)analyzed: it has no stored
// result, its metadata hash changed, or the stored result came from different
// analyzer rules. The last condition is what makes an analyzer upgrade or a
// rollback refresh the lineage by itself.
func needsAnalysis(res *store.MetaRegistryResource, version *store.ColumnLineageVersion) bool {
	if version == nil {
		return true
	}
	if !bytes.Equal(version.MetaHash, res.MetaHash) {
		return true
	}
	return version.AnalyzerVersion != lineage.AnalyzerVersion
}

// takeAnalyzeBatch selects at most limit ready entries from a queue. Keys whose retry
// backoff has not elapsed, and every key over the cap, stay queued for a later tick. The
// entries are not removed here: the drain consumes each one with its captured generation so
// a request that arrives in between survives.
func (a *Analyzer) takeAnalyzeBatch(queue *sync.Map, now time.Time, limit int) []queuedAnalysis {
	if limit <= 0 {
		return nil
	}
	batch := make([]queuedAnalysis, 0, limit)
	queue.Range(func(k, v any) bool {
		if len(batch) >= limit {
			return false
		}
		key, ok := k.(analyzeKey)
		if !ok {
			return true
		}
		generation, ok := v.(uint64)
		if !ok {
			return true
		}
		// A failed object stays queued until its backoff elapses.
		if retry, ok := a.retryMap.Load(key); ok {
			if entry, ok := retry.(analysisRetry); ok && now.Before(entry.nextAt) {
				return true
			}
		}
		batch = append(batch, queuedAnalysis{key: key, generation: generation, queue: queue})
		return true
	})
	return batch
}

// typeLookupGUIDs returns the distinct relation GUIDs whose meta type the analysis still has
// to resolve. The analyzed object's own type is already known, so it is left out. The result
// is sorted so the batch query binds a stable list.
func typeLookupGUIDs(guidLookupSet map[string]struct{}, metaGUID string) []string {
	guids := make([]string, 0, len(guidLookupSet))
	for guid := range guidLookupSet {
		if guid != metaGUID {
			guids = append(guids, guid)
		}
	}
	slices.Sort(guids)
	return guids
}

// drainAndAnalyze analyzes one bounded batch of queued objects and leaves the rest queued.
// Event-driven work is selected first and the scan backlog fills only what is left of the
// budget, so a full-registry backfill delays a just-saved statement by at most one batch.
func (a *Analyzer) drainAndAnalyze(ctx context.Context) {
	now := time.Now()
	batch := a.takeAnalyzeBatch(&a.analyzeMap, now, maxAnalyzeBatch)
	if remaining := maxAnalyzeBatch - len(batch); remaining > 0 {
		batch = append(batch, a.takeAnalyzeBatch(&a.scanMap, now, remaining)...)
	}
	if len(batch) == 0 {
		return
	}

	wp := pool.New().WithMaxGoroutines(MaxGoroutines)
	for _, item := range batch {
		it := item
		wp.Go(func() {
			// A panic in one object must not take the process down or stop the
			// other objects in this batch.
			defer func() {
				if r := recover(); r != nil {
					slog.Error("Lineage analysis panicked",
						slog.String("guid", it.key.MetaGUID),
						slog.String("type", it.key.MetaType.String()),
						slog.Any("panic", r))
					a.metrics.ObserveLineageAnalysis(it.key.MetaType, storepb.Engine_ENGINE_UNSPECIFIED, metrics.AnalysisPanic)
					a.retryAfterFailure(it)
				}
			}()
			a.metrics.IncLineageAnalysisInFlight()
			defer a.metrics.DecLineageAnalysisInFlight()
			objCtx, cancel := context.WithTimeout(ctx, analyzeObjectTimeout)
			defer cancel()
			if err := a.analyzeObject(objCtx, it.key.MetaGUID, it.key.MetaType); err != nil {
				if errors.Is(err, errAnalysisStale) {
					// The text changed under the analysis. Queue the new text now
					// instead of waiting for the hourly scan; the write was dropped,
					// so nothing stale was persisted.
					a.metrics.ObserveLineageAnalysis(it.key.MetaType, storepb.Engine_ENGINE_UNSPECIFIED, metrics.AnalysisStale)
					a.QueueAnalysis(it.key.MetaGUID, it.key.MetaType)
					return
				}
				if analysisTimedOut(objCtx, err) {
					// Nothing was learned about the SQL: retrying is right and recording a
					// failure is not, because that would clear lineage this attempt never
					// inspected.
					slog.Warn("Lineage analysis timed out; it will be retried",
						slog.String("guid", it.key.MetaGUID),
						slog.String("type", it.key.MetaType.String()),
						log.WithError(err))
					// The interrupted branch inside analyzeObject already recorded
					// its own outcome; every other deadline hit is a timeout here.
					if !errors.Is(err, errAnalysisInterrupted) {
						a.metrics.ObserveLineageAnalysis(it.key.MetaType, storepb.Engine_ENGINE_UNSPECIFIED, metrics.AnalysisTimeout)
					}
					a.retryAfterFailure(it)
					return
				}
				slog.Error("Lineage analysis failed",
					slog.String("guid", it.key.MetaGUID),
					slog.String("type", it.key.MetaType.String()),
					log.WithError(err))
				a.retryAfterFailure(it)
				return
			}
			// Remove the entry this attempt served. A request that re-queued the
			// object while the analysis ran stored a newer generation, so its
			// entry stays for the next tick.
			consumeQueuedAnalysis(it)
			a.retryMap.Delete(it.key)
		})
	}
	wp.Wait()
}

// analysisTimedOut reports whether an analysis was cut short by its deadline or by the runner
// stopping, rather than by the statement itself. Either way the SQL was not read, so the
// object is retried and its stored lineage is left alone; only a real parse failure clears it.
func analysisTimedOut(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || ctx.Err() != nil
}

// analyzeObject runs lineage analysis for a single lineage-analyzable object.
func (a *Analyzer) analyzeObject(ctx context.Context, metaGUID string, metaType storepb.MetaType) error {
	// Extract context from GUID: instanceID;database;schema;name
	parts := common.SplitMetaGUID(metaGUID)
	if len(parts) != 4 {
		return a.finishFailedAnalysis(ctx, metaType, storepb.Engine_ENGINE_UNSPECIFIED, storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("invalid GUID format %q", metaGUID)))
	}
	instanceID, database, schema, name := parts[0], parts[1], parts[2], parts[3]

	// Look up instance to get engine type.
	instance, err := a.store.GetInstance(ctx, &store.FindInstanceMessage{ResourceID: &instanceID})
	if err != nil {
		return a.finishFailedAnalysis(ctx, metaType, storepb.Engine_ENGINE_UNSPECIFIED, storeError(ctx, a.store, metaGUID, metaType, err, "failed to get instance"))
	}
	if instance == nil {
		return a.finishFailedAnalysis(ctx, metaType, storepb.Engine_ENGINE_UNSPECIFIED, storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("instance %q not found", instanceID)))
	}
	engine := instance.Metadata.GetEngine()

	// Load the stored metadata. The object type is known, so it is passed with the GUID and
	// the lookup can be answered from the GUID+type cache.
	res, err := a.store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &metaGUID, ObjectType: &metaType})
	if err != nil {
		return a.finishFailedAnalysis(ctx, metaType, engine, storeError(ctx, a.store, metaGUID, metaType, err, "failed to get meta registry"))
	}
	if res == nil {
		return a.finishFailedAnalysis(ctx, metaType, engine, storeError(ctx, a.store, metaGUID, metaType, nil,
			fmt.Sprintf("meta registry entry not found for GUID %q", metaGUID)))
	}

	// Get SQL definition and build the full CREATE VIEW statement.
	definition, wrappedSQL, err := buildSQL(name, engine, metaType, res)
	if err != nil {
		return a.finishFailedAnalysis(ctx, metaType, engine, storeError(ctx, a.store, metaGUID, metaType, err, "failed to build SQL"))
	}
	if definition == "" {
		if emptyDefinitionClearsLineage(metaType) {
			// A statement's text was cleared: the object no longer has the SQL the
			// stored edges describe. Clear them and record why, in the same
			// transaction that marks the new hash analyzed.
			slog.Debug("Lineage analyzer clearing the edges of an object with no definition",
				slog.String("guid", metaGUID), slog.String("type", metaType.String()))
			return a.finishAnalysisWrite(ctx, metaType, engine, metrics.AnalysisDefinitionCleared,
				markAnalysisFailed(ctx, a.store, metaGUID, metaType, res.MetaHash, nil))
		}
		// A view cannot exist without a defining query, so an empty stored
		// definition means this snapshot could not read the body (a privilege-limited
		// information_schema read, a driver partial snapshot) rather than that the SQL
		// was removed. Keep the last correct edges and record why; the next sync that
		// reads the definition changes the hash and re-analyzes.
		slog.Warn("Lineage analyzer kept the edges of an object whose definition was unavailable",
			slog.String("guid", metaGUID), slog.String("type", metaType.String()))
		return a.finishAnalysisWrite(ctx, metaType, engine, metrics.AnalysisDefinitionUnavailable,
			markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash,
				"lineage analysis skipped: the definition was unavailable in this sync"))
	}

	// Run lineage analysis with context so unqualified names resolve correctly.
	ac := catalog.AnalysisContext{InstanceID: instanceID, Database: database, Schema: schema}
	analysisCtx := catalog.WithAnalysisContext(ctx, ac)
	analysisStarted := time.Now()
	relations, err := a.lineage.Analyze(analysisCtx, engine, wrappedSQL)
	a.metrics.ObserveLineageAnalyzeDuration(engine, metaType, time.Since(analysisStarted))

	// What an analysis could not represent does not condemn the rest of it: the
	// analyzers return the edges they did find together with the gap, so the
	// lineage is stored and the gap is recorded beside it.
	var unsupported *lineage.UnsupportedStatementError
	switch {
	case err == nil:
	case errors.As(err, &unsupported):
	case errors.Is(err, lineage.ErrorEngineNotSupported):
		// No analyzer is registered for this engine. Record the skip together
		// with the current hash so the hourly scan stops re-queueing the
		// object, and keep the reason visible in error_message.
		slog.Warn("Lineage analysis skipped: the engine has no lineage analyzer",
			slog.String("guid", metaGUID), slog.String("engine", engine.String()))
		return a.finishAnalysisWrite(ctx, metaType, engine, metrics.AnalysisEngineUnsupported,
			markAnalyzed(ctx, a.store, metaGUID, metaType, res.MetaHash,
				fmt.Sprintf("engine %s has no lineage analyzer; analysis skipped", engine)))
	case ctx.Err() != nil:
		// The deadline hit while the statement was being read. What it says is unknown,
		// so it must not be recorded as a deterministic parse failure; the drain treats
		// the interrupted analysis as transient and retries it.
		slog.Warn("Lineage analysis interrupted",
			slog.String("guid", metaGUID), slog.String("engine", engine.String()), log.WithError(err))
		// The drain owns timeout/stale/panic; this branch owns interrupted, so it is
		// recorded here and the error carries the sentinel that tells the drain not
		// to record a timeout for the same attempt.
		a.metrics.ObserveLineageAnalysis(metaType, engine, metrics.AnalysisInterrupted)
		return fmt.Errorf("%w: %w", errAnalysisInterrupted, ctx.Err())
	default:
		// A parse error is deterministic — the text does not change between
		// retries — so the object is recorded as analyzed at the current hash
		// instead of being re-queued forever by the hourly scan. The analyzers do
		// no other failing I/O: a catalog lookup that fails degrades one relation's
		// columns to a wildcard and is reported as a gap on the version row, which
		// is what keeps a degraded analysis distinguishable from a complete one.
		slog.Warn("Lineage analysis failed on an unsupported statement",
			slog.String("guid", metaGUID), slog.String("engine", engine.String()), log.WithError(err))
		return a.finishAnalysisWrite(ctx, metaType, engine, metrics.AnalysisFailed,
			markAnalysisFailed(ctx, a.store, metaGUID, metaType, res.MetaHash, err))
	}

	// What a partial analysis could not model is stored on the version row
	// alongside the lineage written below.
	partialMessage := ""
	if unsupported != nil {
		partialMessage = unsupported.Error()
	}

	// Convert relations to ColumnLineage rows and collect GUIDs whose meta types
	// should be resolved from the registry.
	guidTypeMap := map[string]storepb.MetaType{metaGUID: metaType}
	guidLookupSet := make(map[string]struct{})
	var lineages []*store.ColumnLineage
	for _, rel := range relations {
		// A statement only names an object down to database/schema level, so the
		// rest comes from the analysis context. AnalyzeSQL resolves its scopes
		// through the same helper.
		sourceID := ac.Complete(rel.Source.Table)

		if rel.Transformation == nil {
			rel.Transformation = make([]model.Transformation, 0)
		}
		srcGUID := sourceID.GUID()

		if metaType == storepb.MetaType_MANUAL_SQL || metaType == storepb.MetaType_APPLICATION_SQL {
			targetID := ac.Complete(rel.Target.Table)
			guidLookupSet[srcGUID] = struct{}{}
			lineages = append(lineages, &store.ColumnLineage{
				MetaGUID:       metaGUID,
				MetaType:       metaType,
				SourceGUID:     srcGUID,
				SourceColumn:   rel.Source.Name,
				TargetGUID:     metaGUID,
				TargetColumn:   rel.Target.Name,
				TargetType:     metaType,
				RelationType:   rel.RelationType,
				Transformation: rel.Transformation,
			})

			if !rel.IsTemp {
				targetGUID := targetID.GUID()
				guidLookupSet[targetGUID] = struct{}{}
				lineages = append(lineages, &store.ColumnLineage{
					MetaGUID:       metaGUID,
					MetaType:       metaType,
					SourceGUID:     metaGUID,
					SourceColumn:   rel.Target.Name,
					SourceType:     metaType,
					TargetGUID:     targetGUID,
					TargetColumn:   rel.Target.Name,
					RelationType:   rel.RelationType,
					Transformation: rel.Transformation,
				})
			}
			continue
		}

		if rel.IsTemp {
			continue
		}

		// A VIEW's or materialized view's relation target is the view itself. Pin it
		// to the analyzed object's GUID instead of re-deriving it from the wrapped
		// name: the wrapper quotes the name for the parser, and a spelling that
		// canonicalizes elsewhere would write an edge into a GUID no registry row has.
		guidLookupSet[srcGUID] = struct{}{}
		lineages = append(lineages, &store.ColumnLineage{
			MetaGUID:       metaGUID,
			MetaType:       metaType,
			SourceGUID:     srcGUID,
			SourceColumn:   rel.Source.Name,
			TargetGUID:     metaGUID,
			TargetColumn:   rel.Target.Name,
			TargetType:     metaType,
			RelationType:   rel.RelationType,
			Transformation: rel.Transformation,
		})
	}

	// A reported statement is classified here and nowhere else: ingestion stores the
	// statement without deciding, and this is the one place the read/write rule lives.
	// The kind is held back until the edges commit (see below), because it is derived
	// state about them.
	var applicationKind store.ApplicationSQLKind
	if metaType == storepb.MetaType_APPLICATION_SQL {
		kind, targetEdges := a.classifyApplicationSQL(ctx, ac, engine, definition, relations, metaGUID)
		applicationKind = kind
		lineages = append(lineages, targetEdges...)
		// The edge names its target by GUID alone, so the lookup below is what
		// gives the target its type. Without it the relation reaches the API with
		// an unspecified target type, and every client that routes by type — the
		// metadata browser's lineage table, for one — loses the link.
		for _, edge := range targetEdges {
			guidLookupSet[edge.TargetGUID] = struct{}{}
		}
	}

	// The relation GUIDs are resolved with one batch query rather than one lookup each: a
	// large join can name hundreds of sources, and each per-GUID lookup would be a serial
	// round trip. A GUID the registry does not know is simply absent from the result.
	if guids := typeLookupGUIDs(guidLookupSet, metaGUID); len(guids) > 0 {
		metas, err := a.store.ListMetaRegistryResourceDigest(ctx, &store.FindMetaRegistryResourceMessage{GUIDs: &guids})
		if err != nil {
			slog.Warn("Lineage analyzer failed to look up source meta types", log.WithError(err))
		} else {
			for _, meta := range metas {
				guidTypeMap[meta.GUID] = meta.ObjectType
			}
		}
	}
	for _, l := range lineages {
		if t, ok := guidTypeMap[l.SourceGUID]; ok {
			l.SourceType = t
		}
		if t, ok := guidTypeMap[l.TargetGUID]; ok {
			l.TargetType = t
		}
	}

	if len(lineages) == 0 {
		slog.Debug("Lineage analyzer found no lineage relations (after filtering temp targets)", slog.String("guid", metaGUID))
	}

	// Persist the result only if the object still holds the text this analysis
	// read. The edges, the version row and any gap message commit together, so a
	// rolled-back write cannot leave the object marked as analyzed, and a result
	// computed for a deleted object is dropped instead of re-creating its edges.
	var partialMessagePtr *string
	if partialMessage != "" {
		partialMessagePtr = &partialMessage
	}
	if err := a.store.ReplaceColumnLineageIfCurrent(ctx, metaGUID, metaType, res.MetaHash, lineage.AnalyzerVersion, lineages, partialMessagePtr); err != nil {
		return a.finishFailedAnalysis(ctx, metaType, engine,
			analysisWriteError(ctx, a.store, metaGUID, metaType, err, "failed to replace column lineage"))
	}
	a.metrics.ObserveLineageAnalysis(metaType, engine, metrics.AnalysisStored)
	columnEdges, tableEdges := analysisEdgeCounts(lineages)
	a.metrics.AddLineageEdges(metaType, metrics.EdgeKindColumn, columnEdges)
	a.metrics.AddLineageEdges(metaType, metrics.EdgeKindTable, tableEdges)
	// The classification is a derived cache for the list page's filter, so it is
	// written only after the edges commit: a kind that claims a classification the
	// graph does not have misleads the filter, while a missing kind is merely stale
	// until the next analysis. A failure to write the cache must not discard edges
	// that were just committed, so it is logged rather than returned.
	if metaType == storepb.MetaType_APPLICATION_SQL {
		if err := a.store.SetApplicationSQLKind(ctx, metaGUID, applicationKind); err != nil {
			slog.Warn("Lineage analyzer failed to record the statement kind",
				slog.String("guid", metaGUID), log.WithError(err))
		}
	}
	return nil
}

// buildSQL extracts the object definition and wraps it as needed for lineage parsing.
func buildSQL(name string, engine storepb.Engine, metaType storepb.MetaType, res *store.MetaRegistryResource) (definition string, wrapped string, err error) {
	switch metaType {
	case storepb.MetaType_VIEW:
		definition = res.Metadata.GetViewMetadata().GetDefinition()
	case storepb.MetaType_MATERIALIZED_VIEW:
		definition = res.Metadata.GetMaterializedViewMetadata().GetDefinition()
	case storepb.MetaType_MANUAL_SQL:
		definition = res.Metadata.GetManualSqlMetadata().GetSqlText()
	case storepb.MetaType_APPLICATION_SQL:
		definition = res.Metadata.GetApplicationSqlMetadata().GetSqlText()
	default:
		return "", "", errors.Errorf("unsupported meta type %v", metaType)
	}
	if definition == "" {
		return "", "", nil
	}
	if metaType == storepb.MetaType_MANUAL_SQL {
		return definition, definition, nil
	}
	if metaType == storepb.MetaType_APPLICATION_SQL {
		// The stored text is the parameterized one, and the MySQL family's grammar has no
		// production that accepts a placeholder, so the analyzer is handed the substituted
		// form. The stored text stays the definition: it is what the object is.
		return definition, sqlsanitize.Analyzable(engine, definition), nil
	}
	// A stored definition that is already a complete statement is used as-is.
	// StarRocks keeps the whole SHOW CREATE MATERIALIZED VIEW output in
	// MaterializedViewMetadata.Definition, so wrapping it in
	// "CREATE MATERIALIZED VIEW <name> AS ..." would produce
	// "... AS CREATE MATERIALIZED VIEW ...", which no parser accepts.
	if isCompleteStatement(definition) {
		return definition, definition, nil
	}

	keyword := "CREATE VIEW"
	if metaType == storepb.MetaType_MATERIALIZED_VIEW {
		keyword = "CREATE MATERIALIZED VIEW"
	}

	var identifier string
	switch engine {
	// The MySQL family, including StarRocks and Doris, quotes identifiers with
	// backticks. MariaDB shares the MySQL driver and its views keep a bare query
	// body, so it needs the same quoting as MySQL.
	case storepb.Engine_MYSQL, storepb.Engine_TIDB, storepb.Engine_MARIADB, storepb.Engine_DORIS, storepb.Engine_STARROCKS:
		identifier = "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case storepb.Engine_POSTGRES:
		identifier = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	case storepb.Engine_MSSQL:
		identifier = "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		// Engines this build has no analyzer for keep the object name as written:
		// they are skipped before the wrapper's identifier could matter, and the
		// fallback keeps that skip visible instead of turning it into a build error.
		identifier = name
	}

	return definition, fmt.Sprintf("%s %s AS %s", keyword, identifier, definition), nil
}

// isCompleteStatement reports whether a stored definition is already a whole
// CREATE statement rather than a bare query body. Leading whitespace and
// comments are skipped first: a complete DDL commonly opens with a `--` or
// `/* */` note, and wrapping such a statement in "CREATE VIEW ... AS" makes it
// unparseable. A bare query body can never begin with CREATE, so the check is
// safe for every engine.
func isCompleteStatement(definition string) bool {
	rest := strings.TrimSpace(definition)
	for {
		switch {
		case strings.HasPrefix(rest, "--"):
			lineEnd := strings.IndexByte(rest, '\n')
			if lineEnd < 0 {
				return false
			}
			rest = strings.TrimSpace(rest[lineEnd+1:])
		case strings.HasPrefix(rest, "/*"):
			commentEnd := strings.Index(rest[2:], "*/")
			if commentEnd < 0 {
				return false
			}
			rest = strings.TrimSpace(rest[2+commentEnd+2:])
		default:
			return hasCreateKeyword(rest)
		}
	}
}

// hasCreateKeyword reports whether s starts with the CREATE keyword followed by
// a separator. It accepts any whitespace between CREATE and what follows, so
// "CREATE\nVIEW" counts as a complete statement.
func hasCreateKeyword(s string) bool {
	const keyword = "CREATE"
	if len(s) < len(keyword) || !strings.EqualFold(s[:len(keyword)], keyword) {
		return false
	}
	rest := s[len(keyword):]
	return len(rest) > 0 && unicode.IsSpace(rune(rest[0]))
}

// errAnalysisStale tells drainAndAnalyze that the result was computed from text
// that has already been replaced, so the object is queued again instead of
// being retried as a failure.
var errAnalysisStale = errors.New("analysis result is stale")

// errAnalysisGone tells analyzeObject that the object disappeared before its
// result could be written, so the result is dropped instead of retried.
var errAnalysisGone = errors.New("analysis object is gone")

// errAnalysisInterrupted marks an analysis cut short by its deadline or by the
// runner's shutdown. analyzeObject records that outcome itself; the sentinel is
// what lets the drain tell this branch apart from every other deadline hit, so
// the same attempt is not also counted as a timeout.
var errAnalysisInterrupted = errors.New("analysis is interrupted")

// finishAnalysisWrite records the outcome of a terminal branch once its state
// write returns. A successful write records the branch result; a failed one is
// classified like any other failed analysis, so a vanished object, a stale result
// or a store error is not reported as the branch result.
func (a *Analyzer) finishAnalysisWrite(ctx context.Context, metaType storepb.MetaType, engine storepb.Engine, result string, writeErr error) error {
	if writeErr != nil {
		return a.finishFailedAnalysis(ctx, metaType, engine, writeErr)
	}
	a.metrics.ObserveLineageAnalysis(metaType, engine, result)
	return nil
}

// finishFailedAnalysis records why an analysis did not reach a result and returns
// the error the caller must return. A vanished object drops its result silently.
// A stale result and a deadline/shutdown are recorded by the drain, which owns
// those outcomes (and re-queues the object); everything else is a store error,
// recorded here because the drain does not count generic failures.
func (a *Analyzer) finishFailedAnalysis(ctx context.Context, metaType storepb.MetaType, engine storepb.Engine, err error) error {
	if result := analysisFailureResult(ctx, err); result != "" {
		a.metrics.ObserveLineageAnalysis(metaType, engine, result)
	}
	if errors.Is(err, errAnalysisGone) {
		return nil
	}
	return err
}

// analysisFailureResult maps a failed analysis to the outcome recorded for it. It
// returns "" when the drain records the outcome instead: a stale result or a
// timeout is retried there. A vanished object is always "gone", even when the
// context expired meanwhile.
func analysisFailureResult(ctx context.Context, err error) string {
	switch {
	case errors.Is(err, errAnalysisGone):
		return metrics.AnalysisObjectGone
	case errors.Is(err, errAnalysisStale), analysisTimedOut(ctx, err):
		return ""
	default:
		return metrics.AnalysisStoreError
	}
}

// analysisEdgeCounts splits the edges an analysis submits to the replace into
// column- and table-level counts. A table-level edge names neither a source nor a
// target column. The counts are an upper bound on what the replace inserts: it
// deletes the old set and re-inserts with ON CONFLICT DO NOTHING, which drops
// duplicates.
func analysisEdgeCounts(lineages []*store.ColumnLineage) (column, table int) {
	for _, l := range lineages {
		if l.SourceColumn == "" && l.TargetColumn == "" {
			table++
		}
	}
	return len(lineages) - table, table
}

// storeError surfaces an error before any result exists. It records the message
// on the object's existing analysis-state row without touching its hash: a
// transient failure must not make a successful result look unanalyzed, and the
// next attempt still compares against the last stored result.
func storeError(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, cause error, msg string) error {
	var full string
	if cause != nil {
		full = fmt.Sprintf("%s: %v", msg, cause)
	} else {
		full = msg
	}
	_ = s.RecordAnalysisError(ctx, metaGUID, metaType, full)
	if cause != nil {
		return errors.Wrap(cause, msg)
	}
	return errors.New(msg)
}

// analysisWriteError classifies a failed result write: a vanished object drops the
// result, a changed one asks for a re-analysis of the new text, and anything else
// is a transient store error that the retry map handles. analyzeObject turns
// errAnalysisGone back into a nil return, so the object is not retried.
func analysisWriteError(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, cause error, msg string) error {
	switch {
	case errors.Is(cause, store.ErrAnalysisGone):
		slog.Debug("Lineage analysis dropped: the object disappeared during analysis",
			slog.String("guid", metaGUID), slog.String("type", metaType.String()))
		return errAnalysisGone
	case errors.Is(cause, store.ErrAnalysisStale):
		return errAnalysisStale
	default:
		return storeError(ctx, s, metaGUID, metaType, cause, msg)
	}
}

// markAnalyzed records a successful analysis or a deliberate skip (an engine
// without an analyzer) with the current metahash and the analyzer version. A
// non-empty errorMessage documents why analysis was skipped while still
// recording the hash, so the object is not retried until its metadata changes
// or the analyzer rules do. It leaves the stored edges alone: a skip that is not
// about this object's SQL has nothing to say about them.
func markAnalyzed(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, metaHash []byte, errorMessage string) error {
	var message *string
	if errorMessage != "" {
		message = &errorMessage
	}
	if err := s.MarkAnalyzedIfCurrent(ctx, metaGUID, metaType, metaHash, lineage.AnalyzerVersion, message); err != nil {
		return analysisWriteError(ctx, s, metaGUID, metaType, err, "failed to record analysis state")
	}
	return nil
}

// emptyDefinitionClearsLineage reports whether an empty stored definition means the
// object's SQL is gone rather than unavailable. A manual or reported statement's text
// can be cleared, and the edges of the SQL it no longer has are wrong. A view or
// materialized view cannot exist without a defining query, so an empty definition there
// is a read failure (for example a MySQL account without SHOW VIEW), and keeping the
// last correct edges is right until the definition is readable again.
func emptyDefinitionClearsLineage(metaType storepb.MetaType) bool {
	switch metaType {
	case storepb.MetaType_MANUAL_SQL, storepb.MetaType_APPLICATION_SQL:
		return true
	default:
		return false
	}
}

// analysisFailureMessage builds the version row's error_message for a
// deterministic analysis failure. A nil cause means there is no SQL to analyze
// at all: the object's definition is empty, which is recorded as a skip rather
// than a parse failure but still clears the previous definition's edges. The
// message is never empty: it is the only explanation the graph offers for a
// graph that was just cleared.
func analysisFailureMessage(cause error) string {
	if cause == nil {
		return "lineage analysis skipped: the object has no SQL definition"
	}
	return fmt.Sprintf("lineage analysis failed: %v", cause)
}

// markAnalysisFailed records a deterministic analysis failure — SQL the analyzer
// could not parse, or an object left with no SQL definition. The current hash is
// stored with the error message so the hourly scan stops re-queueing an unchanged
// definition (a metadata or analyzer change re-queues it), and any lineage a
// previous definition left behind is dropped because it describes SQL the object
// no longer has. Clearing the edges and recording the failure commit together. A
// gap the analysis reported instead of a failure takes the partial path above and
// keeps its edges.
func markAnalysisFailed(ctx context.Context, s *store.Store, metaGUID string, metaType storepb.MetaType, metaHash []byte, cause error) error {
	err := s.MarkAnalysisFailedIfCurrent(ctx, metaGUID, metaType, metaHash, lineage.AnalyzerVersion,
		analysisFailureMessage(cause))
	if err != nil {
		return analysisWriteError(ctx, s, metaGUID, metaType, err, "failed to record analysis failure")
	}
	return nil
}

// classifyApplicationSQL decides whether a reported statement is a read or a write, and
// returns the graph edge a write with no column relation still needs.
//
// The rule, and why it is not simply "the analysis produced a relation": the analyzers
// record column-level derivation, so a statement that reads a column produces a relation
// whose target is the statement, while a statement that writes values from nowhere — the
// ORM's `INSERT INTO t (a, b) VALUES (?, ?)`, or a `DELETE` that references no column —
// produces none at all in every dialect. Those are writes all the same, and without a target
// the graph would show nothing about them, so the target is read from the statement itself.
//
// The table-level edge carries no columns and no transformations, which is the shape the
// OpenLineage table-level path already stores. See
// .agents/docs/application-sql-ingestion.md, "Classification and the table-level edge".
func (a *Analyzer) classifyApplicationSQL(
	ctx context.Context,
	ac catalog.AnalysisContext,
	engine storepb.Engine,
	definition string,
	relations []model.ColumnRelation,
	metaGUID string,
) (store.ApplicationSQLKind, []*store.ColumnLineage) {
	for _, rel := range relations {
		if !rel.IsTemp {
			return store.ApplicationSQLKindWrite, nil
		}
	}

	targets, err := dmltarget.Targets(engine, sqlsanitize.Analyzable(engine, definition))
	if err != nil {
		// Not a write, or one this package cannot name. Either way it has no out-edge, and a
		// SELECT with no source column is not drawn at all — manual SQL has the same hole.
		return store.ApplicationSQLKindRead, nil
	}

	var edges []*store.ColumnLineage
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		// The analyzer produced no relation here, so the name is read from the statement and the
		// registry is asked for the object it belongs to: an engine that binds identifiers
		// case-insensitively may have written `orders` for the object the registry holds as
		// `Orders`, and an edge into a GUID the registry does not have points at nothing.
		id := a.lineage.CanonicalIdentifier(ctx, engine, ac.Complete(target))
		guid := id.GUID()
		if guid == metaGUID {
			continue
		}
		if _, ok := seen[guid]; ok {
			continue
		}
		seen[guid] = struct{}{}
		edges = append(edges, &store.ColumnLineage{
			MetaGUID:     metaGUID,
			MetaType:     storepb.MetaType_APPLICATION_SQL,
			SourceGUID:   metaGUID,
			SourceType:   storepb.MetaType_APPLICATION_SQL,
			TargetGUID:   guid,
			TargetColumn: "",
			// The column is not null in the schema, so an empty list is required rather than a
			// nil one; RelationTypeOf reads it as `direct`.
			Transformation: make([]model.Transformation, 0),
			RelationType:   model.RelationTypeOf(nil),
		})
	}
	if len(edges) == 0 {
		return store.ApplicationSQLKindRead, nil
	}
	return store.ApplicationSQLKindWrite, edges
}
