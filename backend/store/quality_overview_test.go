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

// The coverage numerator only counts a column that is still in the target's
// registry: the read returns each target's current column names from the same
// live-instance object set as the denominator, and keeps a zero-column target as a
// key with no names.
func TestQualityTargetColumnQueryShape(t *testing.T) {
	t.Parallel()

	require.Contains(t, qualityTargetColumnQuery, "JOIN instance i ON i.resource_id = split_part(k.guid, ';', 1) AND i.deleted = FALSE")
	require.Contains(t, qualityTargetColumnQuery, "JOIN meta_registry_resource r ON r.guid = k.guid AND r.object_type = k.object_type")
	require.Contains(t, qualityTargetColumnQuery, "LEFT JOIN LATERAL (")
	require.Contains(t, qualityTargetColumnQuery, "jsonb_array_elements(COALESCE(r.metadata->'tableMetadata'->'columns', '[]'::jsonb))")
	require.Contains(t, qualityTargetColumnQuery, "jsonb_array_elements(COALESCE(r.metadata->'externalTableMetadata'->'columns', '[]'::jsonb))")
	require.Contains(t, qualityTargetColumnQuery, "jsonb_array_elements(COALESCE(r.metadata->'viewMetadata'->'columns', '[]'::jsonb))")
	require.Contains(t, qualityTargetColumnQuery, "jsonb_array_elements(COALESCE(r.metadata->'materializedViewMetadata'->'columns', '[]'::jsonb))")
	require.Contains(t, qualityTargetColumnQuery, "WHERE r.object_type = ANY($3::int2[])",
		"the numerator's object-type set is the denominator's")
	require.NotContains(t, qualityTargetColumnQuery, "quality_rule")
	require.NotContains(t, qualityTargetColumnQuery, "target_path", "target_path.column is never written and must not be read")
	require.NotContains(t, qualityTargetColumnQuery, "SELECT *")
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
