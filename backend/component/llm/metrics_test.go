package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// TestRunAgentLoopRecordsOneCallWithTokens pins the central LLM instrumentation:
// one invocation is one call, labelled with the caller's feature and the provider
// profile, and it carries the tokens the provider reported across the turns.
func TestRunAgentLoopRecordsOneCallWithTokens(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))

	server := usageAcrossTurnsProvider(t)
	cfg := AgentConfig{
		Provider:     ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key", ProfileName: "profile-a"},
		Feature:      "test_feature",
		Metrics:      m,
		MaxTurns:     3,
		Tools:        []ToolDef{{Type: "function"}},
		Executor:     func(ToolCall) ([]ToolResult, error) { return []ToolResult{{Content: "result"}}, nil },
		IncludeUsage: true,
	}
	collectAgentEvents(t, RunAgentLoop(context.Background(), cfg))

	require.Equal(t, 1.0, gatheredLLMCounter(t, registry, "metaxisdata_llm_calls_total", map[string]string{
		"feature":  "test_feature",
		"provider": "profile-a",
		"outcome":  "success",
	}))
	require.Equal(t, 30.0, gatheredLLMCounter(t, registry, "metaxisdata_llm_tokens_total", map[string]string{
		"feature":  "test_feature",
		"provider": "profile-a",
		"kind":     "prompt",
	}))
	require.Equal(t, 7.0, gatheredLLMCounter(t, registry, "metaxisdata_llm_tokens_total", map[string]string{
		"feature":  "test_feature",
		"provider": "profile-a",
		"kind":     "completion",
	}))
	require.Equal(t, uint64(1), gatheredLLMHistogramCount(t, registry, "metaxisdata_llm_call_duration_seconds", map[string]string{
		"feature":  "test_feature",
		"provider": "profile-a",
	}))
	require.Equal(t, 0.0, gatheredLLMCounter(t, registry, "metaxisdata_llm_usage_missing_total", map[string]string{
		"feature": "test_feature",
	}))
}

// TestRunAgentLoopReportsMissingUsage covers the ExplainSQL compatibility path: a
// provider that reports no usage is visible through usage_missing_total instead of
// being silently counted as zero tokens.
func TestRunAgentLoopReportsMissingUsage(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))

	server := noUsageProvider(t)
	cfg := AgentConfig{
		Provider: ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key", ProfileName: "profile-a"},
		Feature:  "explain_sql",
		Metrics:  m,
	}
	collectAgentEvents(t, RunAgentLoop(context.Background(), cfg))

	require.Equal(t, 1.0, gatheredLLMCounter(t, registry, "metaxisdata_llm_calls_total", map[string]string{
		"feature":  "explain_sql",
		"provider": "profile-a",
		"outcome":  "success",
	}))
	require.Equal(t, 1.0, gatheredLLMCounter(t, registry, "metaxisdata_llm_usage_missing_total", map[string]string{
		"feature": "explain_sql",
	}))
	require.Equal(t, 0.0, gatheredLLMCounter(t, registry, "metaxisdata_llm_tokens_total", map[string]string{
		"feature":  "explain_sql",
		"provider": "profile-a",
		"kind":     "prompt",
	}))
}

// TestRunAgentLoopRecordsAnEmptyResponse keeps the empty-response outcome distinct
// from a transport error: both fail the invocation, but only one is the provider's
// answer being unusable.
func TestRunAgentLoopRecordsAnEmptyResponse(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n")
		_, _ = io.WriteString(w, "data: [DONE]\n")
	}))
	t.Cleanup(server.Close)

	cfg := AgentConfig{
		Provider: ResolvedConfig{BaseURL: server.URL, ModelName: "test-model", APIKey: "key", ProfileName: "profile-a"},
		Feature:  "dq_rule_draft",
		Metrics:  m,
		MaxTurns: 1,
	}
	collectAgentEvents(t, RunAgentLoop(context.Background(), cfg))

	require.Equal(t, 1.0, gatheredLLMCounter(t, registry, "metaxisdata_llm_calls_total", map[string]string{
		"feature":  "dq_rule_draft",
		"provider": "profile-a",
		"outcome":  "empty_response",
	}))
	require.Equal(t, 1.0, gatheredLLMCounter(t, registry, "metaxisdata_llm_usage_missing_total", map[string]string{
		"feature": "dq_rule_draft",
	}))
}

// noUsageProvider answers one complete non-streaming completion without a usage
// object, like a gateway that ignores stream_options.
func noUsageProvider(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"}}]}\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n")
		_, _ = io.WriteString(w, "data: [DONE]\n")
	}))
	t.Cleanup(server.Close)
	return server
}

// gatheredLLMCounter returns one counter's value matched on its full label set; a
// series that was never recorded is 0.
func gatheredLLMCounter(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()

	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if len(metric.GetLabel()) != len(labels) {
				continue
			}
			matches := true
			for _, pair := range metric.GetLabel() {
				value, ok := labels[pair.GetName()]
				if !ok || value != pair.GetValue() {
					matches = false
					break
				}
			}
			if matches && metric.GetCounter() != nil {
				return metric.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// gatheredLLMHistogramCount returns one histogram series' observation count.
func gatheredLLMHistogramCount(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) uint64 {
	t.Helper()

	families, err := registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			if len(metric.GetLabel()) != len(labels) {
				continue
			}
			matches := true
			for _, pair := range metric.GetLabel() {
				value, ok := labels[pair.GetName()]
				if !ok || value != pair.GetValue() {
					matches = false
					break
				}
			}
			if matches && metric.GetHistogram() != nil {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}
