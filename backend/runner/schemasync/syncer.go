// Package schemasync is a runner that synchronize database schemas.
package schemasync

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/sourcegraph/conc/pool"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/runner"
	"github.com/Ranxy/metaxisdata/backend/store"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

const (
	instanceSyncInterval        = 15 * time.Minute
	databaseSyncCheckerInterval = 10 * time.Second
	syncTimeout                 = 15 * time.Minute
	// defaultSyncInterval means never sync.
	defaultSyncInterval = 0 * time.Second
	MaximumOutstanding  = 100
	// maxDatabaseSyncRetries bounds one database's backoff retries before it is
	// left to the next instance-level scan.
	maxDatabaseSyncRetries = 3
	// statisticsRefreshInterval decouples the table statistics refresh from the
	// schema diff. Statistics are excluded from the change fingerprint, so
	// refreshing them never writes history or re-queues lineage, but rewriting
	// every table's metadata on every 15-minute sync would still be needless
	// write amplification for a value the API shows as an estimate.
	statisticsRefreshInterval = 1 * time.Hour
)

// NewSyncer creates a schema syncer. notifier reports the outcome of a sync
// operation to the user who asked for it, and a background failure to the
// workspace administrators; a nil notifier leaves the runner silent, which is
// what its own tests want.
func NewSyncer(stores *store.Store, dbFactory *dbfactory.DBFactory, stateCfg *state.State, lineageQueue LineageQueue, lineageRevalidator LineageRevalidator, notifier Notifier, m *metrics.Metrics) *Syncer {
	return &Syncer{
		store:              stores,
		dbFactory:          dbFactory,
		stateCfg:           stateCfg,
		lineageQueue:       lineageQueue,
		lineageRevalidator: lineageRevalidator,
		notifier:           notifier,
		metrics:            m,
	}
}

// LineageQueue is the lineage analysis queue a schema sync feeds. The package
// depends on this narrow interface instead of the concrete analyzer in a sibling
// runner package; *lineageanalyzer.Analyzer satisfies it as is.
type LineageQueue interface {
	QueueAnalysis(metaGUID string, metaType storepb.MetaType)
}

// LineageRevalidator is signalled when a schema sync has added or changed
// metadata. Ingested lineage that named a relation the registry did not have is
// re-checked then, as soon as the relation appears, instead of at the next
// maintenance pass.
type LineageRevalidator interface {
	Trigger()
}

// Syncer is the schema syncer.
type Syncer struct {
	store              *store.Store
	dbFactory          *dbfactory.DBFactory
	stateCfg           *state.State
	lineageQueue       LineageQueue
	lineageRevalidator LineageRevalidator
	databaseSyncMap    sync.Map // map[string]*store.DatabaseMessage
	// databaseSyncRetryMap holds the backoff of a database whose sync failed:
	// the database stays queued and the checker skips it until nextAt, so a
	// transient failure does not wait for the next instance-level scan.
	databaseSyncRetryMap sync.Map // map[string]databaseRetry
	// databaseSyncGate serializes the schema sync of one database. The queue
	// above is drained on dequeue, so it is not an in-flight guard by itself,
	// and the API path calls SyncDatabaseSchema directly.
	databaseSyncGate databaseSyncGate
	// instanceSyncGate serializes the enumeration of one instance. The API and
	// the periodic scan both call SyncInstance, and that step writes the
	// instance row and the database rows.
	instanceSyncGate instanceSyncGate
	// definitionMu guards definitionState: the per-database full-DDL refresh clock
	// and rotation cursor. It is process-local for the same reason the sync gates
	// are, and a restart makes every database due for a refresh, which is the
	// first pass a fresh process wants anyway.
	definitionMu    sync.Mutex
	definitionState map[string]*objectDefinitionState
	// statisticsMu guards statisticsState: when each database last had its table
	// statistics refreshed.
	statisticsMu    sync.Mutex
	statisticsState map[string]time.Time
	// notifier writes the messages a sync operation produces. See operation.go.
	notifier Notifier
	// metrics records sync outcomes; a nil value disables instrumentation.
	metrics *metrics.Metrics
	// operations are the user-visible sync operations still waiting for their
	// databases. The slice is short — one entry per API-triggered sync in flight —
	// and is guarded by operationMu because the API registers them while the
	// checker reports their databases.
	operationMu sync.Mutex
	operations  []*SyncOperation
}

// Name identifies the runner in the server's lifecycle logs.
func (*Syncer) Name() string { return "schemasync" }

