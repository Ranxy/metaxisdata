package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// The coverage denominator is the registry alone. The query must not read the
// quality tables (a target is a registry object, not a rule), and it must count
// columns from the four metadata arrays because views and external tables have no
// COLUMN registry rows.
func TestQualityCoverageQueryShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityCoverageQuery, "FROM meta_registry_resource r")
	require.Contains(t, qualityCoverageQuery, "JOIN instance i ON i.resource_id = split_part(r.guid, ';', 1)")
	require.Contains(t, qualityCoverageQuery, "i.deleted = FALSE")
	require.Contains(t, qualityCoverageQuery, "r.object_type = ANY($1::int2[])")
	require.Contains(t, qualityCoverageQuery, "GROUP BY i.id")
	for _, path := range []string{
		"jsonb_array_length(r.metadata->'tableMetadata'->'columns')",
		"jsonb_array_length(r.metadata->'externalTableMetadata'->'columns')",
		"jsonb_array_length(r.metadata->'viewMetadata'->'columns')",
		"jsonb_array_length(r.metadata->'materializedViewMetadata'->'columns')",
	} {
		require.Contains(t, qualityCoverageQuery, path)
	}
	require.NotContains(t, qualityCoverageQuery, "quality_rule", "the denominator is not derived from the rules")
	require.NotContains(t, qualityCoverageQuery, "target_path", "target_path.column is never written and must not be read")
	require.NotContains(t, qualityCoverageQuery, "SELECT *")
}

// The coverage numerator may only count rules whose target still resolves, so the
// existence probe is a batch (guid, object type) join against the registry and
// reads no quality table.
func TestQualityTargetExistenceQueryShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityTargetExistenceQuery, "FROM meta_registry_resource r")
	// The instance join mirrors the coverage denominator: a soft-deleted instance
	// keeps its registry rows, but those objects are not coverage.
	require.Contains(t, qualityTargetExistenceQuery,
		"JOIN instance i ON i.resource_id = split_part(r.guid, ';', 1) AND i.deleted = FALSE")
	require.Contains(t, qualityTargetExistenceQuery, "JOIN unnest($1::text[], $2::int2[])")
	require.Contains(t, qualityTargetExistenceQuery, "ON r.guid = k.guid AND r.object_type = k.object_type")
	require.NotContains(t, qualityTargetExistenceQuery, "quality_rule")
	require.NotContains(t, qualityTargetExistenceQuery, "SELECT *")
}

// The four monitorable object types are the coverage denominator, and they are
// the only types the query binds.
func TestQualityCoverageObjectTypes(t *testing.T) {
	t.Parallel()

	require.ElementsMatch(t, []int16{
		int16(storepb.MetaType_TABLE),
		int16(storepb.MetaType_EXTERNAL_TABLE),
		int16(storepb.MetaType_VIEW),
		int16(storepb.MetaType_MATERIALIZED_VIEW),
	}, qualityCoverageObjectTypes)
}
