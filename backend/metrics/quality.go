package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Data-quality execution triggers.
const (
	QualityTriggerScheduled = "scheduled"
	QualityTriggerManual    = "manual"
)

// Data-quality execution admission results.
const (
	QualityAdmissionAdmitted = "admitted"
	QualityAdmissionDeferred = "deferred"
)

// Data-quality execution deferral reasons.
const (
	QualityDeferralAdmissionTimeout  = "admission_timeout"
	QualityDeferralInstanceReadError = "instance_read_error"
)

// Data-quality execution abandonment reasons.
const (
	QualityAbandonedAdmissionTimeout  = "admission_timeout"
	QualityAbandonedInstanceReadError = "instance_read_error"
	QualityAbandonedStaleSweep        = "stale_sweep"
	QualityAbandonedPersistFailed     = "persist_failed"
	QualityAbandonedIncomplete        = "incomplete"
	QualityAbandonedPlanFailed        = "plan_failed"
	QualityAbandonedQueueFull         = "queue_full"
	QualityAbandonedMarkRunningFailed = "mark_running_failed"
)

type qualityMetrics struct {
	dueScanDuration       prometheus.Histogram
	dueRules              prometheus.Histogram
	executionsCreated     *prometheus.CounterVec
	executionsCompleted   *prometheus.CounterVec
	executionAdmissions   *prometheus.CounterVec
	executionsDeferred    *prometheus.CounterVec
	executionAbandoned    *prometheus.CounterVec
	executionsInProgress  prometheus.Gauge
	manualEnqueueRejected prometheus.Counter
	ruleExecutions        *prometheus.CounterVec
	ruleExecutionDuration *prometheus.HistogramVec
	ruleExecutionErrors   *prometheus.CounterVec
}

func newQualityMetrics() qualityMetrics {
	return qualityMetrics{
		dueScanDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "due_scan_duration_seconds",
			Help:      "Time spent reading and planning one due-rule scan, in seconds.",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
		}),
		dueRules: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "due_rules",
			Help:      "Due rule rows returned by one scan.",
			Buckets:   []float64{0, 1, 5, 10, 25, 50, 100, 200},
		}),
		executionsCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "executions_created_total",
			Help:      "Data-quality execution rows planned, by trigger.",
		}, []string{"trigger"}),
		executionsCompleted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "executions_completed_total",
			Help:      "Data-quality executions that reached a verdict, by outcome.",
		}, []string{"outcome"}),
		executionAdmissions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "execution_admissions_total",
			Help:      "Connection-budget admission decisions for data-quality batches, by trigger and result.",
		}, []string{"trigger", "result"}),
		executionsDeferred: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "executions_deferred_total",
			Help:      "Scheduled data-quality executions not planned because admission was not possible, by reason.",
		}, []string{"reason"}),
		executionAbandoned: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "executions_abandoned_total",
			Help:      "Data-quality executions abandoned without a verdict, by reason.",
		}, []string{"reason"}),
		executionsInProgress: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "executions_in_progress",
			Help:      "Data-quality executions executing now.",
		}),
		manualEnqueueRejected: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "manual_enqueue_rejected_total",
			Help:      "Manual data-quality batches refused because the queue was full.",
		}),
		ruleExecutions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "rule_executions_total",
			Help:      "Data-quality rule executions, by rule type and status.",
		}, []string{"rule_type", "status"}),
		ruleExecutionDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "rule_execution_duration_seconds",
			Help:      "Data-quality rule execution duration in seconds, by rule type.",
			Buckets: []float64{
				0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600,
			},
		}, []string{"rule_type"}),
		ruleExecutionErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "rule_errors_total",
			Help:      "Data-quality rule executions that ended in an error, by rule type and error code.",
		}, []string{"rule_type", "error_code"}),
	}
}

func (m qualityMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.dueScanDuration,
		m.dueRules,
		m.executionsCreated,
		m.executionsCompleted,
		m.executionAdmissions,
		m.executionsDeferred,
		m.executionAbandoned,
		m.executionsInProgress,
		m.manualEnqueueRejected,
		m.ruleExecutions,
		m.ruleExecutionDuration,
		m.ruleExecutionErrors,
	}
}

