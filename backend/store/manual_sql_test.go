package store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

func TestBuildDeleteManualSQLStatement(t *testing.T) {
	t.Parallel()

	t.Run("without updated_by", func(t *testing.T) {
		t.Parallel()

		query, args := buildDeleteManualSQLStatement("manual-guid", nil)

		require.Contains(t, query, "WHERE guid = $1")
		require.NotContains(t, query, "updated_by =")
		require.Equal(t, []any{"manual-guid"}, args)
	})

	t.Run("with updated_by", func(t *testing.T) {
		t.Parallel()

		updatedBy := "  alice@example.com  "
		query, args := buildDeleteManualSQLStatement("manual-guid", &updatedBy)

		require.Contains(t, query, "updated_by = $1")
		require.Contains(t, query, "WHERE guid = $2")
		require.Equal(t, []any{"alice@example.com", "manual-guid"}, args)
	})
}

func TestBuildDeleteManualSQLMetaRegistryStatement(t *testing.T) {
	t.Parallel()

	guid := `inst;db_1%prod;public;__manual_sql__/summary`
	query, args := buildDeleteManualSQLMetaRegistryStatement(guid)

	require.Contains(t, query, "DELETE FROM meta_registry_resource WHERE")
	require.Contains(t, query, "guid = $1 OR guid LIKE $2")
	require.Contains(t, query, "ESCAPE E'\\\\'")
	require.Equal(t, []any{guid, likePatternEscaper.Replace(guid+common.MetaGUIDSplit) + "%"}, args)
}

func TestBuildDeleteColumnLineageByGUIDStatement(t *testing.T) {
	t.Parallel()

	guid := `inst;db;public;__manual_sql__/summary`
	query, args := buildDeleteColumnLineageByGUIDStatement("column_lineage_version", guid)

	require.Contains(t, query, "DELETE FROM column_lineage_version WHERE")
	require.Contains(t, query, "meta_guid = $1 OR meta_guid LIKE $2")
	require.Contains(t, query, "ESCAPE E'\\\\'")
	require.Equal(t, []any{guid, likePatternEscaper.Replace(guid+common.MetaGUIDSplit) + "%"}, args)
}
