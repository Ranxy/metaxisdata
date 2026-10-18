//go:build integration

package runner

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

// A statistics refresh rewrites what the API shows without moving the change
// fingerprint or writing the history ledger: the fingerprint ignores the statistics
// by construction, and the ledger records definitions, not estimates.
func TestMySQLMetaRegistryStatisticsRefreshKeepsHistoryRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, _ := setupMySQLServiceDatabase(t)
	tableGUID := waitForMetaGUIDByName(ctx, t, env, fmt.Sprintf("%s;%s", instanceID, sourceDatabase), storepb.MetaType_TABLE, "users")
	before := waitForMetaRegistry(ctx, t, env, tableGUID, storepb.MetaType_TABLE)

	historyBefore := countMetadataRows(ctx, t, env,
		`SELECT count(*) FROM meta_registry_resource_history WHERE guid = $1 AND object_type = $2`,
		tableGUID, int16(storepb.MetaType_TABLE))

	updated, ok := proto.Clone(before.Metadata).(*storepb.StoredMetadata)
	require.True(t, ok)
	updated.GetTableMetadata().RowCount = before.Metadata.GetTableMetadata().GetRowCount() + 12345
	metadataBytes, metaHash, err := store.CalcStoreMetaHash(updated)
	require.NoError(t, err)
	require.Equal(t, before.MetaHash, metaHash, "statistics must not move the change fingerprint")

	affected, err := env.Store.BatchUpdateMetaRegistryStatistics(ctx, []*store.CreateMetaRegistryResourceMessage{{
		MetaRegistryResource: store.MetaRegistryResource{GUID: tableGUID, ObjectType: storepb.MetaType_TABLE, MetaHash: metaHash},
		MetadataBytes:        metadataBytes,
	}})
	require.NoError(t, err)
	require.Equal(t, int64(1), affected)

	after := waitForMetaRegistry(ctx, t, env, tableGUID, storepb.MetaType_TABLE)
	require.Equal(t, before.Metadata.GetTableMetadata().GetRowCount()+12345, after.Metadata.GetTableMetadata().GetRowCount())
	require.Equal(t, before.MetaHash, after.MetaHash, "a statistics refresh must not rewrite the fingerprint")

	historyAfter := countMetadataRows(ctx, t, env,
		`SELECT count(*) FROM meta_registry_resource_history WHERE guid = $1 AND object_type = $2`,
		tableGUID, int16(storepb.MetaType_TABLE))
	require.Equal(t, historyBefore, historyAfter, "a statistics refresh must not write history")
}

// Deleting a database's objects cleans their lineage in a fixed number of
// statements: the edges the analyzed objects produced, the edges that name any of
// them as a source or target, and the analysis state. The batch filters by type
// first, so the columns and schemas of a dropped database never probe the lineage
// indexes at all.
func TestMySQLDeleteColumnLineageByMetaGUIDsRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, _ := setupMySQLServiceDatabase(t)
	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	viewGUID := guidPrefix + ";;user_order_view"
	usersGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "users")

	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return len(relations) > 0
	})
	producedBefore := countMetadataRows(ctx, t, env,
		`SELECT count(*) FROM column_lineage WHERE meta_guid = $1`, viewGUID)
	requiredBefore := countMetadataRows(ctx, t, env,
		`SELECT count(*) FROM column_lineage WHERE source_guid = $1 OR target_guid = $1`, usersGUID)
	versionBefore := countMetadataRows(ctx, t, env,
		`SELECT count(*) FROM column_lineage_version WHERE meta_guid = $1`, viewGUID)
	require.Positive(t, producedBefore)
	require.Positive(t, requiredBefore)
	require.Positive(t, versionBefore)

	tx, err := env.Store.GetDB().BeginTx(ctx, nil)
	require.NoError(t, err)
	keys := []store.MetaGUIDKey{
		{GUID: usersGUID, ObjectType: storepb.MetaType_TABLE},
		{GUID: viewGUID, ObjectType: storepb.MetaType_VIEW},
		// A column can never be a lineage endpoint; the batch drops it before it
		// reaches the database.
		{GUID: usersGUID + ";;id", ObjectType: storepb.MetaType_COLUMN},
	}
	require.NoError(t, env.Store.DeleteColumnLineageByMetaGUIDs(ctx, tx, keys))
	require.NoError(t, tx.Commit())

	for query, message := range map[string]string{
		`SELECT count(*) FROM column_lineage WHERE meta_guid = '` + viewGUID + `'`:                                         "the deleted view's produced edges must be gone",
		`SELECT count(*) FROM column_lineage WHERE source_guid = '` + usersGUID + `' OR target_guid = '` + usersGUID + `'`: "the deleted table's edges must be gone",
		`SELECT count(*) FROM column_lineage_version WHERE meta_guid = '` + viewGUID + `'`:                                 "the deleted view's analysis state must be gone",
	} {
		require.Zero(t, countMetadataRows(ctx, t, env, query), message)
	}
}

// countMetadataRows runs one COUNT(*) against the metadata database through the
// observer store, whose cache is disabled so it always reads the committed rows.
func countMetadataRows(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, query string, args ...any) int64 {
	t.Helper()
	var count int64
	require.NoError(t, env.Store.GetDB().QueryRowContext(ctx, query, args...).Scan(&count))
	return count
}
