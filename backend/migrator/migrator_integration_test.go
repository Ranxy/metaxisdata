//go:build integration

package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/blang/semver/v4"
	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" stdlib driver.
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/Ranxy/metaxisdata/backend/test/integration/dockerutil"
)

// These tests exercise the migrator against a real PostgreSQL started with
// testcontainers. They are gated by the `integration` build tag and skipped when
// Docker is unavailable, matching the rest of the integration suite.

func TestMigrateSchemaFreshInstall(t *testing.T) {
	db := newTestDatabase(t)
	ctx := context.Background()

	latest := embeddedLatestVersion(t).String()

	require.NoError(t, MigrateSchema(ctx, db))
	require.True(t, tableExistsQ(t, db, "principal"), "principal table missing after fresh install")
	require.True(t, tableExistsQ(t, db, schemaMigrationHistory), "schema_migration_history missing after fresh install")
	assertHistoryVersions(t, db, latest)

	// A second run is a no-op: still one row, no error.
	require.NoError(t, MigrateSchema(ctx, db))
	assertHistoryVersions(t, db, latest)
}

func TestMigrateSchemaUpgrade(t *testing.T) {
	db := newTestDatabase(t)
	ctx := context.Background()

	latest := embeddedLatestVersion(t)
	require.NoError(t, MigrateSchema(ctx, db))
	assertHistoryVersions(t, db, latest.String())

	// Inject a fake newer incremental via a test FS. The upgrade path does not
	// re-read LATEST.sql (the schema already exists), so the test FS only needs
	// the incremental file.
	latestBuf, err := fs.ReadFile(migrationFS, latestSchemaFileName)
	require.NoError(t, err)
	incrementalPath, incrementalVersion := nextVersionPath(latest)
	testFS := fstest.MapFS{
		latestSchemaFileName: {Data: latestBuf},
		incrementalPath:      {Data: []byte("CREATE TABLE IF NOT EXISTS test_upgrade_marker (id int);")},
	}

	require.NoError(t, migrateSchemaFS(ctx, db, testFS))
	require.True(t, tableExistsQ(t, db, "test_upgrade_marker"), "upgrade migration did not create its marker table")
	assertHistoryVersions(t, db, latest.String(), incrementalVersion)
}

// TestMigrateSchemaLegacyAdoption covers a database created out-of-band from
// LATEST.sql before the framework existed: it has the schema but no version
// ledger. The migrator must adopt it at the baseline version and then apply
// pending incrementals on top.
func TestMigrateSchemaLegacyAdoption(t *testing.T) {
	db := newTestDatabase(t)
	ctx := context.Background()

	latest := embeddedLatestVersion(t)
	require.NoError(t, MigrateSchema(ctx, db))

	// Simulate the pre-framework database: drop the ledger LATEST.sql created.
	_, err := db.ExecContext(ctx, "DROP TABLE schema_migration_history")
	require.NoError(t, err)

	latestBuf, err := fs.ReadFile(migrationFS, latestSchemaFileName)
	require.NoError(t, err)
	incrementalPath, incrementalVersion := nextVersionPath(latest)
	testFS := fstest.MapFS{
		latestSchemaFileName: {Data: latestBuf},
		incrementalPath:      {Data: []byte("CREATE TABLE IF NOT EXISTS test_adoption_marker (id int);")},
	}

	require.NoError(t, migrateSchemaFS(ctx, db, testFS))
	require.True(t, tableExistsQ(t, db, "test_adoption_marker"), "incremental did not run after legacy adoption")
	assertHistoryVersions(t, db, baselineVersion, incrementalVersion)
}

// TestMigrateSchemaLATESTMatchesTheIncrementChain guards the dual-maintenance
// model: applying every incremental on top of LATEST.sql must be a no-op, i.e.
// the incrementals and the cumulative file must not disagree about the schema.
func TestMigrateSchemaLATESTMatchesTheIncrementChain(t *testing.T) {
	ctx := context.Background()

	// Both databases live in one container: starting a container per database
	// made this test needlessly heavy inside the full integration suite.
	host, port := startPostgres(t)
	fresh := newTestDatabaseIn(t, host, port)
	require.NoError(t, MigrateSchema(ctx, fresh))

	chained := newTestDatabaseIn(t, host, port)
	latestBuf, err := fs.ReadFile(migrationFS, latestSchemaFileName)
	require.NoError(t, err)
	_, err = chained.ExecContext(ctx, string(latestBuf))
	require.NoError(t, err)

	files, err := getSortedVersionedFiles(migrationFS)
	require.NoError(t, err)
	for _, f := range files {
		buf, err := fs.ReadFile(migrationFS, f.path)
		require.NoError(t, err)
		_, err = chained.ExecContext(ctx, string(buf))
		require.NoErrorf(t, err, "incremental %s must apply cleanly on top of LATEST.sql", f.path)
	}

	require.Equal(t, catalogSnapshot(t, fresh), catalogSnapshot(t, chained),
		"LATEST.sql and the increment chain describe different schemas")
}

