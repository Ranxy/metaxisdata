package cmd

import (
	"context"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/cli/env"
	"github.com/Ranxy/metaxisdata/cli/output"
)

var databaseListFlags struct {
	instance string
}

func newDatabaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "database",
		Short: "Browse databases",
	}
	cmd.AddCommand(newDatabaseListCmd())
	return cmd
}

func newDatabaseListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List databases",
		Long: `List databases.

Each row carries the database GUID, which is what an analysis scope is made of:
copy it into your project's ` + env.ScopesEnv + ` so the agent does not have to
guess which database a SQL statement belongs to.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			connection, err := current.connect()
			if err != nil {
				return err
			}

			parent := "workspaces/-"
			if databaseListFlags.instance != "" {
				parent = formatInstance(databaseListFlags.instance)
			}

			databases, truncated, nextToken, err := collectPages(cmd.Context(), current.pageSize, current.maxItems, listPageSize,
				func(ctx context.Context, pageToken string, pageSize int32) (page[*v1pb.Database], error) {
					response, err := connection.Database.ListDatabases(ctx, connect.NewRequest(&v1pb.ListDatabasesRequest{
						Parent:    parent,
						PageSize:  pageSize,
						PageToken: pageToken,
					}))
					if err != nil {
						return page[*v1pb.Database]{}, err
					}
					return page[*v1pb.Database]{items: response.Msg.GetDatabases(), next: response.Msg.GetNextPageToken()}, nil
				})
			if err != nil {
				return err
			}

			rows := make([]output.Row, 0, len(databases)+1)
			rows = append(rows, output.Row{"NAME", "GUID", "ENVIRONMENT", "SYNCED"})
			for _, database := range databases {
				rows = append(rows, output.Row{database.GetName(), database.GetGuid(), database.GetEffectiveEnvironment(), database.GetSuccessfulSyncTime().AsTime().Format("2006-01-02 15:04")})
			}

			envelope := map[string]any{
				"databases": databases,
				"truncated": truncated,
			}
			if nextToken != "" {
				envelope["nextPageToken"] = nextToken
			}
			return current.out.Envelope(envelope, rows)
		},
	}
	cmd.Flags().StringVar(&databaseListFlags.instance, "instance", "", "only databases of this instance resource id")
	return cmd
}
