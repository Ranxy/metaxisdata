package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// Data-quality run triggers.
const (
	QualityTriggerScheduled = "scheduled"
	QualityTriggerManual    = "manual"
)

// Data-quality run admission results.
const (
	QualityAdmissionAdmitted = "admitted"
	QualityAdmissionDeferred = "deferred"
)

// Data-quality run deferral reasons.
const (
	QualityDeferralAdmissionTimeout  = "admission_timeout"
	QualityDeferralInstanceReadError = "instance_read_error"
)

// Data-quality run abandonment reasons.
const (
	QualityAbandonedAdmissionTimeout  = "admission_timeout"
	QualityAbandonedInstanceReadError = "instance_read_error"
	QualityAbandonedStaleSweep        = "stale_sweep"
	QualityAbandonedPersistFailed     = "persist_failed"
	QualityAbandonedQueueFull         = "queue_full"
	QualityAbandonedMarkRunningFailed = "mark_running_failed"
)

type qualityMetrics struct {
	dueScanDuration       prometheus.Histogram
	dueRules              prometheus.Histogram
	runsCreated           *prometheus.CounterVec
	runsCompleted         *prometheus.CounterVec
	runAdmissions         *prometheus.CounterVec
	runsDeferred          *prometheus.CounterVec
	runAbandoned          *prometheus.CounterVec
	runsInProgress        prometheus.Gauge
	manualEnqueueRejected prometheus.Counter
	ruleExecutions        *prometheus.CounterVec
	ruleExecutionDuration *prometheus.HistogramVec
	ruleErrors            *prometheus.CounterVec
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
		runsCreated: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "runs_created_total",
			Help:      "Data-quality run rows created, by trigger.",
		}, []string{"trigger"}),
		runsCompleted: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "runs_completed_total",
			Help:      "Data-quality runs completed, by rollup outcome.",
		}, []string{"outcome"}),
		runAdmissions: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "run_admissions_total",
			Help:      "Connection-budget admission decisions for data-quality runs, by trigger and result.",
		}, []string{"trigger", "result"}),
		runsDeferred: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "runs_deferred_total",
			Help:      "Scheduled data-quality runs not created because admission was not possible, by reason.",
		}, []string{"reason"}),
		runAbandoned: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "run_abandoned_total",
			Help:      "Data-quality runs abandoned without a completed rollup, by reason.",
		}, []string{"reason"}),
		runsInProgress: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "runs_in_progress",
			Help:      "Data-quality runs executing now.",
		}),
		manualEnqueueRejected: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "quality",
			Name:      "manual_enqueue_rejected_total",
			Help:      "Manual data-quality runs refused because the queue was full.",
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
		ruleErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
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
		m.runsCreated,
		m.runsCompleted,
		m.runAdmissions,
		m.runsDeferred,
		m.runAbandoned,
		m.runsInProgress,
		m.manualEnqueueRejected,
		m.ruleExecutions,
		m.ruleExecutionDuration,
		m.ruleErrors,
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

// CountQualityRunCreated records a run row created by either trigger.
func (m *Metrics) CountQualityRunCreated(trigger string) {
	if m == nil {
		return
	}
	m.Quality.runsCreated.WithLabelValues(trigger).Inc()
}

// CountQualityRunCompleted records a run that reached a terminal rollup.
func (m *Metrics) CountQualityRunCompleted(outcome storepb.QualityStatus) {
	if m == nil {
		return
	}
	m.Quality.runsCompleted.WithLabelValues(qualityStatusLabel(outcome)).Inc()
}

// CountQualityRunAdmission records one connection-budget admission decision.
func (m *Metrics) CountQualityRunAdmission(trigger string, admitted bool) {
	if m == nil {
		return
	}
	result := QualityAdmissionDeferred
	if admitted {
		result = QualityAdmissionAdmitted
	}
	m.Quality.runAdmissions.WithLabelValues(trigger, result).Inc()
}

// CountQualityRunsDeferred records a scheduled run that was not created.
func (m *Metrics) CountQualityRunsDeferred(reason string) {
	if m == nil {
		return
	}
	m.Quality.runsDeferred.WithLabelValues(reason).Inc()
}

// CountQualityRunAbandoned records a run abandoned without a completed rollup.
func (m *Metrics) CountQualityRunAbandoned(reason string) {
	if m == nil {
		return
	}
	m.Quality.runAbandoned.WithLabelValues(reason).Inc()
}

// IncQualityRunsInProgress records that a run started executing.
func (m *Metrics) IncQualityRunsInProgress() {
	if m == nil {
		return
	}
	m.Quality.runsInProgress.Inc()
}

// DecQualityRunsInProgress records that a run stopped executing.
func (m *Metrics) DecQualityRunsInProgress() {
	if m == nil {
		return
	}
	m.Quality.runsInProgress.Dec()
}

// CountQualityManualEnqueueRejected records a manual run refused by a full queue.
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
	m.Quality.ruleErrors.WithLabelValues(qualityRuleTypeLabel(ruleType), qualityErrorCodeLabel(errorCode)).Inc()
}

// NewQualityManualQueueDepth reports the manual run queue depth at scrape time.
func NewQualityManualQueueDepth(depth func() int) prometheus.Collector {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Subsystem: "quality",
		Name:      "manual_queue_depth",
		Help:      "Manual data-quality runs waiting for the runner.",
	}, func() float64 { return float64(depth()) })
}
