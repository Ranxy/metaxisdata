package db

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// TestRunReadOnlyQueryRecordsPurposeAndOutcome pins the single instrumentation
// layer for target queries: the shared implementation reports the purpose its
// caller passed and whether the statement succeeded.
func TestRunReadOnlyQueryRecordsPurposeAndOutcome(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))

	driver := &fakeDriver{
		columns:         []string{"n"},
		columnTypeNames: []string{"BIGINT"},
		values:          [][]driver.Value{{int64(1)}},
	}
	database := openFakeDB(t, driver)

	_, err := RunReadOnlyQuery(context.Background(), database, "SELECT 1", nil, QueryOptions{
		Purpose: metrics.QueryPurposeRuleAggregate,
		Metrics: m,
	}, ReadOnlyQueryHooks{})
	require.NoError(t, err)

	require.Equal(t, 1.0, gatheredQueryCounter(t, registry, "metaxisdata_query_readonly_total", map[string]string{
		"purpose": metrics.QueryPurposeRuleAggregate,
		"outcome": "success",
	}))
	require.Equal(t, uint64(1), gatheredQueryHistogramCount(t, registry, "metaxisdata_query_readonly_duration_seconds", map[string]string{
		"purpose": metrics.QueryPurposeRuleAggregate,
	}))
}

// An empty purpose must not create an empty label value: it is folded into the
// bounded "unspecified" sentinel, and a failed query reports outcome=failure.
func TestRunReadOnlyQueryLabelsAnEmptyPurposeAndAFailure(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))

	// A nil database fails before touching any statement, so only the outcome and
	// purpose labels are exercised.
	_, err := RunReadOnlyQuery(context.Background(), nil, "SELECT 1", nil, QueryOptions{
		Purpose: metrics.QueryPurposeDryRunProbe,
		Metrics: m,
	}, ReadOnlyQueryHooks{})
	require.Error(t, err)

	require.Equal(t, 1.0, gatheredQueryCounter(t, registry, "metaxisdata_query_readonly_total", map[string]string{
		"purpose": metrics.QueryPurposeDryRunProbe,
		"outcome": "failure",
	}))

	driver := &fakeDriver{
		columns:         []string{"n"},
		columnTypeNames: []string{"BIGINT"},
		values:          [][]driver.Value{{int64(1)}},
	}
	_, err = RunReadOnlyQuery(context.Background(), openFakeDB(t, driver), "SELECT 1", nil, QueryOptions{Metrics: m}, ReadOnlyQueryHooks{})
	require.NoError(t, err)
	require.Equal(t, 1.0, gatheredQueryCounter(t, registry, "metaxisdata_query_readonly_total", map[string]string{
		"purpose": queryPurposeUnspecified,
		"outcome": "success",
	}))
}

// gatheredQueryCounter returns one counter's value matched on its full label set;
// a series that was never recorded is 0.
func gatheredQueryCounter(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) float64 {
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

// gatheredQueryHistogramCount returns one histogram series' observation count.
func gatheredQueryHistogramCount(t *testing.T, registry *prometheus.Registry, name string, labels map[string]string) uint64 {
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
