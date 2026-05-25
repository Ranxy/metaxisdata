package env

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	// Register the MySQL driver for direct setup and mutation SQL used by the integration harness.
	_ "github.com/go-sql-driver/mysql"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/Ranxy/metaxisdata/backend/common"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/generated-go/v1/v1connect"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	integrationAdminEmail    = "integration-admin@example.com"
	integrationAdminPassword = "integration-test-password"
	integrationAdminTitle    = "Integration Admin"
	serviceStartupTimeout    = 60 * time.Second
	databaseSyncTimeout      = 30 * time.Second
	lineageWaitTimeout       = 45 * time.Second
)

var sharedServerBinaryCache = newServerBinaryCache()

// ServiceEnv owns the self-booted infrastructure and the real server process.
type ServiceEnv struct {
	Store *store.Store

	MetadataPGURL string
	BaseURL       string
	PostgresHost  string
	PostgresPort  string
	MySQLHost     string
	MySQLPort     string

	userClient     v1connect.UserServiceClient
	authClient     v1connect.AuthServiceClient
	instanceClient v1connect.InstanceServiceClient
	databaseClient v1connect.DatabaseServiceClient
	lineageClient  v1connect.LineageServiceClient

	httpClient *http.Client
	token      string

	containers []testcontainers.Container
	serverCmd  *exec.Cmd
	serverDone chan error
	serverLogs *lockedBuffer
	serverDir  string
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

type serverBinaryCache struct {
	mu       sync.Mutex
	cond     *sync.Cond
	building bool
	path     string
	dir      string
	err      error
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func newServerBinaryCache() *serverBinaryCache {
	cache := &serverBinaryCache{}
	cache.cond = sync.NewCond(&cache.mu)
	return cache
}

func (c *serverBinaryCache) getOrBuild(ctx context.Context) (string, error) {
	c.mu.Lock()
	for c.building {
		c.cond.Wait()
	}
	if c.path != "" {
		path := c.path
		c.mu.Unlock()
		return path, nil
	}
	c.building = true
	c.err = nil
	c.mu.Unlock()

	path, dir, err := buildIntegrationServerBinary(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.path = path
		c.dir = dir
	} else {
		c.path = ""
		c.dir = ""
	}
	c.err = err
	c.building = false
	c.cond.Broadcast()
	return c.path, c.err
}

func (c *serverBinaryCache) cleanup() {
	c.mu.Lock()
	for c.building {
		c.cond.Wait()
	}
	dir := c.dir
	c.path = ""
	c.dir = ""
	c.err = nil
	c.mu.Unlock()

	if dir != "" {
		_ = os.RemoveAll(dir)
	}
}

// CleanupIntegrationServerBinaryCache removes the cached integration server binary.
func CleanupIntegrationServerBinaryCache() {
	sharedServerBinaryCache.cleanup()
}

// SetupMySQLServiceEnv starts metadata PostgreSQL, source MySQL, the real server process, and API clients.
func SetupMySQLServiceEnv(t *testing.T) *ServiceEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	env, cleanup, err := StartMySQLServiceEnv(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup()
		if t.Failed() {
			t.Logf("integration server logs:\n%s", env.ServerLogs())
		}
	})
	return env
}

// SetupPostgresServiceEnv starts metadata PostgreSQL, seeds a PostgreSQL source database on the same server,
// then starts the real server process and API clients.
func SetupPostgresServiceEnv(t *testing.T) *ServiceEnv {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	env, cleanup, err := StartPostgresServiceEnv(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup()
		if t.Failed() {
			t.Logf("integration server logs:\n%s", env.ServerLogs())
		}
	})
	return env
}

