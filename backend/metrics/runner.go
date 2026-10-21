package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// runnerMetrics instruments the background runners' lifecycle. A runner that
// panics is logged and not restarted, so "up" is the only in-process signal that
// one of them died.
type runnerMetrics struct {
	up     *prometheus.GaugeVec
	panics *prometheus.CounterVec
}

func newRunnerMetrics() runnerMetrics {
	return runnerMetrics{
		up: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "runner",
			Name:      "up",
			Help:      "1 while the background runner is running, 0 after it stopped or panicked.",
		}, []string{"runner"}),
		panics: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "runner",
			Name:      "panics_total",
			Help:      "Background runner panics caught by the shared panic boundary.",
		}, []string{"runner"}),
	}
}

func (m runnerMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.up, m.panics}
}

// RunnerStarted marks a runner as running.
func (m *Metrics) RunnerStarted(name string) {
	if m == nil {
		return
	}
	m.Runner.up.WithLabelValues(name).Set(1)
}

// RunnerStopped marks a runner as no longer running.
func (m *Metrics) RunnerStopped(name string) {
	if m == nil {
		return
	}
	m.Runner.up.WithLabelValues(name).Set(0)
}

// RunnerPanicked records a panic caught by the shared runner boundary.
func (m *Metrics) RunnerPanicked(name string) {
	if m == nil {
		return
	}
	m.Runner.panics.WithLabelValues(name).Inc()
}
