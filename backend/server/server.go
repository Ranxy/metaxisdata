package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/net/http2"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	llmcomp "github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/migrator"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/runner/lineageanalyzer"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"

	"github.com/pkg/errors"
)

const gracefulShutdownPeriod = 10 * time.Second

// runnerShutdownTimeout bounds how long Shutdown waits for the background
// runners after cancelling them. A runner blocked in external database I/O must
// not hold the process open past the graceful period.
const runnerShutdownTimeout = 10 * time.Second

// minJWTSecretLength is the minimum length of the JWT signing key. The key must
// be long enough that it cannot be brute-forced offline.
const minJWTSecretLength = 32

type Server struct {
	runnerWG        sync.WaitGroup
	runnerCtx       context.Context
	runnerCancel    context.CancelFunc
	profile         *config.Profile
	echoServer      *echo.Echo
	store           *store.Store
	startedTS       int64
	lineageAnalyzer *lineageanalyzer.Analyzer
	schemaSync      *schemasync.Syncer
	llmRegistry     *llmcomp.Registry
	// PG server stoppers.
	stopper []func()

	// stateCfg is the shared in-momory state within the server.
	stateCfg *state.State
}

// NewServer creates a server.
func NewServer(ctx context.Context, profile *config.Profile) (*Server, error) {
	s := &Server{
		profile:   profile,
		startedTS: time.Now().Unix(),
	}

	// Display config
	slog.Info("-----Config BEGIN-----")
	slog.Info(fmt.Sprintf("mode=%s", profile.Mode))
	slog.Info("-----Config END-------")

	serverStarted := false
	defer func() {
		if !serverStarted {
			_ = s.Shutdown(ctx)
		}
	}()

	stores, err := store.New(ctx, profile.PgURL, store.WithEncryptionKey(profile.EncryptionKey))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to new store")
	}
	s.store = stores

	// Migrate the metadata schema to the latest embedded version before any
	// subsystem reads from it.
	if err := migrator.MigrateSchema(ctx, stores.GetDB()); err != nil {
		return nil, errors.Wrap(err, "failed to migrate database schema")
	}

	s.runnerCtx, s.runnerCancel = context.WithCancel(ctx)

	dbFactory := dbfactory.New(stores)

	lineage.InitCatalogProvide(stores)

	s.lineageAnalyzer = lineageanalyzer.NewAnalyzer(stores, profile)

	stateCfg, err := state.New()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to initialize state")
	}
	s.stateCfg = stateCfg

	s.schemaSync = schemasync.NewSyncer(stores, dbFactory, profile, stateCfg, s.lineageAnalyzer)

	s.llmRegistry = llmcomp.NewRegistry(stores, profile)

	if err := s.initializeSetting(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to init config")
	}

	if err := s.resolveJWTSecret(ctx); err != nil {
		return nil, err
	}

	// Configure echo server.
	s.echoServer = echo.New()

	if err := configureGrpcRouters(ctx, s.echoServer, s.store, s.profile, s.stateCfg, s.profile.Secret, dbFactory, s.schemaSync, s.llmRegistry); err != nil {
		return nil, errors.Wrapf(err, "failed to configure gRPC routers")
	}

	s.runnerWG.Add(2)
	go s.lineageAnalyzer.Run(s.runnerCtx, &s.runnerWG)
	go s.schemaSync.Run(s.runnerCtx, &s.runnerWG)

	configureEchoRouters(s.echoServer, profile)

	if profile.RuntimeDebug.Load() {
		s.echoServer.Debug = true
		for _, route := range s.echoServer.Routes() {
			fmt.Printf("Path: %s, Method: %s\n", route.Path, route.Method)
		}
	}

	serverStarted = true

	return s, nil
}

// resolveJWTSecret determines the key used to sign and verify access tokens.
//
// The key is never a compiled-in constant: it comes from the JWT_SECRET
// environment variable when set, otherwise from the randomly generated
// per-deployment AUTH_SECRET setting in the database. Because the key is
// deployment-specific, every token signed with a former key stops verifying.
func (s *Server) resolveJWTSecret(ctx context.Context) error {
	if s.profile.Secret == "" {
		setting, err := s.store.GetSetting(ctx, storepb.SettingName_AUTH_SECRET)
		if err != nil {
			return errors.Wrap(err, "failed to load the JWT signing key")
		}
		if setting == nil || setting.Value == "" {
			return errors.New("JWT signing key is not configured: set the JWT_SECRET environment variable")
		}
		s.profile.Secret = setting.Value
	}
	if len(s.profile.Secret) < minJWTSecretLength {
		return errors.Errorf("JWT signing key must be at least %d characters, got %d", minJWTSecretLength, len(s.profile.Secret))
	}
	return nil
}

func (s *Server) Run(_ context.Context, port int) error {
	address := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	s.echoServer.Listener = listener

	go func() {
		if err := s.echoServer.StartH2CServer(address, &http2.Server{}); err != nil {
			slog.Error("http server listen error", log.WithError(err))
		}
	}()
	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	slog.Info("Stopping ...")
	slog.Info("Stopping web server...")

	ctx, cancel := context.WithTimeout(ctx, gracefulShutdownPeriod)
	defer cancel()

	// Cancel the worker
	if s.runnerCancel != nil {
		s.runnerCancel()
	}
	// Shutdown echo. A failure is logged rather than fatal: os.Exit here would
	// skip closing the store and running the stoppers.
	if s.echoServer != nil {
		if err := s.echoServer.Shutdown(ctx); err != nil {
			slog.Error("failed to shut down the web server", log.WithError(err))
		}
	}

	// Wait for the runners with a bound. They are given the chance to observe
	// the cancelled context, but a stuck runner must not block exit forever.
	runnersDone := make(chan struct{})
	go func() {
		s.runnerWG.Wait()
		close(runnersDone)
	}()
	select {
	case <-runnersDone:
	case <-time.After(runnerShutdownTimeout):
		slog.Warn("background runners did not stop within the timeout; exiting anyway",
			slog.Duration("timeout", runnerShutdownTimeout))
	}

	// Close db connection
	if s.store != nil {
		if err := s.store.Close(); err != nil {
			return err
		}
	}

	for _, stopper := range s.stopper {
		stopper()
	}

	return nil
}
