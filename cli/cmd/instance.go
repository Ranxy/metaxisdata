package cmd

import (
	"context"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/cli/output"
)

// listPageSize is the page size the list commands ask for. The server caps it
// per method; asking for the cap keeps the number of round trips down.
const listPageSize = 1000

func newInstanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Browse instances",
	}
	cmd.AddCommand(newInstanceListCmd())
	return cmd
}

func newInstanceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List instances",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			connection, err := current.connect()
			if err != nil {
				return err
			}

			instances, truncated, nextToken, err := collectPages(cmd.Context(), current.pageSize, current.maxItems, listPageSize,
				func(ctx context.Context, pageToken string, pageSize int32) (page[*v1pb.Instance], error) {
					response, err := connection.Instance.ListInstances(ctx, connect.NewRequest(&v1pb.ListInstancesRequest{
						PageSize:  pageSize,
						PageToken: pageToken,
					}))
					if err != nil {
						return page[*v1pb.Instance]{}, err
					}
					return page[*v1pb.Instance]{items: response.Msg.GetInstances(), next: response.Msg.GetNextPageToken()}, nil
				})
			if err != nil {
				return err
			}

			rows := make([]output.Row, 0, len(instances)+1)
			rows = append(rows, output.Row{"NAME", "TITLE", "ENGINE", "ENVIRONMENT"})
			for _, instance := range instances {
				rows = append(rows, output.Row{instance.GetName(), instance.GetTitle(), instance.GetEngine().String(), instance.GetEnvironment()})
			}

			instancesJSON, err := output.ProtoValues(instances)
			if err != nil {
				return err
			}
			envelope := map[string]any{
				"instances": instancesJSON,
				"truncated": truncated,
			}
			if nextToken != "" {
				envelope["nextPageToken"] = nextToken
			}
			return current.out.Envelope(envelope, rows)
		},
	}
}
