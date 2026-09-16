// Package cmd wires the mxd command tree.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Ranxy/metaxisdata/cli/client"
	"github.com/Ranxy/metaxisdata/cli/config"
	"github.com/Ranxy/metaxisdata/cli/env"
	"github.com/Ranxy/metaxisdata/cli/output"
)

// version is the CLI version reported by `mxd version` and sent to the server
// as the device login's client version.
const version = "0.1.0"

// flags holds the global options, one field per flag.
var flags struct {
	server   string
	token    string
	scopes   []string
	config   string
	format   string
	timeout  time.Duration
	debug    bool
	caCert   string
	insecure bool
	pageSize int32
	maxItems int
}

// app is what one invocation resolved from its flags, its environment and the
// credentials file. A command reads it instead of reaching for os.Getenv.
type app struct {
	out         *output.Renderer
	credentials *config.Credentials
	configPath  string
	scopes      []env.Scope
	server      string
	// serverSource and tokenSource record where the value came from, so
	// `config show` can answer "which config is this process actually using".
	serverSource string
	tokenSource  string
	options      client.Options
	pageSize     int32
	maxItems     int

	connection *client.Client
}

// current is the resolved invocation. It is set once by the root command's
// PersistentPreRunE and read by every RunE.
var current *app

// Execute runs the command tree and returns the process exit code.
func Execute() int {
	root := newRootCmd()
	err := root.Execute()
	if err == nil {
		return client.ExitOK
	}
	writeError(err)
	return client.ExitCode(err)
}

