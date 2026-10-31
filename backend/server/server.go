package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/net/http2"

	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/common/version"
	"github.com/Ranxy/metaxisdata/backend/component/dataquality"
	"github.com/Ranxy/metaxisdata/backend/component/dbfactory"
	llmcomp "github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/component/notification"
	"github.com/Ranxy/metaxisdata/backend/component/state"
	"github.com/Ranxy/metaxisdata/backend/config"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/metrics"
	"github.com/Ranxy/metaxisdata/backend/migrator"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/catalog"
	"github.com/Ranxy/metaxisdata/backend/plugin/lineage/engines"
	"github.com/Ranxy/metaxisdata/backend/plugin/openlineage"
	"github.com/Ranxy/metaxisdata/backend/runner"
	"github.com/Ranxy/metaxisdata/backend/runner/lineageanalyzer"
	"github.com/Ranxy/metaxisdata/backend/runner/lineagevalidation"
	"github.com/Ranxy/metaxisdata/backend/runner/maintenance"
	"github.com/Ranxy/metaxisdata/backend/runner/qualityrunner"
	"github.com/Ranxy/metaxisdata/backend/runner/qualitysuggestionrunner"
	"github.com/Ranxy/metaxisdata/backend/runner/schemasync"
	"github.com/Ranxy/metaxisdata/backend/store"

	"github.com/pkg/errors"
)

const gracefulShutdownPeriod = 10 * time.Second

// runnerShutdownTimeout bounds how long Shutdown waits for the background
// runners after cancelling them. A runner blocked in external database I/O must
// not hold the process open past the graceful period.
const runnerShutdownTimeout = 10 * time.Second

// debugLogDrainTimeout bounds how long Shutdown waits for queued LLM debug-log
// writes. The log is best-effort, and the shutdown context has no deadline: a
// database that is already unavailable must not hold process exit for the queue's
// worst-case drain.
const debugLogDrainTimeout = 5 * time.Second

// metricsShutdownTimeout bounds closing the metrics listener. It has its own
// budget because the graceful-shutdown context may already be spent.
const metricsShutdownTimeout = 5 * time.Second

// minJWTSecretLength is the minimum length of the JWT signing key. The key must
// be long enough that it cannot be brute-forced offline.
const minJWTSecretLength = 32

// The API listener serves the SPA and ConnectRPC, and it accepts anonymous
// requests (login, device login, SSO). These bounds are what keeps a client
// that opens a connection and then trickles its request from holding a
// goroutine and a file descriptor forever: the body limit only caps the size of
// a request, not how long the client may take to send it.
//
// WriteTimeout is deliberately zero. The notification subscription and the
// streaming Explain SQL RPC are responses that legitimately stay open for
// minutes, and a write deadline would cut them; a dead peer is bounded by the
// read side and by IdleTimeout instead. The dedicated metrics listener has its
// own complete set of timeouts in startMetricsServer, since it serves one
// bounded scrape request.
const (
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpIdleTimeout       = 120 * time.Second
	// MaxHeaderBytes stays at Go's 1 MiB default: a caller may legitimately send
	// a long User-Agent, which the audit layer bounds and truncates rather than
	// refusing the request. The read deadlines above, not the header ceiling, are
	// what bound a slow client.
	httpMaxHeaderBytes = 1 << 20
)

// A unary ConnectRPC call needs a bound of its own, separate from the listener
// timeouts above: those bound how long a client may take to send a request, not
// how long the handler may take to answer it. Without one, a handler stuck on a
// target database or a model holds a goroutine, its pool connection and, on the
// REST form, a second connection to this same process for as long as the
// dependency stays quiet. Streaming calls are exempt by construction: the
// deadline interceptor leaves them untouched, because a notification
// subscription or a streaming Explain call lives far longer than one request.
const (
	// connectUnaryTimeout bounds one unary call.
	connectUnaryTimeout = 60 * time.Second
	// connectLongUnaryTimeout is the ceiling for the unary methods whose work
	// legitimately outlasts one request. Each of them already bounds its own work
	// — a column profile budgets two 120s queries plus the connection wait, a
	// batch sync carries a 5-minute budget, the LLM suggestions wait on a model —
	// so this is only the outer backstop, and it must stay above the longest of
	// those budgets, the schema sync's 15 minutes. The procedures are listed in
	// connectLongUnaryMethods.
	connectLongUnaryTimeout = 20 * time.Minute
)

