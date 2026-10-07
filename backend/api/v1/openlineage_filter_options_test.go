package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/store"
)

// The store orders and caps each dimension; this only labels the values it
// returns, and a dimension it does not label is dropped rather than misfiled.
func TestDatasetFilterOptionsFromValues(t *testing.T) {
	t.Parallel()

	options := datasetFilterOptionsFromValues([]*store.OpenLineageFilterValue{
		{Dimension: openLineageFilterDatasetNamespace, Value: "postgres://warehouse:5432/analytics", Count: 9},
		{Dimension: openLineageFilterDatasetIntegration, Value: "airflow", Count: 5},
		{Dimension: openLineageFilterDatasetSource, Value: "openlineage", Count: 7},
		{Dimension: openLineageFilterJobNamespace, Value: "prod", Count: 3},
		{Dimension: openLineageFilterDatasetNamespace, Value: "s3://analytics-bucket", Count: 2},
	})

	require.Len(t, options.namespaces, 2)
	assert.Equal(t, "postgres://warehouse:5432/analytics", options.namespaces[0].Value)
	assert.Equal(t, int64(9), options.namespaces[0].Count)
	assert.Equal(t, "s3://analytics-bucket", options.namespaces[1].Value)

	require.Len(t, options.integrations, 1)
	assert.Equal(t, "airflow", options.integrations[0].Value)
	assert.Equal(t, int64(5), options.integrations[0].Count)

	require.Len(t, options.sources, 1)
	assert.Equal(t, "openlineage", options.sources[0].Value)
	assert.Equal(t, int64(7), options.sources[0].Count)

	empty := datasetFilterOptionsFromValues(nil)
	assert.Empty(t, empty.namespaces)
	assert.Empty(t, empty.integrations)
	assert.Empty(t, empty.sources)
}
