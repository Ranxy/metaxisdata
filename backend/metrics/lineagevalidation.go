package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Revalidation trigger results.
const (
	ValidationTriggerQueued    = "queued"
	ValidationTriggerCoalesced = "coalesced"
)

// Revalidation pass outcomes.
const (
	ValidationPassOK               = "ok"
	ValidationPassError            = "error"
	ValidationPassBacklogRemaining = "backlog_remaining"
)

// Revalidation edge results.
const (
	ValidationEdgeRevalidated  = "revalidated"
	ValidationEdgeStillUnknown = "still_unknown"
)

type lineageValidationMetrics struct {
	triggers     *prometheus.CounterVec
	passes       *prometheus.CounterVec
	passDuration prometheus.Histogram
	edges        *prometheus.CounterVec
}

func newLineageValidationMetrics() lineageValidationMetrics {
	return lineageValidationMetrics{
		triggers: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "lineagevalidation",
			Name:      "triggers_total",
			Help:      "Revalidation triggers, by result.",
		}, []string{"result"}),
		passes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "lineagevalidation",
			Name:      "passes_total",
			Help:      "Revalidation passes completed, by outcome.",
		}, []string{"outcome"}),
		passDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "lineagevalidation",
			Name:      "pass_duration_seconds",
			Help:      "Revalidation pass duration in seconds.",
			Buckets:   []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300},
		}),
		edges: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "lineagevalidation",
			Name:      "edges_total",
			Help:      "Edges rewritten or left unknown by revalidation passes, by result.",
		}, []string{"result"}),
	}
}

func (m lineageValidationMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.triggers, m.passes, m.passDuration, m.edges}
}

// CountLineageValidationTrigger records one trigger signal.
func (m *Metrics) CountLineageValidationTrigger(result string) {
	if m == nil {
		return
	}
	m.LineageValidation.triggers.WithLabelValues(result).Inc()
}

// ObserveLineageValidationPass records one pass's outcome, duration and edge
// counts.
func (m *Metrics) ObserveLineageValidationPass(outcome string, duration time.Duration, revalidated, stillUnknown int) {
	if m == nil {
		return
	}
	m.LineageValidation.passes.WithLabelValues(outcome).Inc()
	m.LineageValidation.passDuration.Observe(duration.Seconds())
	if revalidated > 0 {
		m.LineageValidation.edges.WithLabelValues(ValidationEdgeRevalidated).Add(float64(revalidated))
	}
	if stillUnknown > 0 {
		m.LineageValidation.edges.WithLabelValues(ValidationEdgeStillUnknown).Add(float64(stillUnknown))
	}
}
