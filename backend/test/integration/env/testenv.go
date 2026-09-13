package env

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/testcontainers/testcontainers-go"

	"github.com/Ranxy/metaxisdata/backend/test/integration/dockerutil"
)

const (
	postgresImage = "postgres:16-alpine"
	mysqlImage    = "mysql:8.4"

	integrationPostgresHostEnv     = "INTEGRATION_POSTGRES_HOST"
	integrationPostgresPortEnv     = "INTEGRATION_POSTGRES_PORT"
	integrationPostgresDBEnv       = "INTEGRATION_POSTGRES_DB"
	integrationPostgresUserEnv     = "INTEGRATION_POSTGRES_USER"
	integrationPostgresPasswordEnv = "INTEGRATION_POSTGRES_PASSWORD"
	integrationMySQLHostEnv        = "INTEGRATION_MYSQL_HOST"
	integrationMySQLPortEnv        = "INTEGRATION_MYSQL_PORT"
	integrationMySQLUserEnv        = "INTEGRATION_MYSQL_USER"
	integrationMySQLPasswordEnv    = "INTEGRATION_MYSQL_PASSWORD"
)

// ErrDockerUnavailable reports that no Docker-compatible container runtime could
// be reached. Callers turn it into a skip so that the suite behaves as
// documented on machines without Docker.
var ErrDockerUnavailable = dockerutil.ErrUnavailable

// ExternalServicesConfigured reports whether the harness was pointed at
// already-running services through the INTEGRATION_* variables.
func ExternalServicesConfigured() bool {
	return os.Getenv(integrationPostgresHostEnv) != "" || os.Getenv(integrationMySQLHostEnv) != ""
}

// ValidateIntegrationEnv rejects a partial external-service configuration: the
// harness either starts both databases with testcontainers or connects to both
// external services, never a mix.
func ValidateIntegrationEnv() error {
	required := []string{
		integrationPostgresHostEnv,
		integrationPostgresPortEnv,
		integrationMySQLHostEnv,
		integrationMySQLPortEnv,
	}
	var missing, present []string
	for _, name := range required {
		if os.Getenv(name) == "" {
			missing = append(missing, name)
		} else {
			present = append(present, name)
		}
	}
	if len(present) > 0 && len(missing) > 0 {
		return errors.Errorf("partial integration service configuration: %s set but %s missing; set all of them or none",
			strings.Join(present, ", "), strings.Join(missing, ", "))
	}
	return nil
}

// DockerAvailable reports whether a Docker-compatible container runtime is reachable.
func DockerAvailable(ctx context.Context) bool {
	return dockerutil.Available(ctx)
}

// TestEnv owns real DB containers and pre-wired runner dependencies.
// TestEnv owns the database containers started for one integration test.
type TestEnv struct {
	containers []testcontainers.Container
}

func getenvDefault(envName, fallback string) string {
	value, ok := os.LookupEnv(envName)
	if !ok || value == "" {
		return fallback
	}
	return value
}

func seedMySQLSchema(ctx context.Context, host, port string) error {
	dsn := mysqlDSN(host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS it_app;\nUSE it_app;\n"+MySQLFixtureSeedDDL); err != nil {
		return err
	}

	return nil
}

func resetMySQLSchema(ctx context.Context, host, port string) error {
	dsn := mysqlDSN(host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS it_drop_me;\nCREATE DATABASE IF NOT EXISTS it_app;\nUSE it_app;\n"+MySQLFixtureResetDDL); err != nil {
		return err
	}

	return nil
}

func seedPostgresSchema(ctx context.Context, host, port string) error {
	adminDB, err := sql.Open("pgx", postgresDSN(host, port, "postgres"))
	if err != nil {
		return err
	}
	defer adminDB.Close()

	if err := ensurePostgresDatabase(ctx, adminDB, "it_app"); err != nil {
		return err
	}

	appDB, err := sql.Open("pgx", postgresDSN(host, port, "it_app"))
	if err != nil {
		return err
	}
	defer appDB.Close()

	if _, err := appDB.ExecContext(ctx, PostgresFixtureSeedDDL); err != nil {
		return err
	}

	return nil
}

func resetPostgresSchema(ctx context.Context, host, port string) error {
	adminDB, err := sql.Open("pgx", postgresDSN(host, port, "postgres"))
	if err != nil {
		return err
	}
	defer adminDB.Close()

	if _, err := adminDB.ExecContext(ctx, `
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname IN ('it_app', 'it_drop_me') AND pid <> pg_backend_pid();
`); err != nil {
		return err
	}
	if _, err := adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS it_drop_me`); err != nil {
		return err
	}
	if err := ensurePostgresDatabase(ctx, adminDB, "it_app"); err != nil {
		return err
	}

	appDB, err := sql.Open("pgx", postgresDSN(host, port, "it_app"))
	if err != nil {
		return err
	}
	defer appDB.Close()

	if _, err := appDB.ExecContext(ctx, PostgresFixtureResetDDL); err != nil {
		return err
	}

	return nil
}

func ensurePostgresDatabase(ctx context.Context, db *sql.DB, databaseName string) error {
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", databaseName).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quotePostgresIdentifier(databaseName)))
	return err
}

func postgresDSN(host, port, database string) string {
	dsn := url.URL{
		Scheme: "postgres",
		User: url.UserPassword(
			getenvDefault(integrationPostgresUserEnv, "postgres"),
			getenvDefault(integrationPostgresPasswordEnv, "postgres"),
		),
		Host:     net.JoinHostPort(host, port),
		Path:     "/" + database,
		RawQuery: "sslmode=disable",
	}
	return dsn.String()
}

// mysqlDSN builds the DSN used by the harness' direct MySQL connections. The
// credentials come from INTEGRATION_MYSQL_USER/PASSWORD and default to the
// testcontainers image defaults. No database is selected: the setup SQL chooses
// its own with USE.
func mysqlDSN(host, port string) string {
	return fmt.Sprintf("%s:%s@tcp(%s)/?multiStatements=true&parseTime=true",
		getenvDefault(integrationMySQLUserEnv, "root"),
		getenvDefault(integrationMySQLPasswordEnv, "root"),
		net.JoinHostPort(host, port),
	)
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// CreateMySQLInstance stores an active MySQL instance with one ADMIN datasource.
// CreatePostgresInstance stores an active PostgreSQL instance with one ADMIN datasource.
