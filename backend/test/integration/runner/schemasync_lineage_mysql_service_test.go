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
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupMySQLServiceDatabase(t)
	database := env.SyncDatabase(ctx, t, databaseName)
	require.NotNil(t, database.GetSuccessfulSyncTime())

	viewGUID := fmt.Sprintf("%s;%s;;user_order_view", instanceID, sourceDatabase)
	usersGUID := fmt.Sprintf("%s;%s;;users", instanceID, sourceDatabase)
	relations := env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, usersGUID, "name", viewGUID, "user_name")
	})
	require.NotEmpty(t, relations)
}

func TestMySQLLineageUpdatesAfterViewChangeRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupMySQLServiceDatabase(t)

	viewGUID := fmt.Sprintf("%s;%s;;user_order_view", instanceID, sourceDatabase)
	usersGUID := fmt.Sprintf("%s;%s;;users", instanceID, sourceDatabase)
	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, usersGUID, "name", viewGUID, "user_name")
	})

	require.NoError(t, env.ExecMySQL(ctx, fmt.Sprintf(`
USE %s;
CREATE OR REPLACE VIEW user_order_view AS
SELECT u.id AS user_id, u.age AS user_age, o.amount AS order_amount
FROM users u
JOIN orders o ON u.id = o.user_id;
`, quoteMySQLIdentifier(sourceDatabase))))

	env.SyncDatabase(ctx, t, databaseName)
	relations := env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		if !hasAPILineageEdge(relations, usersGUID, "age", viewGUID, "user_age") {
			return false
		}
		return !hasAPILineageEdge(relations, usersGUID, "name", viewGUID, "user_name")
	})
	require.NotEmpty(t, relations)
}

func TestMySQLLineageDeletedWhenViewDroppedRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupMySQLServiceDatabase(t)

	viewGUID := fmt.Sprintf("%s;%s;;user_order_view", instanceID, sourceDatabase)
	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return len(relations) > 0
	})

	require.NoError(t, env.ExecMySQL(ctx, fmt.Sprintf(`
USE %s;
DROP VIEW IF EXISTS user_order_view;
`, quoteMySQLIdentifier(sourceDatabase))))
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
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupMySQLServiceDatabase(t)

	require.NoError(t, env.ExecMySQL(ctx, fmt.Sprintf(`
USE %s;
CREATE TABLE IF NOT EXISTS manual_sql_summary (
  user_id INT PRIMARY KEY,
  user_name VARCHAR(64) NOT NULL
);
`, quoteMySQLIdentifier(sourceDatabase))))
	env.SyncDatabase(ctx, t, databaseName)

	manual := env.CreateManualSQL(ctx, t, databaseName, "sync_active_users", &v1pb.ManualSQL{
		Title:   "Sync Active Users",
		SqlText: "INSERT INTO manual_sql_summary (user_id, user_name) SELECT id, name FROM users",
		Tags:    []string{"integration", "manual-sql"},
		Attributes: map[string]string{
			"owner": "integration-test",
		},
	})

	usersGUID := fmt.Sprintf("%s;%s;;users", instanceID, sourceDatabase)
	summaryGUID := fmt.Sprintf("%s;%s;;manual_sql_summary", instanceID, sourceDatabase)
	relations := env.WaitForContextLineage(ctx, t, manual.GetGuid(), v1pb.MetaType_MANUAL_SQL, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, usersGUID, "id", manual.GetGuid(), "user_id") &&
			hasAPILineageEdge(relations, usersGUID, "name", manual.GetGuid(), "user_name") &&
			hasAPILineageEdge(relations, manual.GetGuid(), "user_id", summaryGUID, "user_id") &&
			hasAPILineageEdge(relations, manual.GetGuid(), "user_name", summaryGUID, "user_name")
	})
	require.NotEmpty(t, relations)
}

