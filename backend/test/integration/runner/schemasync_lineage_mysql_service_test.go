//go:build integration

package runner

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/mysql"
	"github.com/Ranxy/metaxisdata/backend/store"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

func TestMySQLSchemaSyncAndLineageRealServerIntegration(t *testing.T) {
	env, ctx, instanceID, _, databaseName := setupMySQLServiceDatabase(t)
	database := env.SyncDatabase(ctx, t, databaseName)
	require.NotNil(t, database.GetSuccessfulSyncTime())

	viewGUID := instanceID + ";it_app;;user_order_view"
	relations := env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, instanceID+";it_app;;users", "name", viewGUID, "user_name")
	})
	require.NotEmpty(t, relations)

	viewType := storepb.MetaType_VIEW
	require.Eventually(t, func() bool {
		lineages, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &viewGUID, MetaType: &viewType})
		if err != nil {
			return false
		}
		return hasDetailedEdge(lineages, instanceID+";it_app;;users", "name", viewGUID, "user_name")
	}, 15*time.Second, 500*time.Millisecond)
}

func TestMySQLLineageUpdatesAfterViewChangeRealServerIntegration(t *testing.T) {
	env, ctx, instanceID, _, databaseName := setupMySQLServiceDatabase(t)

	viewGUID := instanceID + ";it_app;;user_order_view"
	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, instanceID+";it_app;;users", "name", viewGUID, "user_name")
	})

	require.NoError(t, env.ExecMySQL(ctx, `
USE it_app;
CREATE OR REPLACE VIEW user_order_view AS
SELECT u.id AS user_id, u.age AS user_age, o.amount AS order_amount
FROM users u
JOIN orders o ON u.id = o.user_id;
`))

	env.SyncDatabase(ctx, t, databaseName)
	relations := env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		if !hasAPILineageEdge(relations, instanceID+";it_app;;users", "age", viewGUID, "user_age") {
			return false
		}
		return !hasAPILineageEdge(relations, instanceID+";it_app;;users", "name", viewGUID, "user_name")
	})
	require.NotEmpty(t, relations)
}

func TestMySQLLineageDeletedWhenViewDroppedRealServerIntegration(t *testing.T) {
	env, ctx, instanceID, _, databaseName := setupMySQLServiceDatabase(t)

	viewGUID := instanceID + ";it_app;;user_order_view"
	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return len(relations) > 0
	})

	require.NoError(t, env.ExecMySQL(ctx, `
USE it_app;
DROP VIEW IF EXISTS user_order_view;
`))
	env.SyncDatabase(ctx, t, databaseName)

	viewType := storepb.MetaType_VIEW
	require.Eventually(t, func() bool {
		meta, err := env.Store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &viewGUID, ObjectType: &viewType})
		if err != nil {
			return false
		}
		if meta != nil {
			return false
		}

		lineages, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &viewGUID, MetaType: &viewType})
		if err != nil {
			return false
		}
		if len(lineages) != 0 {
			return false
		}

		version, err := env.Store.GetColumnLineageVersion(ctx, viewGUID, viewType)
		if err != nil {
			return false
		}
		return version == nil
	}, 20*time.Second, 500*time.Millisecond)
}

func TestMySQLManualSQLLineageRealServerIntegration(t *testing.T) {
	env, ctx, instanceID, _, databaseName := setupMySQLServiceDatabase(t)

	require.NoError(t, env.ExecMySQL(ctx, `
USE it_app;
CREATE TABLE IF NOT EXISTS manual_sql_summary (
  user_id INT PRIMARY KEY,
  user_name VARCHAR(64) NOT NULL
);
`))
	env.SyncDatabase(ctx, t, databaseName)

	manual := env.CreateManualSQL(ctx, t, databaseName, "sync_active_users", &v1pb.ManualSQL{
		Title:   "Sync Active Users",
		SqlText: "INSERT INTO manual_sql_summary (user_id, user_name) SELECT id, name FROM users",
		Tags:    []string{"integration", "manual-sql"},
		Attributes: map[string]string{
			"owner": "integration-test",
		},
	})

	summaryGUID := instanceID + ";it_app;;manual_sql_summary"
	relations := env.WaitForContextLineage(ctx, t, manual.GetGuid(), v1pb.MetaType_MANUAL_SQL, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, instanceID+";it_app;;users", "id", manual.GetGuid(), "user_id") &&
			hasAPILineageEdge(relations, instanceID+";it_app;;users", "name", manual.GetGuid(), "user_name") &&
			hasAPILineageEdge(relations, manual.GetGuid(), "user_id", summaryGUID, "user_id") &&
			hasAPILineageEdge(relations, manual.GetGuid(), "user_name", summaryGUID, "user_name")
	})
	require.NotEmpty(t, relations)
}

