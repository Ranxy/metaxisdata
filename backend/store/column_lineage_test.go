package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
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

// A destructive schema sync deletes thousands of objects inside its write transaction,
// most of them columns that are never lineage endpoints. The cleanup filters by type and
// probes each lineage index once per direction instead of once per deleted object; losing a
// direction would leave edges that name a GUID the registry no longer has.
func TestColumnLineageDeleteByMetaGUIDsShape(t *testing.T) {
	t.Parallel()

	// Only the endpoints that can appear on an edge and the objects that can own one are
	// passed to the database; every other deleted object is dropped before the query.
	endpoints, producers := splitLineageDeletions([]MetaGUIDKey{
		{GUID: "t1", ObjectType: storepb.MetaType_TABLE},
		{GUID: "v1", ObjectType: storepb.MetaType_VIEW},
		{GUID: "mv1", ObjectType: storepb.MetaType_MATERIALIZED_VIEW},
		{GUID: "e1", ObjectType: storepb.MetaType_EXTERNAL_TABLE},
		{GUID: "c1", ObjectType: storepb.MetaType_COLUMN},
		{GUID: "s1", ObjectType: storepb.MetaType_SCHEMA},
		{GUID: "d1", ObjectType: storepb.MetaType_DATABASE},
		{GUID: "q1", ObjectType: storepb.MetaType_SEQUENCE},
		{GUID: "f1", ObjectType: storepb.MetaType_FUNCTION},
	})
	require.Equal(t, []string{"t1", "v1", "mv1", "e1"}, endpoints)
	require.Equal(t, []string{"v1", "mv1"}, producers)

	require.Contains(t, columnLineageDeleteByEndpointStatement, "DELETE FROM column_lineage")
	require.Contains(t, columnLineageDeleteByEndpointStatement, "source_guid = ANY($1::text[])")
	require.Contains(t, columnLineageDeleteByEndpointStatement, "target_guid = ANY($1::text[])")
	require.NotContains(t, columnLineageDeleteByEndpointStatement, "meta_guid")

	require.Contains(t, columnLineageDeleteByProducerStatement, "meta_guid = ANY($1::text[])")
	require.Contains(t, columnLineageVersionDeleteByProducerStatement, "DELETE FROM column_lineage_version")
	require.Contains(t, columnLineageVersionDeleteByProducerStatement, "meta_guid = ANY($1::text[])")

	// The cleanup is metadata-adjacent, not history: it must never touch the ledger.
	for _, statement := range []string{
		columnLineageDeleteByEndpointStatement,
		columnLineageDeleteByProducerStatement,
		columnLineageVersionDeleteByProducerStatement,
	} {
		require.NotContains(t, statement, "meta_registry_resource_history")
	}
}
