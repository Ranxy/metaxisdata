// Package migrator applies and upgrades the metaxisdata metadata schema.
//
// This is a port of laelia's metadata migrator
// (backend/manager/migration/migrator.go): embedded, forward-only,
// semver-ordered SQL migrations with a single cumulative baseline file for
// fresh installs.
//
// # Layout
//
// The migration tree is embedded via go:embed:
//
//	migration/
//	  LATEST.sql                  full cumulative schema at the newest version
//	  {MAJOR.MINOR}/
//	    {NNNN}##{desc}.sql        incremental migration, version MAJOR.MINOR.NNNN
//
// The current application version is 0.1, so incrementals live under
// migration/0.1/ and the baseline version is 0.1.0.
//
// # Dual-maintenance model
//
// LATEST.sql is the cumulative schema at the newest version and is applied only
// to fresh installs. Every future schema change must BOTH (a) append the
// idempotent DDL to migration/LATEST.sql AND (b) add an incremental file at
// migration/{MAJOR.MINOR}/{NNNN}##{desc}.sql. The incremental file is what
// existing deployments execute at startup; LATEST.sql is never re-run on an
// existing deployment.
package migrator

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pkg/errors"
)

const (
	// latestSchemaFileName is the cumulative baseline applied to fresh installs.
	latestSchemaFileName = "migration/LATEST.sql"

	// baselineVersion is the version recorded for an existing deployment that
	// already has the full schema but predates the migration framework. It is
	// also the floor for the latest version when there are no incremental files.
	baselineVersion = "0.1.0"

	// advisoryLockKey is an arbitrary fixed int64 used as the pg_advisory_lock
	// key so that in HA deployments only one replica runs migrations.
	advisoryLockKey = 9012345678901234

	// advisoryLockTimeout bounds the wait for the advisory lock. lock_timeout
	// applies to advisory locks too, so a replica stuck mid-migration makes this
	// startup fail loudly instead of hanging forever.
	advisoryLockTimeout = "2min"

	// schemaSentinelTable is the table whose existence distinguishes a truly
	// fresh install from an existing deployment. principal is one of the first
	// tables LATEST.sql creates.
	schemaSentinelTable = "principal"

	// schemaMigrationHistory is the version-tracking table name.
	schemaMigrationHistory = "schema_migration_history"
)

// schemaMigrationHistoryDDL creates the version ledger. It mirrors the
// declaration in migration/LATEST.sql (guarded by a unit test) and is used to
// adopt a database that predates the migration framework.
const schemaMigrationHistoryDDL = `CREATE TABLE IF NOT EXISTS schema_migration_history (
    id BIGSERIAL PRIMARY KEY,
    version TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_schema_migration_history_unique_version
    ON schema_migration_history (version);`

//go:embed migration
var migrationFS embed.FS

// MigrateSchema migrates the metadata database schema to the latest embedded
// version. It is safe to call on every server startup: fresh installs apply
// LATEST.sql, and existing deployments apply only pending incrementals. A
// session-level advisory lock serializes migrations across replicas.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrateSchemaFS(ctx, db, migrationFS)
}

