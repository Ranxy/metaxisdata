package lineageanalyzer

import (
	"bytes"
	"log/slog"
	"slices"
	"sync"
	"time"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/store"
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
