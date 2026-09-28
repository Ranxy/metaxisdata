package v1

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestToFilterOptionsOrdersByCountThenValue(t *testing.T) {
	t.Parallel()

	options := toFilterOptions(map[string]int64{
		"prod":    9,
		"staging": 5,
		"dev":     5,
		"local":   1,
	})

	require.Len(t, options, 4)
	assert.Equal(t, []string{"prod", "dev", "staging", "local"}, filterOptionValues(options))
	assert.Equal(t, []int64{9, 5, 5, 1}, filterOptionCounts(options))
}

func TestToFilterOptionsKeepsTheMostCommonWhenCapped(t *testing.T) {
	t.Parallel()

	counts := make(map[string]int64, maxOpenLineageFilterOptions+10)
	for i := range maxOpenLineageFilterOptions + 10 {
		// Higher index means more common, so the cap has to drop the low end.
		counts[fmt.Sprintf("ns-%03d", i)] = int64(i) + 1
	}

	options := toFilterOptions(counts)

	require.Len(t, options, maxOpenLineageFilterOptions)
	assert.Equal(t, fmt.Sprintf("ns-%03d", maxOpenLineageFilterOptions+9), options[0].Value)
}

func TestCollectDatasetFilterOptionsReadsNamespacesAndRuns(t *testing.T) {
	t.Parallel()

	runs := []*store.OpenLineageRunMessage{
		{
			Integration: "airflow",
			Source:      "scheduler",
			RawPayload: []byte(`{
				"eventType":"COMPLETE",
				"run":{"runId":"run-1"},
				"job":{"namespace":"prod","name":"jobA"},
				"inputs":[{"namespace":"postgres://warehouse:5432/analytics","name":"public.orders"}],
				"outputs":[{"namespace":"s3://analytics-bucket","name":"exports/orders_snapshot"}]
			}`),
		},
		{
			Integration: "dbt",
			Source:      "transform",
			RawPayload: []byte(`{
				"eventType":"COMPLETE",
				"run":{"runId":"run-2"},
				"job":{"namespace":"prod","name":"jobB"},
				"inputs":[{"namespace":"postgres://warehouse:5432/analytics","name":"public.orders"}]
			}`),
		},
		{
			// No inputs or outputs: it names no dataset, so it must not offer an
			// integration or a source the Datasets page cannot filter to.
			Integration: "spark",
			Source:      "spark",
			RawPayload: []byte(`{
				"eventType":"COMPLETE",
				"run":{"runId":"run-3"},
				"job":{"namespace":"prod","name":"jobC"}
			}`),
		},
	}

	options := collectDatasetFilterOptions(runs)

	assert.Equal(t, []string{
		"postgres://warehouse:5432/analytics",
		"s3://analytics-bucket",
	}, filterOptionValues(options.namespaces))
	assert.Equal(t, []int64{2, 1}, filterOptionCounts(options.namespaces))

	assert.Equal(t, []string{"airflow", "dbt"}, filterOptionValues(options.integrations))
	assert.Equal(t, []string{"scheduler", "transform"}, filterOptionValues(options.sources))
}

func TestCollectDatasetFilterOptionsSkipsUnparsablePayloads(t *testing.T) {
	t.Parallel()

	options := collectDatasetFilterOptions([]*store.OpenLineageRunMessage{
		{Integration: "airflow", RawPayload: []byte(`not json`)},
	})

	assert.Empty(t, options.namespaces)
	assert.Empty(t, options.integrations)
	assert.Empty(t, options.sources)
}

func filterOptionValues(options []*v1pb.OpenLineageFilterOption) []string {
	values := make([]string, 0, len(options))
	for _, option := range options {
		values = append(values, option.Value)
	}
	return values
}

func filterOptionCounts(options []*v1pb.OpenLineageFilterOption) []int64 {
	counts := make([]int64, 0, len(options))
	for _, option := range options {
		counts = append(counts, option.Count)
	}
	return counts
}
