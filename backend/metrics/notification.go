package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Notification delivery outcomes, as the dispatcher classifies one attempt.
const (
	NotificationDeliverySent     = "sent"
	NotificationDeliveryFailed   = "failed"
	NotificationDeliveryDead     = "dead"
	NotificationDeliveryDeferred = "deferred"
)

type notificationMetrics struct {
	deliveriesAttempted *prometheus.CounterVec
	deliveryDuration    prometheus.Histogram
}

func newNotificationMetrics() notificationMetrics {
	return notificationMetrics{
		deliveriesAttempted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "notification",
			Name:      "deliveries_attempted_total",
			Help:      "Outbound notification delivery attempts by outcome.",
		}, []string{"outcome"}),
		deliveryDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "notification",
			Name:      "delivery_duration_seconds",
			Help:      "Time one outbound notification delivery attempt took, in seconds.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}),
	}
}

func (m notificationMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.deliveriesAttempted,
		m.deliveryDuration,
	}
}

// CountNotificationDelivery records one finished delivery attempt.
func (m *Metrics) CountNotificationDelivery(outcome string) {
	if m == nil {
		return
	}
	m.Notification.deliveriesAttempted.WithLabelValues(outcome).Inc()
}

// ObserveNotificationDeliveryDuration records how long one attempt took.
func (m *Metrics) ObserveNotificationDeliveryDuration(duration time.Duration) {
	if m == nil {
		return
	}
	m.Notification.deliveryDuration.Observe(duration.Seconds())
}
