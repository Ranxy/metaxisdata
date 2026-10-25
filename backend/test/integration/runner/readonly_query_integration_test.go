//go:build integration

package runner

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/db"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Quality rules execute through the READ_ONLY data source only. These scenarios
// prove the two halves against the real engines: the factory refuses an instance
// without a read-only source, and the driver runs a SELECT, caps what it
// returns, and refuses a write.
func TestPostgresReadOnlyQueryRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedPostgresServiceEnvNoReset(t)
	ctx := context.Background()
	sourceDatabase := postgresServiceSourceDatabaseName(t)
	require.NoError(t, preparePostgresSourceDatabase(ctx, env, sourceDatabase))
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_ = env.ExecPostgres(cleanupCtx, "postgres", fmt.Sprintf(`
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '%s' AND pid <> pg_backend_pid();
`, quotePostgresStringLiteral(sourceDatabase)))
		_ = env.ExecPostgres(cleanupCtx, "postgres", fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quotePostgresIdentifier(sourceDatabase)))
	})

	factory := dbfactory.New(env.Store)
	resourceID := readonlyResourceID(t)
	username, password := readonlyCredentials("INTEGRATION_POSTGRES_USER", "INTEGRATION_POSTGRES_PASSWORD", "postgres", "postgres")
	database := &store.DatabaseMessage{InstanceID: resourceID, DatabaseName: sourceDatabase}

	adminOnly := readonlyInstance(resourceID+"-admin", storepb.Engine_POSTGRES, env.PostgresHost, env.PostgresPort, sourceDatabase, username, password, false)
	driver, err := factory.GetReadOnlyDatabaseDriver(ctx, adminOnly, database)
	require.Nil(t, driver)
	require.ErrorIs(t, err, dbfactory.ErrNoReadOnlyDataSource)

	instance := readonlyInstance(resourceID, storepb.Engine_POSTGRES, env.PostgresHost, env.PostgresPort, sourceDatabase, username, password, true)
	driver, err = factory.GetReadOnlyDatabaseDriver(ctx, instance, database)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, driver.Close(context.Background()))
	})
	queryer, ok := driver.(db.ReadOnlyQueryer)
	require.True(t, ok, "the PostgreSQL driver must implement ReadOnlyQueryer")

	result, err := queryer.QueryReadOnly(ctx, "SELECT id, name, age FROM public.users ORDER BY id", nil, db.QueryOptions{MaxRows: 10})
	require.NoError(t, err)
	require.Equal(t, []string{"id", "name", "age"}, result.Columns)
	require.Equal(t, [][]any{{int64(1), "alice", int64(31)}}, result.Rows)
	require.False(t, result.Truncated)

	result, err = queryer.QueryReadOnly(ctx, "SELECT n FROM generate_series(1, 3) AS n", nil, db.QueryOptions{MaxRows: 2})
	require.NoError(t, err)
	require.Len(t, result.Rows, 2)
	require.True(t, result.Truncated)

	result, err = queryer.QueryReadOnly(ctx, "SELECT 'abcdef'", nil, db.QueryOptions{MaxBytes: 3})
	require.NoError(t, err)
	require.Empty(t, result.Rows)
	require.True(t, result.Truncated)

	_, err = queryer.QueryReadOnly(ctx, "INSERT INTO public.users (id, name, age) VALUES (2, 'bob', 20)", nil, db.QueryOptions{})
	requireReadOnlyRejection(t, err)

	started := time.Now()
	_, err = queryer.QueryReadOnly(ctx, "SELECT pg_sleep(5)", nil, db.QueryOptions{Timeout: 250 * time.Millisecond})
	requireTimeout(t, err, started)
}

