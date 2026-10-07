package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The dataset aggregate is what keeps a page load from reading every run's
// payload: it groups and counts in SQL and returns a bounded number of small
// rows. The shape is the invariant, so it is pinned here.
func TestBuildOpenLineageDatasetAggregateQuery(t *testing.T) {
	t.Parallel()

	query, args := buildOpenLineageDatasetAggregateQuery(&FindOpenLineageDatasetMessage{})
	require.Contains(t, query, "GROUP BY d.namespace, d.name")
	require.Contains(t, query, "ORDER BY last_seen DESC NULLS LAST, d.name, d.namespace")
	require.Contains(t, query, "LIMIT $1")
	require.NotContains(t, query, "raw_payload")
	require.Equal(t, []any{maxOpenLineageDatasetGroups}, args)

	namespace, integration, source := "ns", "airflow", "openlineage"
	columnLineageOnly := true
	query, args = buildOpenLineageDatasetAggregateQuery(&FindOpenLineageDatasetMessage{
		Namespace:         &namespace,
		Integration:       &integration,
		Source:            &source,
		ColumnLineageOnly: &columnLineageOnly,
	})
	require.Equal(t, []any{"ns", "airflow", "openlineage", maxOpenLineageDatasetGroups}, args)
	require.Contains(t, query, "LIMIT $4")

	// The group filters stay in HAVING: a WHERE on integration or source would
	// strip the other values of a group the filter kept, and the list displays
	// the whole set.
	_, afterFrom, found := strings.Cut(query, "FROM openlineage_run_dataset d")
	require.True(t, found)
	where, having, found := strings.Cut(afterFrom, "HAVING")
	require.True(t, found)
	require.Contains(t, where, "d.namespace = $1")
	require.NotContains(t, where, "d.integration")
	require.NotContains(t, where, "d.source")
	require.NotContains(t, where, "has_column_lineage")
	require.Contains(t, having, "BOOL_OR(d.integration = $2)")
	require.Contains(t, having, "BOOL_OR(d.source = $3)")
	require.Contains(t, having, "BOOL_OR(d.has_column_lineage)")
}

// A detail request names a dataset by the spellings that resolved to it. Each
// pair has to stay a pair: binding namespaces and names as two lists would let
// a namespace of one pair combine with the name of another.
func TestOpenLineageDatasetPairClause(t *testing.T) {
	t.Parallel()

	clause, args := openLineageDatasetPairClause([]OpenLineageDatasetPair{
		{Namespace: "ns1", Name: "public.orders"},
		{Namespace: "ns2", Name: "public.daily_orders"},
	}, 0)
	require.Equal(t, "(d.namespace = $1 AND d.name = $2) OR (d.namespace = $3 AND d.name = $4)", clause)
	require.Equal(t, []any{"ns1", "public.orders", "ns2", "public.daily_orders"}, args)

	clause, args = openLineageDatasetPairClause([]OpenLineageDatasetPair{{Namespace: "ns", Name: "t"}}, 2)
	require.Equal(t, "(d.namespace = $3 AND d.name = $4)", clause)
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
