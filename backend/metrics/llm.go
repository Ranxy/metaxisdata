package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// LLM call outcomes.
const (
	LLMOutcomeSuccess       = "success"
	LLMOutcomeError         = "error"
	LLMOutcomeEmptyResponse = "empty_response"
	LLMOutcomeCanceled      = "canceled"
)

// LLM token kinds.
const (
	LLMTokenPrompt     = "prompt"
	LLMTokenCompletion = "completion"
)

// LLM quota kinds.
const (
	LLMQuotaAnalysis       = "analysis"
	LLMQuotaColumnAnalysis = "column_analysis"
)

type llmMetrics struct {
	calls           *prometheus.CounterVec
	callDuration    *prometheus.HistogramVec
	tokens          *prometheus.CounterVec
	usageMissing    *prometheus.CounterVec
	parseFailures   *prometheus.CounterVec
	quotaRejections *prometheus.CounterVec
}

func newLLMMetrics() llmMetrics {
	return llmMetrics{
		calls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "llm",
			Name:      "calls_total",
			Help:      "LLM completion requests, by feature, provider and outcome.",
		}, []string{"feature", "provider", "outcome"}),
		callDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "llm",
			Name:      "call_duration_seconds",
			Help:      "LLM completion request duration in seconds, by feature and provider.",
			Buckets:   []float64{0.25, 0.5, 1, 2.5, 5, 10, 20, 30, 60, 120, 300},
		}, []string{"feature", "provider"}),
		tokens: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "llm",
			Name:      "tokens_total",
			Help:      "Provider-reported tokens, by feature, provider and kind.",
		}, []string{"feature", "provider", "kind"}),
		usageMissing: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "llm",
			Name:      "usage_missing_total",
			Help:      "LLM completions for which the provider reported no token usage, by feature.",
		}, []string{"feature"}),
		parseFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "llm",
			Name:      "parse_failures_total",
			Help:      "LLM responses rejected by the strict response validator, by feature.",
		}, []string{"feature"}),
		quotaRejections: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "llm",
			Name:      "quota_rejections_total",
			Help:      "LLM analyses refused by the daily or concurrency quota, by kind.",
		}, []string{"kind"}),
	}
}

func (m llmMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.calls,
		m.callDuration,
		m.tokens,
		m.usageMissing,
		m.parseFailures,
		m.quotaRejections,
	}
}

// ObserveLLMCall records one completed LLM completion request and the tokens the
// provider reported for it. A provider that reports no usage is observed through
// CountLLMUsageMissing instead; zero token counts are not recorded here.
func (m *Metrics) ObserveLLMCall(feature, provider, outcome string, duration time.Duration, promptTokens, completionTokens int64) {
	if m == nil {
		return
	}
	m.LLM.calls.WithLabelValues(feature, provider, outcome).Inc()
	m.LLM.callDuration.WithLabelValues(feature, provider).Observe(duration.Seconds())
	if promptTokens > 0 {
		m.LLM.tokens.WithLabelValues(feature, provider, LLMTokenPrompt).Add(float64(promptTokens))
	}
	if completionTokens > 0 {
		m.LLM.tokens.WithLabelValues(feature, provider, LLMTokenCompletion).Add(float64(completionTokens))
	}
}

// CountLLMUsageMissing records a completion whose provider reported no usage.
func (m *Metrics) CountLLMUsageMissing(feature string) {
	if m == nil {
		return
	}
	m.LLM.usageMissing.WithLabelValues(feature).Inc()
}

// CountLLMParseFailure records an LLM response the strict validator rejected.
func (m *Metrics) CountLLMParseFailure(feature string) {
	if m == nil {
		return
	}
	m.LLM.parseFailures.WithLabelValues(feature).Inc()
}

// CountLLMQuotaRejection records an LLM analysis refused by quota.
func (m *Metrics) CountLLMQuotaRejection(kind string) {
	if m == nil {
		return
	}
	m.LLM.quotaRejections.WithLabelValues(kind).Inc()
}
