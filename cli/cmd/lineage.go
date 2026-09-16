package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/cli/client"
	"github.com/Ranxy/metaxisdata/cli/env"
	"github.com/Ranxy/metaxisdata/cli/output"
)

var lineageFlags struct {
	file        string
	depth       int32
	direction   string
	column      string
	includeTemp bool
}

func newLineageCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lineage",
		Short: "Trace where data comes from and what it feeds",
	}
	cmd.AddCommand(newLineageSQLCmd(), newLineageGraphCmd())
	return cmd
}

func newLineageSQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sql",
		Short: "Analyze a SQL statement and report its column-level lineage",
		Long: `Analyze one SQL statement.

A statement cannot be resolved without a database context, so at least one
analysis scope is required. Scopes come from ` + env.ScopesEnv + `; pass --scope
to narrow the selection for one call. The statement is analyzed once per scope,
independently: the scopes may live on different instances, and their results are
never merged.

Temporary relations, whose target only exists inside the statement, are hidden
unless --include-temp is given: for a statement that writes somewhere real they
are duplicates of the real target, and for one that writes nowhere they describe
the query's own output columns. A scope left with nothing to show says so rather
than looking empty.

With --depth the resolved targets are expanded into a multi-level graph, one per
scope.`,
		Args: cobra.NoArgs,
		RunE: runLineageSQL,
	}
	cmd.Flags().StringVar(&lineageFlags.file, "file", "", "read the statement from this file, or - for stdin")
	cmd.Flags().Int32Var(&lineageFlags.depth, "depth", 0, "also expand each resolved target this many levels (0-10)")
	cmd.Flags().BoolVar(&lineageFlags.includeTemp, "include-temp", false, "also report temporary relations, whose target only exists inside the statement")
	return cmd
}

func runLineageSQL(cmd *cobra.Command, _ []string) error {
	if len(current.scopes) == 0 {
		return client.Usage("no analysis scope configured").
			WithCode("scope_required").
			WithHint("set %s, for example %s='dev=<guid>'; the GUIDs come from `mxd database list` or `mxd meta list --type SCHEMA`. Which scopes a project uses is a per-project decision, so keep it in your own project docs",
				env.ScopesEnv, env.ScopesEnv)
	}

	if lineageFlags.depth < 0 || lineageFlags.depth > maxGraphDepth {
		return client.Usage("--depth must be between 0 and %d", maxGraphDepth)
	}

	sqlText, err := readSQL(lineageFlags.file)
	if err != nil {
		return err
	}
	if strings.TrimSpace(sqlText) == "" {
		return client.Usage("the SQL statement is empty")
	}

	connection, err := current.connect()
	if err != nil {
		return err
	}

	// The server caps one request at ten scopes; a wider selection is sent in
	// batches and merged, so "--scope all" keeps working.
	const maxScopesPerRequest = 10
	type scopeResult struct {
		scopeName  string
		scopeGUID  string
		relations  []*v1pb.AnalyzeSQLRelation
		graphs     []*v1pb.GetLineageGraphResponse
		warnings   []string
		hiddenTemp int
	}

	var results []scopeResult
	var requestWarnings []string

	for start := 0; start < len(current.scopes); start += maxScopesPerRequest {
		end := min(start+maxScopesPerRequest, len(current.scopes))
		batch := current.scopes[start:end]

		request := &v1pb.AnalyzeSQLRequest{SqlText: sqlText}
		for _, scope := range batch {
			request.Scopes = append(request.Scopes, &v1pb.AnalysisScope{Name: scope.Name, Guid: scope.GUID})
		}

		response, err := connection.Lineage.AnalyzeSQL(cmd.Context(), connect.NewRequest(request))
		if err != nil {
			return err
		}
		requestWarnings = append(requestWarnings, response.Msg.GetWarnings()...)

		for _, analyzed := range response.Msg.GetResults() {
			// The filter is about what to show: the graph expansion below still
			// walks the real targets, and it never used temporary ones anyway.
			relations, hiddenTemp := visibleRelations(analyzed.GetRelations(), lineageFlags.includeTemp)

			result := scopeResult{
				scopeName:  analyzed.GetScopeName(),
				scopeGUID:  analyzed.GetScopeGuid(),
				relations:  relations,
				hiddenTemp: hiddenTemp,
				warnings:   slices.Clone(analyzed.GetWarnings()),
			}
			if hiddenTemp > 0 && len(relations) == 0 {
				// Everything was hidden, so staying quiet would read as "this
				// statement has no lineage" instead of "the answer is the
				// query's own columns". Say which it is.
				result.warnings = append(result.warnings,
					fmt.Sprintf("all %d relations of this scope are temporary: the statement's result is not written anywhere; pass --include-temp to see them", hiddenTemp))
			}
			if lineageFlags.depth > 0 {
				graphs, err := expandScope(cmd.Context(), connection, analyzed, lineageFlags.depth)
				if err != nil {
					return err
				}
				result.graphs = graphs
			}
			results = append(results, result)
		}
	}

	// The shape stays grouped even for one scope, so a caller never has to
	// branch on how many scopes it asked for.
	payload := make([]map[string]any, 0, len(results))
	rows := make([]output.Row, 0)
	rows = append(rows, output.Row{"SCOPE", "SOURCE", "COLUMN", "TARGET", "TARGET COLUMN", "TEMP"})
	for _, result := range results {
		relationsJSON, err := output.ProtoValues(result.relations)
		if err != nil {
			return err
		}
		entry := map[string]any{
			"scopeName": result.scopeName,
			"scopeGuid": result.scopeGUID,
			"relations": relationsJSON,
			"warnings":  output.EnsureSlice(result.warnings),
		}
		if result.hiddenTemp > 0 {
			entry["tempRelationsHidden"] = result.hiddenTemp
		}
		if result.graphs != nil {
			graphsJSON, err := output.ProtoValues(result.graphs)
			if err != nil {
				return err
			}
			entry["graphs"] = graphsJSON
		}
		payload = append(payload, entry)

		for _, relation := range result.relations {
			rows = append(rows, output.Row{
				result.scopeName,
				relation.GetSourceGuid(),
				relation.GetSourceColumn(),
				relation.GetTargetGuid(),
				relation.GetTargetColumn(),
				fmt.Sprintf("%t", relation.GetIsTemp()),
			})
		}
	}

	if current.out.Format() == output.FormatTable {
		// A table has nowhere to carry warnings, and "no rows" would read as
		// "no lineage", so they are printed instead of dropped.
		for _, result := range results {
			for _, warning := range result.warnings {
				current.out.Progress("%s: %s", result.scopeName, warning)
			}
		}
	}

	envelope := map[string]any{
		"results":  payload,
		"warnings": output.EnsureSlice(requestWarnings),
	}
	return current.out.Envelope(envelope, rows)
}

