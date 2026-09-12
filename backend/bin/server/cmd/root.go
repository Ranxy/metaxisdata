package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgconn"
	"github.com/spf13/cobra"

	"github.com/Ranxy/metaxisdata/backend/common"
	"github.com/Ranxy/metaxisdata/backend/common/log"
	"github.com/Ranxy/metaxisdata/backend/server"
)

// -----------------------------------Global constant BEGIN----------------------------------------
const (
	greetingBanner = `
___________________________________________________________________________________________

███╗   ███╗███████╗████████╗ █████╗ ██╗  ██╗██╗███████╗    ██████╗  █████╗ ████████╗ █████╗ 
████╗ ████║██╔════╝╚══██╔══╝██╔══██╗╚██╗██╔╝██║██╔════╝    ██╔══██╗██╔══██╗╚══██╔══╝██╔══██╗
██╔████╔██║█████╗     ██║   ███████║ ╚███╔╝ ██║███████╗    ██║  ██║███████║   ██║   ███████║
██║╚██╔╝██║██╔══╝     ██║   ██╔══██║ ██╔██╗ ██║╚════██║    ██║  ██║██╔══██║   ██║   ██╔══██║
██║ ╚═╝ ██║███████╗   ██║   ██║  ██║██╔╝ ██╗██║███████║    ██████╔╝██║  ██║   ██║   ██║  ██║
╚═╝     ╚═╝╚══════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝╚══════╝    ╚═════╝ ╚═╝  ╚═╝   ╚═╝   ╚═╝  ╚═╝                                                                  

%s
___________________________________________________________________________________________

`
)

// -----------------------------------Command Line Config BEGIN------------------------------------.
var (
	flags struct {
		// Used for command line config
		port int
		// output logs in json format
		enableJSONLogging bool
		debug             bool
		// comma-separated browser origins allowed to call the server with
		// credentials. Empty installs no CORS middleware at all.
		corsAllowOrigins string
	}

	rootCmd = &cobra.Command{
		Use:   "database management server",
		Short: "database management server",
		Run: func(_ *cobra.Command, _ []string) {
			start()
		},
	}
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().IntVar(&flags.port, "port", 8080, "port where server runs. Default to 80")
	rootCmd.PersistentFlags().BoolVar(&flags.enableJSONLogging, "enable-json-logging", false, "enable output logs in json format")
	rootCmd.PersistentFlags().BoolVar(&flags.debug, "debug", false, "whether to enable debug level logging")
	rootCmd.PersistentFlags().StringVar(&flags.corsAllowOrigins, "cors-allow-origins", "", "comma-separated browser origins allowed to call the API with credentials; empty disables CORS")
}

// defaultDevCORSOrigins matches the Vite dev server, which proxies /v1 and
// /metaxisdata.v1 to the backend. Any other origin must be opted in explicitly
// with --cors-allow-origins; the previous "any origin in dev" behavior allowed
// credentialed cross-site requests from anywhere.
var defaultDevCORSOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}

// parseCORSAllowOrigins splits the comma-separated flag and drops empty entries
// and trailing slashes, which browsers never include in an Origin header.
func parseCORSAllowOrigins(raw string) []string {
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(strings.TrimRight(part, "/")); trimmed != "" {
			origins = append(origins, trimmed)
		}
	}
	return origins
}

// setupLogging installs the process-wide logger. Without it slog.Default keeps
// its built-in TextHandler pinned at Info, so --debug and --enable-json-logging
// had no effect and log.Replace never ran.
func setupLogging(enableJSONLogging bool) {
	opts := &slog.HandlerOptions{
		AddSource:   true,
		Level:       log.LogLevel,
		ReplaceAttr: log.Replace,
	}
	var handler slog.Handler
	if enableJSONLogging {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(handler))
}

func start() {
	if flags.debug {
		log.LogLevel.Set(slog.LevelDebug)
	}
	setupLogging(flags.enableJSONLogging)

	profile := activeProfile()
	profile.CORSAllowOrigins = parseCORSAllowOrigins(flags.corsAllowOrigins)
	if len(profile.CORSAllowOrigins) == 0 && profile.Mode == common.ReleaseModeDev {
		profile.CORSAllowOrigins = defaultDevCORSOrigins
	}

	if profile.PgURL == "" {
		slog.Error("must set PG_URL environment variable")
		return
	}

	var s *server.Server
	var err error
	// Setup signal handlers.
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	// Trigger graceful shutdown on SIGINT or SIGTERM.
	// The default signal sent by the `kill` command is SIGTERM,
	// which is taken as the graceful shutdown signal for many systems, eg., Kubernetes, Gunicorn.
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-c
		slog.Info(fmt.Sprintf("%s received.", sig.String()))
		if s != nil {
			_ = s.Shutdown(ctx)
		}
		cancel()
	}()

	s, err = server.NewServer(ctx, profile)
	if err != nil {
		if pge, ok := errors.AsType[*pgconn.PgError](err); ok {
			slog.Error("Cannot new server", log.WithError(err), "detail", pge.Detail, "hint", pge.Hint)
			return
		}
		slog.Error("Cannot new server", log.WithError(err))
		return
	}

	fmt.Printf(greetingBanner, fmt.Sprintf("Server has started on port %d 🚀", flags.port))

	// Execute program.
	if err := s.Run(ctx, flags.port); err != nil {
		if err != http.ErrServerClosed {
			slog.Error(err.Error())
			_ = s.Shutdown(ctx)
			cancel()
		}
	}

	// Wait for CTRL-C.
	<-ctx.Done()
}
