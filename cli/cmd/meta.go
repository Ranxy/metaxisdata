package cmd

import (
	"context"
	"strings"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/cli/client"
	"github.com/Ranxy/metaxisdata/cli/output"
)

var metaFlags struct {
	metaType         string
	parentGUIDPrefix string
	keyword          string
}

func newMetaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "meta",
		Short: "Browse metadata: tables, views, columns and their DDL",
	}
	cmd.AddCommand(newMetaListCmd(), newMetaGetCmd(), newMetaSearchCmd(), newMetaDDLCmd())
	return cmd
}

func newMetaListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <parent-guid>",
		Short: "List the objects directly under a GUID",
		Long: `List the children of a metadata object.

Every result carries its GUID, so a listing can be fed straight into
` + "`mxd meta get`" + `, ` + "`mxd meta ddl`" + ` or the lineage commands. On MySQL-family
engines a database has an empty-named schema level, which shows up as one extra
step: the GUID of that level ends with ";".`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			connection, err := current.connect()
			if err != nil {
				return err
			}

			metaType, err := parseMetaType(metaFlags.metaType)
			if err != nil {
				return err
			}
			// A ListMetadata response has no page token of its own: each meta
			// type carries its own. Walk one type when the caller named it, and
			// otherwise make a single call and hand back the per-type tokens so
			// the caller can continue where it wants to.
			type group struct {
				MetaType      v1pb.MetaType          `json:"metaType"`
				List          []*v1pb.StoredMetadata `json:"list"`
				NextPageToken string                 `json:"nextPageToken,omitempty"`
			}
			var groups []group
			truncated := false

			if metaType != v1pb.MetaType_UNSPECIFIED {
				groups, truncated, _, err = collectPages(cmd.Context(), current.pageSize, current.maxItems, listPageSize,
					func(ctx context.Context, pageToken string, pageSize int32) (page[group], error) {
						pageRequest := &v1pb.ListMetadataRequest{
							ParentGuid: args[0],
							PageSize:   pageSize,
							PageToken:  pageToken,
						}
						if metaType != v1pb.MetaType_UNSPECIFIED {
							pageRequest.MetaType = &metaType
						}

						response, err := connection.Database.ListMetadata(ctx, connect.NewRequest(pageRequest))
						if err != nil {
							return page[group]{}, err
						}
						var items []group
						next := ""
						for _, stored := range response.Msg.GetTypesStoredMetadata() {
							items = append(items, group{
								MetaType: stored.GetMetaType(),
								List:     stored.GetList(),
							})
							next = stored.GetNextPageToken()
						}
						return page[group]{items: items, next: next}, nil
					})
				if err != nil {
					return err
				}
			} else {
				pageSize := current.pageSize
				if pageSize <= 0 {
					pageSize = listPageSize
				}
				pageRequest := &v1pb.ListMetadataRequest{ParentGuid: args[0], PageSize: pageSize}
				response, err := connection.Database.ListMetadata(cmd.Context(), connect.NewRequest(pageRequest))
				if err != nil {
					return err
				}
				for _, stored := range response.Msg.GetTypesStoredMetadata() {
					groups = append(groups, group{
						MetaType:      stored.GetMetaType(),
						List:          stored.GetList(),
						NextPageToken: stored.GetNextPageToken(),
					})
					if stored.GetNextPageToken() != "" {
						truncated = true
					}
				}
			}

			rows := make([]output.Row, 0)
			rows = append(rows, output.Row{"GUID", "TYPE", "NAME"})
			for _, item := range groups {
				for _, stored := range item.List {
					rows = append(rows, output.Row{stored.GetGuid(), item.MetaType.String(), storedName(stored)})
				}
			}

			payload := make([]map[string]any, 0, len(groups))
			for _, item := range groups {
				listJSON, err := output.ProtoValues(item.List)
				if err != nil {
					return err
				}
				entry := map[string]any{
					"metaType": item.MetaType.String(),
					"list":     listJSON,
				}
				if item.NextPageToken != "" {
					entry["nextPageToken"] = item.NextPageToken
				}
				payload = append(payload, entry)
			}
			return current.out.Envelope(map[string]any{
				"typesStoredMetadata": payload,
				"truncated":           truncated,
			}, rows)
		},
	}
	cmd.Flags().StringVar(&metaFlags.metaType, "type", "", "only list this meta type, e.g. TABLE")
	return cmd
}

func newMetaGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <guid>",
		Short: "Show one metadata object in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			connection, err := current.connect()
			if err != nil {
				return err
			}
			metaType, err := parseMetaType(metaFlags.metaType)
			if err != nil {
				return err
			}

			response, err := connection.Database.GetMetadata(cmd.Context(), connect.NewRequest(&v1pb.GetMetadataRequest{
				Guid:     args[0],
				MetaType: metaType,
			}))
			if err != nil {
				return err
			}
			return current.out.Message(response.Msg, nil)
		},
	}
	cmd.Flags().StringVar(&metaFlags.metaType, "type", "", "meta type of the GUID, when it is ambiguous")
	return cmd
}

func newMetaSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <keyword>",
		Short: "Find metadata by name",
		Long: `Search the metadata registry.

This is how a GUID is discovered from a name, which is what makes the rest of
the commands usable without memorising identifiers.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			connection, err := current.connect()
			if err != nil {
				return err
			}
			metaType, err := parseMetaType(metaFlags.metaType)
			if err != nil {
				return err
			}

			results, truncated, nextToken, err := collectPages(cmd.Context(), current.pageSize, current.maxItems, 50,
				func(ctx context.Context, pageToken string, pageSize int32) (page[*v1pb.SearchMetadataResult], error) {
					pageRequest := &v1pb.SearchMetadataRequest{
						SearchStr: args[0],
						PageSize:  pageSize,
						PageToken: pageToken,
					}
					if metaType != v1pb.MetaType_UNSPECIFIED {
						pageRequest.MetaType = &metaType
					}
					if metaFlags.parentGUIDPrefix != "" {
						prefix := metaFlags.parentGUIDPrefix
						pageRequest.ParentGuidPrefix = &prefix
					}

					response, err := connection.Database.SearchMetadata(ctx, connect.NewRequest(pageRequest))
					if err != nil {
						return page[*v1pb.SearchMetadataResult]{}, err
					}
					return page[*v1pb.SearchMetadataResult]{items: response.Msg.GetResults(), next: response.Msg.GetNextPageToken()}, nil
				})
			if err != nil {
				return err
			}

			rows := make([]output.Row, 0)
			rows = append(rows, output.Row{"GUID", "TYPE", "NAME"})
			for _, result := range results {
				rows = append(rows, output.Row{result.GetGuid(), result.GetMetaType().String(), storedName(result.GetMetadata())})
			}

			resultsJSON, err := output.ProtoValues(results)
			if err != nil {
				return err
			}
			envelope := map[string]any{
				"results":   resultsJSON,
				"truncated": truncated,
			}
			if nextToken != "" {
				envelope["nextPageToken"] = nextToken
			}
			return current.out.Envelope(envelope, rows)
		},
	}
	cmd.Flags().StringVar(&metaFlags.metaType, "type", "", "only search this meta type, e.g. TABLE")
	cmd.Flags().StringVar(&metaFlags.parentGUIDPrefix, "parent-guid-prefix", "", "only search inside this GUID subtree")
	return cmd
}

func newMetaDDLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ddl <guid>",
		Short: "Print the DDL of one metadata object",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			connection, err := current.connect()
			if err != nil {
				return err
			}
			metaType, err := parseMetaType(metaFlags.metaType)
			if err != nil {
				return err
			}

			response, err := connection.Database.GetSchemaString(cmd.Context(), connect.NewRequest(&v1pb.GetSchemaStringRequest{
				Guid:     args[0],
				MetaType: metaType,
			}))
			if err != nil {
				return err
			}
			return current.out.JSON(map[string]string{"guid": args[0], "schema": response.Msg.GetSchema()})
		},
	}
	cmd.Flags().StringVar(&metaFlags.metaType, "type", "", "meta type of the GUID, when it is ambiguous")
	return cmd
}

// parseMetaType reads a meta type name. An empty value stays unspecified, which
// lets the server infer the type from the GUID.
func parseMetaType(value string) (v1pb.MetaType, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return v1pb.MetaType_UNSPECIFIED, nil
	}
	number, ok := v1pb.MetaType_value[strings.ToUpper(trimmed)]
	if !ok {
		return v1pb.MetaType_UNSPECIFIED, client.Usage("unknown meta type %q", value).
			WithHint("valid types include TABLE, VIEW, MATERIALIZED_VIEW, COLUMN, MANUAL_SQL")
	}
	return v1pb.MetaType(number), nil
}

// storedName reads the object name out of a stored metadata payload, which is
// what a listing shows next to the GUID.
func storedName(stored *v1pb.StoredMetadata) string {
	switch typed := stored.GetType().(type) {
	case *v1pb.StoredMetadata_DatabaseSchemaMetadata:
		return typed.DatabaseSchemaMetadata.GetName()
	case *v1pb.StoredMetadata_SchemaMetadata:
		return typed.SchemaMetadata.GetName()
	case *v1pb.StoredMetadata_TableMetadata:
		return typed.TableMetadata.GetName()
	case *v1pb.StoredMetadata_ExternalTableMetadata:
		return typed.ExternalTableMetadata.GetName()
	case *v1pb.StoredMetadata_ViewMetadata:
		return typed.ViewMetadata.GetName()
	case *v1pb.StoredMetadata_MaterializedViewMetadata:
		return typed.MaterializedViewMetadata.GetName()
	case *v1pb.StoredMetadata_FunctionMetadata:
		return typed.FunctionMetadata.GetName()
	case *v1pb.StoredMetadata_ProcedureMetadata:
		return typed.ProcedureMetadata.GetName()
	case *v1pb.StoredMetadata_SequenceMetadata:
		return typed.SequenceMetadata.GetName()
	case *v1pb.StoredMetadata_ColumnMetadata:
		return typed.ColumnMetadata.GetName()
	case *v1pb.StoredMetadata_ManualSqlMetadata:
		return typed.ManualSqlMetadata.GetTitle()
	default:
		return ""
	}
}