func TestMySQLReadOnlyQueryRealServerIntegration(t *testing.T) {
	t.Parallel()

	env := sharedMySQLServiceEnvNoReset(t)
	ctx := context.Background()
	sourceDatabase := mysqlServiceSourceDatabaseName(t)
	require.NoError(t, prepareMySQLSourceDatabase(ctx, env, sourceDatabase))
	t.Cleanup(func() {
		_ = env.ExecMySQL(context.Background(), fmt.Sprintf("DROP DATABASE IF EXISTS %s;", quoteMySQLIdentifier(sourceDatabase)))
	})

	factory := dbfactory.New(env.Store)
	resourceID := readonlyResourceID(t)
	username, password := readonlyCredentials("INTEGRATION_MYSQL_USER", "INTEGRATION_MYSQL_PASSWORD", "root", "root")
	database := &store.DatabaseMessage{InstanceID: resourceID, DatabaseName: sourceDatabase}

	adminOnly := readonlyInstance(resourceID+"-admin", storepb.Engine_MYSQL, env.MySQLHost, env.MySQLPort, sourceDatabase, username, password, false)
	driver, err := factory.GetReadOnlyDatabaseDriver(ctx, adminOnly, database)
	require.Nil(t, driver)
	require.ErrorIs(t, err, dbfactory.ErrNoReadOnlyDataSource)

	instance := readonlyInstance(resourceID, storepb.Engine_MYSQL, env.MySQLHost, env.MySQLPort, sourceDatabase, username, password, true)
	driver, err = factory.GetReadOnlyDatabaseDriver(ctx, instance, database)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, driver.Close(context.Background()))
	})
	queryer, ok := driver.(db.ReadOnlyQueryer)
	require.True(t, ok, "the MySQL driver must implement ReadOnlyQueryer")

	result, err := queryer.QueryReadOnly(ctx, "SELECT id, name, age FROM users ORDER BY id", nil, db.QueryOptions{MaxRows: 10})
	require.NoError(t, err)
	require.Equal(t, []string{"id", "name", "age"}, result.Columns)
	require.Equal(t, [][]any{{int64(1), "alice", int64(31)}}, result.Rows)
	require.False(t, result.Truncated)

	result, err = queryer.QueryReadOnly(ctx, "SELECT n FROM (SELECT 1 AS n UNION ALL SELECT 2 UNION ALL SELECT 3) AS series", nil, db.QueryOptions{MaxRows: 2})
	require.NoError(t, err)
	require.Len(t, result.Rows, 2)
	require.True(t, result.Truncated)

	result, err = queryer.QueryReadOnly(ctx, "SELECT 'abcdef'", nil, db.QueryOptions{MaxBytes: 3})
	require.NoError(t, err)
	require.Empty(t, result.Rows)
	require.True(t, result.Truncated)

	_, err = queryer.QueryReadOnly(ctx, "INSERT INTO users (id, name, age) VALUES (2, 'bob', 20)", nil, db.QueryOptions{})
	requireReadOnlyRejection(t, err)

	// The deadline has to be what reports the timeout: MySQL's own
	// MAX_EXECUTION_TIME interrupt is not enough, because it reports an
	// interrupted SLEEP as the successful row 1 instead of an error.
	started := time.Now()
	_, err = queryer.QueryReadOnly(ctx, "SELECT SLEEP(5)", nil, db.QueryOptions{Timeout: 250 * time.Millisecond})
	requireTimeout(t, err, started)
}

func readonlyResourceID(t *testing.T) string {
	t.Helper()

	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(t.Name()))
	return fmt.Sprintf("it-readonly-%08x", hasher.Sum32())
}

func readonlyCredentials(userEnv, passwordEnv, fallbackUser, fallbackPassword string) (string, string) {
	return readonlyEnvOrDefault(userEnv, fallbackUser), readonlyEnvOrDefault(passwordEnv, fallbackPassword)
}

func readonlyEnvOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func readonlyInstance(resourceID string, engine storepb.Engine, host, port, database, username, password string, withReadOnly bool) *store.InstanceMessage {
	dataSources := []*storepb.DataSource{readonlyDataSource(storepb.DataSourceType_ADMIN, host, port, database, username, password)}
	if withReadOnly {
		dataSources = append(dataSources, readonlyDataSource(storepb.DataSourceType_READ_ONLY, host, port, database, username, password))
	}
	return &store.InstanceMessage{
		ResourceID: resourceID,
		Metadata: &storepb.Instance{
			Title:       resourceID,
			Engine:      engine,
			DataSources: dataSources,
		},
	}
}

func readonlyDataSource(sourceType storepb.DataSourceType, host, port, database, username, password string) *storepb.DataSource {
	return &storepb.DataSource{
		Type:     sourceType,
		Username: username,
		Password: password,
		Host:     host,
		Port:     port,
		Database: database,
	}
}

// requireReadOnlyRejection accepts both spellings the engines use:
// PostgreSQL says "read-only transaction", MySQL says "READ ONLY transaction".
func requireReadOnlyRejection(t *testing.T, err error) {
	t.Helper()

	require.Error(t, err)
	message := strings.ToLower(err.Error())
	require.True(t, strings.Contains(message, "read-only") || strings.Contains(message, "read only"),
		"expected the engine to reject a write in a read-only transaction, got: %v", err)
}

func requireTimeout(t *testing.T, err error, started time.Time) {
	t.Helper()

	require.Error(t, err)
	require.Less(t, time.Since(started), 4*time.Second, "the query must be stopped by its timeout instead of completing")
}
