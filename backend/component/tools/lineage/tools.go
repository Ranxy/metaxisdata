package lineage

// The lineage tool bodies. MCP and the built-in agent run these; the transport
// keeps its schema, permission and audit envelope.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	"github.com/Ranxy/metaxisdata/backend/component/tools/metadata"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

const (
	// maxAnalyzeSQLScopes mirrors the AnalyzeSQL RPC's own bound.
	maxAnalyzeSQLScopes = 10
	// MaxAnalyzeSQLScopes is the exported bound the transport's contract tests read.
	MaxAnalyzeSQLScopes = maxAnalyzeSQLScopes
	// maxSQLLength mirrors the AnalyzeSQL RPC's own request cap.
	maxSQLLength = 1 << 20
	// maxExpandedTargets bounds the graph walks one analysis may trigger.
	maxExpandedTargets = 20
	// defaultGraphDepth is what a lineage question expands when the caller does not
	// say; two hops is usually enough to answer "what feeds this".
	defaultGraphDepth = 3
	// maxGraphDepth mirrors the GetLineageGraph RPC's own bound.
	maxGraphDepth = 10
)

// Reader is the slice of the lineage service the tools use.
type Reader interface {
	AnalyzeSQL(context.Context, *connect.Request[v1pb.AnalyzeSQLRequest]) (*connect.Response[v1pb.AnalyzeSQLResponse], error)
	GetLineageGraph(context.Context, *connect.Request[v1pb.GetLineageGraphRequest]) (*connect.Response[v1pb.GetLineageGraphResponse], error)
}

// Tools runs the lineage tools.
type Tools struct {
	lineage  Reader
	resolver *metadata.Resolver
}

// NewTools builds the lineage tool set over the lineage service and the shared
// name resolver.
func NewTools(lineage Reader, resolver *metadata.Resolver) *Tools {
	return &Tools{lineage: lineage, resolver: resolver}
}

// parseDirection maps the direction onto the lineage type: SOURCE is upstream,
// the direction in which the object is a target.
func ParseDirection(value string) (v1pb.LineageType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "both":
		return v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED, nil
	case "up", "upstream", "source":
		return v1pb.LineageType_SOURCE, nil
	case "down", "downstream", "target":
		return v1pb.LineageType_TARGET, nil
	default:
		return v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED, metadata.InvalidArgument("unknown direction %q, expected up, down or both", value)
	}
}

// --- analyze_sql ---

type AnalyzeSQLArgs struct {
	SQL    string                   `json:"sql"`
	Scopes []metadata.AnalysisScope `json:"scopes"`
	Depth  int32                    `json:"depth"`
}

type analyzeSQLScopeResult struct {
	ScopeName   string          `json:"scopeName,omitempty"`
	ScopeGUID   string          `json:"scopeGuid"`
	Relations   json.RawMessage `json:"relations"`
	Temporary   json.RawMessage `json:"temporaryRelations"`
	Diagnostics json.RawMessage `json:"diagnostics"`
	Omitted     int32           `json:"omittedDiagnosticCount,omitempty"`
	Warnings    []string        `json:"warnings"`
	Graphs      json.RawMessage `json:"graphs,omitempty"`
}

type analyzeSQLResult struct {
	Results  []analyzeSQLScopeResult `json:"results"`
	Warnings []string                `json:"warnings"`
}

func (t *Tools) AnalyzeSQL(ctx context.Context, raw json.RawMessage) (any, error) {
	var args AnalyzeSQLArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.SQL) == "" {
		return nil, metadata.InvalidArgument("sql is required")
	}
	if len(args.SQL) > maxSQLLength {
		return nil, metadata.InvalidArgument("the statement is larger than the %d byte limit", maxSQLLength)
	}
	if len(args.Scopes) == 0 {
		return nil, t.resolver.ScopeRequired(ctx)
	}
	if len(args.Scopes) > maxAnalyzeSQLScopes {
		return nil, metadata.InvalidArgument("at most %d scopes may be analyzed at once", maxAnalyzeSQLScopes)
	}
	if args.Depth < 0 || args.Depth > maxGraphDepth {
		return nil, metadata.InvalidArgument("depth must be between 0 and %d", maxGraphDepth)
	}

	request := &v1pb.AnalyzeSQLRequest{SqlText: args.SQL}
	for _, scope := range args.Scopes {
		guid, err := t.resolver.ResolveScope(ctx, scope)
		if err != nil {
			return nil, err
		}
		request.Scopes = append(request.Scopes, &v1pb.AnalysisScope{
			// The name is an opaque label the server echoes back, so results can be
			// attributed to the scope they came from.
			Name: scopeLabel(scope),
			Guid: guid,
		})
	}
	response, err := t.lineage.AnalyzeSQL(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}

	result := analyzeSQLResult{
		Results:  make([]analyzeSQLScopeResult, 0, len(response.Msg.GetResults())),
		Warnings: metadata.StringsOrEmpty(response.Msg.GetWarnings()),
	}
	for _, analyzed := range response.Msg.GetResults() {
		relations, temporary, err := splitTemporaryRelations(analyzed.GetRelations())
		if err != nil {
			return nil, err
		}
		diagnostics, err := metadata.ProtoJSONValues(analyzed.GetDiagnostics())
		if err != nil {
			return nil, err
		}
		scopeResult := analyzeSQLScopeResult{
			ScopeName:   analyzed.GetScopeName(),
			ScopeGUID:   analyzed.GetScopeGuid(),
			Relations:   relations,
			Temporary:   temporary,
			Diagnostics: diagnostics,
			Omitted:     analyzed.GetOmittedDiagnosticCount(),
			Warnings:    metadata.StringsOrEmpty(analyzed.GetWarnings()),
		}
		if args.Depth > 0 {
			graphs, warning, err := t.expandTargets(ctx, analyzed, args.Depth)
			if err != nil {
				return nil, err
			}
			if warning != "" {
				scopeResult.Warnings = append(scopeResult.Warnings, warning)
			}
			if len(graphs) > 0 {
				encoded, err := metadata.ProtoJSONValues(graphs)
				if err != nil {
					return nil, err
				}
				scopeResult.Graphs = encoded
			}
		}
		result.Results = append(result.Results, scopeResult)
	}
	return result, nil
}

