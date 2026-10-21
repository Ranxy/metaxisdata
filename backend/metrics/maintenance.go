package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type maintenanceMetrics struct {
	passes       *prometheus.CounterVec
	passDuration prometheus.Histogram
	deletedRows  *prometheus.CounterVec
	itemFailures *prometheus.CounterVec
}

func newMaintenanceMetrics() maintenanceMetrics {
	return maintenanceMetrics{
		passes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "maintenance",
			Name:      "passes_total",
			Help:      "Maintenance passes completed, by outcome.",
		}, []string{"outcome"}),
		passDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "maintenance",
			Name:      "pass_duration_seconds",
			Help:      "Maintenance pass duration in seconds.",
			Buckets:   []float64{1, 10, 60, 300, 900, 1800, 3600},
		}),
		deletedRows: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "maintenance",
			Name:      "deleted_rows_total",
			Help:      "Rows removed by a maintenance pass, by retention item.",
		}, []string{"item"}),
		itemFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "maintenance",
			Name:      "item_failures_total",
			Help:      "Maintenance items that returned an error, by item.",
		}, []string{"item"}),
	}
}

func (m maintenanceMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.passes, m.passDuration, m.deletedRows, m.itemFailures}
}

// ObserveMaintenancePass records one completed pass, the rows each item removed
// and the items that failed. deletedRows must not include the lineage items,
// which count edges rather than rows.
func (m *Metrics) ObserveMaintenancePass(ok bool, duration time.Duration, deletedRows map[string]int64, failedItems []string) {
	if m == nil {
		return
	}
	passOutcome := "partial"
	if ok {
		passOutcome = "ok"
	}
	m.Maintenance.passes.WithLabelValues(passOutcome).Inc()
	m.Maintenance.passDuration.Observe(duration.Seconds())
	for item, rows := range deletedRows {
		if rows <= 0 {
			continue
		}
		m.Maintenance.deletedRows.WithLabelValues(item).Add(float64(rows))
	}
	for _, item := range failedItems {
		m.Maintenance.itemFailures.WithLabelValues(item).Inc()
	}
}
