package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

type instanceConnectionMetrics struct {
	tryAcquireRejected prometheus.Counter
}

func newInstanceConnectionMetrics() instanceConnectionMetrics {
	return instanceConnectionMetrics{
		tryAcquireRejected: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "instance_connection",
			Name:      "try_acquire_rejected_total",
			Help:      "Non-blocking instance connection requests refused because no slot was free.",
		}),
	}
}

func (m instanceConnectionMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.tryAcquireRejected}
}

// CountInstanceConnectionTryAcquireRejected records a non-blocking lease
// request that was refused.
func (m *Metrics) CountInstanceConnectionTryAcquireRejected() {
	if m == nil {
		return
	}
	m.InstanceConnection.tryAcquireRejected.Inc()
}

// NewInstanceConnectionInUse reports the total number of leased instance
// connections at scrape time.
func NewInstanceConnectionInUse(stats func() int) prometheus.Collector {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "instance_connection",
		Name:      "in_use",
		Help:      "Instance connection leases currently held.",
	}, func() float64 { return float64(stats()) })
}

// NewInstanceConnectionWaiters reports the requests waiting for an instance
// connection, by priority class, at scrape time.
func NewInstanceConnectionWaiters(stats func() map[string]int) prometheus.Collector {
	return newFuncGauge("instance_connection", "waiters",
		"Requests waiting for an instance connection budget slot, by priority.", []string{"priority"},
		func(emit func(labelValues []string, value float64)) {
			for priority, waiters := range stats() {
				emit([]string{priority}, float64(waiters))
			}
		})
}