// Run starts the schema syncer. It blocks until ctx is cancelled.
func (s *Syncer) Run(ctx context.Context) {
	sp := pool.New()
	sp.Go(func() {
		s.trySyncAllOnce(ctx)
		// Jitter the fixed interval so a restart does not keep every process's
		// scan aligned with the other runners' passes.
		interval := runner.Jitter(instanceSyncInterval)
		slog.Debug("Schema syncer's first scan finished; running every interval", slog.Duration("interval", interval))
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.trySyncAllOnce(ctx)
			case <-ctx.Done(): // if cancel() execute
				return
			}
		}
	})

	sp.Go(func() {
		ticker := time.NewTicker(databaseSyncCheckerInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.expireOperations(ctx, time.Now())
				instances, err := s.store.ListInstances(ctx, &store.FindInstanceMessage{})
				if err != nil {
					// A transient store error must not stop the checker for the
					// lifetime of the process; skip this tick and retry.
					slog.Error("Failed to list instance", log.WithError(err))
					continue
				}
				instanceMap := make(map[string]*store.InstanceMessage)
				for _, instance := range instances {
					instanceMap[instance.ResourceID] = instance
				}
				now := time.Now()
				dbwp := pool.New().WithMaxGoroutines(MaximumOutstanding)
				s.databaseSyncMap.Range(func(key, value any) bool {
					database, ok := value.(*store.DatabaseMessage)
					if !ok {
						return true
					}
					if _, ok := instanceMap[database.InstanceID]; !ok {
						slog.Debug("Instance not found",
							slog.String("instance", database.InstanceID))
						// The instance is gone (or not visible any more); drop the
						// entry instead of retrying and logging it every tick
						// forever. An operation waiting for this database is told:
						// no further attempt will ever report for it.
						s.databaseSyncRetryMap.Delete(key)
						s.databaseSyncMap.Delete(key)
						s.completeDatabase(ctx, database, errors.Errorf("instance %q is no longer there", database.InstanceID))
						return true
					}

					// A failed database stays queued but is skipped until its
					// backoff elapses, so a tick does not retry it immediately.
					if !s.retryDue(key, now) {
						return true
					}

					s.databaseSyncMap.Delete(key)
					dbwp.Go(func() {
						defer func() {
							// A panic inside one database sync must not propagate
							// out of Wait() and take the checker goroutine (or the
							// process) down with it.
							if r := recover(); r != nil {
								err, ok := r.(error)
								if !ok {
									err = errors.Errorf("%v", r)
								}
								slog.Error("Database schema sync PANIC RECOVER",
									slog.String("instance", database.InstanceID),
									slog.String("database", database.DatabaseName),
									log.WithError(err))
								s.completeDatabase(ctx, database, err)
								if s.scheduleRetry(database) {
									s.metrics.CountSchemaSyncRetriesExhausted(instanceEngine(instanceMap[database.InstanceID]))
								}
							}
						}()
						slog.Debug("Sync database schema", slog.String("instance", database.InstanceID), slog.String("database", database.DatabaseName))
						if err := s.SyncDatabaseSchema(ctx, database); err != nil {
							if errors.Is(err, errInstanceConnectionsExhausted) {
								// The per-instance limiter is saturated; keep the
								// database queued and retry on a later tick. This is
								// contention, not a failed sync, so it does not
								// advance the backoff.
								s.metrics.CountConnectionLimitDeferral(metrics.DeferralDatabase)
								s.databaseSyncMap.Store(database.String(), database)
								return
							}
							if errors.Is(err, ErrDatabaseHidden) {
								// The target no longer has the row and the sync hid it.
								// There is nothing to retry and nothing an
								// administrator could fix; the operation waiting for
								// this database hears the outcome.
								s.completeDatabase(ctx, database, err)
								return
							}
							slog.Warn("Failed to sync database schema",
								slog.String("instance", database.InstanceID),
								slog.String("databaseName", database.DatabaseName),
								log.WithError(err))
							// The operation reports this first result; the retries below
							// are the checker's own recovery, and only their exhaustion is
							// worth interrupting an administrator for.
							s.completeDatabase(ctx, database, err)
							if s.scheduleRetry(database) {
								s.metrics.CountSchemaSyncRetriesExhausted(instanceEngine(instanceMap[database.InstanceID]))
								s.notifyBackgroundDatabaseFailure(ctx, instanceMap[database.InstanceID], database, err)
							}
							return
						}
						s.databaseSyncRetryMap.Delete(database.String())
						s.completeDatabase(ctx, database, nil)
					})
					return true
				})
				dbwp.Wait()
			case <-ctx.Done(): // if cancel() execute
				return
			}
		}
	})
	sp.Wait()
}