// migrateSchemaFS is the testable core of MigrateSchema; fsys is the migration
// tree to draw LATEST.sql and incremental files from (the embedded tree in
// production, an fstest.MapFS in tests).
func migrateSchemaFS(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to acquire connection")
	}
	defer func() { _ = conn.Close() }()

	// Bound the acquisition wait, then drop the bound again so the migrations
	// themselves are not subject to it.
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET lock_timeout = '%s'", advisoryLockTimeout)); err != nil {
		return errors.Wrap(err, "failed to set migration lock timeout")
	}
	defer func() {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), "RESET lock_timeout"); err != nil {
			slog.Error("Failed to reset migration lock timeout", "error", err)
		}
	}()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return errors.Wrap(err, "failed to acquire migration advisory lock")
	}
	// Release on a context that is never cancelled. A cancelled startup context
	// would make the unlock fail, and the pooled connection would then hold the
	// session-level advisory lock for the life of the process, deadlocking every
	// future migration.
	defer func() {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", advisoryLockKey); err != nil {
			slog.Error("Failed to release migration advisory lock", "error", err)
		}
	}()

	files, err := getSortedVersionedFiles(fsys)
	if err != nil {
		return err
	}
	latestVersion := computeLatestVersion(files)

	schemaExists, err := tableExists(ctx, conn, schemaSentinelTable)
	if err != nil {
		return errors.Wrap(err, "failed to check schema sentinel table")
	}

	// E1: truly fresh install — apply the cumulative baseline and record the
	// latest version. LATEST.sql creates schema_migration_history itself.
	if !schemaExists {
		buf, err := fs.ReadFile(fsys, latestSchemaFileName)
		if err != nil {
			return errors.Wrapf(err, "failed to read latest schema %q", latestSchemaFileName)
		}
		if err := executeMigration(ctx, conn, string(buf), latestVersion.String()); err != nil {
			return err
		}
		slog.Info(fmt.Sprintf("Initialized database schema with version %s.", latestVersion))
		return nil
	}

	// E2: existing deployment — apply every incremental migration newer than the
	// recorded version. The version table is created by LATEST.sql on fresh
	// installs; a database that predates the framework has the schema but not
	// the ledger, so adopt it at the baseline version first.
	historyExists, err := tableExists(ctx, conn, schemaMigrationHistory)
	if err != nil {
		return errors.Wrap(err, "failed to check schema migration history table")
	}
	if !historyExists {
		if err := adoptLegacySchema(ctx, conn); err != nil {
			return err
		}
	}

	recorded, err := getLatestDatabaseVersion(ctx, conn)
	if err != nil {
		return err
	}
	if recorded == nil {
		return errors.New("the latest database version is not found")
	}
	// A binary older than the database must not run: the ledger already records
	// versions this build does not know about, so it would silently start
	// against a schema it cannot understand.
	if recorded.GT(latestVersion) {
		return errors.Errorf("database schema version %s is newer than the newest version this binary knows (%s); refusing to start", recorded, latestVersion)
	}

	for _, f := range files {
		if f.version.LE(*recorded) {
			continue
		}

		buf, err := fs.ReadFile(fsys, f.path)
		if err != nil {
			return errors.Wrapf(err, "failed to read file %q", f.path)
		}
		version := f.version.String()
		slog.Info(fmt.Sprintf("Migrating %s.", version))

		if err := executeMigration(ctx, conn, string(buf), version); err != nil {
			return err
		}
	}

	slog.Info(fmt.Sprintf("Current schema version: %s", latestVersion))
	return nil
}

// adoptLegacySchema records the baseline version for a database that already
// has the full schema but no version ledger (it predates the migration
// framework, or was created out-of-band from LATEST.sql). The schema is assumed
// to match the baseline; future incrementals then apply on top of it.
func adoptLegacySchema(ctx context.Context, conn *sql.Conn) error {
	// The sentinel table alone is too weak a signal: a partially created or
	// unrelated schema can contain `principal` without any of the rest of the
	// baseline, and the incrementals would then be applied on top of an unknown
	// shape.
	if err := verifyBaselineSchema(ctx, conn); err != nil {
		return err
	}

	slog.Warn("Database predates the schema migration framework; adopting it at the baseline version. It must already match the cumulative schema in migration/LATEST.sql.",
		"version", baselineVersion)

	txn, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin adoption transaction")
	}
	defer txn.Rollback()

	if _, err := txn.ExecContext(ctx, schemaMigrationHistoryDDL); err != nil {
		return errors.Wrap(err, "failed to create schema migration history table")
	}
	if _, err := txn.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (version) VALUES ($1)", schemaMigrationHistory),
		baselineVersion,
	); err != nil {
		return errors.Wrapf(err, "failed to record baseline version %s", baselineVersion)
	}

	return txn.Commit()
}

// baselineSchemaSentinels are tables the baseline schema always creates and that
// no incremental drops. A database that has the sentinel table but misses any of
// them is not a schema this framework can safely adopt.
var baselineSchemaSentinels = []string{
	"setting",
	"policy",
	"user_group",
	"instance",
	"db",
	"meta_registry_resource",
	"meta_registry_resource_history",
	"manual_sql",
	"column_lineage",
	"audit_log",
}

func verifyBaselineSchema(ctx context.Context, conn *sql.Conn) error {
	for _, table := range baselineSchemaSentinels {
		exists, err := tableExists(ctx, conn, table)
		if err != nil {
			return errors.Wrapf(err, "failed to check baseline table %q", table)
		}
		if !exists {
			return errors.Errorf("refusing to adopt the legacy schema: baseline table %q is missing", table)
		}
	}
	return nil
}

type versionedFile struct {
	version *semver.Version
	path    string
}

// getSortedVersionedFiles walks fsys (the embedded migration tree) and returns
// every incremental migration file, sorted ascending by semver version. The
// cumulative LATEST.sql is excluded — it is not a versioned migration.
func getSortedVersionedFiles(fsys fs.FS) ([]versionedFile, error) {
	var files []versionedFile
	if err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if path == latestSchemaFileName {
			return nil
		}

		v, err := getVersionFromPath(path)
		if err != nil {
			return err
		}
		files = append(files, versionedFile{
			version: v,
			path:    path,
		})
		return nil
	}); err != nil {
		return nil, err
	}
	slices.SortFunc(files, func(a, b versionedFile) int {
		if a.version.LT(*b.version) {
			return -1
		} else if a.version.GT(*b.version) {
			return 1
		}
		return 0
	})

	// Two files claiming the same version would both execute and the second
	// ledger insert would fail halfway through applying it, so reject the tree
	// before any migration runs.
	seen := make(map[string]string, len(files))
	for _, f := range files {
		key := f.version.String()
		if previous, ok := seen[key]; ok {
			return nil, errors.Errorf("duplicate migration version %s in %q and %q", key, previous, f.path)
		}
		seen[key] = f.path
	}
	return files, nil
}