func TestMySQLSyncInstanceMarksDroppedDatabaseDeletedRealServerIntegration(t *testing.T) {
	env := sharedMySQLServiceEnv(t)
	ctx := context.Background()

	instanceID := "it-mysql-drop-db-svc"
	instance, err := env.CreateMySQLInstance(ctx, instanceID)
	require.NoError(t, err)

	require.NoError(t, env.ExecMySQL(ctx, `
CREATE DATABASE IF NOT EXISTS it_drop_me;
USE it_drop_me;
CREATE TABLE IF NOT EXISTS t1 (id INT PRIMARY KEY);
`))

	resp := env.SyncInstance(ctx, t, instance.GetName(), false)
	require.Contains(t, resp.GetDatabases(), "it_drop_me")

	require.NoError(t, env.ExecMySQL(ctx, `DROP DATABASE IF EXISTS it_drop_me;`))
	env.SyncInstance(ctx, t, instance.GetName(), false)

	require.Eventually(t, func() bool {
		databaseName := "it_drop_me"
		dropped, err := env.Store.GetDatabaseV2(ctx, &store.FindDatabaseMessage{InstanceID: &instanceID, DatabaseName: &databaseName, ShowDeleted: true})
		if err != nil || dropped == nil {
			return false
		}
		return dropped.Deleted
	}, 15*time.Second, 500*time.Millisecond)
}

func setupMySQLServiceDatabase(t *testing.T) (*integrationenv.ServiceEnv, context.Context, string, string, string) {
	t.Helper()

	env := sharedMySQLServiceEnv(t)
	ctx := context.Background()
	instanceID := mysqlServiceInstanceID(t)
	instance, err := env.CreateMySQLInstance(ctx, instanceID)
	require.NoError(t, err)

	databaseName := common.FormatDatabase(instanceID, "it_app")
	_ = env.EnsureDatabaseVisible(ctx, t, instance.GetName(), "it_app")
	return env, ctx, instanceID, instance.GetName(), databaseName
}

func mysqlServiceInstanceID(t *testing.T) string {
	t.Helper()

	const prefix = "it-mysql-svc-"
	const hashWidth = 8

	base := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	maxBaseLen := 63 - len(prefix) - 1 - hashWidth
	if len(base) <= maxBaseLen {
		return prefix + base
	}

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(base))
	trimmed := strings.TrimRight(base[:maxBaseLen], "-")
	if trimmed == "" {
		trimmed = "case"
	}

	return fmt.Sprintf("%s%s-%08x", prefix, trimmed, hasher.Sum32())
}

func hasAPILineageEdge(relations []*v1pb.LineageRelation, sourceGUID, sourceColumn, targetGUID, targetColumn string) bool {
	for _, relation := range relations {
		if relation.SourceGuid == sourceGUID && relation.SourceColumn == sourceColumn && relation.TargetGuid == targetGUID && relation.TargetColumn == targetColumn {
			return true
		}
	}
	return false
}

func hasDetailedEdge(lineages []*store.ColumnLineage, sourceGUID, sourceColumn, targetGUID, targetColumn string) bool {
	for _, item := range lineages {
		if item.SourceGUID == sourceGUID && item.SourceColumn == sourceColumn && item.TargetGUID == targetGUID && item.TargetColumn == targetColumn {
			return true
		}
	}
	return false
}
