//go:build integration

package runner

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/mysql"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/mysql"
	"github.com/Ranxy/metaxisdata/backend/store"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

func TestMySQLSchemaSyncAndLineageIntegration(t *testing.T) {
	env, ctx, instanceID, database := setupMySQLSyncedDatabase(t)
	viewGUID := instanceID + ";it_app;;user_order_view"

	assertLineageEventually(t, env, ctx, viewGUID, func(lineages []*store.ColumnLineage) bool {
		return len(lineages) >= 3 && hasEdge(lineages, instanceID+";it_app;;users", "name", "user_name")
	})

	// Keep database variable used for symmetry with other tests and future extensions.
	require.NotNil(t, database)
}

func TestMySQLLineageUpdatesAfterViewChange(t *testing.T) {
	env, ctx, instanceID, database := setupMySQLSyncedDatabase(t)

	viewGUID := instanceID + ";it_app;;user_order_view"
	assertLineageEventually(t, env, ctx, viewGUID, func(lineages []*store.ColumnLineage) bool {
		return hasEdge(lineages, instanceID+";it_app;;users", "name", "user_name")
	})

	require.NoError(t, env.ExecMySQL(ctx, `
USE it_app;
CREATE OR REPLACE VIEW user_order_view AS
SELECT u.id AS user_id, u.age AS user_age, o.amount AS order_amount
FROM users u
JOIN orders o ON u.id = o.user_id;
`))

	require.NoError(t, env.Syncer.SyncDatabaseSchema(ctx, database))

	assertLineageEventually(t, env, ctx, viewGUID, func(lineages []*store.ColumnLineage) bool {
		if !hasEdge(lineages, instanceID+";it_app;;users", "age", "user_age") {
			return false
		}
		return !hasEdge(lineages, instanceID+";it_app;;users", "name", "user_name")
	})
}

func TestMySQLLineageDeletedWhenViewDropped(t *testing.T) {
	env, ctx, instanceID, database := setupMySQLSyncedDatabase(t)

	viewGUID := instanceID + ";it_app;;user_order_view"
	assertLineageEventually(t, env, ctx, viewGUID, func(lineages []*store.ColumnLineage) bool {
		return len(lineages) > 0
	})

	require.NoError(t, env.ExecMySQL(ctx, `
USE it_app;
DROP VIEW IF EXISTS user_order_view;
`))
	require.NoError(t, env.Syncer.SyncDatabaseSchema(ctx, database))

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
	}, 20*time.Second, time.Second)
}

func TestMySQLManualSQLLineageIntegration(t *testing.T) {
	env, ctx, instanceID, database := setupMySQLSyncedDatabase(t)

	require.NoError(t, env.ExecMySQL(ctx, `
USE it_app;
CREATE TABLE IF NOT EXISTS manual_sql_summary (
  user_id INT PRIMARY KEY,
  user_name VARCHAR(64) NOT NULL
);
`))
	require.NoError(t, env.Syncer.SyncDatabaseSchema(ctx, database))

	manual, err := env.Store.CreateManualSQL(ctx, &store.ManualSQLMessage{
		ManualSQLID:        "sync_active_users",
		InstanceResourceID: instanceID,
		DatabaseName:       "it_app",
		SchemaName:         "",
		Name:               "sync_active_users",
		Title:              "Sync Active Users",
		SQLText:            "INSERT INTO manual_sql_summary (user_id, user_name) SELECT id, name FROM users",
		Tags:               []string{"integration", "manual-sql"},
		Attributes: map[string]string{
			"owner": "integration-test",
		},
	})
	require.NoError(t, err)

	env.Analyzer.QueueAnalysis(manual.GUID, storepb.MetaType_MANUAL_SQL)

	manualGUID := manual.GUID
	summaryGUID := instanceID + ";it_app;;manual_sql_summary"
	manualType := storepb.MetaType_MANUAL_SQL
	assertLineageEventuallyForType(t, env, ctx, manualGUID, manualType, func(lineages []*store.ColumnLineage) bool {
		return hasDetailedEdge(lineages, instanceID+";it_app;;users", "id", manualGUID, "user_id") &&
			hasDetailedEdge(lineages, instanceID+";it_app;;users", "name", manualGUID, "user_name") &&
			hasDetailedEdge(lineages, manualGUID, "user_id", summaryGUID, "user_id") &&
			hasDetailedEdge(lineages, manualGUID, "user_name", summaryGUID, "user_name")
	})
}

