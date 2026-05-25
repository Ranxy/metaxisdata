package env

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/runner/lineageanalyzer"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	postgresImage = "postgres:16-alpine"
	mysqlImage    = "mysql:8.4"

	integrationPostgresHostEnv = "INTEGRATION_POSTGRES_HOST"
	integrationPostgresPortEnv = "INTEGRATION_POSTGRES_PORT"
	integrationPostgresDBEnv   = "INTEGRATION_POSTGRES_DB"
	integrationMySQLHostEnv    = "INTEGRATION_MYSQL_HOST"
	integrationMySQLPortEnv    = "INTEGRATION_MYSQL_PORT"
)

// TestEnv owns real DB containers and pre-wired runner dependencies.
type TestEnv struct {
	Store    *store.Store
	Analyzer *lineageanalyzer.Analyzer
	Syncer   *schemasync.Syncer

	PostgresHost string
	PostgresPort string
	MySQLHost    string
	MySQLPort    string

	containers []testcontainers.Container
}

// ExecMySQL executes SQL against the MySQL server used by the test env.
func (e *TestEnv) ExecMySQL(ctx context.Context, statement string) error {
	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/?multiStatements=true&parseTime=true", e.MySQLHost, e.MySQLPort)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, statement)
	return err
}

// SetupMySQLEnv starts PostgreSQL + MySQL and wires Store/Syncer/Analyzer.
func SetupMySQLEnv(t *testing.T) *TestEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	env := &TestEnv{}

	pgHost, pgPort, pgDBName := startPostgres(ctx, t, env)
	env.PostgresHost = pgHost
	env.PostgresPort = pgPort
	pgURL := fmt.Sprintf("postgres://postgres:postgres@%s:%s/%s?sslmode=disable", pgHost, pgPort, pgDBName)

	stores, err := store.New(ctx, pgURL, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stores.Close() })
	env.Store = stores

	require.NoError(t, applyMigratorSQL(ctx, stores.GetDB()))
	_, err = stores.UpsertSettingV2(ctx, &store.SetSettingMessage{
		Name:  storepb.SettingName_AUTH_SECRET,
		Value: "integration-test-secret",
	})
	require.NoError(t, err)

	mysqlHost, mysqlPort := startMySQL(ctx, t, env)
	env.MySQLHost = mysqlHost
	env.MySQLPort = mysqlPort

	require.NoError(t, seedMySQLSchema(ctx, mysqlHost, mysqlPort))

	lineage.InitCatalogProvide(stores)
	analyzer := lineageanalyzer.NewAnalyzer(stores, &config.Profile{})
	stateCfg, err := state.New()
	require.NoError(t, err)
	syncer := schemasync.NewSyncer(stores, dbfactory.New(stores), &config.Profile{}, stateCfg, analyzer)
	env.Analyzer = analyzer
	env.Syncer = syncer

	t.Cleanup(func() {
		for _, c := range env.containers {
			_ = c.Terminate(context.Background())
		}
	})

	return env
}

func startPostgres(ctx context.Context, t *testing.T, env *TestEnv) (host, port, dbName string) {
	t.Helper()

	const db = "metaxisdata"
	if cfg, ok := lookupIntegrationServiceEnv(t, integrationPostgresHostEnv, integrationPostgresPortEnv); ok {
		host = cfg[integrationPostgresHostEnv]
		port = cfg[integrationPostgresPortEnv]
		dbName = getenvDefault(integrationPostgresDBEnv, db)

		waitForPostgresReady(ctx, t, host, port, dbName)
		return host, port, dbName
	}

	req := testcontainers.ContainerRequest{
		Image: postgresImage,
		Env: map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       db,
		},
		ExposedPorts: []string{"5432/tcp"},
		WaitingFor: wait.ForListeningPort("5432/tcp").
			WithStartupTimeout(90 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		skipIfDockerUnavailable(t, err)
		require.NoError(t, err)
	}
	env.containers = append(env.containers, container)

	host, err = container.Host(ctx)
	require.NoError(t, err)
	mappedPort, err := container.MappedPort(ctx, "5432/tcp")
	require.NoError(t, err)

	waitForPostgresReady(ctx, t, host, mappedPort.Port(), db)

	return host, mappedPort.Port(), db
}

