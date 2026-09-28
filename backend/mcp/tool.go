package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

const (
	// defaultPageSize is what a listing returns when the caller does not say. A
	// model's context is the scarce resource here: 25 rows answer the question,
	// a thousand bury it.
	defaultPageSize = 25
	// maxPageSize caps one listing. A larger request is clamped rather than
	// refused, because the caller gets a truthful next-page token either way.
	maxPageSize = 200
	// maxAnalyzeSQLScopes mirrors the AnalyzeSQL RPC's own bound.
	maxAnalyzeSQLScopes = 10
	// maxSQLLength mirrors the AnalyzeSQL RPC's own request cap.
	maxSQLLength = 1 << 20
)

// toolDefinitions is the tool table. It is a method because each tool closes over
// the configured read services.
func (s *Server) toolDefinitions() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "list_instances",
			Description: "List the database instances this workspace has registered. Start here when you do not yet know which instance a question is about.",
			RPC:         "metaxisdata.v1.InstanceService.ListInstances",
			Permission:  permission.InstancesList,
			Schema: objectSchema(map[string]any{
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}),
			Run: s.runListInstances,
		},
		{
			Name:        "list_databases",
			Description: "List the databases of one instance, each with the guid that identifies it. Use it to find the database a SQL statement or an object belongs to.",
			RPC:         "metaxisdata.v1.DatabaseService.ListDatabases",
			Permission:  permission.DatabasesList,
			Schema: objectSchema(map[string]any{
				"instance":   stringProperty("instance name or instances/<id>"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}, "instance"),
			Run: s.runListDatabases,
		},
		{
			Name:        "search_metadata",
			Description: "Find registry objects by name. This is the entry point when you have a name but no reference; it returns each match's guid, type and parent so the next call can address it exactly.",
			RPC:         "metaxisdata.v1.DatabaseService.SearchMetadata",
			Permission:  permission.DatabasesRead,
			Schema: objectSchema(map[string]any{
				"keyword":    stringProperty("the name, or part of it"),
				"meta_type":  stringProperty("restrict to one type, for example TABLE"),
				"instance":   stringProperty("restrict the search to this instance"),
				"database":   stringProperty("restrict the search to this database (needs instance)"),
				"schema":     stringProperty("restrict the search to this schema (needs instance and database)"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}, "keyword"),
			Run: s.runSearchMetadata,
		},
		{
			Name:        "list_metadata",
			Description: "List what is directly under an object: the schemas of a database, the tables and views of a schema, the columns of a table. Use get_metadata when you want one object's full shape.",
			RPC:         "metaxisdata.v1.DatabaseService.ListMetadata",
			Permission:  permission.DatabasesRead,
			Schema: objectSchema(map[string]any{
				"parent":     objectRefSchema("the object whose children you want; give a guid, or {instance, database, schema?}"),
				"meta_type":  stringProperty("only list this type, for example TABLE"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}, "parent"),
			Run: s.runListMetadata,
		},
		{
			Name:        "get_metadata",
			Description: "Return one object's full structure: columns with their types, indexes, keys and partitions. Ask for this after search_metadata or list_metadata gave you the object.",
			RPC:         "metaxisdata.v1.DatabaseService.GetMetadata",
			Permission:  permission.DatabasesRead,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to read; give a guid, or {instance, database, schema?, name}"),
				"meta_type": stringProperty("the object's type, when the reference is a bare guid"),
			}, "object"),
			Run: s.runGetMetadata,
		},
		{
			Name:        "get_ddl",
			Description: "Return one object's definition as its source engine reports it. Use it when the question is about the SQL that defines a table, view or function.",
			RPC:         "metaxisdata.v1.DatabaseService.GetSchemaString",
			Permission:  permission.DatabasesRead,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to read; give a guid, or {instance, database, schema?, name}"),
				"meta_type": stringProperty("the object's type, when the reference is a bare guid"),
			}, "object"),
			Run: s.runGetDDL,
		},
		{
			Name:        "analyze_sql",
			Description: "Analyze one SQL statement and report, column by column, what it reads and what it writes. Use this for a statement you have in hand; use get_lineage_graph for an object already in the registry. Each scope is analyzed on its own and reported separately.",
			RPC:         "metaxisdata.v1.LineageService.AnalyzeSQL",
			Permission:  permission.LineageGet,
			Schema: objectSchema(map[string]any{
				"sql": stringProperty("the statement to analyze"),
				"scopes": map[string]any{
					"type":        "array",
					"description": "one entry per database the statement should be resolved against; required, because unqualified names cannot say which instance they belong to",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"instance": stringProperty("instance name or instances/<id>"),
							"database": stringProperty("database name or its guid"),
							"schema":   stringProperty("schema name, when the engine has one"),
						},
						"required": []string{"instance", "database"},
					},
				},
				"depth": map[string]any{
					"type":        "integer",
					"description": "also expand each resolved target this many levels (0-10, default 0)",
				},
			}, "sql"),
			Run: s.runAnalyzeSQL,
		},
		{
			Name:        "get_lineage_graph",
			Description: "Return the multi-level lineage around one object: what it is built from, or what it feeds. The graph is column-level, and an object whose definition yields no column-level edge does not appear.",
			RPC:         "metaxisdata.v1.LineageService.GetLineageGraph",
			Permission:  permission.LineageGet,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to start from; give a guid, or {instance, database, schema?, name}"),
				"depth":     map[string]any{"type": "integer", "description": "how many hops to expand (1-10, default 3)"},
				"direction": stringProperty("up (where data comes from), down (what it feeds) or both; default both"),
				"column":    stringProperty("keep only the edges that touch this column"),
			}, "object"),
			Run: s.runGetLineageGraph,
		},
		{
			Name:        "whoami",
			Description: "Report who the caller is and which permissions it holds. Use it before concluding that a question is unanswerable: the permissions say what this client may read.",
			RPC:         "metaxisdata.v1.UserService.GetCurrentUser",
			Schema:      objectSchema(map[string]any{}),
			Run:         s.runWhoami,
		},
	}
}

