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
	env, ctx, instanceID, _, databaseName := setupPostgresServiceDatabase(t)
	database := env.SyncDatabase(ctx, t, databaseName)
	require.NotNil(t, database.GetSuccessfulSyncTime())

	usersGUID := waitForMetaGUIDByName(ctx, t, env, instanceID+";it_app", storepb.MetaType_TABLE, "users")
	viewGUID := waitForMetaGUIDByName(ctx, t, env, instanceID+";it_app", storepb.MetaType_VIEW, "user_order_view")
	viewType := storepb.MetaType_VIEW
	version := waitForLineageVersion(ctx, t, env, viewGUID, viewType)
	require.Nil(t, version.ErrorMessage, "unexpected lineage analysis error: %v", version.ErrorMessage)

	relations := env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, usersGUID, "name", viewGUID, "user_name")
	})
	require.NotEmpty(t, relations)

	require.Eventually(t, func() bool {
		lineages, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &viewGUID, MetaType: &viewType})
		if err != nil {
			return false
		}
		return hasDetailedEdge(lineages, usersGUID, "name", viewGUID, "user_name")
	}, 15*time.Second, 500*time.Millisecond)
}

func TestPostgresLineageUpdatesAfterViewChangeRealServerIntegration(t *testing.T) {
	env, ctx, instanceID, _, databaseName := setupPostgresServiceDatabase(t)

	usersGUID := waitForMetaGUIDByName(ctx, t, env, instanceID+";it_app", storepb.MetaType_TABLE, "users")
	viewGUID := waitForMetaGUIDByName(ctx, t, env, instanceID+";it_app", storepb.MetaType_VIEW, "user_order_view")
	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, usersGUID, "name", viewGUID, "user_name")
	})

	require.NoError(t, env.ExecPostgres(ctx, "it_app", `
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
	env, ctx, instanceID, _, databaseName := setupPostgresServiceDatabase(t)

	viewGUID := waitForMetaGUIDByName(ctx, t, env, instanceID+";it_app", storepb.MetaType_VIEW, "user_order_view")
	env.WaitForContextLineage(ctx, t, viewGUID, v1pb.MetaType_VIEW, func(relations []*v1pb.LineageRelation) bool {
		return len(relations) > 0
	})

	require.NoError(t, env.ExecPostgres(ctx, "it_app", `DROP VIEW IF EXISTS public.user_order_view;`))
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

func TestPostgresManualSQLLineageRealServerIntegration(t *testing.T) {
	env, ctx, instanceID, _, databaseName := setupPostgresServiceDatabase(t)

	require.NoError(t, env.ExecPostgres(ctx, "it_app", `
CREATE TABLE IF NOT EXISTS public.manual_sql_summary (
  user_id INT PRIMARY KEY,
  user_name TEXT NOT NULL
);
`))
	env.SyncDatabase(ctx, t, databaseName)
	usersGUID := waitForMetaGUIDByName(ctx, t, env, instanceID+";it_app", storepb.MetaType_TABLE, "users")
	summaryGUID := waitForMetaGUIDByName(ctx, t, env, instanceID+";it_app", storepb.MetaType_TABLE, "manual_sql_summary")

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

func TestPostgresSyncInstanceMarksDroppedDatabaseDeletedRealServerIntegration(t *testing.T) {
	env := sharedPostgresServiceEnv(t)
	ctx := context.Background()

	instanceID := "it-pg-drop-db-svc"
	instance, err := env.CreatePostgresInstance(ctx, instanceID)
	require.NoError(t, err)

	require.NoError(t, env.ExecPostgres(ctx, "postgres", `CREATE DATABASE it_drop_me;`))
	require.NoError(t, env.ExecPostgres(ctx, "it_drop_me", `CREATE TABLE IF NOT EXISTS public.t1 (id INT PRIMARY KEY);`))

	resp := env.SyncInstance(ctx, t, instance.GetName(), false)
	require.Contains(t, resp.GetDatabases(), "it_drop_me")

	require.NoError(t, env.ExecPostgres(ctx, "postgres", `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'it_drop_me' AND pid <> pg_backend_pid();`))
	require.NoError(t, env.ExecPostgres(ctx, "postgres", `DROP DATABASE IF EXISTS it_drop_me;`))
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

func setupPostgresServiceDatabase(t *testing.T) (*integrationenv.ServiceEnv, context.Context, string, string, string) {
	t.Helper()

	env := sharedPostgresServiceEnv(t)
	ctx := context.Background()
	instanceID := postgresServiceInstanceID(t)
	instance, err := env.CreatePostgresInstance(ctx, instanceID)
	require.NoError(t, err)

	databaseName := common.FormatDatabase(instanceID, "it_app")
	_ = env.EnsureDatabaseVisible(ctx, t, instance.GetName(), "it_app")
	return env, ctx, instanceID, instance.GetName(), databaseName
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

func waitForMetaGUIDByName(ctx context.Context, t *testing.T, env *integrationenv.ServiceEnv, guidPrefix string, metaType storepb.MetaType, name string) string {
	t.Helper()

	var guid string
	instancePrefix := strings.SplitN(guidPrefix, ";", 2)[0]
	prefixes := []string{guidPrefix}
	if instancePrefix != guidPrefix {
		prefixes = append(prefixes, instancePrefix)
	}

	var availableGUIDs []string
	require.Eventually(t, func() bool {
		availableGUIDs = availableGUIDs[:0]
		for _, prefix := range prefixes {
			resources, err := env.Store.ListMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{
				GUIDPrefix: &prefix,
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
