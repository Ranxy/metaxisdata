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
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/pg"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/lineage/postgresql"
	"github.com/Ranxy/metaxisdata/backend/store"
	integrationenv "github.com/Ranxy/metaxisdata/backend/test/integration/env"
)

func TestPostgresSchemaSyncAndLineageRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	database := env.SyncDatabase(ctx, t, databaseName)
	require.NotNil(t, database.GetSuccessfulSyncTime())

	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	usersGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "users")
	viewGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_VIEW, "user_order_view")
	viewType := storepb.MetaType_VIEW
	version := waitForLineageVersion(ctx, t, env, viewGUID, viewType)
	require.Nil(t, version.ErrorMessage, "unexpected lineage analysis error: %v", version.ErrorMessage)

	relations := env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, usersGUID, "name", viewGUID, "user_name")
	})
	require.NotEmpty(t, relations)
}

func TestPostgresLineageUpdatesAfterViewChangeRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)

	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	usersGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "users")
	viewGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_VIEW, "user_order_view")
	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, usersGUID, "name", viewGUID, "user_name")
	})

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE OR REPLACE VIEW public.user_order_view AS
SELECT u.id AS user_id, u.age::text AS user_name
FROM public.users u;
`))

	env.SyncDatabase(ctx, t, databaseName)
	relations := env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		if !hasAPILineageEdge(relations, usersGUID, "age", viewGUID, "user_name") {
			return false
		}
		return !hasAPILineageEdge(relations, usersGUID, "name", viewGUID, "user_name")
	})
	require.NotEmpty(t, relations)
}

func TestPostgresLineageDeletedWhenViewDroppedRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)

	viewGUID := waitForMetaGUIDByName(ctx, t, env, fmt.Sprintf("%s;%s", instanceID, sourceDatabase), storepb.MetaType_VIEW, "user_order_view")
	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return len(relations) > 0
	})
	asOfBeforeDrop := time.Now().UTC()

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `DROP VIEW IF EXISTS public.user_order_view;`))
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

		return true
	}, 20*time.Second, 500*time.Millisecond)

	historical, err := env.Store.GetMetaRegistryAsOf(ctx, &store.FindMetaRegistryResourceMessage{GUID: &viewGUID, ObjectType: &viewType}, asOfBeforeDrop)
	require.NoError(t, err)
	require.NotNil(t, historical)
	require.Equal(t, "user_order_view", historical.Metadata.GetViewMetadata().GetName())

	history := queryMetaHistorySummary(ctx, t, env, viewGUID, viewType)
	require.Equal(t, 1, history.Total)
	require.Equal(t, 0, history.Open)
}

func TestPostgresManualSQLLineageRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE IF NOT EXISTS public.manual_sql_summary (
  user_id INT PRIMARY KEY,
  user_name TEXT NOT NULL
);
`))
	env.SyncDatabase(ctx, t, databaseName)
	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	usersGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "users")
	summaryGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "manual_sql_summary")

	manual := env.CreateManualSQL(ctx, t, databaseName, "sync_active_users", &v1pb.ManualSQL{
		Title:   "Sync Active Users",
		SqlText: "INSERT INTO public.manual_sql_summary (user_id, user_name) SELECT id, name FROM public.users",
		Tags:    []string{"integration", "manual-sql"},
		Attributes: map[string]string{
			"owner": "integration-test",
		},
	})

	relations := env.WaitForContextLineage(ctx, t, manual.GetGuid(), v1pb.MetaType_MANUAL_SQL, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, usersGUID, "id", manual.GetGuid(), "user_id") &&
			hasAPILineageEdge(relations, usersGUID, "name", manual.GetGuid(), "user_name") &&
			hasAPILineageEdge(relations, manual.GetGuid(), "user_id", summaryGUID, "user_id") &&
			hasAPILineageEdge(relations, manual.GetGuid(), "user_name", summaryGUID, "user_name")
	})
	require.NotEmpty(t, relations)
}

func TestPostgresColumnMetadataHistoryRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	env.SyncDatabase(ctx, t, databaseName)

	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	usersGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_TABLE, "users")
	ageGUID := waitForMetaGUIDByName(ctx, t, env, usersGUID, storepb.MetaType_COLUMN, "age")
	columnType := storepb.MetaType_COLUMN

	original := waitForMetaRegistry(ctx, t, env, ageGUID, columnType)
	require.Empty(t, original.Metadata.GetColumnMetadata().GetComment())
	asOfBeforeCommentChange := time.Now().UTC()

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `COMMENT ON COLUMN public.users.age IS 'age in years';`))
	env.SyncDatabase(ctx, t, databaseName)

	updated := waitForMetaRegistry(ctx, t, env, ageGUID, columnType)
	require.Equal(t, "age in years", updated.Metadata.GetColumnMetadata().GetComment())

	historical, err := env.Store.GetMetaRegistryAsOf(ctx, &store.FindMetaRegistryResourceMessage{GUID: &ageGUID, ObjectType: &columnType}, asOfBeforeCommentChange)
	require.NoError(t, err)
	require.NotNil(t, historical)
	require.Empty(t, historical.Metadata.GetColumnMetadata().GetComment())

	history := queryMetaHistorySummary(ctx, t, env, ageGUID, columnType)
	require.Equal(t, 2, history.Total)
	require.Equal(t, 1, history.Open)
	require.Equal(t, 1, history.Closed)
}

func TestPostgresManualSQLMetadataHistoryRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, _, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	env.SyncDatabase(ctx, t, databaseName)
	asOfBeforeCreate := time.Now().UTC()

	manual := env.CreateManualSQL(ctx, t, databaseName, "history_active_users", &v1pb.ManualSQL{
		Title:   "History Active Users",
		SqlText: "SELECT id, name FROM public.users",
		Tags:    []string{"integration", "history"},
	})
	manualType := storepb.MetaType_MANUAL_SQL
	current := waitForMetaRegistry(ctx, t, env, manual.GetGuid(), manualType)
	require.Equal(t, "History Active Users", current.Metadata.GetManualSqlMetadata().GetTitle())

	manualGUID := manual.GetGuid()
	beforeCreate, err := env.Store.GetMetaRegistryAsOf(ctx, &store.FindMetaRegistryResourceMessage{GUID: &manualGUID, ObjectType: &manualType}, asOfBeforeCreate)
	require.NoError(t, err)
	require.Nil(t, beforeCreate)

	asOfBeforeDelete := time.Now().UTC()
	env.DeleteManualSQL(ctx, t, manual.GetName())
	waitForMetaRegistryDeleted(ctx, t, env, manual.GetGuid(), manualType)

	historical, err := env.Store.GetMetaRegistryAsOf(ctx, &store.FindMetaRegistryResourceMessage{GUID: &manualGUID, ObjectType: &manualType}, asOfBeforeDelete)
	require.NoError(t, err)
	require.NotNil(t, historical)
	require.Equal(t, manual.GetTitle(), historical.Metadata.GetManualSqlMetadata().GetTitle())

	history := queryMetaHistorySummary(ctx, t, env, manualGUID, manualType)
	require.Equal(t, 1, history.Total)
	require.Equal(t, 0, history.Open)
	require.Equal(t, 1, history.Closed)

	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `SELECT 1;`))
}