// protoJSON renders one message the way the rest of the platform does —
// lowerCamelCase field names, enums by name — but compactly and without the unset
// fields the CLI emits, because a model reading a listing does not need a page of
// empty strings.
func protoJSON(message proto.Message) (json.RawMessage, error) {
	payload, err := protojson.MarshalOptions{}.Marshal(message)
	if err != nil {
		return nil, internalFailure(err)
	}
	return json.RawMessage(payload), nil
}

// protoJSONValues renders a list; an empty list becomes [] rather than null, so
// "nothing to report" never looks like "not reported".
func protoJSONValues[T proto.Message](messages []T) (json.RawMessage, error) {
	values := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		value, err := protoJSON(message)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, internalFailure(err)
	}
	return json.RawMessage(payload), nil
}

// --- list_instances ---

type listInstancesArgs struct {
	PageSize  int32  `json:"page_size"`
	PageToken string `json:"page_token"`
}

type instanceRow struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Engine      string `json:"engine"`
	Environment string `json:"environment"`
}

type listInstancesResult struct {
	Instances     []instanceRow `json:"instances"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

func (s *Server) runListInstances(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args listInstancesArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	response, err := s.config.Instances.ListInstances(ctx, connect.NewRequest(&v1pb.ListInstancesRequest{
		PageSize:  pageSize,
		PageToken: args.PageToken,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	result := listInstancesResult{Instances: make([]instanceRow, 0, len(response.Msg.GetInstances()))}
	for _, instance := range response.Msg.GetInstances() {
		result.Instances = append(result.Instances, instanceRow{
			Name:        instance.GetName(),
			Title:       instance.GetTitle(),
			Engine:      instance.GetEngine().String(),
			Environment: instance.GetEnvironment(),
		})
	}
	result.NextPageToken = response.Msg.GetNextPageToken()
	return result, nil
}

// --- list_databases ---

type listDatabasesArgs struct {
	Instance  string `json:"instance"`
	PageSize  int32  `json:"page_size"`
	PageToken string `json:"page_token"`
}

type databaseRow struct {
	Name        string `json:"name"`
	GUID        string `json:"guid"`
	Environment string `json:"environment,omitempty"`
	Synced      string `json:"synced,omitempty"`
}

type listDatabasesResult struct {
	Databases     []databaseRow `json:"databases"`
	NextPageToken string        `json:"nextPageToken,omitempty"`
}

func (s *Server) runListDatabases(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args listDatabasesArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Instance) == "" {
		return nil, newToolError(codeInvalidArgument, "instance is required",
			"list_instances shows the instances this workspace has")
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	instanceName, err := s.resolveInstance(ctx, args.Instance)
	if err != nil {
		return nil, err
	}
	response, err := s.config.Databases.ListDatabases(ctx, connect.NewRequest(&v1pb.ListDatabasesRequest{
		Parent:    instanceName,
		PageSize:  pageSize,
		PageToken: args.PageToken,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	result := listDatabasesResult{Databases: make([]databaseRow, 0, len(response.Msg.GetDatabases()))}
	for _, database := range response.Msg.GetDatabases() {
		row := databaseRow{
			Name:        lastPathSegment(database.GetName()),
			GUID:        database.GetGuid(),
			Environment: database.GetEffectiveEnvironment(),
		}
		if synced := database.GetSuccessfulSyncTime(); synced != nil && synced.AsTime().Unix() > 0 {
			row.Synced = synced.AsTime().UTC().Format(time.RFC3339)
		}
		result.Databases = append(result.Databases, row)
	}
	result.NextPageToken = response.Msg.GetNextPageToken()
	return result, nil
}

// --- search_metadata ---

type searchMetadataArgs struct {
	Keyword   string `json:"keyword"`
	MetaType  string `json:"meta_type"`
	Instance  string `json:"instance"`
	Database  string `json:"database"`
	Schema    string `json:"schema"`
	PageSize  int32  `json:"page_size"`
	PageToken string `json:"page_token"`
}

type objectRow struct {
	GUID       string `json:"guid"`
	Name       string `json:"name"`
	MetaType   string `json:"metaType"`
	ParentGUID string `json:"parentGuid,omitempty"`
}

type objectListResult struct {
	Objects       []objectRow `json:"objects"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
}