func startMySQL(ctx context.Context, t *testing.T, env *TestEnv) (host, port string) {
	t.Helper()
	if cfg, ok := lookupIntegrationServiceEnv(t, integrationMySQLHostEnv, integrationMySQLPortEnv); ok {
		host = cfg[integrationMySQLHostEnv]
		port = cfg[integrationMySQLPortEnv]

		waitForMySQLReady(ctx, t, host, port)
		return host, port
	}

	req := testcontainers.ContainerRequest{
		Image: mysqlImage,
		Env: map[string]string{
			"MYSQL_ROOT_PASSWORD": "root",
		},
		ExposedPorts: []string{"3306/tcp"},
		WaitingFor: wait.ForListeningPort("3306/tcp").
			WithStartupTimeout(120 * time.Second),
	}
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		skipIfDockerUnavailable(t, err)
		require.NoError(t, err)
	}
	env.containers = append(env.containers, container)

	host, err = container.Host(ctx)
	require.NoError(t, err)
	mappedPort, err := container.MappedPort(ctx, "3306/tcp")
	require.NoError(t, err)

	waitForMySQLReady(ctx, t, host, mappedPort.Port())

	return host, mappedPort.Port()
}

func waitForPostgresReady(ctx context.Context, t *testing.T, host, port, dbName string) {
	t.Helper()

	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/%s?sslmode=disable", host, port, dbName)
	require.Eventually(t, func() bool {
		stores, openErr := store.New(ctx, dsn, false)
		if openErr != nil {
			return false
		}
		_ = stores.Close()
		return true
	}, 45*time.Second, 1*time.Second)
}

func waitForMySQLReady(ctx context.Context, t *testing.T, host, port string) {
	t.Helper()

	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/?multiStatements=true&parseTime=true", host, port)
	require.Eventually(t, func() bool {
		db, openErr := sql.Open("mysql", dsn)
		if openErr != nil {
			return false
		}
		defer db.Close()
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return db.PingContext(pingCtx) == nil
	}, 45*time.Second, 1*time.Second)
}

func lookupIntegrationServiceEnv(t *testing.T, envNames ...string) (map[string]string, bool) {
	t.Helper()

	values := make(map[string]string, len(envNames))
	hasAny := false
	for _, envName := range envNames {
		value, ok := os.LookupEnv(envName)
		if ok && value != "" {
			hasAny = true
			values[envName] = value
		}
	}
	if !hasAny {
		return nil, false
	}

	for _, envName := range envNames {
		value := os.Getenv(envName)
		require.NotEmpty(t, value, "environment variable %s must be set when using external integration services", envName)
		values[envName] = value
	}
	return values, true
}

func getenvDefault(envName, fallback string) string {
	value, ok := os.LookupEnv(envName)
	if !ok || value == "" {
		return fallback
	}
	return value
}

func seedMySQLSchema(ctx context.Context, host, port string) error {
	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/?multiStatements=true&parseTime=true", host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
CREATE DATABASE IF NOT EXISTS it_app;
USE it_app;
CREATE TABLE IF NOT EXISTS users (
  id INT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  age INT NOT NULL
);
CREATE TABLE IF NOT EXISTS orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount DECIMAL(10,2) NOT NULL
);
CREATE OR REPLACE VIEW user_order_view AS
SELECT u.id AS user_id, u.name AS user_name, o.amount AS order_amount
FROM users u
JOIN orders o ON u.id = o.user_id;
INSERT INTO users (id, name, age) VALUES (1, 'alice', 31)
  ON DUPLICATE KEY UPDATE name = VALUES(name), age = VALUES(age);
