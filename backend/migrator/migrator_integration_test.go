//go:build integration

package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
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
	ctx := context.Background()

	host, port := startPostgres(t)
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