func (s *Server) runSearchMetadata(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args searchMetadataArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Keyword) == "" {
		return nil, invalidArgument("keyword is required")
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	request := &v1pb.SearchMetadataRequest{
		SearchStr: args.Keyword,
		PageSize:  pageSize,
		PageToken: args.PageToken,
	}
	if metaType != v1pb.MetaType_UNSPECIFIED {
		request.MetaType = &metaType
	}
	// A scope narrows the search; without one it is a search across the workspace,
	// which is what a caller who has only a name wants.
	if strings.TrimSpace(args.Instance) != "" && strings.TrimSpace(args.Database) != "" {
		parentGUID, err := s.resolveScope(ctx, analysisScope{Instance: args.Instance, Database: args.Database, Schema: args.Schema})
		if err != nil {
			return nil, err
		}
		request.ParentGuidPrefix = &parentGUID
	}
	response, err := s.config.Databases.SearchMetadata(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	result := objectListResult{Objects: make([]objectRow, 0, len(response.Msg.GetResults()))}
	for _, found := range response.Msg.GetResults() {
		result.Objects = append(result.Objects, objectRow{
			GUID:       found.GetGuid(),
			Name:       storedMetadataName(found.GetMetadata()),
			MetaType:   found.GetMetaType().String(),
			ParentGUID: parentGUIDOf(found.GetGuid()),
		})
	}
	result.NextPageToken = response.Msg.GetNextPageToken()
	return result, nil
}

// --- list_metadata ---

type listMetadataArgs struct {
	Parent    objectRef `json:"parent"`
	MetaType  string    `json:"meta_type"`
	PageSize  int32     `json:"page_size"`
	PageToken string    `json:"page_token"`
}

type metadataListResult struct {
	Objects []objectRow `json:"objects"`
	// NextPageTokens is per type, because a listing that was not narrowed to one
	// type carries a token per type: continuing it means naming a type.
	NextPageTokens map[string]string `json:"nextPageTokens,omitempty"`
}

func (s *Server) runListMetadata(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args listMetadataArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	parent, err := s.resolveParent(ctx, args.Parent, metaType)
	if err != nil {
		return nil, err
	}
	request := &v1pb.ListMetadataRequest{
		ParentGuid: parent.GUID,
		PageSize:   pageSize,
		PageToken:  args.PageToken,
	}
	if metaType != v1pb.MetaType_UNSPECIFIED {
		request.MetaType = &metaType
	}
	response, err := s.config.Databases.ListMetadata(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	result := metadataListResult{Objects: make([]objectRow, 0)}
	tokens := map[string]string{}
	for _, group := range response.Msg.GetTypesStoredMetadata() {
		for _, stored := range group.GetList() {
			result.Objects = append(result.Objects, objectRow{
				GUID:       stored.GetGuid(),
				Name:       storedMetadataName(stored),
				MetaType:   group.GetMetaType().String(),
				ParentGUID: parent.GUID,
			})
		}
		if group.GetNextPageToken() != "" {
			tokens[group.GetMetaType().String()] = group.GetNextPageToken()
		}
	}
	if len(tokens) > 0 {
		result.NextPageTokens = tokens
	}
	return result, nil
}

// --- get_metadata ---

type getMetadataArgs struct {
	Object   objectRef `json:"object"`
	MetaType string    `json:"meta_type"`
}

type getMetadataResult struct {
	GUID     string          `json:"guid"`
	MetaType string          `json:"metaType,omitempty"`
	Name     string          `json:"name,omitempty"`
	Metadata json.RawMessage `json:"metadata"`
}

func (s *Server) runGetMetadata(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args getMetadataArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	object, err := s.resolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	response, err := s.config.Databases.GetMetadata(ctx, connect.NewRequest(&v1pb.GetMetadataRequest{
		Guid:     object.GUID,
		MetaType: object.MetaType,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	metadata, err := protoJSON(response.Msg.GetMetadata())
	if err != nil {
		return nil, err
	}
	return getMetadataResult{
		GUID:     object.GUID,
		MetaType: object.MetaType.String(),
		Name:     object.Name,
		Metadata: metadata,
	}, nil
}

// --- get_ddl ---

type getDDLArgs struct {
	Object   objectRef `json:"object"`
	MetaType string    `json:"meta_type"`
}

type getDDLResult struct {
	GUID   string `json:"guid"`
	Schema string `json:"schema"`
}

func (s *Server) runGetDDL(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args getDDLArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	object, err := s.resolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	response, err := s.config.Databases.GetSchemaString(ctx, connect.NewRequest(&v1pb.GetSchemaStringRequest{
		Guid:     object.GUID,
		MetaType: object.MetaType,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	return getDDLResult{GUID: object.GUID, Schema: response.Msg.GetSchema()}, nil
}

// --- analyze_sql ---

type analyzeSQLArgs struct {
	SQL    string          `json:"sql"`
	Scopes []analysisScope `json:"scopes"`
	Depth  int32           `json:"depth"`
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

func (s *Server) runAnalyzeSQL(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args analyzeSQLArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.SQL) == "" {
		return nil, invalidArgument("sql is required")
	}
	if len(args.SQL) > maxSQLLength {
		return nil, invalidArgument("the statement is larger than the %d byte limit", maxSQLLength)
	}
	if len(args.Scopes) == 0 {
		return nil, s.scopeRequired(ctx)
	}
	if len(args.Scopes) > maxAnalyzeSQLScopes {
		return nil, invalidArgument("at most %d scopes may be analyzed at once", maxAnalyzeSQLScopes)
	}
	if args.Depth < 0 || args.Depth > maxGraphDepth {
		return nil, invalidArgument("depth must be between 0 and %d", maxGraphDepth)
	}

	request := &v1pb.AnalyzeSQLRequest{SqlText: args.SQL}
	for _, scope := range args.Scopes {
		guid, err := s.resolveScope(ctx, scope)
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
	response, err := s.config.Lineage.AnalyzeSQL(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}

	result := analyzeSQLResult{
		Results:  make([]analyzeSQLScopeResult, 0, len(response.Msg.GetResults())),
		Warnings: response.Msg.GetWarnings(),
	}
	for _, analyzed := range response.Msg.GetResults() {
		relations, temporary, err := splitTemporaryRelations(analyzed.GetRelations())
		if err != nil {
			return nil, err
		}
		diagnostics, err := protoJSONValues(analyzed.GetDiagnostics())
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
			Warnings:    analyzed.GetWarnings(),
		}
		if args.Depth > 0 {
			graphs, err := s.expandTargets(ctx, analyzed, args.Depth)
			if err != nil {
				return nil, err
			}
			if len(graphs) > 0 {
				encoded, err := protoJSONValues(graphs)
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
	existing, err := protoJSONValues(existingRelations)
	if err != nil {
		return nil, nil, err
	}
	temporary, err := protoJSONValues(temporaryRelations)
	if err != nil {
		return nil, nil, err
	}
	return existing, temporary, nil
}

// expandTargets walks the graph from each real target the statement produced. A
// target the registry has no entry for cannot be expanded, and that is not a
// reason to lose the analysis.
func (s *Server) expandTargets(ctx context.Context, analyzed *v1pb.AnalyzeSQLResult, depth int32) ([]*v1pb.GetLineageGraphResponse, error) {
	var graphs []*v1pb.GetLineageGraphResponse
	seen := map[string]bool{}
	for _, relation := range analyzed.GetRelations() {
		target := relation.GetTargetGuid()
		if target == "" || relation.GetIsTemp() || seen[target] {
			continue
		}
		seen[target] = true
		response, err := s.config.Lineage.GetLineageGraph(ctx, connect.NewRequest(&v1pb.GetLineageGraphRequest{
			Guid:  target,
			Depth: depth,
		}))
		if err != nil {
			if connect.CodeOf(err) == connect.CodeNotFound {
				continue
			}
			return nil, toolErrorFromRPC(err)
		}
		graphs = append(graphs, response.Msg)
	}
	return graphs, nil
}

// scopeLabel names a scope for attribution only; the guid is what resolves it.
func scopeLabel(scope analysisScope) string {
	if scope.Schema != "" {
		return scope.Database + "." + scope.Schema
	}
	return scope.Database
}

// --- get_lineage_graph ---

type getLineageGraphArgs struct {
	Object    objectRef `json:"object"`
	Depth     int32     `json:"depth"`
	Direction string    `json:"direction"`
	Column    string    `json:"column"`
}

func (s *Server) runGetLineageGraph(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args getLineageGraphArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	lineageType, err := parseDirection(args.Direction)
	if err != nil {
		return nil, err
	}
	depth := args.Depth
	if depth == 0 {
		depth = defaultGraphDepth
	}
	if depth < 1 || depth > maxGraphDepth {
		return nil, invalidArgument("depth must be between 1 and %d", maxGraphDepth)
	}
	object, err := s.resolveObject(ctx, args.Object, v1pb.MetaType_UNSPECIFIED)
	if err != nil {
		return nil, err
	}
	response, err := s.config.Lineage.GetLineageGraph(ctx, connect.NewRequest(&v1pb.GetLineageGraphRequest{
		Guid:        object.GUID,
		LineageType: lineageType,
		Depth:       depth,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	graph := response.Msg
	if args.Column != "" {
		graph = filterGraphByColumn(graph, args.Column)
	}
	return protoJSON(graph)
}

const (
	// defaultGraphDepth is what a lineage question expands when the caller does not
	// say; two hops is usually enough to answer "what feeds this".
	defaultGraphDepth = 3
	// maxGraphDepth mirrors the GetLineageGraph RPC's own bound.
	maxGraphDepth = 10
)

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

// --- whoami ---

type whoamiResult struct {
	User        whoamiUser `json:"user"`
	Permissions []string   `json:"permissions"`
}

type whoamiUser struct {
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
}

func (s *Server) runWhoami(ctx context.Context, _ *store.UserMessage, _ json.RawMessage) (any, error) {
	response, err := s.config.Principals.GetCurrentUser(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	return whoamiResult{
		User:        whoamiUser{Email: response.Msg.GetEmail(), Name: response.Msg.GetTitle()},
		Permissions: response.Msg.GetPermissions(),
	}, nil
}

// --- shared helpers ---

// resolveParent resolves the object whose children a listing wants. A reference
// with a name is an ordinary object; without one it is a container — a database
// or a schema — which is the common case for "what is in here".
func (s *Server) resolveParent(ctx context.Context, ref objectRef, metaType v1pb.MetaType) (resolvedObject, error) {
	if ref.Name == "" {
		return s.resolveContainer(ctx, ref)
	}
	return s.resolveObject(ctx, ref, metaType)
}

// resolveContainer resolves a database or schema reference, which has no name of
// its own to look up.
func (s *Server) resolveContainer(ctx context.Context, ref objectRef) (resolvedObject, error) {
	if ref.GUID != "" {
		return resolvedObject{GUID: ref.GUID}, nil
	}
	if ref.Instance == "" || ref.Database == "" {
		return resolvedObject{}, newToolError(codeInvalidArgument, "parent needs a guid, or an instance and a database",
			"address it as {instance, database, schema?}, or pass a guid from an earlier result")
	}
	instanceName, err := s.resolveInstance(ctx, ref.Instance)
	if err != nil {
		return resolvedObject{}, err
	}
	guid, err := s.resolveDatabase(ctx, instanceName, ref.Database)
	if err != nil {
		return resolvedObject{}, err
	}
	if ref.Schema == "" {
		return resolvedObject{GUID: guid}, nil
	}
	schemaGUID, err := s.resolveSchema(ctx, guid, ref.Schema)
	if err != nil {
		return resolvedObject{}, err
	}
	return resolvedObject{GUID: schemaGUID}, nil
}

// normalizePageSize applies the listing budget: a negative size is a caller
// mistake, and a larger one is clamped rather than refused, because the answer
// stays truthful either way.
func normalizePageSize(requested int32) (int32, error) {
	switch {
	case requested < 0:
		return 0, invalidArgument("page_size must not be negative")
	case requested == 0:
		return defaultPageSize, nil
	case requested > maxPageSize:
		return maxPageSize, nil
	default:
		return requested, nil
	}
}

// parseMetaType reads a meta type name. An empty value stays unspecified, which
// lets the server infer the type from a GUID.
func parseMetaType(value string) (v1pb.MetaType, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return v1pb.MetaType_UNSPECIFIED, nil
	}
	number, ok := v1pb.MetaType_value[strings.ToUpper(trimmed)]
	if !ok {
		return v1pb.MetaType_UNSPECIFIED, newToolError(codeInvalidArgument, fmt.Sprintf("unknown meta type %q", value),
			"valid types include TABLE, VIEW, MATERIALIZED_VIEW, COLUMN, SCHEMA, MANUAL_SQL")
	}
	return v1pb.MetaType(number), nil
}

// parseDirection maps the direction onto the lineage type: SOURCE is upstream,
// the direction in which the object is a target.
func parseDirection(value string) (v1pb.LineageType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "both":
		return v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED, nil
	case "up", "upstream", "source":
		return v1pb.LineageType_SOURCE, nil
	case "down", "downstream", "target":
		return v1pb.LineageType_TARGET, nil
	default:
		return v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED, invalidArgument("unknown direction %q, expected up, down or both", value)
	}
}

// parentGUIDOf drops an object's last segment, which is its parent: the segment
// layout is the server's own, so this reads a GUID rather than building one.
func parentGUIDOf(guid string) string {
	if index := strings.LastIndex(guid, ";"); index > 0 {
		return guid[:index]
	}
	return ""
}

// objectSchema builds a tool's input schema. Every tool takes an object; a caller
// that sends no arguments at all is still well formed.
func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func objectRefSchema(description string) map[string]any {
	return map[string]any{
		"type":        "object",
		"description": description,
		"properties": map[string]any{
			"guid":     stringProperty("a guid from an earlier result; the other fields are then ignored"),
			"instance": stringProperty("instance name or instances/<id>"),
			"database": stringProperty("database name, or its guid"),
			"schema":   stringProperty("schema name, when the engine has one"),
			"name":     stringProperty("the object's name"),
		},
	}
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func pageSizeProperty() map[string]any {
	return map[string]any{"type": "integer", "description": fmt.Sprintf("rows to return (default %d, max %d)", defaultPageSize, maxPageSize)}
}

func pageTokenProperty() map[string]any {
	return map[string]any{"type": "string", "description": "continuation token from a previous call"}
}