func TestMySQLSyncInstanceMarksDroppedDatabaseDeleted(t *testing.T) {
	env := integrationenv.SetupMySQLEnv(t)
	ctx := context.Background()

	instanceID := "it-mysql-drop-db"
	instance, err := env.CreateMySQLInstance(ctx, instanceID)
	require.NoError(t, err)

	// Add an extra database to verify deletion behavior.
	require.NoError(t, env.ExecMySQL(ctx, `
CREATE DATABASE IF NOT EXISTS it_drop_me;
USE it_drop_me;
CREATE TABLE IF NOT EXISTS t1 (id INT PRIMARY KEY);
`))

	_, _, newDatabases, err := env.Syncer.SyncInstance(ctx, instance)
	require.NoError(t, err)
	require.True(t, containsDatabase(newDatabases, "it_drop_me"))

	require.NoError(t, env.ExecMySQL(ctx, `DROP DATABASE IF EXISTS it_drop_me;`))

	_, _, _, err = env.Syncer.SyncInstance(ctx, instance)
	require.NoError(t, err)

	dropped, err := env.Store.GetDatabaseV2(ctx, &store.FindDatabaseMessage{InstanceID: &instanceID, DatabaseName: new("it_drop_me"), ShowDeleted: true})
	require.NoError(t, err)
	require.NotNil(t, dropped)
	require.True(t, dropped.Deleted)
}

func setupMySQLSyncedDatabase(t *testing.T) (*integrationenv.TestEnv, context.Context, string, *store.DatabaseMessage) {
	t.Helper()
	env := integrationenv.SetupMySQLEnv(t)
	ctx := context.Background()

	instanceID := "it-mysql-1-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	instance, err := env.CreateMySQLInstance(ctx, instanceID)
	require.NoError(t, err)

	startAnalyzer(t, ctx, env)

	_, _, newDatabases, err := env.Syncer.SyncInstance(ctx, instance)
	require.NoError(t, err)
	require.True(t, containsDatabase(newDatabases, "it_app"))

	database, err := env.Store.GetDatabaseV2(ctx, &store.FindDatabaseMessage{InstanceID: &instanceID, DatabaseName: new("it_app")})
	require.NoError(t, err)
	require.NotNil(t, database)

	require.NoError(t, env.Syncer.SyncDatabaseSchema(ctx, database))
	return env, ctx, instanceID, database
}

func startAnalyzer(t *testing.T, ctx context.Context, env *integrationenv.TestEnv) {
	t.Helper()
	analyzerCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go env.Analyzer.Run(analyzerCtx, &wg)
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
}

func assertLineageEventually(t *testing.T, env *integrationenv.TestEnv, ctx context.Context, viewGUID string, check func([]*store.ColumnLineage) bool) {
	t.Helper()
	viewType := storepb.MetaType_VIEW
	assertLineageEventuallyForType(t, env, ctx, viewGUID, viewType, check)
}

func assertLineageEventuallyForType(t *testing.T, env *integrationenv.TestEnv, ctx context.Context, guid string, metaType storepb.MetaType, check func([]*store.ColumnLineage) bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		lineages, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &guid, MetaType: &metaType})
		if err != nil {
			return false
		}
		return check(lineages)
	}, 30*time.Second, time.Second)
}

func ptr[T any](v T) *T {
	return &v
}

func containsDatabase(databases []*store.DatabaseMessage, name string) bool {
	for _, db := range databases {
		if db.DatabaseName == name {
			return true
		}
	}
	return false
}

func hasEdge(lineages []*store.ColumnLineage, sourceGUID, sourceColumn, targetColumn string) bool {
	for _, item := range lineages {
		if item.SourceGUID == sourceGUID && item.SourceColumn == sourceColumn && item.TargetColumn == targetColumn {
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
