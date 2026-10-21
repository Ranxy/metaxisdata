package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// httpMetrics instruments the Echo server. Labels use the registered route
// template, never the raw request path, and drop the caller-controlled Host
// header the previous middleware exposed.
type httpMetrics struct {
	requests        *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

func newHTTPMetrics() httpMetrics {
	return httpMetrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "HTTP requests processed, by method, route template and response status.",
		}, []string{"method", "route", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request latency in seconds, by method and route template.",
			Buckets: []float64{
				0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300, 1800, 3600,
			},
		}, []string{"method", "route"}),
	}
}

func (m httpMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.requests, m.requestDuration}
}

// ObserveHTTPRequest records one completed HTTP request. route is the Echo route
// template (or the "unmatched"/"spa" placeholder), and status the response
// status.
func (m *Metrics) ObserveHTTPRequest(method, route string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	m.HTTP.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
	m.HTTP.requestDuration.WithLabelValues(method, route).Observe(duration.Seconds())
}
