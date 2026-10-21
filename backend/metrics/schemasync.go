package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Schema-sync stages.
const (
	SyncStageInstance = "instance"
	SyncStageDatabase = "database"
)

// Schema-sync failure reasons. They mirror what the sync paths can prove today;
// a driver error carries no finer classification, so none is invented here.
const (
	FailureReasonConnectionLimit = "connection_limit"
	FailureReasonHidden          = "hidden"
	FailureReasonNotFound        = "not_found"
	FailureReasonTimeout         = "timeout"
	FailureReasonCanceled        = "canceled"
	FailureReasonOther           = "other"
)

// Schema-sync driver calls.
const (
	DriverCallSyncInstance = "sync_instance"
	DriverCallSyncDBSchema = "sync_db_schema"
)

// Schema-sync connection-budget deferral kinds.
const (
	DeferralDatabase = "database"
	DeferralInstance = "instance"
)

type schemasyncMetrics struct {
	scans                *prometheus.CounterVec
	scanDuration         prometheus.Histogram
	instanceSyncs        *prometheus.CounterVec
	instanceSyncDuration *prometheus.HistogramVec
	databaseSyncs        *prometheus.CounterVec
	databaseSyncDuration *prometheus.HistogramVec
	failures             *prometheus.CounterVec
	driverCallDuration   *prometheus.HistogramVec
	metaResourcesWritten *prometheus.CounterVec
	metaResourcesDeleted *prometheus.CounterVec
	retries              *prometheus.CounterVec
	retriesExhausted     *prometheus.CounterVec
	connectionDeferrals  *prometheus.CounterVec
}

func newSchemasyncMetrics() schemasyncMetrics {
	syncBuckets := []float64{1, 5, 15, 30, 60, 120, 300, 600, 900}
	return schemasyncMetrics{
		scans: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "scans_total",
			Help:      "Instance scans completed, by outcome.",
		}, []string{"outcome"}),
		scanDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "scan_duration_seconds",
			Help:      "Instance scan duration in seconds.",
			Buckets:   syncBuckets,
		}),
		instanceSyncs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "instance_syncs_total",
			Help:      "Instance enumeration syncs completed, by engine and outcome.",
		}, []string{"engine", "outcome"}),
		instanceSyncDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "instance_sync_duration_seconds",
			Help:      "Instance enumeration sync duration in seconds, by engine.",
			Buckets:   syncBuckets,
		}, []string{"engine"}),
		databaseSyncs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "database_syncs_total",
			Help:      "Database schema syncs completed, by engine and outcome.",
		}, []string{"engine", "outcome"}),
		databaseSyncDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "database_sync_duration_seconds",
			Help:      "Database schema sync duration in seconds, by engine.",
			Buckets:   syncBuckets,
		}, []string{"engine"}),
		failures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "failures_total",
			Help:      "Schema sync failures, by stage and reason.",
		}, []string{"stage", "reason"}),
		driverCallDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "driver_call_duration_seconds",
			Help:      "Source database driver call duration in seconds, by engine and call.",
			Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 900},
		}, []string{"engine", "call"}),
		metaResourcesWritten: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "meta_resources_written_total",
			Help:      "Metadata resources created or updated by committed syncs, by object type.",
		}, []string{"object_type"}),
		metaResourcesDeleted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "meta_resources_deleted_total",
			Help:      "Metadata resources deleted by committed syncs, by object type.",
		}, []string{"object_type"}),
		retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "retries_total",
			Help:      "Database syncs re-queued with backoff, by attempt number.",
		}, []string{"attempt"}),
		retriesExhausted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "retries_exhausted_total",
			Help:      "Databases whose retry budget ran out, by engine.",
		}, []string{"engine"}),
		connectionDeferrals: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "schemasync",
			Name:      "connection_limit_deferrals_total",
			Help:      "Sync attempts skipped because the instance connection budget was saturated, by kind.",
		}, []string{"kind"}),
	}
}

