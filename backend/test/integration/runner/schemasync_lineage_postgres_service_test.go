//go:build integration

package runner

import (
	"context"

	"connectrpc.com/connect"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	_ "github.com/Ranxy/metaxisdata/backend/plugin/db/pg"
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

// A materialized view's output columns are absent from INFORMATION_SCHEMA.COLUMNS,
// so the sync reads them from the catalog, and a wildcard over the view expands
// to them during lineage analysis. PostgreSQL re-deparses a view's stored
// definition with any wildcard already expanded, so the wildcard that reaches
// the analyzer is the one a user wrote in manual SQL.
func TestPostgresMaterializedViewColumnsRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	require.NoError(t, env.ExecPostgres(ctx, sourceDatabase, `
DROP MATERIALIZED VIEW IF EXISTS public.user_mv;
CREATE MATERIALIZED VIEW public.user_mv AS
SELECT u.id AS user_id, u.name AS user_name
FROM public.users u;
`))
	env.SyncDatabase(ctx, t, databaseName)

	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	mvGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_MATERIALIZED_VIEW, "user_mv")
	mvMeta := waitForMetaRegistry(ctx, t, env, mvGUID, storepb.MetaType_MATERIALIZED_VIEW)
	columns := []string{}
	for _, column := range mvMeta.Metadata.GetMaterializedViewMetadata().GetColumns() {
		columns = append(columns, column.GetName())
	}
	require.Equal(t, []string{"user_id", "user_name"}, columns)

	manual := env.CreateManualSQL(ctx, t, databaseName, "select-user-mv", &v1pb.ManualSQL{
		Title:   "Select User MV",
		SqlText: "SELECT * FROM public.user_mv",
	})
	relations := env.WaitForContextLineage(ctx, t, manual.GetGuid(), v1pb.MetaType_MANUAL_SQL, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, mvGUID, "user_id", manual.GetGuid(), "user_id") &&
			hasAPILineageEdge(relations, mvGUID, "user_name", manual.GetGuid(), "user_name")
	})
	require.NotEmpty(t, relations)
}