// writeError renders the machine-readable error envelope. The format is known
// by then, so a table-mode invocation still gets the JSON envelope on stderr:
// that stream is always machine-readable.
func writeError(err error) {
	renderer := output.New(output.FormatJSON)
	if current != nil {
		renderer = current.out
	}

	payload := output.Error{Code: output.CodeOf(err), Message: err.Error()}
	var usage *client.UsageError
	if errors.As(err, &usage) {
		payload.Code = usage.Code
		payload.Hint = usage.Hint
	}
	renderer.WriteError(payload)

	if flags.debug {
		_, _ = fmt.Fprintf(os.Stderr, "debug: %+v\n", err)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mxd",
		Short: "Talk to a metaxisdata server from a terminal or an agent",
		Long: `mxd is a client for metaxisdata. It answers what a metadata object looks
like, where a SQL statement moves data, and what depends on what.

Every command writes exactly one JSON document to stdout; progress and errors
go to stderr. Analysis scopes are read from ` + env.ScopesEnv + `, never stored.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := resolve(cmd)
			if err != nil {
				return err
			}
			current = resolved
			return nil
		},
	}

	persistent := root.PersistentFlags()
	persistent.StringVar(&flags.server, "server", "", "server address; saved by 'auth login', so later commands do not need it")
	persistent.StringVar(&flags.token, "token", "", "bearer token to use instead of the stored one")
	persistent.StringArrayVar(&flags.scopes, "scope", nil, "analysis scope to use: a configured name, a GUID, or all")
	persistent.StringVar(&flags.config, "config", "", "credentials file to use instead of the default")
	persistent.StringVar(&flags.format, "format", string(output.FormatJSON), "output format: json or table")
	persistent.DurationVar(&flags.timeout, "timeout", 30*time.Second, "timeout for one request")
	persistent.BoolVar(&flags.debug, "debug", false, "print request failures in full")
	persistent.StringVar(&flags.caCert, "ca-cert", "", "extra PEM certificate to trust (self-signed deployments)")
	persistent.BoolVar(&flags.insecure, "insecure", false, "skip TLS certificate verification")
	persistent.Int32Var(&flags.pageSize, "page-size", 0, "items per request when listing")
	persistent.IntVar(&flags.maxItems, "max-items", 1000, "most items to collect when listing")

	root.AddCommand(
		newAuthCmd(),
		newConfigCmd(),
		newInstanceCmd(),
		newDatabaseCmd(),
		newMetaCmd(),
		newLineageCmd(),
		newVersionCmd(),
	)
	return root
}

// resolve turns the flags, the environment and the credentials file into the
// invocation's settings. The precedence is flags, then the environment, then
// the credentials file; a value that is still missing is only refused by the
// command that actually needs it.
func resolve(cmd *cobra.Command) (*app, error) {
	format, err := output.ParseFormat(flags.format)
	if err != nil {
		return nil, client.Usage("%v", err)
	}

	// A command that only needs the output format must keep working when the
	// credentials file is missing or corrupt: `mxd version` is the first thing
	// anyone runs on a broken installation.
	if noConfigCommands[cmd.Name()] {
		return &app{out: output.New(format), maxItems: flags.maxItems, pageSize: flags.pageSize}, nil
	}

	configPath := flags.config
	if configPath == "" {
		configPath, err = config.Path()
		if err != nil {
			return nil, client.Usage("%v", err).WithCode("config_invalid")
		}
	}
	credentials, err := config.Load(configPath)
	if err != nil {
		// A local file the caller can fix, so it is reported as their input
		// rather than as a server failure.
		return nil, client.Usage("%v", err).WithCode("config_invalid")
	}

	resolved := &app{
		out:         output.New(format),
		credentials: credentials,
		configPath:  configPath,
		pageSize:    flags.pageSize,
		maxItems:    flags.maxItems,
		options: client.Options{
			CACert:   flags.caCert,
			Insecure: flags.insecure,
			Timeout:  flags.timeout,
		},
	}

	switch {
	case flags.server != "":
		resolved.server, resolved.serverSource = flags.server, "flag"
	case os.Getenv(env.ServerEnv) != "":
		resolved.server, resolved.serverSource = os.Getenv(env.ServerEnv), "env:"+env.ServerEnv
	case credentials.Server != "":
		resolved.server, resolved.serverSource = credentials.Server, "file:"+configPath
	default:
		resolved.serverSource = "none"
	}

	switch {
	case flags.token != "":
		resolved.options.Token, resolved.tokenSource = flags.token, "flag"
	case os.Getenv(env.TokenEnv) != "":
		resolved.options.Token, resolved.tokenSource = os.Getenv(env.TokenEnv), "env:"+env.TokenEnv
	case credentials.Token != "":
		resolved.options.Token, resolved.tokenSource = credentials.Token, "file:"+configPath
	default:
		resolved.tokenSource = "none"
	}

	// Scopes live only in the environment: they are never written anywhere.
	resolved.scopes, err = env.ParseScopes(os.Getenv(env.ScopesEnv))
	if err != nil {
		return nil, client.Usage("%v", err)
	}
	resolved.scopes, err = env.Select(resolved.scopes, flags.scopes)
	if err != nil {
		return nil, client.Usage("%v", err)
	}

	if resolved.options.Insecure {
		resolved.out.Progress("Warning: TLS certificate verification is disabled.")
	}
	return resolved, nil
}

// noConfigCommands do not read the credentials file, so a broken one cannot stop
// them. Only commands that never look at it belong here: `config show` reports
// what the file says, so it has to fail loudly when the file is unreadable.
var noConfigCommands = map[string]bool{
	"version":    true,
	"completion": true,
}

// connect builds the clients, refusing an invocation that has no address yet.
func (a *app) connect() (*client.Client, error) {
	if a.connection != nil {
		return a.connection, nil
	}
	connection, err := client.New(a.server, a.options)
	if err != nil {
		return nil, err
	}
	a.connection = connection
	return connection, nil
}

// requireServer reports the address, refusing an invocation that has none. A
// fresh machine has neither a flag nor a stored address, and guessing one would
// send the request somewhere nobody asked for.
func (a *app) requireServer() (string, error) {
	if a.server == "" {
		return "", client.ServerRequired()
	}
	return a.server, nil
}

// newVersionCmd reports the CLI version.
func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the CLI version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return current.out.JSON(map[string]string{"version": version})
		},
	}
}

// formatInstance accepts an instance resource id with or without the resource
// name prefix, so `--instance 1` and `--instance instances/1` both work.
func formatInstance(instance string) string {
	if strings.HasPrefix(instance, "instances/") {
		return instance
	}
	return "instances/" + instance
}

// describeUser renders a user for the JSON envelopes.
func describeUser(email, name string) map[string]string {
	if email == "" && name == "" {
		return nil
	}
	return map[string]string{"email": email, "name": name}
}
