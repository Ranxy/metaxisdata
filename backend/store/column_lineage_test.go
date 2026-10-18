package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The dataset pages ask column_lineage whether a dataset has column lineage, so
// the two reads have to answer it from the edge table alone: one batched probe
// for the list, one own-side column read for the detail. A table-level edge names
// no column and must not answer either of them.
func TestColumnLineageDatasetPageQueries(t *testing.T) {
	t.Parallel()

	// The list probe: one query per up-to-1000 batch, index-served by the
	// endpoint columns, and both directions. Each UNION branch has to keep the
	// non-empty-column predicate; losing it on one side would count the
	// table-level edge of the other side's endpoint.
	endpointBranches := strings.Split(columnLineageEndpointGUIDQuery, "UNION")
	require.Len(t, endpointBranches, 2)
	for _, branch := range endpointBranches {
		require.Contains(t, branch, "guid = ANY($1)")
		require.Contains(t, branch, "(source_column <> '' OR target_column <> '')")
	}
	require.Contains(t, endpointBranches[0], "target_guid = ANY($1)")
	require.Contains(t, endpointBranches[1], "source_guid = ANY($1)")
	require.NotContains(t, columnLineageEndpointGUIDQuery, "openlineage_run_dataset")
	require.NotContains(t, columnLineageEndpointGUIDQuery, "raw_payload")
	require.False(t, strings.Contains(columnLineageEndpointGUIDQuery, "JOIN"),
		"the probe must stay two index scans, not a join")
	require.Greater(t, maxColumnLineageProbeGuids, 0)

	// The detail read: each column paired with its own side of the edge, bounded.
	columnBranches := strings.Split(columnLineageColumnQuery, "UNION")
	require.Len(t, columnBranches, 2)
	require.Contains(t, columnBranches[0], "target_column AS column_name")
	require.Contains(t, columnBranches[0], "target_guid = $1")
	require.Contains(t, columnBranches[0], "target_column <> ''")
	require.Contains(t, columnBranches[1], "source_column")
	require.Contains(t, columnBranches[1], "source_guid = $1")
	require.Contains(t, columnBranches[1], "source_column <> ''")
	require.Contains(t, columnLineageColumnQuery, "LIMIT $2")
	require.NotContains(t, columnLineageColumnQuery, "openlineage_run_dataset")
}
