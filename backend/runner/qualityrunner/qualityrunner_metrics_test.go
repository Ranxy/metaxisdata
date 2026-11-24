package qualityrunner

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// TestObserveRuleExecutionRecordsOneAttemptPerResult pins the funnel's labels and
// values: every written result costs one execution counter and one duration
// observation, and an ERROR result also costs one error counter under its code. The
// rule-type label is the type the execution was judged with: the snapshot when it
// was read, the due scan's head row when the snapshot read failed, and unspecified
// for a manual run that has neither.
func TestObserveRuleExecutionRecordsOneAttemptPerResult(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))
	runner := &Runner{metrics: m}

	runner.observeRuleExecution(&ruleExecution{
		result: &store.QualityExecutionMessage{
			Status:     storepb.QualityStatus_QUALITY_STATUS_FAIL,
			DurationMS: 250,
		},
		snapshot: &dataquality.RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT},
		head:     &store.QualityRuleMessage{RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE},
	})
	runner.observeRuleExecution(&ruleExecution{
		result: &store.QualityExecutionMessage{
			Status:     storepb.QualityStatus_QUALITY_STATUS_ERROR,
			ErrorCode:  dataquality.ErrCodeTimeout,
			DurationMS: 1000,
		},
		head: &store.QualityRuleMessage{RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE},
	})
	runner.observeRuleExecution(&ruleExecution{
		result: &store.QualityExecutionMessage{
			Status: storepb.QualityStatus_QUALITY_STATUS_SKIPPED,
		},
	})

	// The executed snapshot wins over the mutable head row.
	require.Equal(t, 1.0, gatheredQualityCounter(t, registry, "metaxisdata_quality_rule_executions_total", map[string]string{
		"rule_type": "row_count",
		"status":    "fail",
	}))
	// A failed snapshot read labels the attempt from the head.
	require.Equal(t, 1.0, gatheredQualityCounter(t, registry, "metaxisdata_quality_rule_executions_total", map[string]string{
		"rule_type": "unique",
		"status":    "error",
	}))
	require.Equal(t, 1.0, gatheredQualityCounter(t, registry, "metaxisdata_quality_rule_errors_total", map[string]string{
		"rule_type":  "unique",
		"error_code": "timeout",
	}))
	// A manual run has neither snapshot nor head, so its label is unspecified.
	require.Equal(t, 1.0, gatheredQualityCounter(t, registry, "metaxisdata_quality_rule_executions_total", map[string]string{
		"rule_type": "unspecified",
		"status":    "skipped",
	}))
	require.Equal(t, uint64(3), gatheredQualityHistogramCount(t, registry, "metaxisdata_quality_rule_execution_duration_seconds", map[string]string{
		"rule_type": "row_count",
	})+
		gatheredQualityHistogramCount(t, registry, "metaxisdata_quality_rule_execution_duration_seconds", map[string]string{
			"rule_type": "unique",
		})+
		gatheredQualityHistogramCount(t, registry, "metaxisdata_quality_rule_execution_duration_seconds", map[string]string{
			"rule_type": "unspecified",
		}))
}

// gatheredQualityCounter returns one counter's value matched on its full label
// set; a series that was never recorded is 0.
func gatheredQualityCounter(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) float64 {
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

// gatheredQualityHistogramCount returns one histogram series' observation count.
func gatheredQualityHistogramCount(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) uint64 {
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
