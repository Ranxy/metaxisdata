package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The recipient lookup feeds a notification, so it must skip what is not
// monitored: a deleted or archived rule is not a watcher, and the paired
// predicate keeps the request from matching cross combinations.
func TestBuildListQualitySchemaChangeTargetsQueryFiltersLiveRules(t *testing.T) {
	t.Parallel()

	query := buildListQualitySchemaChangeTargetsQuery()
	require.Contains(t, query, "deleted = FALSE")
	require.Contains(t, query, "state <> $3")
	require.Contains(t, query, "ARRAY_AGG(DISTINCT owner_id)")
	require.Contains(t, query, "(target_guid, target_type::int) IN (SELECT * FROM unnest($1::text[], $2::int[]))")
	require.NotContains(t, query, "ANY($1)")
	require.NotContains(t, query, "ANY($2)")
}

// Re-baselining moves only the structure snapshot. It must not touch the
// definition (so no revision is written) and must skip archived rows, whose
// snapshot nobody reads; the IS DISTINCT guard keeps the reported count honest.
func TestBuildBaselineQualityRulesToSchemaHashQueryMovesOnlyTheSnapshot(t *testing.T) {
	t.Parallel()

	query := buildBaselineQualityRulesToSchemaHashQuery()
	require.Contains(t, query, "SET target_meta_hash = $3, updated_at = now()")
	require.Contains(t, query, "state <> $4")
	require.Contains(t, query, "target_meta_hash IS DISTINCT FROM $3")
	require.NotContains(t, query, "INSERT INTO quality_rule_revision")
	require.NotContains(t, query, "revision = revision + 1")
	// The definition fields must not move: a re-baseline is not an edit.
	require.NotContains(t, query, "config =")
	require.NotContains(t, query, "rule_type =")
}
