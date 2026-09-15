package cmd

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/emptypb"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/cli/authflow"
	"github.com/Ranxy/metaxisdata/cli/client"
	"github.com/Ranxy/metaxisdata/cli/config"
	"github.com/Ranxy/metaxisdata/cli/env"
)

// authLoginFlags are the login-specific flags, kept out of the global set.
var authLoginFlags struct {
	noBrowser      bool
	prefillURL     bool
	serviceAccount string
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Sign in to a server",
	}
	cmd.AddCommand(newAuthLoginCmd(), newAuthStatusCmd(), newAuthLogoutCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in with a device login",
		Long: `Sign in by approving a request in a browser.

The command prints a URL and a code, waits for you to approve the request on the
server's /device page, and then stores the resulting token. Pass --server the
first time: the address is saved, so later commands do not need it.`,
		Args: cobra.NoArgs,
		RunE: runAuthLogin,
	}
	cmd.Flags().BoolVar(&authLoginFlags.noBrowser, "no-browser", false, "do not try to open a browser")
	cmd.Flags().BoolVar(&authLoginFlags.prefillURL, "prefill-url", false, "print the confirmation URL with the code already filled in")
	cmd.Flags().StringVar(&authLoginFlags.serviceAccount, "service-account", "", "sign in as a service account using "+env.ServiceKeyEnv)
	return cmd
}

func runAuthLogin(cmd *cobra.Command, _ []string) error {
	server, err := current.requireServer()
	if err != nil {
		return err
	}

	if authLoginFlags.serviceAccount != "" {
		return runServiceAccountLogin(cmd.Context(), server)
	}

	connection, err := current.connect()
	if err != nil {
		return err
	}

	result, err := authflow.Run(cmd.Context(), connection.Auth, current.out.Progress, authflow.Options{
		ClientName:    "mxd",
		ClientVersion: version,
		Timeout:       flags.timeout,
		PrefillURL:    authLoginFlags.prefillURL,
		NoBrowser:     authLoginFlags.noBrowser,
		// The workspace may have no external URL, in which case the server
		// cannot name the page and the address we are already talking to is the
		// only sensible one to print.
		ServerURL:   server,
		OpenBrowser: openBrowser,
	})
	switch {
	case errors.Is(err, authflow.ErrDenied):
		return errors.Wrap(client.ErrDeviceSessionExpired, "the request was denied in the browser")
	case errors.Is(err, authflow.ErrExpired):
		return errors.Wrap(client.ErrDeviceSessionExpired, "the request was not approved in time")
	case err != nil:
		return err
	}

	return storeLogin(server, result.Token, result.ExpiresIn, result.User.GetEmail(), result.User.GetTitle())
}

// runServiceAccountLogin exchanges a service account's key for an API token.
// The token lasts an hour, so it is meant for a CI job rather than for a person.
func runServiceAccountLogin(ctx context.Context, server string) error {
	serviceKey := strings.TrimSpace(os.Getenv(env.ServiceKeyEnv))
	if serviceKey == "" {
		return client.Usage("--service-account needs the account's key in %s", env.ServiceKeyEnv)
	}

	connection, err := current.connect()
	if err != nil {
		return err
	}
	response, err := connection.Auth.Login(ctx, connect.NewRequest(&v1pb.LoginRequest{
		Email:    authLoginFlags.serviceAccount,
		Password: serviceKey,
	}))
	if err != nil {
		return err
	}
	if response.Msg.GetToken() == "" {
		return errors.New("the server did not return a token for the service account")
	}
	user := response.Msg.GetUser()
	return storeLogin(server, response.Msg.GetToken(), int64(time.Hour.Seconds()), user.GetEmail(), user.GetTitle())
}

// storeLogin persists the address and the token together. Keeping them a pair
// matters: a token is issued by one server, so remembering it next to another
// address would fail every later request for no visible reason.
func storeLogin(server, token string, expiresIn int64, email, name string) error {
	if previous := current.credentials.Server; previous != "" && previous != server {
		current.out.Progress("server changed: %s -> %s", previous, server)
	}
	current.credentials.Server = server
	current.credentials.Token = token
	current.credentials.User = &config.User{Email: email, Name: name}
	current.credentials.TokenExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)

	if err := config.Save(current.configPath, current.credentials); err != nil {
		return err
	}
	current.out.Progress("Credentials saved to %s", current.configPath)

	envelope := map[string]any{
		"status":         "approved",
		"server":         server,
		"tokenExpiresAt": current.credentials.TokenExpiresAt.UTC().Format(time.RFC3339),
	}
	if user := describeUser(email, name); user != nil {
		envelope["user"] = user
	}
	return current.out.JSON(envelope)
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show who the CLI is signed in as",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := current.requireServer(); err != nil {
				return err
			}
			connection, err := current.connect()
			if err != nil {
				return err
			}
			response, err := connection.User.GetCurrentUser(cmd.Context(), connect.NewRequest(&emptypb.Empty{}))
			if err != nil {
				return err
			}

			envelope := map[string]any{
				"server":      current.server,
				"tokenSource": current.tokenSource,
				"user":        describeUser(response.Msg.GetEmail(), response.Msg.GetTitle()),
			}
			if !current.credentials.TokenExpiresAt.IsZero() {
				envelope["tokenExpiresAt"] = current.credentials.TokenExpiresAt.UTC().Format(time.RFC3339)
			}
			return current.out.JSON(envelope)
		},
	}
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke the stored token",
		Long: `Revoke the stored token on the server and forget it locally.

The server address is kept, so signing in again does not need --server.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var revokeErr error
			if _, err := current.requireServer(); err == nil {
				if connection, err := current.connect(); err == nil {
					_, revokeErr = connection.Auth.Logout(cmd.Context(), connect.NewRequest(&v1pb.LogoutRequest{}))
				}
			}

			current.credentials.Clear()
			if err := config.Save(current.configPath, current.credentials); err != nil {
				return err
			}
			if revokeErr != nil && connect.CodeOf(revokeErr) != connect.CodeUnauthenticated {
				// The token is gone locally either way; report only a failure
				// that actually leaves something behind on the server.
				return revokeErr
			}
			return current.out.JSON(map[string]any{"status": "signed_out", "server": current.credentials.Server})
		},
	}
}

// openBrowser makes a best-effort attempt to open a URL. Failing is normal on a
// server, and the URL has already been printed.
func openBrowser(url string) error {
	var command string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
	case "windows":
		command, args = "cmd", []string{"/c", "start"}
	default:
		command = "xdg-open"
	}
	return exec.Command(command, append(args, url)...).Start()
}
