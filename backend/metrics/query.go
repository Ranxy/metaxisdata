package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Read-only target-query purposes. They mirror the call sites in the data
// quality executor, the profiler and the rule preview.
const (
	QueryPurposeRuleAggregate    = "rule_aggregate"
	QueryPurposeRuleSample       = "rule_sample"
	QueryPurposeRuleCELSample    = "rule_cel_sample"
	QueryPurposeProfileAggregate = "profile_aggregate"
	QueryPurposeProfileSample    = "profile_sample"
	QueryPurposeDryRunProbe      = "dry_run_probe"
)

type queryMetrics struct {
	readOnlyTotal    *prometheus.CounterVec
	readOnlyDuration *prometheus.HistogramVec
}

func newQueryMetrics() queryMetrics {
	return queryMetrics{
		readOnlyTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "query",
			Name:      "readonly_total",
			Help:      "Read-only statements run against a target database, by purpose and outcome.",
		}, []string{"purpose", "outcome"}),
		readOnlyDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "query",
			Name:      "readonly_duration_seconds",
			Help:      "Read-only target query duration in seconds, by purpose.",
			Buckets: []float64{
				0.001, 0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120,
			},
		}, []string{"purpose"}),
	}
}

func (m queryMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.readOnlyTotal, m.readOnlyDuration}
}

// ObserveReadOnlyQuery records one read-only target query.
func (m *Metrics) ObserveReadOnlyQuery(purpose string, ok bool, duration time.Duration) {
	if m == nil {
		return
	}
	m.Query.readOnlyTotal.WithLabelValues(purpose, outcomeLabel(ok)).Inc()
	m.Query.readOnlyDuration.WithLabelValues(purpose).Observe(duration.Seconds())
}