// splitTemporaryRelations separates a statement's real relations from the ones
// whose target exists only inside the statement. Both are returned: hiding the
// temporary ones would turn "where do these output columns come from" — the only
// question a bare SELECT answers — into a second call.
func splitTemporaryRelations(relations []*v1pb.AnalyzeSQLRelation) (json.RawMessage, json.RawMessage, error) {
	existingRelations := make([]*v1pb.AnalyzeSQLRelation, 0, len(relations))
	temporaryRelations := make([]*v1pb.AnalyzeSQLRelation, 0)
	for _, relation := range relations {
		if relation.GetIsTemp() {
			temporaryRelations = append(temporaryRelations, relation)
			continue
		}
		existingRelations = append(existingRelations, relation)
	}
	existing, err := metadata.ProtoJSONValues(existingRelations)
	if err != nil {
		return nil, nil, err
	}
	temporary, err := metadata.ProtoJSONValues(temporaryRelations)
	if err != nil {
		return nil, nil, err
	}
	return existing, temporary, nil
}

// expandTargets walks the graph from each real target the statement produced. A
// target the registry has no entry for cannot be expanded, and that is not a reason
// to lose the analysis; neither is a statement with more targets than the budget,
// which comes back with the first ones and a warning saying so.
func (t *Tools) expandTargets(ctx context.Context, analyzed *v1pb.AnalyzeSQLResult, depth int32) ([]*v1pb.GetLineageGraphResponse, string, error) {
	var graphs []*v1pb.GetLineageGraphResponse
	seen := map[string]bool{}
	for _, relation := range analyzed.GetRelations() {
		target := relation.GetTargetGuid()
		if target == "" || relation.GetIsTemp() || seen[target] {
			continue
		}
		seen[target] = true
		if len(graphs) >= maxExpandedTargets {
			return graphs, fmt.Sprintf("only the first %d targets were expanded; ask per target for the rest", maxExpandedTargets), nil
		}
		response, err := t.lineage.GetLineageGraph(ctx, connect.NewRequest(&v1pb.GetLineageGraphRequest{
			Guid:  target,
			Depth: depth,
		}))
		if err != nil {
			if connect.CodeOf(err) == connect.CodeNotFound {
				continue
			}
			return nil, "", metadata.FromRPC(err)
		}
		graphs = append(graphs, response.Msg)
	}
	return graphs, "", nil
}

// scopeLabel names a scope for attribution only; the guid is what resolves it.
func scopeLabel(scope metadata.AnalysisScope) string {
	if scope.Schema != "" {
		return scope.Database + "." + scope.Schema
	}
	return scope.Database
}

// --- get_lineage_graph ---

type GetLineageGraphArgs struct {
	Object    metadata.ObjectRef `json:"object"`
	Depth     int32              `json:"depth"`
	Direction string             `json:"direction"`
	Column    string             `json:"column"`
}

func (t *Tools) GetLineageGraph(ctx context.Context, raw json.RawMessage) (any, error) {
	var args GetLineageGraphArgs
	if err := metadata.DecodeArguments(raw, &args); err != nil {
		return nil, err
	}
	lineageType, err := ParseDirection(args.Direction)
	if err != nil {
		return nil, err
	}
	depth := args.Depth
	if depth == 0 {
		depth = defaultGraphDepth
	}
	if depth < 1 || depth > maxGraphDepth {
		return nil, metadata.InvalidArgument("depth must be between 1 and %d", maxGraphDepth)
	}
	object, err := t.resolver.ResolveObject(ctx, args.Object, v1pb.MetaType_UNSPECIFIED)
	if err != nil {
		return nil, err
	}
	response, err := t.lineage.GetLineageGraph(ctx, connect.NewRequest(&v1pb.GetLineageGraphRequest{
		Guid:        object.GUID,
		LineageType: lineageType,
		Depth:       depth,
	}))
	if err != nil {
		return nil, metadata.FromRPC(err)
	}
	graph := response.Msg
	if args.Column != "" {
		graph = filterGraphByColumn(graph, args.Column)
	}
	return metadata.ProtoJSON(graph)
}

// filterGraphByColumn keeps the edges of one column and the nodes they still
// connect. It mirrors the CLI's filter: the answer to "where does this column
// come from" should not carry the rest of the graph.
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