func (m schemasyncMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.scans,
		m.scanDuration,
		m.instanceSyncs,
		m.instanceSyncDuration,
		m.databaseSyncs,
		m.databaseSyncDuration,
		m.failures,
		m.driverCallDuration,
		m.metaResourcesWritten,
		m.metaResourcesDeleted,
		m.retries,
		m.retriesExhausted,
		m.connectionDeferrals,
	}
}

// ObserveSchemaSyncScan records one instance scan.
func (m *Metrics) ObserveSchemaSyncScan(ok bool, duration time.Duration) {
	if m == nil {
		return
	}
	m.Schemasync.scans.WithLabelValues(outcomeLabel(ok)).Inc()
	m.Schemasync.scanDuration.Observe(duration.Seconds())
}

// ObserveInstanceSync records one real instance enumeration attempt. It must be
// recorded by the owner of the per-instance gate only: a waiter reuses the
// owner's result and is not a second sync.
func (m *Metrics) ObserveInstanceSync(engine storepb.Engine, ok bool, duration time.Duration) {
	if m == nil {
		return
	}
	engineValue := engineLabel(engine)
	m.Schemasync.instanceSyncs.WithLabelValues(engineValue, outcomeLabel(ok)).Inc()
	m.Schemasync.instanceSyncDuration.WithLabelValues(engineValue).Observe(duration.Seconds())
}

// ObserveDatabaseSync records one real database schema sync. It must be recorded
// by the owner of the per-resource gate only.
func (m *Metrics) ObserveDatabaseSync(engine storepb.Engine, ok bool, duration time.Duration) {
	if m == nil {
		return
	}
	engineValue := engineLabel(engine)
	m.Schemasync.databaseSyncs.WithLabelValues(engineValue, outcomeLabel(ok)).Inc()
	m.Schemasync.databaseSyncDuration.WithLabelValues(engineValue).Observe(duration.Seconds())
}

// CountSchemaSyncFailure records one failed schema sync attempt.
func (m *Metrics) CountSchemaSyncFailure(stage, reason string) {
	if m == nil {
		return
	}
	m.Schemasync.failures.WithLabelValues(stage, reason).Inc()
}

// ObserveDriverCall records one call to a source database driver.
func (m *Metrics) ObserveDriverCall(engine storepb.Engine, call string, duration time.Duration) {
	if m == nil {
		return
	}
	m.Schemasync.driverCallDuration.WithLabelValues(engineLabel(engine), call).Observe(duration.Seconds())
}

// AddMetaResourcesWritten records metadata rows created or updated by a
// committed sync. It must be called after the transaction commits.
func (m *Metrics) AddMetaResourcesWritten(metaType storepb.MetaType, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.Schemasync.metaResourcesWritten.WithLabelValues(objectTypeLabel(metaType)).Add(float64(count))
}

// AddMetaResourcesDeleted records metadata rows deleted by a committed sync.
func (m *Metrics) AddMetaResourcesDeleted(metaType storepb.MetaType, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.Schemasync.metaResourcesDeleted.WithLabelValues(objectTypeLabel(metaType)).Add(float64(count))
}

// CountSchemaSyncRetry records one database sync re-queued with backoff.
func (m *Metrics) CountSchemaSyncRetry(attempt int) {
	if m == nil {
		return
	}
	m.Schemasync.retries.WithLabelValues(strconv.Itoa(attempt)).Inc()
}

// CountSchemaSyncRetriesExhausted records a database whose retry budget ran out.
func (m *Metrics) CountSchemaSyncRetriesExhausted(engine storepb.Engine) {
	if m == nil {
		return
	}
	m.Schemasync.retriesExhausted.WithLabelValues(engineLabel(engine)).Inc()
}

// CountConnectionLimitDeferral records a sync attempt skipped because the
// instance connection budget was saturated. It is backpressure, not a failure.
func (m *Metrics) CountConnectionLimitDeferral(kind string) {
	if m == nil {
		return
	}
	m.Schemasync.connectionDeferrals.WithLabelValues(kind).Inc()
}