func TestPostgresSyncInstanceMarksDroppedDatabaseDeletedRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()

	instanceID := postgresServiceInstanceID(t)
	droppedDatabaseName := postgresServiceDropDatabaseName(t)
	instance, err := env.CreatePostgresInstance(ctx, instanceID)
	require.NoError(t, err)

	require.NoError(t, env.ExecPostgres(ctx, "postgres", fmt.Sprintf(`
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '%s' AND pid <> pg_backend_pid();
`, quotePostgresStringLiteral(droppedDatabaseName))))
	require.NoError(t, env.ExecPostgres(ctx, "postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quotePostgresIdentifier(droppedDatabaseName))))
	require.NoError(t, env.ExecPostgres(ctx, "postgres", fmt.Sprintf("CREATE DATABASE %s;", quotePostgresIdentifier(droppedDatabaseName))))
	require.NoError(t, env.ExecPostgres(ctx, droppedDatabaseName, `CREATE TABLE IF NOT EXISTS public.t1 (id INT PRIMARY KEY);`))

	resp := env.SyncInstance(ctx, t, instance.GetName(), false)
	require.Contains(t, resp.GetDatabases(), droppedDatabaseName)

	require.NoError(t, env.ExecPostgres(ctx, "postgres", fmt.Sprintf(`
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '%s' AND pid <> pg_backend_pid();
`, quotePostgresStringLiteral(droppedDatabaseName))))
	require.NoError(t, env.ExecPostgres(ctx, "postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quotePostgresIdentifier(droppedDatabaseName))))
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

func setupPostgresServiceDatabase(t *testing.T) (*integrationenv.ServiceEnv, context.Context, string, string, string) {
	t.Helper()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	instanceID := postgresServiceInstanceID(t)
	sourceDatabase := postgresServiceSourceDatabaseName(t)
	instance, err := env.CreatePostgresInstance(ctx, instanceID)
	require.NoError(t, err)
	require.NoError(t, preparePostgresSourceDatabase(ctx, env, sourceDatabase))
	t.Cleanup(func() {
		_ = env.ExecPostgres(context.Background(), "postgres", fmt.Sprintf(`
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '%s' AND pid <> pg_backend_pid();
`, quotePostgresStringLiteral(sourceDatabase)))
		_ = env.ExecPostgres(context.Background(), "postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quotePostgresIdentifier(sourceDatabase)))
	})

	databaseName := common.FormatDatabase(instanceID, sourceDatabase)
	_ = env.EnsureDatabaseVisible(ctx, t, instance.GetName(), sourceDatabase)
	return env, ctx, instanceID, sourceDatabase, databaseName
}

func preparePostgresSourceDatabase(ctx context.Context, env *integrationenv.ServiceEnv, sourceDatabase string) error {
	if err := env.ExecPostgres(ctx, "postgres", fmt.Sprintf(`
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '%s' AND pid <> pg_backend_pid();
`, quotePostgresStringLiteral(sourceDatabase))); err != nil {
		return err
	}
	if err := env.ExecPostgres(ctx, "postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quotePostgresIdentifier(sourceDatabase))); err != nil {
		return err
	}
	if err := env.ExecPostgres(ctx, "postgres", fmt.Sprintf("CREATE DATABASE %s;", quotePostgresIdentifier(sourceDatabase))); err != nil {
		return err
	}

	return env.ExecPostgres(ctx, sourceDatabase, `
CREATE TABLE public.users (
  id INT PRIMARY KEY,
  name TEXT NOT NULL,
  age INT NOT NULL
);
CREATE TABLE public.orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount NUMERIC(10,2) NOT NULL
);
CREATE OR REPLACE VIEW public.user_order_view AS
SELECT u.id AS user_id, u.name AS user_name
FROM public.users u;
INSERT INTO public.users (id, name, age) VALUES (1, 'alice', 31);
INSERT INTO public.orders (id, user_id, amount) VALUES (1, 1, 9.99);
`)
}

func postgresServiceInstanceID(t *testing.T) string {
	t.Helper()

	const prefix = "it-pg-svc-"
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

func postgresServiceSourceDatabaseName(t *testing.T) string {
	t.Helper()

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(t.Name()))
	return fmt.Sprintf("it_app_%08x", hasher.Sum32())
}

func postgresServiceDropDatabaseName(t *testing.T) string {
	t.Helper()

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(t.Name()))
	return fmt.Sprintf("it_drop_%08x", hasher.Sum32())
}

func quotePostgresIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quotePostgresStringLiteral(name string) string {
	return strings.ReplaceAll(name, "'", "''")
}

func waitForMetaGUIDByName(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guidPrefix string, metaType storepb.MetaType, name string) string {
	t.Helper()

	var guid string

	var availableGUIDs []string
	require.Eventually(t, func() bool {
		availableGUIDs = availableGUIDs[:0]
		resources, err := env.Store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
			GUIDPrefix: &guidPrefix,
			ObjectType: &metaType,
		})
		if err != nil {
			return false
		}
		for _, resource := range resources {
			availableGUIDs = append(availableGUIDs, resource.GUID)
			if strings.HasSuffix(resource.GUID, ";"+name) {
				guid = resource.GUID
				return true
			}
		}
		return false
	}, 15*time.Second, 500*time.Millisecond, "meta not found for prefix=%s type=%s name=%s available=%v", guidPrefix, metaType.String(), name, availableGUIDs)
	return guid
}

func waitForLineageVersion(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guid string, metaType storepb.MetaType) *store.ColumnLineageVersion {
	t.Helper()

	var version *store.ColumnLineageVersion
	require.Eventually(t, func() bool {
		var err error
		version, err = env.Store.GetColumnLineageVersion(ctx, guid, metaType)
		if err != nil {
			return false
		}
		return version != nil
	}, 20*time.Second, 500*time.Millisecond, "lineage version not found for guid=%s metaType=%s", guid, metaType.String())
	return version
}

func waitForMetaRegistry(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guid string, metaType storepb.MetaType) *store.MetaRegistryResource {
	t.Helper()

	var meta *store.MetaRegistryResource
	require.Eventually(t, func() bool {
		var err error
		meta, err = env.Store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &guid, ObjectType: &metaType})
		if err != nil {
			return false
		}
		return meta != nil
	}, 20*time.Second, 500*time.Millisecond, "meta registry not found for guid=%s metaType=%s", guid, metaType.String())
	return meta
}

func waitForMetaRegistryDeleted(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guid string, metaType storepb.MetaType) {
	t.Helper()

	require.Eventually(t, func() bool {
		meta, err := env.Store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &guid, ObjectType: &metaType})
		if err != nil {
			return false
		}
		return meta == nil
	}, 20*time.Second, 500*time.Millisecond, "meta registry still exists for guid=%s metaType=%s", guid, metaType.String())
}

type metaHistorySummary struct {
	Total  int
	Open   int
	Closed int
}

func queryMetaHistorySummary(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guid string, metaType storepb.MetaType) metaHistorySummary {
	t.Helper()

	var summary metaHistorySummary
	err := env.Store.GetDB().QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE valid_to IS NULL),
			COUNT(*) FILTER (WHERE valid_to IS NOT NULL)
		FROM meta_registry_resource_history
		WHERE guid = $1 AND object_type = $2
	`, guid, metaType).Scan(&summary.Total, &summary.Open, &summary.Closed)
	require.NoError(t, err)
	return summary
}
