package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgconn"
	"github.com/spf13/cobra"

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
		port        int
		externalURL string
		// output logs in json format
		enableJSONLogging bool
		debug             bool
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
	rootCmd.PersistentFlags().StringVar(&flags.externalURL, "external-url", "", "the external URL the server is reachable at; used to build SSO callback URLs when the workspace setting does not already set one")
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