// getVersionFromPath parses a migration path of the form
// "migration/{MAJOR.MINOR}/{NNNN}##{desc}.sql" into the semver version
// "{MAJOR.MINOR}.{NNNN}". Malformed paths return an error so a bad filename
// fails loudly at startup rather than being silently skipped.
func getVersionFromPath(path string) (*semver.Version, error) {
	s := strings.TrimPrefix(path, "migration/")
	splits := strings.Split(s, "/")
	if len(splits) != 2 {
		return nil, errors.Errorf("invalid migration path %q", path)
	}
	splits2 := strings.Split(splits[1], "##")
	if len(splits2) != 2 {
		return nil, errors.Errorf("invalid migration path %q", path)
	}
	if len(splits2[0]) != 4 {
		return nil, errors.Errorf("migration filename prefix %q must be exactly four digits such as '0001'", splits2[0])
	}
	patch, err := strconv.ParseInt(splits2[0], 10, 64)
	if err != nil {
		return nil, errors.Wrapf(err, "migration filename prefix %q should be four digits integer such as '0000'", splits2[0])
	}

	v := fmt.Sprintf("%s.%d", splits[0], patch)
	version, err := semver.Parse(v)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid version %q", v)
	}
	return &version, nil
}

// computeLatestVersion returns the newest migration version, floored at
// baselineVersion so an empty migration tree (only LATEST.sql) yields a valid
// version instead of panicking on files[len(files)-1].
func computeLatestVersion(files []versionedFile) semver.Version {
	latest := semver.MustParse(baselineVersion)
	for _, f := range files {
		if f.version.GT(latest) {
			latest = *f.version
		}
	}
	return latest
}

// executeMigration runs a migration's SQL and records its version in a single
// transaction, so a migration either fully applies and is recorded or fully
// rolls back and is not recorded. The whole file is sent as one multi-statement
// ExecContext (pgx uses the simple protocol when no args are passed, which lets
// the server parse statement boundaries and dollar-quoting).
func executeMigration(ctx context.Context, conn *sql.Conn, statement string, version string) error {
	var currentUser, currentDatabase string
	_ = conn.QueryRowContext(ctx, "SELECT current_user, current_database()").Scan(&currentUser, &currentDatabase)

	txn, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return errors.Wrap(err, "failed to begin transaction")
	}
	defer txn.Rollback()

	if _, err := txn.ExecContext(ctx, statement); err != nil {
		var sqlState string
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			sqlState = string(pgErr.Code)
		}
		stmtPreview, truncated := truncateString(statement, 100)
		if truncated {
			stmtPreview += "..."
		}
		return errors.Errorf("migration %s failed\n"+
			"Statement: %s\n"+
			"User: %s\n"+
			"Database: %s\n"+
			"Error: %v\n"+
			"SQLSTATE: %s",
			version, stmtPreview, currentUser, currentDatabase, err, sqlState)
	}
	if _, err := txn.ExecContext(ctx,
		fmt.Sprintf("INSERT INTO %s (version) VALUES ($1)", schemaMigrationHistory),
		version,
	); err != nil {
		return errors.Wrapf(err, "failed to record migration version %s", version)
	}

	return txn.Commit()
}

// getLatestDatabaseVersion returns the most recently applied schema version, or
// nil if the version table is empty.
func getLatestDatabaseVersion(ctx context.Context, conn *sql.Conn) (*semver.Version, error) {
	var v string
	if err := conn.QueryRowContext(ctx,
		fmt.Sprintf("SELECT version FROM %s ORDER BY id DESC", schemaMigrationHistory),
	).Scan(&v); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to query latest database version")
	}

	version, err := semver.Make(v)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid version %q", v)
	}
	return &version, nil
}

// tableExists reports whether a base table named table exists in the schema an
// unqualified CREATE TABLE would target (the first schema on the search_path).
// Filtering on table_schema matters: information_schema.tables spans every
// schema in the database, so a same-named table elsewhere (a tenant schema, a
// leftover) would otherwise be mistaken for the metadata schema itself.
func tableExists(ctx context.Context, conn *sql.Conn, table string) (bool, error) {
	var ok bool
	if err := conn.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE' AND table_name = $1)",
		table,
	).Scan(&ok); err != nil {
		return false, err
	}
	return ok, nil
}

// truncateString returns the first n bytes of s and whether it was truncated.
func truncateString(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	return s[:n], true
}