// configureHTTPServerTimeouts applies the API listener's connection bounds.
func configureHTTPServerTimeouts(e *echo.Echo) {
	e.Server.ReadHeaderTimeout = httpReadHeaderTimeout
	e.Server.ReadTimeout = httpReadTimeout
	e.Server.IdleTimeout = httpIdleTimeout
	e.Server.MaxHeaderBytes = httpMaxHeaderBytes
	// No WriteTimeout: see the comment on the constants above.
	e.Server.WriteTimeout = 0
}

// newEchoServer builds the API listener with its connection bounds applied, so
// a server assembled here cannot inadvertently serve without them.
func newEchoServer() *echo.Echo {
	e := echo.New()
	configureHTTPServerTimeouts(e)
	return e
}

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
	lineageValidate *lineagevalidation.Runner
	maintenance     *maintenance.Runner
	qualityRunner   *qualityrunner.Runner
	// suggestionRunner executes the durable AI drafting jobs the API queues.
	suggestionRunner *qualitysuggestionrunner.Runner
	llmRegistry      *llmcomp.Registry

	// metrics owns the process instruments, metricsRegistry is the private
	// Prometheus registry they are exposed through, and metricsServer is the
	// dedicated listener serving it (nil when --metrics-addr is empty).
	metrics         *metrics.Metrics
	metricsRegistry *prometheus.Registry
	metricsServer   *http.Server
	metricsListener net.Listener

	// stateCfg is the shared in-momory state within the server.
	stateCfg *state.State
	// notifier is the one writer of the notification table.
	notifier *notification.Service
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

	stores, err := store.New(ctx, profile.PgURL)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to new store")
	}
	s.store = stores

	// Migrate the metadata schema to the latest embedded version before any
	// subsystem reads from it.
	if err := migrator.MigrateSchema(ctx, stores.GetDB()); err != nil {
		return nil, errors.Wrap(err, "failed to migrate database schema")
	}

	// Metrics are process-local and pull-only. The registry is private so a test
	// can build a second server in one process, and so the endpoint carries what
	// this server registered rather than whatever other packages put on the
	// deprecated default registry.
	s.metrics = metrics.New()
	s.metricsRegistry = prometheus.NewRegistry()
	for _, collector := range []prometheus.Collector{
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.NewBuildInfoCollector(version.Version, version.GitCommit),
		metrics.NewDBPoolCollector(s.store.DBStats),
	} {
		if err := s.metricsRegistry.Register(collector); err != nil {
			return nil, errors.Wrap(err, "failed to register metrics collector")
		}
	}
	if err := s.metrics.Register(s.metricsRegistry); err != nil {
		return nil, errors.Wrap(err, "failed to register metrics")
	}

	s.runnerCtx, s.runnerCancel = context.WithCancel(ctx)

	dbFactory := dbfactory.New(stores)

	// The LLM registry is process-wide: the explain and suggestion handlers, the
	// background drafting runner and the LLM settings page all read the same list.
	s.llmRegistry = llmcomp.NewRegistry(stores, profile)

	// Which engines have an analyzer is a compile-time list, so the process builds
	// the lineage analyzer once and hands the same value to every consumer. It is
	// no longer package state an init function fills in, which is what kept a test
	// and the server from agreeing on what was registered.
	lineageEngines := lineage.NewAnalyzer(catalog.NewCatalogProvide(stores), engines.Registrations()...)

	s.lineageAnalyzer = lineageanalyzer.NewAnalyzer(stores, lineageEngines, s.metrics)

	stateCfg, err := state.New(s.metrics)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to initialize state")
	}
	s.stateCfg = stateCfg

	// The maintenance pass and the post-sync runner revalidate the same ingested
	// edges, so they share one processor and its lock.
	// The revalidation paths resolve datasets to check ingested edges, not to report
	// them: only ingestion names an administrator's inbox as the sink.
	ingestedLineage := openlineage.NewProcessor(stores, lineageEngines, nil, s.metrics)
	s.lineageValidate = lineagevalidation.NewRunner(ingestedLineage, s.metrics)
	s.maintenance = maintenance.NewRunner(stores, ingestedLineage, s.metrics)

	// The notification service is what a background runner reports through: a
	// finished sync operation to the user who asked for it, and an ingestion or
	// sync failure to the workspace administrators.
	s.notifier = notification.New(stores)
	s.schemaSync = schemasync.NewSyncer(stores, dbFactory, stateCfg, s.lineageAnalyzer, s.lineageValidate, s.notifier, s.metrics)
	// The quality runner executes the rules the API creates: scheduled rules on its
	// own tick, manual executions from the queue the service hands it.
	s.qualityRunner = qualityrunner.NewRunner(stores, dbFactory, stateCfg, s.notifier, s.metrics)

	// The LLM quota and the drafting pipeline are shared by the synchronous
	// suggestion RPC and the background drafting runner, so a job spends the same
	// daily budget a request does.
	llmQuota := dataquality.NewLLMQuota(s.metrics)
	suggester := dataquality.NewSuggester(stores, s.llmRegistry, llmQuota, s.metrics)
	s.suggestionRunner = qualitysuggestionrunner.NewRunner(stores, suggester)

	if err := s.initializeSetting(ctx); err != nil {
		return nil, errors.Wrap(err, "failed to init config")
	}

	if err := s.resolveJWTSecret(ctx); err != nil {
		return nil, err
	}

	if err := s.resolveCredentialCipher(ctx); err != nil {
		return nil, err
	}

	// Configure echo server.
	s.echoServer = newEchoServer()

	if err := configureGrpcRouters(ctx, s.echoServer, s.store, s.profile, s.stateCfg, s.profile.Secret, dbFactory, s.schemaSync, s.qualityRunner, s.llmRegistry, suggester, llmQuota, lineageEngines, s.notifier, s.metrics); err != nil {
		return nil, errors.Wrapf(err, "failed to configure gRPC routers")
	}

	// Keeping the set in one list (instead of a hand-written Add(4) and four
	// goroutines) keeps the wait group count and the actual runners in sync, and
	// gives each one the shared panic boundary in runner.Go.
	runners := []runner.Runner{s.lineageAnalyzer, s.schemaSync, s.qualityRunner, s.suggestionRunner, s.lineageValidate, s.maintenance}
	for _, r := range runners {
		runner.Go(s.runnerCtx, &s.runnerWG, r, s.metrics)
	}

	// Scrape-time gauges read live state, so they register after the runners and
	// the connection governor they observe exist.
	for _, collector := range []prometheus.Collector{
		metrics.NewLineageAnalysisQueueDepth(s.lineageAnalyzer.QueueStats),
		metrics.NewInstanceConnectionInUse(stateCfg.ConnectionInUse),
		metrics.NewInstanceConnectionWaiters(stateCfg.ConnectionWaiters),
		metrics.NewQualityManualQueueDepth(s.qualityRunner.ManualQueueDepth),
	} {
		if err := s.metricsRegistry.Register(collector); err != nil {
			return nil, errors.Wrap(err, "failed to register metrics collector")
		}
	}

	configureEchoRouters(s.echoServer, profile, s.metrics)

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
// The key is the randomly generated per-deployment AUTH_SECRET setting in the
// database, created on first startup: it is never a compiled-in constant, a
// flag or an environment variable. Because it is deployment-specific and
// stable, tokens survive restarts; every token signed with a former key stops
// verifying.
func (s *Server) resolveJWTSecret(ctx context.Context) error {
	setting, err := s.store.GetSetting(ctx, storepb.SettingName_AUTH_SECRET)
	if err != nil {
		return errors.Wrap(err, "failed to load the JWT signing key")
	}
	if setting == nil || setting.Value == "" {
		return errors.New("JWT signing key is not configured: the AUTH_SECRET setting is missing")
	}
	s.profile.Secret = setting.Value
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

	// A test builds a bare Server with no profile; only the real one opts into
	// the metrics listener.
	if s.profile != nil && s.profile.MetricsAddr != "" {
		if err := s.startMetricsServer(); err != nil {
			// The API listener is already bound but Serve has not started, so
			// http.Server.Shutdown would not close it; without this the port stays
			// bound for the process lifetime after a failed startup.
			_ = listener.Close()
			return err
		}
	}

	go func() {
		// A graceful shutdown stops the server, which makes it return
		// http.ErrServerClosed; that is the expected end of the goroutine, not a
		// listen failure. Anything else is a real error worth reporting.
		if err := s.echoServer.StartH2CServer(address, &http2.Server{}); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	// End the live notification subscriptions before draining the web server. A
	// stream is an in-flight request that never finishes on its own, and Shutdown
	// waits for those: without this, every stop spends the graceful period waiting
	// for clients to close their tabs.
	if s.notifier != nil {
		s.notifier.Close()
	}
	// Shutdown echo. A failure is logged rather than fatal: os.Exit here would
	// skip closing the store.
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

	// The metrics endpoint is a second listener. It holds no durable state and
	// only reads in-memory instruments, so it is closed before the store is. It
	// gets its own short budget: by this point the graceful context may already be
	// spent on the main server and the runners.
	if s.metricsServer != nil {
		metricsCtx, cancelMetrics := context.WithTimeout(context.Background(), metricsShutdownTimeout)
		metricsErr := s.metricsServer.Shutdown(metricsCtx)
		cancelMetrics()
		if metricsErr != nil {
			slog.Warn("failed to shut down the metrics server", log.WithError(metricsErr))
		}
	}

	// Drain the LLM debug-log writers before the pool closes: they write to the
	// store, so a queued entry would otherwise be written to a closed connection
	// pool. The drain gets its own short budget because the shutdown context has no
	// deadline, and a stuck database must not keep the process alive.
	debugDrainCtx, cancelDebugDrain := context.WithTimeout(ctx, debugLogDrainTimeout)
	debugDrainErr := llmcomp.ShutdownDebugLogWorkers(debugDrainCtx)
	cancelDebugDrain()
	if debugDrainErr != nil {
		slog.Warn("LLM debug log writers did not drain before the store closed", log.WithError(debugDrainErr))
	}

	// Close db connection
	if s.store != nil {
		if err := s.store.Close(); err != nil {
			return err
		}
	}

	return nil
}

// startMetricsServer binds the dedicated metrics address and serves the private
// registry's /metrics. It is opt-in: an empty --metrics-addr never reaches it,
// and the API port never serves metrics.
func (s *Server) startMetricsServer() error {
	address := s.profile.MetricsAddr
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.Wrapf(err, "failed to listen on the metrics address %q", address)
	}
	s.metricsListener = listener
	s.metricsServer = &http.Server{
		Handler:           s.metricsHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		// A graceful shutdown makes Serve return http.ErrServerClosed; anything
		// else is a real serve failure worth reporting.
		if err := s.metricsServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server listen error", log.WithError(err))
		}
	}()
	return nil
}

// metricsHandler serves the private registry. It is deliberately a single GET
// route: the metrics port is not a second API, and a scrape is a read.
func (s *Server) metricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		promhttp.HandlerFor(s.metricsRegistry, promhttp.HandlerOpts{}).ServeHTTP(w, r)
	}))
	return mux
}