INSERT INTO orders (id, user_id, amount) VALUES (1, 1, 9.99)
  ON DUPLICATE KEY UPDATE user_id = VALUES(user_id), amount = VALUES(amount);
`); err != nil {
		return err
	}

	return nil
}

func resetMySQLSchema(ctx context.Context, host, port string) error {
	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/?multiStatements=true&parseTime=true", host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
DROP DATABASE IF EXISTS it_drop_me;
CREATE DATABASE IF NOT EXISTS it_app;
USE it_app;
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
`); err != nil {
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

	if _, err := appDB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS public.users (
  id INT PRIMARY KEY,
  name TEXT NOT NULL,
  age INT NOT NULL
);
CREATE TABLE IF NOT EXISTS public.orders (
  id INT PRIMARY KEY,
  user_id INT NOT NULL,
  amount NUMERIC(10,2) NOT NULL
);
CREATE OR REPLACE VIEW public.user_order_view AS
SELECT u.id AS user_id, u.name AS user_name
FROM public.users u;
INSERT INTO public.users (id, name, age) VALUES (1, 'alice', 31)
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, age = EXCLUDED.age;
INSERT INTO public.orders (id, user_id, amount) VALUES (1, 1, 9.99)
ON CONFLICT (id) DO UPDATE SET user_id = EXCLUDED.user_id, amount = EXCLUDED.amount;
`); err != nil {
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

	if _, err := appDB.ExecContext(ctx, `
DROP VIEW IF EXISTS public.user_order_view;
DROP TABLE IF EXISTS public.manual_sql_summary;
DROP TABLE IF EXISTS public.orders;
DROP TABLE IF EXISTS public.users;
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
`); err != nil {
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
	return fmt.Sprintf("postgres://postgres:postgres@%s:%s/%s?sslmode=disable", host, port, database)
}

func quotePostgresIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func applyMigratorSQL(ctx context.Context, db *sql.DB) error {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return errors.Errorf("failed to locate testenv.go path")
	}
	migratorPath := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../../migrator/latest.sql"))
	content, err := os.ReadFile(migratorPath)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, string(content))
	return err
}

func skipIfDockerUnavailable(t *testing.T, err error) {
	t.Helper()
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "docker") && strings.Contains(msg, "daemon") {
		t.Skipf("docker is unavailable: %v", err)
	}
}

// CreateMySQLInstance stores an active MySQL instance with one ADMIN datasource.
func (e *TestEnv) CreateMySQLInstance(ctx context.Context, instanceID string) (*store.InstanceMessage, error) {
	return e.Store.CreateInstanceV2(ctx, &store.InstanceMessage{
		ResourceID: instanceID,
		Metadata: &storepb.Instance{
			Engine:       storepb.Engine_MYSQL,
			Activation:   true,
			SyncInterval: durationpb.New(time.Minute),
			DataSources: []*storepb.DataSource{
				{
					Id:       "admin",
					Type:     storepb.DataSourceType_ADMIN,
					Username: "root",
					Password: "root",
					Host:     e.MySQLHost,
					Port:     e.MySQLPort,
				},
			},
		},
	})
}

// CreatePostgresInstance stores an active PostgreSQL instance with one ADMIN datasource.
func (e *TestEnv) CreatePostgresInstance(ctx context.Context, instanceID string) (*store.InstanceMessage, error) {
	return e.Store.CreateInstanceV2(ctx, &store.InstanceMessage{
		ResourceID: instanceID,
		Metadata: &storepb.Instance{
			Engine:       storepb.Engine_POSTGRES,
			Activation:   true,
			SyncInterval: durationpb.New(time.Minute),
			DataSources: []*storepb.DataSource{
				{
					Id:       "admin",
					Type:     storepb.DataSourceType_ADMIN,
					Username: "postgres",
					Password: "postgres",
					Host:     e.PostgresHost,
					Port:     e.PostgresPort,
					Database: "postgres",
				},
			},
		},
	})
}
