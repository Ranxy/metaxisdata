package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/Ranxy/metaxisdata/cli/env"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the configuration this process resolved",
	}
	cmd.AddCommand(newConfigShowCmd(), newConfigPathCmd())
	return cmd
}

// newConfigShowCmd reports the effective settings and where each came from.
// It is read-only: scopes are never written anywhere, and this is the one place
// that says so out loud.
func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show the effective server, credentials and scopes",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			scopeSource := "none"
			if os.Getenv(env.ScopesEnv) != "" {
				scopeSource = "env:" + env.ScopesEnv
			}

			scopes := make([]map[string]string, 0, len(current.scopes))
			for _, scope := range current.scopes {
				entry := map[string]string{"guid": scope.GUID}
				if scope.Name != "" {
					entry["name"] = scope.Name
				}
				scopes = append(scopes, entry)
			}

			tokenValue := ""
			if current.options.Token != "" {
				tokenValue = "[REDACTED]"
			}

			return current.out.JSON(map[string]any{
				"server": map[string]string{
					"value":  current.server,
					"source": current.serverSource,
				},
				"token": map[string]string{
					"value":  tokenValue,
					"source": current.tokenSource,
				},
				"configFile":  current.configPath,
				"scopes":      scopes,
				"scopeSource": scopeSource,
			})
		},
	}
}

func newConfigPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the credentials file in use",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return current.out.JSON(map[string]string{"configFile": current.configPath})
		},
	}
}