// A foreign table's columns are ordinary catalog rows in
// INFORMATION_SCHEMA.COLUMNS (relkind 'f'), so the sync always collected them;
// only the catalog's wildcard expansion did not report them.
func TestPostgresForeignTableWildcardRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	// file_fdw is a contrib module and installing it needs a superuser, so an
	// external-service deployment may not be able to provide this shape.
	if err := env.ExecPostgres(ctx, sourceDatabase, `
CREATE EXTENSION IF NOT EXISTS file_fdw;
CREATE SERVER IF NOT EXISTS integration_file_fdw FOREIGN DATA WRAPPER file_fdw;
CREATE FOREIGN TABLE IF NOT EXISTS public.foreign_users (
  user_id INT,
  user_name TEXT
) SERVER integration_file_fdw OPTIONS (filename '/nonexistent.csv', format 'csv');
`); err != nil {
		t.Skipf("file_fdw foreign table is unavailable: %v", err)
	}
	env.SyncDatabase(ctx, t, databaseName)

	guidPrefix := fmt.Sprintf("%s;%s", instanceID, sourceDatabase)
	foreignGUID := waitForMetaGUIDByName(ctx, t, env, guidPrefix, storepb.MetaType_EXTERNAL_TABLE, "foreign_users")
	foreignMeta := waitForMetaRegistry(ctx, t, env, foreignGUID, storepb.MetaType_EXTERNAL_TABLE)
	require.Len(t, foreignMeta.Metadata.GetExternalTableMetadata().GetColumns(), 2)

	manual := env.CreateManualSQL(ctx, t, databaseName, "select-foreign-users", &v1pb.ManualSQL{
		Title:   "Select Foreign Users",
		SqlText: "SELECT * FROM public.foreign_users",
	})
	relations := env.WaitForContextLineage(ctx, t, manual.GetGuid(), v1pb.MetaType_MANUAL_SQL, func(relations []*v1pb.LineageRelation) bool {
		return hasAPILineageEdge(relations, foreignGUID, "user_id", manual.GetGuid(), "user_id") &&
			hasAPILineageEdge(relations, foreignGUID, "user_name", manual.GetGuid(), "user_name")
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
	var (
		metaLeft    bool
		lineageLeft int
	)
	dropped := assert.Eventually(t, func() bool {
		meta, err := env.Store.GetMetaRegistry(ctx, &store.FindMetaRegistryResourceMessage{GUID: &viewGUID, ObjectType: &viewType})
		if err != nil {
			return false
		}
		metaLeft = meta != nil

		lineages, err := env.Store.ListColumnLineage(ctx, &store.FindColumnLineageMessage{MetaGUID: &viewGUID, MetaType: &viewType})
		if err != nil {
			return false
		}
		lineageLeft = len(lineages)

		return !metaLeft && lineageLeft == 0
	}, 20*time.Second, 500*time.Millisecond)
	require.Truef(t, dropped, "the dropped view still has a meta row=%t and %d lineage rows", metaLeft, lineageLeft)

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

	// Deliberately mixed-case and unquoted: PostgreSQL folds these to
	// public.manual_sql_summary / public.users, and the analyzer must emit the
	// folded names so the source GUID resolves to the registry (PG-FU-3).
	manual := env.CreateManualSQL(ctx, t, databaseName, "sync-active-users", &v1pb.ManualSQL{
		Title:   "Sync Active Users",
		SqlText: "INSERT INTO Public.Manual_SQL_Summary (User_ID, User_Name) SELECT ID, Name FROM Public.Users",
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

	env, ctx, _, _, databaseName := setupPostgresServiceDatabase(t)
	env.SyncDatabase(ctx, t, databaseName)
	asOfBeforeCreate := time.Now().UTC()

	manual := env.CreateManualSQL(ctx, t, databaseName, "history-active-users", &v1pb.ManualSQL{
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
		dropped, err := env.Store.GetDatabase(ctx, &store.FindDatabaseMessage{InstanceID: &instanceID, DatabaseName: &databaseName, ShowDeleted: true})
		if err != nil || dropped == nil {
			return false
		}
		return dropped.Deleted
	}, 15*time.Second, 500*time.Millisecond)
}

// A per-database sync is an observation that the database is gone: the target
// answers "database does not exist", so the row must be hidden right away rather
// than staying visible (with a failing sync) until the next instance enumeration.
func TestPostgresPerDatabaseSyncHidesDroppedDatabaseRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, databaseName := setupPostgresServiceDatabase(t)
	env.SyncDatabase(ctx, t, databaseName)

	require.NoError(t, env.ExecPostgres(ctx, "postgres", fmt.Sprintf(`
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '%s' AND pid <> pg_backend_pid();
`, quotePostgresStringLiteral(sourceDatabase))))
	require.NoError(t, env.ExecPostgres(ctx, "postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quotePostgresIdentifier(sourceDatabase))))

	requireDroppedDatabaseHidden(t, ctx, env, instanceID, sourceDatabase, databaseName)
}

// requireDroppedDatabaseHidden drives the per-database sync of a database the target has
// dropped, and asserts the row ends up hidden without waiting for an instance
// enumeration.
//
// A queued instance-wide sync can make that observation first: the fixture's
// EnsureDatabaseVisible goes through SyncInstance, which enqueues the discovered
// database into the checker's ten-second queue, and that entry outlives the fixture's
// own direct sync. The server refuses to sync an already-hidden database rather than
// hiding it again, so NotFound from this call is the same observation arriving first —
// what the test pins is the end state.
func requireDroppedDatabaseHidden(t *testing.T, ctx context.Context, env *integrationenv.ServiceEnv, instanceID, sourceDatabase, databaseName string) {
	t.Helper()

	if err := env.SyncDatabaseRaw(ctx, databaseName); err != nil {
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err),
			"only a database that is already hidden explains a refused sync: %v", err)
	}

	row, err := env.Store.GetDatabase(ctx, &store.FindDatabaseMessage{InstanceID: &instanceID, DatabaseName: &sourceDatabase, ShowDeleted: true})
	require.NoError(t, err)
	require.NotNil(t, row)
	require.True(t, row.Deleted, "a database the target no longer has must not stay visible")
}

// A queued entry that outlives the instance sync which hid its database must not
// show the row again: only an instance enumeration decides visibility, and no
// enumeration runs inside this window.
func TestPostgresDeletedDatabaseIsNotShownByASyncRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, sourceDatabase, _ := setupPostgresServiceDatabase(t)
	instance, err := env.GetInstance(ctx, common.FormatInstance(instanceID))
	require.NoError(t, err)

	// Queue every database of the instance, then hide the row immediately: the
	// queued entry still carries Deleted=false, so the checker picks it up on its
	// next tick and syncs a database the registry says is gone.
	env.SyncInstance(ctx, t, instance.GetName(), true)
	deleted := true
	_, err = env.Store.UpdateDatabase(ctx, &store.UpdateDatabaseMessage{
		InstanceID:   instanceID,
		DatabaseName: sourceDatabase,
		Deleted:      &deleted,
	})
	require.NoError(t, err)

	require.Never(t, func() bool {
		row, err := env.Store.GetDatabase(context.Background(), &store.FindDatabaseMessage{InstanceID: &instanceID, DatabaseName: &sourceDatabase, ShowDeleted: true})
		return err == nil && row != nil && !row.Deleted
	}, 15*time.Second, 500*time.Millisecond)
}

// Two syncs of one database must not run at once: with a single outstanding
// connection allowed, both requests can only succeed if one waits for the other
// instead of opening its own.
func TestPostgresConcurrentDatabaseSyncCoalescesRealServerIntegration(t *testing.T) {
	t.Parallel()

	env, ctx, instanceID, _, databaseName := setupPostgresServiceDatabase(t)

	// CreateInstance queues every database, and the checker drains that queue
	// within a tick; let it finish so no periodic sync competes below.
	time.Sleep(11 * time.Second)
	require.NoError(t, env.SetInstanceMaximumConnections(ctx, common.FormatInstance(instanceID), 1))

	const callers = 4
	start := make(chan struct{})
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			<-start
			errs[i] = env.SyncDatabaseRaw(ctx, databaseName)
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoErrorf(t, err, "concurrent sync %d must reuse the running sync's result", i)
	}
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
	// Sync the fixture through the per-database call instead of leaving it to the
	// instance-wide pass. That pass is a 10 second ticker that discovers every
	// database on the shared server, and the per-instance limit of 10 connections
	// makes it drop some of them until its next tick, so waiting for this
	// database's objects races the rest of the suite.
	env.SyncDatabase(ctx, t, databaseName)
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

	return env.ExecPostgres(ctx, sourceDatabase, integrationenv.PostgresFixtureResetDDL)
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

	var (
		guid           string
		availableGUIDs []string
	)
	found := assert.Eventually(t, func() bool {
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
	}, 15*time.Second, 500*time.Millisecond)
	require.Truef(t, found, "meta not found for prefix=%s type=%s name=%s available=%v", guidPrefix, metaType.String(), name, availableGUIDs)
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
