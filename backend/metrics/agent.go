package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// agentMetrics instruments the tool executions of the built-in agent products.
// Labels stay bounded: the tool names come from the static spec whitelist and the
// product is the spec name.
type agentMetrics struct {
	calls     *prometheus.CounterVec
	duration  *prometheus.HistogramVec
	resultLen *prometheus.HistogramVec
}

func newAgentMetrics() agentMetrics {
	return agentMetrics{
		calls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "agent",
			Name:      "tool_calls_total",
			Help:      "Tool executions by product, tool and outcome.",
		}, []string{"product", "tool", "outcome"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "agent",
			Name:      "tool_call_duration_seconds",
			Help:      "Tool execution duration in seconds, by product and tool.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"product", "tool"}),
		resultLen: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "agent",
			Name:      "tool_result_bytes",
			Help:      "Model-visible tool result size in bytes, by product and tool.",
			Buckets:   []float64{64, 256, 1024, 4096, 8192, 16384, 65536},
		}, []string{"product", "tool"}),
	}
}

func (m agentMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.calls, m.duration, m.resultLen}
}

// ObserveAgentToolCall records one tool execution. A nil Metrics records nothing.
func (m *Metrics) ObserveAgentToolCall(product, tool, outcome string, duration time.Duration, resultBytes int) {
	if m == nil {
		return
	}
	m.Agent.calls.WithLabelValues(product, tool, outcome).Inc()
	m.Agent.duration.WithLabelValues(product, tool).Observe(duration.Seconds())
	m.Agent.resultLen.WithLabelValues(product, tool).Observe(float64(resultBytes))
}
