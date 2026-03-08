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
)

// TestEnv owns real DB containers and pre-wired runner dependencies.
type TestEnv struct {
	Store    *store.Store
	Analyzer *lineageanalyzer.Analyzer
	Syncer   *schemasync.Syncer

	MySQLHost string
	MySQLPort string

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

	return host, mappedPort.Port(), db
}

func startMySQL(ctx context.Context, t *testing.T, env *TestEnv) (host, port string) {
	t.Helper()

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

	// Wait until SQL is reachable, not just TCP.
	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/?multiStatements=true&parseTime=true", host, mappedPort.Port())
	require.Eventually(t, func() bool {
		db, openErr := sql.Open("mysql", dsn)
		if openErr != nil {
			return false
		}
		defer db.Close()
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return db.PingContext(pingCtx) == nil
	}, 45*time.Second, 1*time.Second)

	return host, mappedPort.Port()
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