// ObserveQualityDueScan records one due-rule scan's planning cost.
func (m *Metrics) ObserveQualityDueScan(duration time.Duration) {
	if m == nil {
		return
	}
	m.Quality.dueScanDuration.Observe(duration.Seconds())
}

// ObserveQualityDueRules records how many rules one scan found due.
func (m *Metrics) ObserveQualityDueRules(count int) {
	if m == nil {
		return
	}
	m.Quality.dueRules.Observe(float64(count))
}

// CountQualityExecutionCreated records execution rows planned by either trigger.
func (m *Metrics) CountQualityExecutionCreated(trigger string) {
	if m == nil {
		return
	}
	m.Quality.executionsCreated.WithLabelValues(trigger).Inc()
}

// CountQualityExecutionCompleted records an execution that reached a verdict.
func (m *Metrics) CountQualityExecutionCompleted(outcome storepb.QualityStatus) {
	if m == nil {
		return
	}
	m.Quality.executionsCompleted.WithLabelValues(qualityStatusLabel(outcome)).Inc()
}

// CountQualityExecutionAdmission records one connection-budget admission decision
// for a batch of executions.
func (m *Metrics) CountQualityExecutionAdmission(trigger string, admitted bool) {
	if m == nil {
		return
	}
	result := QualityAdmissionDeferred
	if admitted {
		result = QualityAdmissionAdmitted
	}
	m.Quality.executionAdmissions.WithLabelValues(trigger, result).Inc()
}

// CountQualityExecutionsDeferred records scheduled executions that were not
// planned because their instance could not be admitted.
func (m *Metrics) CountQualityExecutionsDeferred(reason string) {
	if m == nil {
		return
	}
	m.Quality.executionsDeferred.WithLabelValues(reason).Inc()
}

// CountQualityExecutionAbandoned records an execution abandoned without a verdict.
func (m *Metrics) CountQualityExecutionAbandoned(reason string) {
	if m == nil {
		return
	}
	m.Quality.executionAbandoned.WithLabelValues(reason).Inc()
}

// IncQualityExecutionsInProgress records that an execution started running.
func (m *Metrics) IncQualityExecutionsInProgress() {
	if m == nil {
		return
	}
	m.Quality.executionsInProgress.Inc()
}

// DecQualityExecutionsInProgress records that an execution stopped running.
func (m *Metrics) DecQualityExecutionsInProgress() {
	if m == nil {
		return
	}
	m.Quality.executionsInProgress.Dec()
}

// CountQualityManualEnqueueRejected records a manual batch refused by a full queue.
func (m *Metrics) CountQualityManualEnqueueRejected() {
	if m == nil {
		return
	}
	m.Quality.manualEnqueueRejected.Inc()
}

// ObserveQualityRuleExecution records one rule attempt's status and duration.
func (m *Metrics) ObserveQualityRuleExecution(ruleType storepb.QualityRuleType, status storepb.QualityStatus, duration time.Duration) {
	if m == nil {
		return
	}
	ruleTypeValue := qualityRuleTypeLabel(ruleType)
	m.Quality.ruleExecutions.WithLabelValues(ruleTypeValue, qualityStatusLabel(status)).Inc()
	m.Quality.ruleExecutionDuration.WithLabelValues(ruleTypeValue).Observe(duration.Seconds())
}

// CountQualityRuleError records a rule execution outcome that carries a stable
// error code.
func (m *Metrics) CountQualityRuleError(ruleType storepb.QualityRuleType, errorCode string) {
	if m == nil || errorCode == "" {
		return
	}
	m.Quality.ruleExecutionErrors.WithLabelValues(qualityRuleTypeLabel(ruleType), qualityErrorCodeLabel(errorCode)).Inc()
}

// NewQualityManualQueueDepth reports the manual batch queue depth at scrape time.
func NewQualityManualQueueDepth(depth func() int) prometheus.Collector {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "quality",
		Name:      "manual_queue_depth",
		Help:      "Manual data-quality batches waiting for the runner.",
	}, func() float64 { return float64(depth()) })
}
