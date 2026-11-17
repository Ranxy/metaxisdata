package mcp

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// One dispatch records one tool call in the shared tool metrics, labelled with the
// MCP surface as the product.
func TestDispatchRecordsToolMetrics(t *testing.T) {
	t.Parallel()

	registry := prometheus.NewRegistry()
	m := metrics.New()
	require.NoError(t, m.Register(registry))

	readers := &fakeReaders{listInstances: oneInstance()}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	server.config.Metrics = m
	definition := toolByName(t, server, "list_instances")

	_, err := server.dispatch(context.Background(), requestFor(t, testUser(), definition.Name, `{}`), definition)
	require.NoError(t, err)

	families, err := registry.Gather()
	require.NoError(t, err)
	found := false
	for _, family := range families {
		if family.GetName() != "metaxisdata_agent_tool_calls_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			if labels["product"] != "mcp" || labels["tool"] != "list_instances" {
				continue
			}
			require.Equal(t, "success", labels["outcome"])
			require.Equal(t, float64(1), metric.GetCounter().GetValue())
			found = true
		}
	}
	require.True(t, found, "the MCP tool call metric was not recorded")
}
