package store

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The dataset list reads the aggregate the ingest transaction maintains, not the
// reference table: grouping a dataset's whole history on every request was the
// cost this replaced. The window is applied before the filters, so a filtered
// list can only show datasets the detail resolves — its GUID lookup runs over the
// unfiltered window.
func TestBuildOpenLineageDatasetAggregateQuery(t *testing.T) {
	t.Parallel()

	query, args := buildOpenLineageDatasetAggregateQuery(&FindOpenLineageDatasetMessage{})
	require.Contains(t, query, openLineageDatasetWindowCTE())
	require.Contains(t, query, "ORDER BY last_seen DESC NULLS LAST, name, namespace")
	require.NotContains(t, query, "openlineage_run_dataset")
	require.NotContains(t, query, "GROUP BY")
	require.NotContains(t, query, "raw_payload")
	require.Empty(t, args)

	namespace, integration, source := "ns", "airflow", "openlineage"
	columnLineageOnly := true
	query, args = buildOpenLineageDatasetAggregateQuery(&FindOpenLineageDatasetMessage{
		Namespace:         &namespace,
		Integration:       &integration,
		Source:            &source,
		ColumnLineageOnly: &columnLineageOnly,
	})
	require.Equal(t, []any{"ns", "airflow", "openlineage"}, args)

	// The filters run after the window, and they read the maintained aggregate
	// or probe the member table's primary key; none of them widens the window.
	_, afterWindow, found := strings.Cut(query, "FROM dataset_window w")
	require.True(t, found)
	require.Contains(t, afterWindow, "w.namespace = $1")
	require.Contains(t, afterWindow, "filter_member.value = $2")
	require.Contains(t, afterWindow, "filter_member.value = $3")
	require.Contains(t, afterWindow, "w.column_lineage_ref_count > 0")
	require.Contains(t, afterWindow, "kind = '"+openLineageDatasetMemberIntegration+"'")
	require.Contains(t, afterWindow, "kind = '"+openLineageDatasetMemberSource+"'")

	// The window itself is the same capped one the detail resolves against.
	window, afterLimit, found := strings.Cut(query, "LIMIT "+strconv.Itoa(maxOpenLineageDatasetGroups))
	require.True(t, found)
	require.NotContains(t, window, "w.namespace")
	require.Contains(t, afterLimit, "w.namespace = $1")
}

// The detail resolves the requested GUID over the window, and the list reads the
// same one: a dataset that a filtered list shows is one the detail can open.
func TestOpenLineageDatasetWindowIsTheSameForListAndDetail(t *testing.T) {
	t.Parallel()

	window := openLineageDatasetWindowCTE()
	require.Contains(t, window, "AS MATERIALIZED")
	require.Contains(t, window, "FROM openlineage_dataset")
	require.Contains(t, window, "ORDER BY last_seen DESC NULLS LAST, name, namespace")
	require.Contains(t, window, "LIMIT "+strconv.Itoa(maxOpenLineageDatasetGroups))

	list, _ := buildOpenLineageDatasetAggregateQuery(&FindOpenLineageDatasetMessage{})
	require.True(t, strings.HasPrefix(list, window))
}

// A detail request names a dataset by the spellings that resolved to it. Each
// pair has to stay a pair: binding namespaces and names as two lists would let
// a namespace of one pair combine with the name of another.
func TestOpenLineageDatasetPairClause(t *testing.T) {
	t.Parallel()

	clause, args := openLineageDatasetPairClause([]OpenLineageDatasetPair{
		{Namespace: "ns1", Name: "public.orders"},
		{Namespace: "ns2", Name: "public.daily_orders"},
	}, 0, "d")
	require.Equal(t, "(d.namespace = $1 AND d.name = $2) OR (d.namespace = $3 AND d.name = $4)", clause)
	require.Equal(t, []any{"ns1", "public.orders", "ns2", "public.daily_orders"}, args)

	clause, args = openLineageDatasetPairClause([]OpenLineageDatasetPair{{Namespace: "ns", Name: "t"}}, 2, "ds")
	require.Equal(t, "(ds.namespace = $3 AND ds.name = $4)", clause)
	require.Equal(t, []any{"ns", "t"}, args)
}

// Only a caller that asked for the payload gets it: the list projection is what
// the run list and the task list build their links from, and reading the
// payloads of a page is the amplification this replaced.
func TestOpenLineageRunPayloadColumn(t *testing.T) {
	t.Parallel()

	require.Equal(t, "raw_payload", openLineageRunPayloadColumn(true))
	require.NotEqual(t, "raw_payload", openLineageRunPayloadColumn(false))
	require.Contains(t, openLineageRunPayloadColumn(false), "NULL")
}

// The values a page shows are read capped and ordered, and the facets a detail
// expands come from a bounded, newest-first window of references. Sorting every
// facet a dataset ever had, or reading every value it ever carried, is what made
// one detail request unbounded.
func TestOpenLineageDatasetReadsAreBounded(t *testing.T) {
	t.Parallel()

	integrations := openLineageDatasetMemberArrayColumn("w", openLineageDatasetMemberIntegration)
	require.Contains(t, integrations, "array_agg(value ORDER BY value)")
	require.Contains(t, integrations, "FROM openlineage_dataset_member")
	require.Contains(t, integrations, "kind = 'integration'")
	require.Contains(t, integrations, "LIMIT "+strconv.Itoa(maxOpenLineageDatasetDistinctValues))

	sources := openLineageDatasetMemberArrayColumn("w", openLineageDatasetMemberSource)
	require.Contains(t, sources, "kind = 'source'")

	cte := openLineageDatasetRecentRefsCTE("(d.namespace = $1 AND d.name = $2)")
	require.Contains(t, cte, "WITH recent AS")
	require.Contains(t, cte, "ORDER BY d.event_time DESC NULLS LAST, d.id DESC")
	require.Contains(t, cte, fmt.Sprintf("LIMIT %d", maxOpenLineageDatasetRecentRefs))
}
