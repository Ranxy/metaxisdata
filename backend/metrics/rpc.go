package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// rpcMetrics instruments the ConnectRPC interceptor chain.
type rpcMetrics struct {
	requests        *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

func newRPCMetrics() rpcMetrics {
	return rpcMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "rpc",
			Name:      "requests_total",
			Help:      "ConnectRPC calls completed, by service, method and Connect code.",
		}, []string{"service", "method", "code"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "rpc",
			Name:      "request_duration_seconds",
			Help:      "Unary ConnectRPC latency in seconds, by service and method.",
			// The long unary methods are bounded at twenty minutes, so the buckets run
			// to 1200 seconds instead of stopping where the default ceiling does.
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1200},
		}, []string{"service", "method"}),
	}
}

func (m rpcMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.requests, m.requestDuration}
}

// ObserveRPCUnary records one completed unary call. code is the client-visible
// Connect code ("ok" on success).
func (m *Metrics) ObserveRPCUnary(service, method, code string, duration time.Duration) {
	if m == nil {
		return
	}
	m.RPC.requests.WithLabelValues(service, method, code).Inc()
	m.RPC.requestDuration.WithLabelValues(service, method).Observe(duration.Seconds())
}

// ObserveRPCStream records one finished server-streaming call. It counts the
// call but does not observe a duration: a notification subscription is
// long-lived by design, and its duration is not a latency signal.
func (m *Metrics) ObserveRPCStream(service, method, code string) {
	if m == nil {
		return
	}
	m.RPC.requests.WithLabelValues(service, method, code).Inc()
}
