package schemasync

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// databaseRetry is the backoff state of a failed database schema sync.
type databaseRetry struct {
	attempts int
	nextAt   time.Time
}

// objectDefinitionState is one database's DDL refresh state: when it last completed
// a full refresh, and where the rotation over a database above the fetch limit stands.
type objectDefinitionState struct {
	lastFullRefresh time.Time
	cursor          int
}

// databaseSyncRetryBackoff returns the delay before the next attempt. The last
// step equals instanceSyncInterval: past it the 15-minute instance scan is the
// retry.
func databaseSyncRetryBackoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	default:
		return instanceSyncInterval
	}
}

// enqueueDatabase queues a database for the checker. A fresh request (an API full
// sync or the periodic scan) supersedes any pending backoff. A deleted row is never
// queued: it is kept as history, and only the instance enumeration revives it.
func (s *Syncer) enqueueDatabase(database *store.DatabaseMessage) {
	if database == nil || database.Deleted {
		return
	}
	key := database.String()
	s.databaseSyncRetryMap.Delete(key)
	s.databaseSyncMap.Store(key, database)
}

// forgetQueuedDatabase drops a database from the checker's queue and its backoff.
// A soft-deleted row is history: syncing it would write LastSyncTime to a database
// the target no longer has, and only the instance enumeration revives it.
func (s *Syncer) forgetQueuedDatabase(key string) {
	s.databaseSyncRetryMap.Delete(key)
	s.databaseSyncMap.Delete(key)
}

// scheduleRetry re-queues a failed database with a bounded backoff; the checker
// skips it until nextAt. After the last attempt the database is left to the next
// instance-level scan instead of backing off forever. It reports whether the
// retries are exhausted, which is the point at which a background failure is
// worth telling the administrators about.
func (s *Syncer) scheduleRetry(database *store.DatabaseMessage) bool {
	key := database.String()
	attempts := 1
	if v, ok := s.databaseSyncRetryMap.Load(key); ok {
		if entry, ok := v.(databaseRetry); ok {
			attempts = entry.attempts + 1
		}
	}
	if attempts > maxDatabaseSyncRetries {
		s.databaseSyncRetryMap.Delete(key)
		slog.Warn("Database schema sync gave up after repeated failures; waiting for the next instance scan",
			slog.String("instance", database.InstanceID),
			slog.String("database", database.DatabaseName))
		return true
	}
	s.databaseSyncRetryMap.Store(key, databaseRetry{attempts: attempts, nextAt: time.Now().Add(databaseSyncRetryBackoff(attempts))})
	s.databaseSyncMap.Store(key, database)
	s.metrics.CountSchemaSyncRetry(attempts)
	return false
}

// retryDue reports whether a queued database may be synced at now. A database
// whose sync failed stays queued but is skipped until its backoff elapses.
func (s *Syncer) retryDue(key any, now time.Time) bool {
	v, ok := s.databaseSyncRetryMap.Load(key)
	if !ok {
		return true
	}
	entry, ok := v.(databaseRetry)
	if !ok {
		return true
	}
	return !now.Before(entry.nextAt)
}

// trySyncAllOnce runs one instance scan under a bound of two intervals. A scan
// that wedges on a target or on the store must not outlive two intervals — every
// tick would otherwise stack another scan on top of it — while a large estate
// still has the whole 30 minutes to enumerate its instances and queue their
// databases.
func (s *Syncer) trySyncAllOnce(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 2*instanceSyncInterval)
	defer cancel()
	started := time.Now()
	ok := s.trySyncAll(ctx)
	s.metrics.ObserveSchemaSyncScan(ok, time.Since(started))
}