// TestMigrateSchemaSerializesConcurrentReplicas covers the advisory lock: two
// replicas starting at once must not both apply the baseline.
func TestMigrateSchemaSerializesConcurrentReplicas(t *testing.T) {
	db := newTestDatabase(t)
	ctx := context.Background()

	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			errs <- MigrateSchema(ctx, db)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	// Only the first replica migrated; the ledger must hold exactly one row.
	assertHistoryVersions(t, db, embeddedLatestVersion(t).String())
}

// catalogSnapshot renders the tables, columns and indexes of the current schema
// as sorted strings so two databases can be compared.
func catalogSnapshot(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
		SELECT table_name || '.' || column_name || ':' || data_type || ':' || is_nullable
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		UNION ALL
		SELECT indexname || ':' || indexdef
		FROM pg_indexes
		WHERE schemaname = current_schema()
		ORDER BY 1`)
	require.NoError(t, err)
	defer rows.Close()

	var snapshot []string
	for rows.Next() {
		var entry string
		require.NoError(t, rows.Scan(&entry))
		snapshot = append(snapshot, entry)
	}
	require.NoError(t, rows.Err())
	return snapshot
}

// --- helpers ---

// embeddedLatestVersion computes the newest version of the real embedded
// migration tree, mirroring what a fresh install records. Hard-coding the number
// here would break the gate every time an incremental lands.
func embeddedLatestVersion(t *testing.T) semver.Version {
	t.Helper()
	files, err := getSortedVersionedFiles(migrationFS)
	require.NoError(t, err)
	return computeLatestVersion(files)
}

// nextVersionPath returns an injected-incremental filename one patch above the
// given version, e.g. latest 0.1.0 -> migration/0.1/0001##test.sql.
func nextVersionPath(v semver.Version) (path, version string) {
	next := semver.Version{Major: v.Major, Minor: v.Minor, Patch: v.Patch + 1}
	return fmt.Sprintf("migration/%d.%d/%04d##test.sql", next.Major, next.Minor, next.Patch), next.String()
}

func tableExistsQ(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var ok bool
	require.NoError(t, db.QueryRowContext(context.Background(),
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)",
		table).Scan(&ok))
	return ok
}

func assertHistoryVersions(t *testing.T, db *sql.DB, want ...string) {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		fmt.Sprintf("SELECT version FROM %s ORDER BY id ASC", schemaMigrationHistory))
	require.NoError(t, err)
	defer rows.Close()

	var got []string
	for rows.Next() {
		var v string
		require.NoError(t, rows.Scan(&v))
		got = append(got, v)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, want, got)
}

var testDBCounter int64

// newTestDatabase starts a PostgreSQL container and creates a uniquely named
// throwaway database inside it.
func newTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	host, port := startPostgres(t)
	return newTestDatabaseIn(t, host, port)
}

// newTestDatabaseIn creates a throwaway database inside an already running
// PostgreSQL, so one test can build several databases without starting a
// container per database.
func newTestDatabaseIn(t *testing.T, host, port string) *sql.DB {
	t.Helper()
	ctx := context.Background()

	admin, err := sql.Open("pgx", testDSN(host, port, "metaxisdata"))
	require.NoError(t, err)
	// t.Cleanup is LIFO: the drop below runs before this close.
	t.Cleanup(func() { _ = admin.Close() })

	name := fmt.Sprintf("migrator_test_%d_%d", time.Now().UnixNano(), atomic.AddInt64(&testDBCounter, 1))
	_, err = admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`)
	require.NoError(t, err)

	db, err := sql.Open("pgx", testDSN(host, port, name))
	require.NoError(t, err)
	t.Cleanup(func() {
		// Close the pool before dropping, then fail the test if the drop does
		// not happen — a leaked test database used to be swallowed silently.
		_ = db.Close()
		_, dropErr := admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
		require.NoError(t, dropErr)
	})
	return db
}

func startPostgres(t *testing.T) (host, port string) {
	t.Helper()
	ctx := context.Background()
	if !dockerutil.Available(ctx) {
		t.Skip("docker is unavailable")
	}
	req := testcontainers.ContainerRequest{
		Image: "postgres:16-alpine",
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "metaxisdata",
		},
		ExposedPorts: []string{"5432/tcp"},
		WaitingFor:   wait.ForListeningPort("5432/tcp").WithStartupTimeout(90 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		require.Truef(t, dockerutil.IsUnavailable(err), "failed to start PostgreSQL container: %v", err)
		t.Skipf("docker is unavailable: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	host, err = container.Host(ctx)
	require.NoError(t, err)
	mapped, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)
	return host, mapped.Port()
}

func testDSN(host, port, database string) string {
	return fmt.Sprintf("postgres://postgres:postgres@%s:%s/%s?sslmode=disable", host, port, database)
}
