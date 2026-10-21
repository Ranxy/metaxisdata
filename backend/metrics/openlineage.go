package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// OpenLineage ingestion routes.
const (
	IngestRouteEvent = "event"
	IngestRouteBatch = "batch"
)

// OpenLineage ingestion event outcomes.
const (
	IngestResultAccepted = "accepted"
	IngestResultRejected = "rejected"
)

// OpenLineage ingestion rejection reasons.
const (
	IngestReasonParse   = "parse"
	IngestReasonLimits  = "limits"
	IngestReasonScope   = "scope"
	IngestReasonPersist = "persist"
	IngestReasonProcess = "process"
)

type openLineageMetrics struct {
	ingestFailures        *prometheus.CounterVec
	ingestEvents          *prometheus.CounterVec
	ingestProcessDuration *prometheus.HistogramVec
	edges                 *prometheus.CounterVec
}

func newOpenLineageMetrics() openLineageMetrics {
	return openLineageMetrics{
		ingestFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "openlineage",
			Name:      "ingest_failures_total",
			Help:      "OpenLineage ingestion requests refused or failed, by failure kind and route.",
		}, []string{"failure_kind", "route"}),
		ingestEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "openlineage",
			Name:      "ingest_events_total",
			Help:      "OpenLineage events accepted or rejected, by route, result and reason.",
		}, []string{"route", "result", "reason"}),
		ingestProcessDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "openlineage",
			Name:      "ingest_process_duration_seconds",
			Help:      "Lineage derivation time per ingested event, in seconds, by route.",
			Buckets:   []float64{0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"route"}),
		edges: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "openlineage",
			Name:      "edges_total",
			Help:      "Lineage edges committed from ingested runs, by kind.",
		}, []string{"kind"}),
	}
}

func (m openLineageMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.ingestFailures,
		m.ingestEvents,
		m.ingestProcessDuration,
		m.edges,
	}
}

// CountOpenLineageIngestFailure records one refused or failed ingestion request.
func (m *Metrics) CountOpenLineageIngestFailure(failureKind, route string) {
	if m == nil {
		return
	}
	m.OpenLineage.ingestFailures.WithLabelValues(failureKind, route).Inc()
}

// AddOpenLineageIngestEvents records event-level ingestion outcomes. count is
// the number of events the outcome covers.
func (m *Metrics) AddOpenLineageIngestEvents(route, result, reason string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.OpenLineage.ingestEvents.WithLabelValues(route, result, reason).Add(float64(count))
}

// ObserveOpenLineageProcessDuration records lineage derivation time for one
// ingested event.
func (m *Metrics) ObserveOpenLineageProcessDuration(route string, duration time.Duration) {
	if m == nil {
		return
	}
	m.OpenLineage.ingestProcessDuration.WithLabelValues(route).Observe(duration.Seconds())
}

// AddOpenLineageEdges records lineage edges committed from one ingested run.
func (m *Metrics) AddOpenLineageEdges(kind string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.OpenLineage.edges.WithLabelValues(kind).Add(float64(count))
}