// maxGraphDepth is the deepest expansion GetLineageGraph accepts. A larger
// request would be refused by the server, so it is refused here with a message
// that names the flag.
const maxGraphDepth = 10

// expandScope walks the graph from every real target the statement produced,
// and reports each scope's graph on its own so the environments stay distinct.
// depth is the number of levels to expand beyond the statement's own relations,
// which is why it is passed through unchanged.
func expandScope(ctx context.Context, connection *client.Client, analyzed *v1pb.AnalyzeSQLResult, depth int32) ([]*v1pb.GetLineageGraphResponse, error) {
	graphDepth := depth

	var graphs []*v1pb.GetLineageGraphResponse
	seen := map[string]bool{}
	for _, relation := range analyzed.GetRelations() {
		target := relation.GetTargetGuid()
		if target == "" || relation.GetIsTemp() || seen[target] {
			continue
		}
		seen[target] = true

		response, err := connection.Lineage.GetLineageGraph(ctx, connect.NewRequest(&v1pb.GetLineageGraphRequest{
			Guid:  target,
			Depth: graphDepth,
		}))
		if err != nil {
			// A target that the registry has no entry for cannot be expanded,
			// and that is not a reason to lose the analysis.
			if connect.CodeOf(err) == connect.CodeNotFound {
				continue
			}
			return nil, err
		}
		graphs = append(graphs, response.Msg)
	}
	return graphs, nil
}

func newLineageGraphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph <guid>",
		Short: "Show the multi-level lineage around one object",
		Long: `Expand the lineage graph around one metadata object.

The graph is column-level, and the table-level view is the collapse of those
edges: an object whose definition yields no column-level relation at all does not
appear.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			connection, err := current.connect()
			if err != nil {
				return err
			}

			lineageType, err := parseDirection(lineageFlags.direction)
			if err != nil {
				return err
			}
			depth := lineageFlags.depth
			if depth <= 0 {
				depth = 3
			}
			if depth > maxGraphDepth {
				return client.Usage("--depth must be between 1 and %d", maxGraphDepth)
			}

			response, err := connection.Lineage.GetLineageGraph(cmd.Context(), connect.NewRequest(&v1pb.GetLineageGraphRequest{
				Guid:        args[0],
				LineageType: lineageType,
				Depth:       depth,
			}))
			if err != nil {
				return err
			}
			graph := response.Msg

			if lineageFlags.column != "" {
				graph = filterGraphByColumn(graph, lineageFlags.column)
			}

			rows := make([]output.Row, 0)
			rows = append(rows, output.Row{"DISTANCE", "GUID", "TYPE", "NAME"})

			if current.out.Format() == output.FormatTable {
				for _, node := range graph.GetNodes() {
					rows = append(rows, output.Row{
						fmt.Sprintf("%d", node.GetDistance()),
						node.GetGuid(),
						node.GetMetaType().String(),
						node.GetName(),
					})
				}
				return current.out.Table(rows)
			}
			return current.out.Message(graph, nil)
		},
	}
	cmd.Flags().Int32Var(&lineageFlags.depth, "depth", 3, "how many hops to expand (1-10)")
	cmd.Flags().StringVar(&lineageFlags.direction, "direction", "both", "up, down or both")
	cmd.Flags().StringVar(&lineageFlags.column, "column", "", "only keep edges that touch this column")
	return cmd
}

// visibleRelations splits a statement's relations into the ones to report and
// the temporary ones the default hides.
//
// A temporary relation only exists today for a statement whose result is not
// written anywhere (a bare SELECT, or one whose only targets are CTEs): as soon
// as a real target exists the server drops the synthetic ones as duplicates of
// it. So hiding them by default can empty a scope, which the caller reports
// rather than letting it look like the statement has no lineage.
func visibleRelations(relations []*v1pb.AnalyzeSQLRelation, includeTemp bool) (visible []*v1pb.AnalyzeSQLRelation, hiddenTemp int) {
	if includeTemp {
		return relations, 0
	}
	visible = make([]*v1pb.AnalyzeSQLRelation, 0, len(relations))
	for _, relation := range relations {
		if relation.GetIsTemp() {
			hiddenTemp++
			continue
		}
		visible = append(visible, relation)
	}
	return visible, hiddenTemp
}

// filterGraphByColumn keeps the edges of one column and the nodes they still
// connect, which is how "where does this column come from" is answered.
func filterGraphByColumn(graph *v1pb.GetLineageGraphResponse, column string) *v1pb.GetLineageGraphResponse {
	filtered := &v1pb.GetLineageGraphResponse{
		RootGuid:         graph.GetRootGuid(),
		DepthReached:     graph.GetDepthReached(),
		Truncated:        graph.GetTruncated(),
		ExternalDatasets: graph.GetExternalDatasets(),
	}
	keep := map[string]bool{graph.GetRootGuid(): true}
	for _, edge := range graph.GetEdges() {
		if edge.GetSourceColumn() != column && edge.GetTargetColumn() != column {
			continue
		}
		filtered.Edges = append(filtered.Edges, edge)
		keep[edge.GetSourceGuid()] = true
		keep[edge.GetTargetGuid()] = true
	}
	for _, node := range graph.GetNodes() {
		if keep[node.GetGuid()] {
			filtered.Nodes = append(filtered.Nodes, node)
		}
	}
	return filtered
}

// parseDirection maps the flag onto the lineage type. SOURCE means upstream,
// which is the direction the object is the target of.
func parseDirection(value string) (v1pb.LineageType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "both":
		return v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED, nil
	case "up", "upstream", "source":
		return v1pb.LineageType_SOURCE, nil
	case "down", "downstream", "target":
		return v1pb.LineageType_TARGET, nil
	default:
		return v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED, client.Usage("unknown direction %q, expected up, down or both", value)
	}
}

// readSQL reads the statement from a file, from stdin, or from the flag's own
// value when it is neither.
func readSQL(path string) (string, error) {
	if path == "" {
		return "", client.Usage("pass the statement with --file (use - for stdin)")
	}
	if path == "-" {
		content, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read the statement from stdin: %w", err)
		}
		return string(content), nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	return string(content), nil
}