// trySyncAll runs one instance scan and reports whether it completed. A store
// read failure or a panic leaves the scan incomplete; an individual instance's
// failed sync is reported by that sync's own counter, not here.
func (s *Syncer) trySyncAll(ctx context.Context) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			err, isError := r.(error)
			if !isError {
				err = errors.Errorf("%v", r)
			}
			slog.Error("Instance syncer PANIC RECOVER", log.WithError(err), log.Stack("panic-stack"))
			ok = false
		}
	}()

	wp := pool.New().WithMaxGoroutines(MaximumOutstanding)
	instances, err := s.store.ListInstances(ctx, &store.FindInstanceMessage{})
	if err != nil {
		slog.Error("Failed to retrieve instances", log.WithError(err))
		return false
	}
	instanceNow := time.Now()
	// The instances whose sync failed in this round, including the ones skipped
	// because their connection limit was saturated. Their databases are filtered
	// out below: the instance failure is already reported, and syncing each of
	// its databases would retry the same broken connection and raise a failure
	// notice of its own for a cause the administrators already know.
	var failedMu sync.Mutex
	failedInstances := make(map[string]struct{})
	for _, instance := range instances {
		if !shouldSyncNow(getOrDefaultSyncInterval(instance), getOrDefaultLastSyncTime(instance.Metadata.LastSyncTime), instanceNow) {
			continue
		}

		wp.Go(func() {
			slog.Debug("Sync instance schema", slog.String("instance", instance.ResourceID))
			if _, _, _, err := s.SyncInstance(ctx, instance); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					// The scan budget (or the server shutting down) cancelled this
					// sync. It is not a broken instance: LastSyncTime did not advance,
					// so the next scan picks it up, and neither the administrators nor
					// the remaining databases of the round should treat it as a
					// failure.
					slog.Debug("Instance sync stopped by the scan budget; it stays due for the next scan",
						slog.String("instance", instance.ResourceID), log.WithError(err))
					return
				}
				if errors.Is(err, errInstanceConnectionsExhausted) {
					// A saturated connection budget defers the instance to the next
					// scan instead of failing it; that is backpressure, not a broken
					// instance, and it has its own counter.
					s.metrics.CountConnectionLimitDeferral(metrics.DeferralInstance)
				}
				s.reportInstanceSyncFailure(ctx, instance, err)
				failedMu.Lock()
				failedInstances[instance.ResourceID] = struct{}{}
				failedMu.Unlock()
			}
		})
	}
	wp.Wait()

	instancesMap := map[string]*store.InstanceMessage{}
	for _, instance := range instances {
		instancesMap[instance.ResourceID] = instance
	}

	// The instance round above can take up to a sync timeout, so the database due check is
	// made against a fresh clock: a database that became due during this round would
	// otherwise wait a whole extra interval because it was compared to the round's start.
	databaseNow := time.Now()

	databases, err := s.store.ListDatabases(ctx, &store.FindDatabaseMessage{})
	if err != nil {
		slog.Error("Failed to retrieve databases", log.WithError(err))
		return false
	}
	for _, database := range databasesToEnqueue(databases, instancesMap, failedInstances, databaseNow) {
		s.enqueueDatabase(database)
	}
	return true
}

// databasesToEnqueue returns the databases the round should queue: those of a
// known instance whose own sync did not fail and that are due. An instance whose
// sync failed — or was skipped because its connection limit was saturated — is
// left out so that one broken instance does not produce one failure notice per
// database.
func databasesToEnqueue(databases []*store.DatabaseMessage, instances map[string]*store.InstanceMessage, failedInstances map[string]struct{}, now time.Time) []*store.DatabaseMessage {
	enqueue := make([]*store.DatabaseMessage, 0, len(databases))
	for _, database := range databases {
		instance, ok := instances[database.InstanceID]
		if !ok {
			continue
		}
		if _, failed := failedInstances[database.InstanceID]; failed {
			continue
		}
		// The database inherits the sync interval from the instance.
		if !shouldSyncNow(getOrDefaultSyncInterval(instance), getOrDefaultLastSyncTime(database.Metadata.GetLastSyncTime()), now) {
			continue
		}
		enqueue = append(enqueue, database)
	}
	return enqueue
}
