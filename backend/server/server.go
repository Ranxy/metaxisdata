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
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/config"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/store"

	"github.com/pkg/errors"
)

const gracefulShutdownPeriod = 10 * time.Second

type Server struct {
	runnerWG   sync.WaitGroup
	profile    *config.Profile
	echoServer *echo.Echo
	store      *store.Store
	startedTS  int64
	// PG server stoppers.
	stopper []func()

	// stateCfg is the shared in-momory state within the server.
	stateCfg *state.State

	// boot specifies that whether the server boot correctly
	cancel context.CancelFunc
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

	stores, err := store.New(ctx, profile.PgURL, false)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to new store")
	}
	s.store = stores

	lineage.InitCatalogProvide(stores)

	stateCfg, err := state.New()
	if err != nil {
		return nil, errors.Wrapf(err, "failed to initialize state")
	}
	s.stateCfg = stateCfg

	if err := s.initializeSetting(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to init config")
	}
	// Configure echo server.
	s.echoServer = echo.New()

	if err := configureGrpcRouters(ctx, s.echoServer, s.store, s.profile, s.stateCfg, s.profile.Secret); err != nil {
		return nil, errors.Wrapf(err, "failed to configure gRPC routers")
	}
	configureEchoRouters(s.echoServer, profile)

	s.echoServer.Debug = true
	for _, route := range s.echoServer.Routes() {
		fmt.Printf("Path: %s, Method: %s\n", route.Path, route.Method)
	}

	serverStarted = true

	return s, nil
}

func (s *Server) Run(ctx context.Context, port int) error {
	_, cancel := context.WithCancel(ctx)
	s.cancel = cancel

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
	if s.cancel != nil {
		s.cancel()
	}

	// Shutdown echo
	if s.echoServer != nil {
		if err := s.echoServer.Shutdown(ctx); err != nil {
			s.echoServer.Logger.Fatal(err)
		}
	}

	s.runnerWG.Wait()

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