// StartMySQLServiceEnv starts metadata PostgreSQL, source MySQL, and the real server process.
// The returned cleanup function is idempotent and should be called once the shared environment is no longer needed.
func StartMySQLServiceEnv(ctx context.Context) (*ServiceEnv, func(), error) {
	bootstrap := &TestEnv{}
	pgHost, pgPort, pgDBName, err := startPostgresForEnv(ctx, bootstrap, "mysql")
	if err != nil {
		return nil, nil, err
	}
	pgURL := fmt.Sprintf("postgres://postgres:postgres@%s:%s/%s?sslmode=disable", pgHost, pgPort, pgDBName)

	stores, err := store.New(ctx, pgURL, false)
	if err != nil {
		cleanupServiceResources(nil, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}
	if err := applyMigratorSQL(ctx, stores.GetDB()); err != nil {
		cleanupServiceResources(stores, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}
	if err := stores.Close(); err != nil {
		cleanupServiceResources(stores, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}

	mysqlHost, mysqlPort, err := startMySQLForEnv(ctx, bootstrap)
	if err != nil {
		cleanupServiceResources(nil, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}
	if err := seedMySQLSchema(ctx, mysqlHost, mysqlPort); err != nil {
		cleanupServiceResources(nil, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}

	baseURL, serverDir, serverCmd, serverDone, serverLogs, err := startServerProcess(ctx, pgURL)
	if err != nil {
		cleanupServiceResources(nil, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}
	if err := waitForHTTPReady(ctx, baseURL); err != nil {
		cleanupServiceResources(nil, bootstrap.containers, serverDir, serverCmd, serverDone)
		return nil, nil, err
	}

	inspectStore, err := store.New(ctx, pgURL, false)
	if err != nil {
		cleanupServiceResources(nil, bootstrap.containers, serverDir, serverCmd, serverDone)
		return nil, nil, err
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	env := &ServiceEnv{
		Store:          inspectStore,
		MetadataPGURL:  pgURL,
		BaseURL:        baseURL,
		PostgresHost:   pgHost,
		PostgresPort:   pgPort,
		MySQLHost:      mysqlHost,
		MySQLPort:      mysqlPort,
		httpClient:     httpClient,
		userClient:     v1connect.NewUserServiceClient(httpClient, baseURL),
		authClient:     v1connect.NewAuthServiceClient(httpClient, baseURL),
		instanceClient: v1connect.NewInstanceServiceClient(httpClient, baseURL),
		databaseClient: v1connect.NewDatabaseServiceClient(httpClient, baseURL),
		lineageClient:  v1connect.NewLineageServiceClient(httpClient, baseURL),
		containers:     bootstrap.containers,
		serverCmd:      serverCmd,
		serverDone:     serverDone,
		serverLogs:     serverLogs,
		serverDir:      serverDir,
	}
	if err := env.bootstrapAdmin(ctx); err != nil {
		cleanupServiceResources(inspectStore, bootstrap.containers, serverDir, serverCmd, serverDone)
		return nil, nil, err
	}

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			cleanupServiceResources(env.Store, env.containers, env.serverDir, env.serverCmd, env.serverDone)
		})
	}
	return env, cleanup, nil
}

// StartPostgresServiceEnv starts metadata PostgreSQL, a seeded PostgreSQL source database, and the real server process.
// The returned cleanup function is idempotent and should be called once the shared environment is no longer needed.
func StartPostgresServiceEnv(ctx context.Context) (*ServiceEnv, func(), error) {
	bootstrap := &TestEnv{}
	pgHost, pgPort, pgDBName, err := startPostgresForEnv(ctx, bootstrap, "postgres")
	if err != nil {
		return nil, nil, err
	}
	pgURL := fmt.Sprintf("postgres://postgres:postgres@%s:%s/%s?sslmode=disable", pgHost, pgPort, pgDBName)

	stores, err := store.New(ctx, pgURL, false)
	if err != nil {
		cleanupServiceResources(nil, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}
	if err := applyMigratorSQL(ctx, stores.GetDB()); err != nil {
		cleanupServiceResources(stores, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}
	if err := stores.Close(); err != nil {
		cleanupServiceResources(stores, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}
	if err := seedPostgresSchema(ctx, pgHost, pgPort); err != nil {
		cleanupServiceResources(nil, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}

	baseURL, serverDir, serverCmd, serverDone, serverLogs, err := startServerProcess(ctx, pgURL)
	if err != nil {
		cleanupServiceResources(nil, bootstrap.containers, "", nil, nil)
		return nil, nil, err
	}
	if err := waitForHTTPReady(ctx, baseURL); err != nil {
		cleanupServiceResources(nil, bootstrap.containers, serverDir, serverCmd, serverDone)
		return nil, nil, err
	}

	inspectStore, err := store.New(ctx, pgURL, false)
	if err != nil {
		cleanupServiceResources(nil, bootstrap.containers, serverDir, serverCmd, serverDone)
		return nil, nil, err
	}

	httpClient := &http.Client{Timeout: 5 * time.Second}
	env := &ServiceEnv{
		Store:          inspectStore,
		MetadataPGURL:  pgURL,
		BaseURL:        baseURL,
		PostgresHost:   pgHost,
		PostgresPort:   pgPort,
		httpClient:     httpClient,
		userClient:     v1connect.NewUserServiceClient(httpClient, baseURL),
		authClient:     v1connect.NewAuthServiceClient(httpClient, baseURL),
		instanceClient: v1connect.NewInstanceServiceClient(httpClient, baseURL),
		databaseClient: v1connect.NewDatabaseServiceClient(httpClient, baseURL),
		lineageClient:  v1connect.NewLineageServiceClient(httpClient, baseURL),
		containers:     bootstrap.containers,
		serverCmd:      serverCmd,
		serverDone:     serverDone,
		serverLogs:     serverLogs,
		serverDir:      serverDir,
	}
	if err := env.bootstrapAdmin(ctx); err != nil {
		cleanupServiceResources(inspectStore, bootstrap.containers, serverDir, serverCmd, serverDone)
		return nil, nil, err
	}

	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			cleanupServiceResources(env.Store, env.containers, env.serverDir, env.serverCmd, env.serverDone)
		})
	}
	return env, cleanup, nil
}

// ResetMySQLSource restores the shared MySQL source server to the baseline schema used by the integration tests.
func (e *ServiceEnv) ResetMySQLSource(ctx context.Context) error {
	return resetMySQLSchema(ctx, e.MySQLHost, e.MySQLPort)
}

// ResetPostgresSource restores the shared PostgreSQL source server to the baseline schema used by the integration tests.
func (e *ServiceEnv) ResetPostgresSource(ctx context.Context) error {
	return resetPostgresSchema(ctx, e.PostgresHost, e.PostgresPort)
}

// ServerLogs returns the buffered output from the shared integration server process.
func (e *ServiceEnv) ServerLogs() string {
	if e == nil || e.serverLogs == nil {
		return ""
	}
	return e.serverLogs.String()
}

// CreateMySQLInstance creates an instance against the self-booted MySQL source database.
func (e *ServiceEnv) CreateMySQLInstance(ctx context.Context, instanceID string) (*v1pb.Instance, error) {
	resp, err := e.instanceClient.CreateInstance(ctx, authorizedRequest(e.token, &v1pb.CreateInstanceRequest{
		InstanceId: instanceID,
		Instance: &v1pb.Instance{
			Title:        instanceID,
			Engine:       v1pb.Engine_MYSQL,
			Environment:  common.FormatEnvironment("test"),
			Activation:   true,
			SyncInterval: durationpb.New(time.Minute),
			DataSources: []*v1pb.DataSource{
				{
					Id:       "admin",
					Type:     v1pb.DataSourceType_ADMIN,
					Username: "root",
					Password: "root",
					Host:     e.MySQLHost,
					Port:     e.MySQLPort,
				},
			},
		},
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// CreatePostgresInstance creates an instance against the PostgreSQL source database.
func (e *ServiceEnv) CreatePostgresInstance(ctx context.Context, instanceID string) (*v1pb.Instance, error) {
	resp, err := e.instanceClient.CreateInstance(ctx, authorizedRequest(e.token, &v1pb.CreateInstanceRequest{
		InstanceId: instanceID,
		Instance: &v1pb.Instance{
			Title:        instanceID,
			Engine:       v1pb.Engine_POSTGRES,
			Environment:  common.FormatEnvironment("test"),
			Activation:   true,
			SyncInterval: durationpb.New(time.Minute),
			DataSources: []*v1pb.DataSource{
				{
					Id:       "admin",
					Type:     v1pb.DataSourceType_ADMIN,
					Username: "postgres",
					Password: "postgres",
					Host:     e.PostgresHost,
					Port:     e.PostgresPort,
					Database: "postgres",
				},
			},
		},
	}))
	if err != nil {
		return nil, err
	}
	return resp.Msg, nil
}

// ExecMySQL executes SQL directly against the source MySQL server used by the scenario.
func (e *ServiceEnv) ExecMySQL(ctx context.Context, statement string) error {
	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/?multiStatements=true&parseTime=true", e.MySQLHost, e.MySQLPort)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, statement)
	return err
}

// ExecPostgres executes SQL directly against the PostgreSQL server used by the scenario.
func (e *ServiceEnv) ExecPostgres(ctx context.Context, databaseName, statement string) error {
	db, err := sql.Open("pgx", postgresDSN(e.PostgresHost, e.PostgresPort, databaseName))
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, statement)
	return err
}

// SyncInstance explicitly refreshes the database inventory for an instance.
func (e *ServiceEnv) SyncInstance(ctx context.Context, t *testing.T, instanceName string, enableFullSync bool) *v1pb.SyncInstanceResponse {
	t.Helper()

	resp, err := e.instanceClient.SyncInstance(ctx, authorizedRequest(e.token, &v1pb.SyncInstanceRequest{
		Name:           instanceName,
		EnableFullSync: enableFullSync,
	}))
	require.NoError(t, err)
	return resp.Msg
}

// CreateManualSQL creates manual SQL through the public API so lineage is queued through the real service path.
func (e *ServiceEnv) CreateManualSQL(ctx context.Context, t *testing.T, parent, manualSQLID string, manualSQL *v1pb.ManualSQL) *v1pb.ManualSQL {
	t.Helper()

	resp, err := e.databaseClient.CreateManualSQL(ctx, authorizedRequest(e.token, &v1pb.CreateManualSQLRequest{
		Parent:      parent,
		ManualSql:   manualSQL,
		ManualSqlId: manualSQLID,
	}))
	require.NoError(t, err)
	return resp.Msg
}

// EnsureDatabaseVisible returns the named database, falling back to SyncInstance once when needed.
func (e *ServiceEnv) EnsureDatabaseVisible(ctx context.Context, t *testing.T, instanceName, databaseName string) *v1pb.Database {
	t.Helper()

	database := e.findDatabase(ctx, t, instanceName, databaseName)
	if database != nil {
		return database
	}

	_, err := e.instanceClient.SyncInstance(ctx, authorizedRequest(e.token, &v1pb.SyncInstanceRequest{Name: instanceName}))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		database = e.findDatabase(ctx, t, instanceName, databaseName)
		return database != nil
	}, 15*time.Second, 500*time.Millisecond)
	return database
}

// SyncDatabase syncs a single database and waits until the sync timestamp advances.
func (e *ServiceEnv) SyncDatabase(ctx context.Context, t *testing.T, databaseName string) *v1pb.Database {
	t.Helper()

	triggerTime := time.Now()
	_, err := e.databaseClient.SyncDatabase(ctx, authorizedRequest(e.token, &v1pb.SyncDatabaseRequest{Name: databaseName}))
	require.NoError(t, err)

	var database *v1pb.Database
	require.Eventually(t, func() bool {
		database = e.getDatabaseByFullName(ctx, t, databaseName)
		if database == nil || database.SuccessfulSyncTime == nil {
			return false
		}
		return !database.SuccessfulSyncTime.AsTime().Before(triggerTime)
	}, databaseSyncTimeout, 500*time.Millisecond)
	return database
}

// WaitForContextLineage waits until the lineage API returns the expected context relations.
func (e *ServiceEnv) WaitForContextLineage(ctx context.Context, t *testing.T, guid string, metaType v1pb.MetaType, predicate func([]*v1pb.LineageRelation) bool) []*v1pb.LineageRelation {
	t.Helper()

	var relations []*v1pb.LineageRelation
	var lastErr error
	require.Eventuallyf(t, func() bool {
		resp, err := e.lineageClient.GetLineageForContext(ctx, authorizedRequest(e.token, &v1pb.GetLineageForContextRequest{
			Guid:     guid,
			MetaType: metaType,
		}))
		if err != nil {
			lastErr = err
			return false
		}
		lastErr = nil
		relations = resp.Msg.GetRelations()
		return predicate(relations)
	}, lineageWaitTimeout, time.Second, "lineage not ready for guid=%s metaType=%s lastErr=%v relations=%v", guid, metaType.String(), lastErr, relations)
	return relations
}

func authorizedRequest[T any](token string, msg *T) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set("Authorization", "Bearer "+token)
	return req
}

func (e *ServiceEnv) bootstrapAdmin(ctx context.Context) error {
	_, err := e.userClient.CreateUser(ctx, connect.NewRequest(&v1pb.CreateUserRequest{
		User: &v1pb.User{
			Email:    integrationAdminEmail,
			Title:    integrationAdminTitle,
			Password: integrationAdminPassword,
			UserType: v1pb.UserType_USER,
		},
	}))
	if err != nil {
		return err
	}

	resp, err := e.authClient.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{
		Email:    integrationAdminEmail,
		Password: integrationAdminPassword,
	}))
	if err != nil {
		return err
	}
	e.token = resp.Msg.GetToken()
	return nil
}

func (e *ServiceEnv) findDatabase(ctx context.Context, t *testing.T, instanceName, databaseName string) *v1pb.Database {
	t.Helper()

	resp, err := e.databaseClient.ListDatabase(ctx, authorizedRequest(e.token, &v1pb.ListDatabaseRequest{
		Parent:   instanceName,
		PageSize: 1000,
	}))
	require.NoError(t, err)
	for _, database := range resp.Msg.GetDatabases() {
		if database.GetName() == common.FormatDatabase(strings.TrimPrefix(instanceName, "instances/"), databaseName) {
			return database
		}
	}
	return nil
}

func (e *ServiceEnv) getDatabaseByFullName(ctx context.Context, t *testing.T, fullDatabaseName string) *v1pb.Database {
	t.Helper()
	instanceName, databaseName, err := splitDatabaseName(fullDatabaseName)
	require.NoError(t, err)
	return e.findDatabase(ctx, t, instanceName, databaseName)
}

func startServerProcess(ctx context.Context, pgURL string) (string, string, *exec.Cmd, chan error, *lockedBuffer, error) {
	port, err := reservePort()
	if err != nil {
		return "", "", nil, nil, nil, err
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	logs := &lockedBuffer{}

	binaryPath, err := sharedServerBinaryCache.getOrBuild(ctx)
	if err != nil {
		return "", "", nil, nil, nil, err
	}

	root, err := repoRoot()
	if err != nil {
		return "", "", nil, nil, nil, err
	}

	cmd := exec.CommandContext(ctx, binaryPath, "--port", strconv.Itoa(port))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PG_URL="+pgURL)
	cmd.Stdout = logs
	cmd.Stderr = logs
	if err := cmd.Start(); err != nil {
		return "", "", nil, nil, nil, err
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	return baseURL, "", cmd, done, logs, nil
}

func buildIntegrationServerBinary(ctx context.Context) (string, string, error) {
	root, err := repoRoot()
	if err != nil {
		return "", "", err
	}

	binaryDir, err := os.MkdirTemp("", "metaxisdata-integration-server-bin-*")
	if err != nil {
		return "", "", err
	}
	binaryPath := filepath.Join(binaryDir, "metaxisdata-integration-server")

	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", binaryPath, "./backend/bin/server/main.go")
	buildCmd.Dir = root
	buildCmd.Env = os.Environ()

	var output bytes.Buffer
	buildCmd.Stdout = &output
	buildCmd.Stderr = &output
	if err := buildCmd.Run(); err != nil {
		_ = os.RemoveAll(binaryDir)
		return "", "", errors.Wrapf(err, "failed to build integration server binary: %s", strings.TrimSpace(output.String()))
	}

	return binaryPath, binaryDir, nil
}

func shutdownServerProcess(cmd *exec.Cmd, done chan error) {
	if cmd == nil || done == nil {
		return
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
}

func waitForHTTPReady(ctx context.Context, baseURL string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(serviceStartupTimeout)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/not-found", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return errors.Errorf("server did not become ready within %v", serviceStartupTimeout)
}

func reservePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func repoRoot() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.Errorf("failed to locate service_env.go path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "../../../..")), nil
}

func splitDatabaseName(fullName string) (string, string, error) {
	parts := strings.Split(fullName, "/")
	if len(parts) != 4 || parts[0] != "instances" || parts[2] != "databases" {
		return "", "", errors.Errorf("invalid database name %q", fullName)
	}
	return strings.Join(parts[:2], "/"), parts[3], nil
}

func startPostgresForEnv(ctx context.Context, env *TestEnv, metadataScope string) (string, string, string, error) {
	const db = "metaxisdata"
	if host, ok := os.LookupEnv(integrationPostgresHostEnv); ok && host != "" {
		port := os.Getenv(integrationPostgresPortEnv)
		if port == "" {
			return "", "", "", errors.Errorf("environment variable %s must be set when using external integration services", integrationPostgresPortEnv)
		}
		if err := waitForPostgresReadyNoTest(ctx, host, port, "postgres"); err != nil {
			return "", "", "", err
		}
		baseDBName := getenvDefault(integrationPostgresDBEnv, db)
		dbName := integrationMetadataDatabaseName(baseDBName, metadataScope)
		if err := recreatePostgresDatabase(ctx, host, port, dbName); err != nil {
			return "", "", "", err
		}
		if err := waitForPostgresReadyNoTest(ctx, host, port, dbName); err != nil {
			return "", "", "", err
		}
		return host, port, dbName, nil
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
		return "", "", "", err
	}
	env.containers = append(env.containers, container)

	host, err := container.Host(ctx)
	if err != nil {
		return "", "", "", err
	}
	mappedPort, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return "", "", "", err
	}
	if err := waitForPostgresReadyNoTest(ctx, host, mappedPort.Port(), db); err != nil {
		return "", "", "", err
	}
	return host, mappedPort.Port(), db, nil
}

func integrationMetadataDatabaseName(baseName, metadataScope string) string {
	baseName = strings.TrimSpace(baseName)
	if baseName == "" {
		baseName = "metaxisdata"
	}
	metadataScope = strings.TrimSpace(metadataScope)
	if metadataScope == "" {
		return baseName
	}
	return fmt.Sprintf("%s_%s_integration", baseName, metadataScope)
}

func recreatePostgresDatabase(ctx context.Context, host, port, databaseName string) error {
	adminDB, err := sql.Open("pgx", postgresDSN(host, port, "postgres"))
	if err != nil {
		return err
	}
	defer adminDB.Close()

	if _, err := adminDB.ExecContext(ctx, `
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = $1 AND pid <> pg_backend_pid();
`, databaseName); err != nil {
		return err
	}
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", quotePostgresIdentifier(databaseName))); err != nil {
		return err
	}
	if _, err := adminDB.ExecContext(ctx, fmt.Sprintf("CREATE DATABASE %s", quotePostgresIdentifier(databaseName))); err != nil {
		return err
	}
	return nil
}

func startMySQLForEnv(ctx context.Context, env *TestEnv) (string, string, error) {
	if host, ok := os.LookupEnv(integrationMySQLHostEnv); ok && host != "" {
		port := os.Getenv(integrationMySQLPortEnv)
		if port == "" {
			return "", "", errors.Errorf("environment variable %s must be set when using external integration services", integrationMySQLPortEnv)
		}
		if err := waitForMySQLReadyNoTest(ctx, host, port); err != nil {
			return "", "", err
		}
		return host, port, nil
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
		return "", "", err
	}
	env.containers = append(env.containers, container)

	host, err := container.Host(ctx)
	if err != nil {
		return "", "", err
	}
	mappedPort, err := container.MappedPort(ctx, "3306/tcp")
	if err != nil {
		return "", "", err
	}
	if err := waitForMySQLReadyNoTest(ctx, host, mappedPort.Port()); err != nil {
		return "", "", err
	}
	return host, mappedPort.Port(), nil
}

func waitForPostgresReadyNoTest(ctx context.Context, host, port, dbName string) error {
	dsn := fmt.Sprintf("postgres://postgres:postgres@%s:%s/%s?sslmode=disable", host, port, dbName)
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		db, err := sql.Open("pgx", dsn)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			pingErr := db.PingContext(pingCtx)
			cancel()
			_ = db.Close()
			if pingErr == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return errors.Errorf("postgres did not become ready at %s:%s/%s", host, port, dbName)
}

func waitForMySQLReadyNoTest(ctx context.Context, host, port string) error {
	dsn := fmt.Sprintf("root:root@tcp(%s:%s)/?multiStatements=true&parseTime=true", host, port)
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		db, err := sql.Open("mysql", dsn)
		if err == nil {
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			pingErr := db.PingContext(pingCtx)
			cancel()
			_ = db.Close()
			if pingErr == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return errors.Errorf("mysql did not become ready at %s:%s", host, port)
}

func cleanupServiceResources(stores *store.Store, containers []testcontainers.Container, serverDir string, cmd *exec.Cmd, done chan error) {
	if stores != nil {
		_ = stores.Close()
	}
	shutdownServerProcess(cmd, done)
	for _, c := range containers {
		_ = c.Terminate(context.Background())
	}
	if serverDir != "" {
		_ = os.RemoveAll(serverDir)
	}
}