func TestMySQLSyncInstanceMarksDroppedDatabaseDeletedRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedMySQLServiceEnvNoReset(t)
	ctx := context.Background()

	instanceID := mysqlServiceInstanceID(t)
	droppedDatabaseName := mysqlServiceDropDatabaseName(t)
	instance, err := env.CreateMySQLInstance(ctx, instanceID)
	require.NoError(t, err)

	require.NoError(t, env.ExecMySQL(ctx, fmt.Sprintf(`
CREATE DATABASE IF NOT EXISTS %s;
USE %s;
CREATE TABLE IF NOT EXISTS t1 (id INT PRIMARY KEY);
`, quoteMySQLIdentifier(droppedDatabaseName), quoteMySQLIdentifier(droppedDatabaseName))))

	resp := env.SyncInstance(ctx, t, instance.GetName(), false)
	require.Contains(t, resp.GetDatabases(), droppedDatabaseName)

	require.NoError(t, env.ExecMySQL(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quoteMySQLIdentifier(droppedDatabaseName))))
	env.SyncInstance(ctx, t, instance.GetName(), false)

	require.Eventually(t, func() bool {
		databaseName := droppedDatabaseName
		dropped, err := env.Store.GetDatabaseV2(ctx, &store.FindDatabaseMessage{InstanceID: &instanceID, DatabaseName: &databaseName, ShowDeleted: true})
		if err != nil || dropped == nil {
			return false
		}
		return dropped.Deleted
	}, 15*time.Second, 500*time.Millisecond)
}

func setupMySQLServiceDatabase(t *testing.T) (*integrationenv.ServiceEnv, context.Context, string, string, string) {
	t.Helper()

	env := sharedMySQLServiceEnvNoReset(t)
	ctx := context.Background()
	instanceID := mysqlServiceInstanceID(t)
	sourceDatabase := mysqlServiceSourceDatabaseName(t)
	instance, err := env.CreateMySQLInstance(ctx, instanceID)
	require.NoError(t, err)
	require.NoError(t, prepareMySQLSourceDatabase(ctx, env, sourceDatabase))
	t.Cleanup(func() {
		_ = env.ExecMySQL(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quoteMySQLIdentifier(sourceDatabase)))
	})

	databaseName := common.FormatDatabase(instanceID, sourceDatabase)
	_ = env.EnsureDatabaseVisible(ctx, t, instance.GetName(), sourceDatabase)
	return env, ctx, instanceID, sourceDatabase, databaseName
}

func prepareMySQLSourceDatabase(ctx context.Context, env *integrationenv.ServiceEnv, sourceDatabase string) error {
	return env.ExecMySQL(ctx, fmt.Sprintf(`
CREATE DATABASE IF NOT EXISTS %s;
USE %s;
DROP VIEW IF EXISTS user_order_view;
DROP TABLE IF EXISTS manual_sql_summary;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS users;
CREATE TABLE users (
  id INT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  age INT NOT NULL
);
CREATE TABLE orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount DECIMAL(10,2) NOT NULL
);
CREATE OR REPLACE VIEW user_order_view AS
SELECT u.id AS user_id, u.name AS user_name, o.amount AS order_amount
FROM users u
JOIN orders o ON u.id = o.user_id;
INSERT INTO users (id, name, age) VALUES (1, 'alice', 31);
INSERT INTO orders (id, user_id, amount) VALUES (1, 1, 9.99);
`, quoteMySQLIdentifier(sourceDatabase), quoteMySQLIdentifier(sourceDatabase)))
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

func mysqlServiceSourceDatabaseName(t *testing.T) string {
	t.Helper()

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(t.Name()))
	return fmt.Sprintf("it_app_%08x", hasher.Sum32())
}

func mysqlServiceDropDatabaseName(t *testing.T) string {
	t.Helper()

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(t.Name()))
	return fmt.Sprintf("it_drop_%08x", hasher.Sum32())
}

func quoteMySQLIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
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
