package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Lineage analysis outcomes.
const (
	AnalysisStored                = "stored"
	AnalysisEngineUnsupported     = "engine_unsupported"
	AnalysisDefinitionUnavailable = "definition_unavailable"
	AnalysisDefinitionCleared     = "definition_cleared"
	AnalysisFailed                = "analysis_failed"
	AnalysisInterrupted           = "interrupted"
	AnalysisStoreError            = "store_error"
	AnalysisObjectGone            = "gone"
	AnalysisStale                 = "stale"
	AnalysisTimeout               = "timeout"
	AnalysisPanic                 = "panic"
)

// Lineage edge kinds.
const (
	EdgeKindColumn = "column"
	EdgeKindTable  = "table"
)

type lineageMetrics struct {
	analysisObjects  *prometheus.CounterVec
	analyzeDuration  *prometheus.HistogramVec
	analysisInFlight prometheus.Gauge
	retriesExhausted *prometheus.CounterVec
	edgesWritten     *prometheus.CounterVec
}

func newLineageMetrics() lineageMetrics {
	return lineageMetrics{
		analysisObjects: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "lineage",
			Name:      "analysis_objects_total",
			Help:      "Objects whose lineage analysis reached a terminal outcome, by object type, engine and result.",
		}, []string{"meta_type", "engine", "result"}),
		analyzeDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "lineage",
			Name:      "analyze_duration_seconds",
			Help:      "SQL analysis duration in seconds, by engine and object type.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120},
		}, []string{"engine", "meta_type"}),
		analysisInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "lineage",
			Name:      "analysis_in_flight",
			Help:      "Lineage analyses running now.",
		}),
		retriesExhausted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "lineage",
			Name:      "analysis_retries_exhausted_total",
			Help:      "Objects whose analysis retry budget ran out and now wait for the next full scan, by object type.",
		}, []string{"meta_type"}),
		edgesWritten: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "lineage",
			Name:      "edges_written_total",
			Help:      "Lineage edges submitted to the edge-set replace, by object type and kind.",
		}, []string{"meta_type", "kind"}),
	}
}

func (m lineageMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.analysisObjects,
		m.analyzeDuration,
		m.analysisInFlight,
		m.retriesExhausted,
		m.edgesWritten,
	}
}

// ObserveLineageAnalysis records one terminal outcome of one object's analysis.
func (m *Metrics) ObserveLineageAnalysis(metaType storepb.MetaType, engine storepb.Engine, result string) {
	if m == nil {
		return
	}
	m.Lineage.analysisObjects.WithLabelValues(analysisMetaTypeLabel(metaType), engineLabel(engine), result).Inc()
}

// ObserveLineageAnalyzeDuration records the time one SQL analysis took.
func (m *Metrics) ObserveLineageAnalyzeDuration(engine storepb.Engine, metaType storepb.MetaType, duration time.Duration) {
	if m == nil {
		return
	}
	m.Lineage.analyzeDuration.WithLabelValues(engineLabel(engine), analysisMetaTypeLabel(metaType)).Observe(duration.Seconds())
}

// IncLineageAnalysisInFlight records that one analysis worker started.
func (m *Metrics) IncLineageAnalysisInFlight() {
	if m == nil {
		return
	}
	m.Lineage.analysisInFlight.Inc()
}

// DecLineageAnalysisInFlight records that one analysis worker finished.
func (m *Metrics) DecLineageAnalysisInFlight() {
	if m == nil {
		return
	}
	m.Lineage.analysisInFlight.Dec()
}

// CountLineageAnalysisRetriesExhausted records an object that exhausted its
// analysis retries and now waits for the next full scan.
func (m *Metrics) CountLineageAnalysisRetriesExhausted(metaType storepb.MetaType) {
	if m == nil {
		return
	}
	m.Lineage.retriesExhausted.WithLabelValues(analysisMetaTypeLabel(metaType)).Inc()
}

// AddLineageEdges records edges submitted to the edge-set replace for one
// object. It is write volume, not the resulting graph size.
func (m *Metrics) AddLineageEdges(metaType storepb.MetaType, kind string, count int) {
	if m == nil || count <= 0 {
		return
	}
	m.Lineage.edgesWritten.WithLabelValues(analysisMetaTypeLabel(metaType), kind).Add(float64(count))
}

// NewLineageAnalysisQueueDepth reports the analyzer's two queues at scrape time.
func NewLineageAnalysisQueueDepth(stats func() (eventQueue, scanQueue int)) prometheus.Collector {
	return newFuncGauge("lineage", "analysis_queue_depth",
		"Objects waiting for lineage analysis, by queue.", []string{"queue"},
		func(emit func(labelValues []string, value float64)) {
			eventQueue, scanQueue := stats()
			emit([]string{"event"}, float64(eventQueue))
			emit([]string{"scan"}, float64(scanQueue))
		})
}
