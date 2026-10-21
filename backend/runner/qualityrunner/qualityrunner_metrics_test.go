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

// TestRuleExecutionTypeFallsBackFromSnapshotToHead pins the label derivation for
// the per-rule funnel: an execution that failed before its snapshot was read still
// names its type from the due scan's head row, and only a manual run with neither
// falls back to unspecified.
func TestRuleExecutionTypeFallsBackFromSnapshotToHead(t *testing.T) {
	t.Parallel()

	require.Equal(t,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
		ruleExecutionType(&ruleExecution{
			snapshot: &dataquality.RuleSnapshot{Type: storepb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT},
			head:     &store.QualityRuleMessage{RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE},
		}),
		"the executed snapshot wins over the mutable head")
	require.Equal(t,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE,
		ruleExecutionType(&ruleExecution{head: &store.QualityRuleMessage{RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE}}),
		"a failed snapshot read falls back to the head's rule type")
	require.Equal(t,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED,
		ruleExecutionType(&ruleExecution{}),
		"a manual run has neither snapshot nor head")
	require.Equal(t,
		storepb.QualityRuleType_QUALITY_RULE_TYPE_UNSPECIFIED,
		ruleExecutionType(nil))
}

// TestObserveRuleExecutionRecordsOneAttemptPerResult pins the funnel's labels and
// values: one execution counter, one duration observation and, only when the result
// carries a code, one error counter.
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
	})
	runner.observeRuleExecution(&ruleExecution{
		result: &store.QualityExecutionMessage{
			Status:     storepb.QualityStatus_QUALITY_STATUS_ERROR,
			ErrorCode:  dataquality.ErrCodeTimeout,
			DurationMS: 1000,
		},
		head: &store.QualityRuleMessage{RuleType: storepb.QualityRuleType_QUALITY_RULE_TYPE_UNIQUE},
	})

	require.Equal(t, 1.0, gatheredQualityCounter(t, registry, "metaxisdata_quality_rule_executions_total", map[string]string{
		"rule_type": "row_count",
		"status":    "fail",
	}))
	require.Equal(t, 1.0, gatheredQualityCounter(t, registry, "metaxisdata_quality_rule_executions_total", map[string]string{
		"rule_type": "unique",
		"status":    "error",
	}))
	require.Equal(t, 1.0, gatheredQualityCounter(t, registry, "metaxisdata_quality_rule_errors_total", map[string]string{
		"rule_type":  "unique",
		"error_code": "timeout",
	}))
	require.Equal(t, uint64(2), gatheredQualityHistogramCount(t, registry, "metaxisdata_quality_rule_execution_duration_seconds", map[string]string{
		"rule_type": "row_count",
	})+
		gatheredQualityHistogramCount(t, registry, "metaxisdata_quality_rule_execution_duration_seconds", map[string]string{
			"rule_type": "unique",
		}))

	// Nothing to record: no panic and no extra series.
	runner.observeRuleExecution(nil)
	runner.observeRuleExecution(&ruleExecution{})
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
