package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

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
			Name:        "get_quality_signal",
			Description: "Report whether one object is watched by data quality checks and how healthy it is: worst latest result, last run, quality score, recent pass rate, column coverage and open issues. This answers \"is this table healthy / should I trust it\". A table with no check at all is not monitored, which the answer reports as monitored: false rather than failing; a table whose checks are all paused reports allPaused: true with enabledRuleCount: 0 and no verdict, because nothing is being verified until a check is resumed.",
			RPC:         "metaxisdata.v1.DataQualityService.ListQualityTargets",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to read; give a guid, or {instance, database, schema?, name}"),
				"meta_type": stringProperty("the object's type, when the reference is a bare guid"),
			}, "object"),
			Run: s.runGetQualitySignal,
		},
		{
			Name:        "list_quality_targets",
			Description: "List the tables and views data quality checks watch, with their health, quality score, column coverage, latest run and schedule. Use it to find which tables need attention, filtered by a name substring or by their worst latest result. Use get_quality_target to open one of them, and get_quality_signal for one object's trust signal.",
			RPC:         "metaxisdata.v1.DataQualityService.ListQualityTargets",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				"search":         stringProperty("a case-insensitive substring of the target's path or guid"),
				"worst_status":   stringProperty("only targets whose worst latest result is PASS, WARN, FAIL, ERROR, SKIPPED or NOT_EVALUATED"),
				"has_open_issue": boolProperty("only targets with an unresolved issue; false means no filter"),
				"page_size":      pageSizeProperty(),
				"page_token":     pageTokenProperty(),
			}),
			Run: s.runListQualityTargets,
		},
		{
			Name:        "get_quality_target",
			Description: "Open one table's data quality workspace: its health summary, every check with its configuration and latest result, its schedule and its unresolved problems. Use get_quality_signal for the one-line verdict, list_quality_targets to find the table, and get_quality_execution for the evidence behind a failed run.",
			RPC:         "metaxisdata.v1.DataQualityService.GetQualityTarget",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				"object":    objectRefSchema("the object to read; give a guid, or {instance, database, schema?, name}"),
				"meta_type": stringProperty("the object's type, when the reference is a bare guid"),
			}, "object"),
			Run: s.runGetQualityTarget,
		},
		{
			Name:        "list_quality_issues",
			Description: "List the data quality problem ledger: what is open, acknowledged or resolved, and which problems are muted, with severity, occurrence count and who acknowledged them. Use get_quality_target for one table's checks and health, list_quality_executions for the runs behind an issue, and get_quality_execution for the evidence of one run.",
			RPC:         "metaxisdata.v1.DataQualityService.ListQualityIssues",
			Permission:  permission.DataQualityIssuesList,
			Schema: objectSchema(map[string]any{
				"object":     objectRefSchema("only issues of this object; give a guid, or {instance, database, schema?, name}"),
				"rule":       stringProperty("only issues of this rule, by resource name"),
				"status":     stringProperty("only issues in this state: OPEN, ACKNOWLEDGED or RESOLVED"),
				"severity":   stringProperty("only issues of this severity: WARNING, ERROR or CRITICAL"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}),
			Run: s.runListQualityIssues,
		},
		{
			Name:        "list_quality_executions",
			Description: "List recent quality check runs with their verdict, thresholds and evidence flags, optionally narrowed to one object, rule, state, outcome or trigger. Use get_quality_execution for one run's full evidence — the SQL that ran and the failed sample rows.",
			RPC:         "metaxisdata.v1.DataQualityService.ListQualityExecutions",
			Permission:  permission.DataQualityExecutionsList,
			Schema: objectSchema(map[string]any{
				"object":     objectRefSchema("only executions against this object; give a guid, or {instance, database, schema?, name}"),
				"rule":       stringProperty("only executions of this rule, by resource name"),
				"state":      stringProperty("only runs in this lifecycle: PENDING, RUNNING, COMPLETED or ABANDONED"),
				"outcome":    stringProperty("only runs with this verdict: PASS, WARN, FAIL, ERROR, SKIPPED or NOT_EVALUATED"),
				"trigger":    stringProperty("only runs started this way: MANUAL or SCHEDULED"),
				"page_size":  pageSizeProperty(),
				"page_token": pageTokenProperty(),
			}),
			Run: s.runListQualityExecutions,
		},
		{
			Name:        "get_quality_execution",
			Description: "Return one quality check run's full evidence: the SQL that executed, the failed sample rows, the thresholds it was judged against, any baseline and the sampling and truncation flags. Use it to explain a verdict yourself; list_quality_executions finds a run.",
			RPC:         "metaxisdata.v1.DataQualityService.GetQualityExecution",
			Permission:  permission.DataQualityExecutionsGet,
			Schema: objectSchema(map[string]any{
				"execution": stringProperty("the execution's resource name, as a listing returned it"),
			}, "execution"),
			Run: s.runGetQualityExecution,
		},
		{
			Name:        "get_quality_impact",
			Description: "Explain one table's quality state by its lineage: the downstream assets its data reaches, and the upstream targets whose checks failed or whose structure changed in the window before now. It is derived from the recorded lineage, so an empty answer means the lineage records no such object, not that nothing consumes it. Use get_quality_target for the checks themselves and get_quality_execution for one run's evidence.",
			RPC:         "metaxisdata.v1.DataQualityService.GetQualityTargetImpact",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				"object":       objectRefSchema("the table or view to explain; give a guid, or {instance, database, schema?, name}"),
				"meta_type":    stringProperty("the object's type, when the reference is a bare guid"),
				"depth":        map[string]any{"type": "integer", "description": "how many lineage levels downstream to walk (1-3, default 2)"},
				"window_hours": map[string]any{"type": "integer", "description": "how far back upstream evidence is looked for, in hours (1-168, default 24)"},
			}, "object"),
			Run: s.runGetQualityImpact,
		},
		{
			Name:        "get_quality_consumption_coverage",
			Description: "Measure how much of the data other assets consume is watched: registry assets that another object's lineage reads, how many of them have an enabled check, and the unmonitored ones with the most consumers. This is not the coverage get_quality_signal reports: there every monitorable object counts, here only objects something reads. Use list_quality_targets or get_quality_signal to look at one of the gaps.",
			RPC:         "metaxisdata.v1.DataQualityService.GetQualityConsumptionCoverage",
			Permission:  permission.DataQualityRulesList,
			Schema: objectSchema(map[string]any{
				"page_size": pageSizeProperty(),
			}),
			Run: s.runGetQualityConsumptionCoverage,
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

// stringsOrEmpty keeps a list field a list. A nil slice marshals to null, and a
// model reading null would have to tell "there are none" from "not reported".
func stringsOrEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
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
	// which is what a caller who has only a name wants. A partial scope is
	// refused rather than ignored: silently searching everything would answer a
	// narrower question than the caller asked.
	scoped := strings.TrimSpace(args.Instance) != "" || strings.TrimSpace(args.Database) != "" || strings.TrimSpace(args.Schema) != ""
	if scoped {
		if strings.TrimSpace(args.Instance) == "" || strings.TrimSpace(args.Database) == "" {
			return nil, newToolError(codeInvalidArgument, "a scoped search needs instance and database",
				"search the whole workspace by leaving instance, database and schema out")
		}
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
	if args.PageToken != "" && metaType == v1pb.MetaType_UNSPECIFIED {
		// A listing that was not narrowed to one type carries one token per type,
		// so a token without a type says nothing about where to continue.
		return nil, newToolError(codeInvalidArgument, "page_token needs meta_type",
			"a listing that was not narrowed to one type carries one token per type; name the type you are continuing")
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
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		// The registry cannot infer an object's type from its GUID, and the RPC
		// refuses without one. Saying that here is the difference between a model
		// retrying the same call and one that fixes it.
		return nil, newToolError(codeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
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
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		return nil, newToolError(codeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
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

// --- get_quality_signal ---

type getQualitySignalArgs struct {
	Object   objectRef `json:"object"`
	MetaType string    `json:"meta_type"`
}

// qualitySignalResult is the answer for a monitored object: every trust signal
// the metadata browser shows, in one object.
type qualitySignalResult struct {
	GUID      string `json:"guid"`
	MetaType  string `json:"metaType"`
	Name      string `json:"name,omitempty"`
	Monitored bool   `json:"monitored"`
	// AllPaused says every check on the object is paused: the checks exist but
	// nothing is being verified. The counters still describe the target, while the
	// verdict fields are omitted — a paused check's last result is history, not a
	// current health claim.
	AllPaused            bool     `json:"allPaused"`
	WorstStatus          string   `json:"worstStatus,omitempty"`
	LastRunTime          string   `json:"lastRunTime,omitempty"`
	QualityScore         *float64 `json:"qualityScore,omitempty"`
	RecentPassRate       *float64 `json:"recentPassRate,omitempty"`
	RecentExecutionCount int32    `json:"recentExecutionCount"`
	CoveredColumnCount   int32    `json:"coveredColumnCount"`
	TotalColumnCount     int32    `json:"totalColumnCount"`
	OpenIssueCount       int32    `json:"openIssueCount"`
	EnabledRuleCount     int32    `json:"enabledRuleCount"`
	SchemaChanged        bool     `json:"schemaChanged"`
	Orphaned             bool     `json:"orphaned"`
}

// qualitySignalUnmonitoredResult is the answer for an object with no live rule.
// It is deliberately not an error: the object exists and simply has no checks, so
// its signal is empty rather than unknown.
type qualitySignalUnmonitoredResult struct {
	GUID      string `json:"guid"`
	MetaType  string `json:"metaType"`
	Name      string `json:"name,omitempty"`
	Monitored bool   `json:"monitored"`
}

// runGetQualitySignal reads one object's trust signal. The quality API takes an
// exact-target selection for this: a selection is unpaged, keeps the request
// order, and omits a target with no live rule, which is how "not monitored" is
// told apart from "no such object".
func (s *Server) runGetQualitySignal(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args getQualitySignalArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		return nil, newToolError(codeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
	}
	object, err := s.resolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	if !qualitySignalMetaTypes[object.MetaType] {
		// A rule can only watch a table or view. Any other object therefore has no
		// signal to read, and the quality RPC would refuse the key: "nothing watches
		// this" is the truthful answer.
		return qualitySignalNotMonitored(object), nil
	}
	response, err := s.config.Quality.ListQualityTargets(ctx, connect.NewRequest(&v1pb.ListQualityTargetsRequest{
		// "workspaces/-" is the current workspace, the way every quality RPC names it.
		Parent: "workspaces/-",
		Targets: []*v1pb.QualityTargetKey{{
			TargetGuid: object.GUID,
			MetaType:   object.MetaType,
		}},
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	summary := qualityTargetSummaryOf(response.Msg.GetQualityTargets(), object.GUID, object.MetaType)
	if summary == nil {
		// A selection omits a key with no live rule. The object exists; nothing
		// watches it, which is an answer rather than a failure.
		return qualitySignalNotMonitored(object), nil
	}
	// The aggregate counts paused checks too, so an all-paused target still carries
	// its paused checks' last verdict. That is history, not a current health claim:
	// report the state and omit the verdict rather than let a model call a paused
	// table healthy.
	allPaused := summary.GetEnabledRuleCount() <= 0
	result := qualitySignalResult{
		GUID:                 object.GUID,
		MetaType:             object.MetaType.String(),
		Name:                 qualitySignalName(object.Name, summary),
		Monitored:            true,
		AllPaused:            allPaused,
		LastRunTime:          qualitySignalTime(summary.GetLastRunTime()),
		QualityScore:         summary.QualityScore,
		RecentPassRate:       summary.RecentPassRate,
		RecentExecutionCount: summary.GetRecentExecutionCount(),
		CoveredColumnCount:   summary.GetCoveredColumnCount(),
		TotalColumnCount:     summary.GetTotalColumnCount(),
		OpenIssueCount:       summary.GetOpenIssueCount(),
		EnabledRuleCount:     summary.GetEnabledRuleCount(),
		SchemaChanged:        summary.GetSchemaChanged(),
		Orphaned:             summary.GetOrphaned(),
	}
	if !allPaused {
		result.WorstStatus = qualitySignalStatus(summary.GetWorstStatus())
	}
	return result, nil
}

// qualitySignalMetaTypes are the object types a quality rule can watch; any other
// object has no signal to read.
var qualitySignalMetaTypes = map[v1pb.MetaType]bool{
	v1pb.MetaType_TABLE:             true,
	v1pb.MetaType_EXTERNAL_TABLE:    true,
	v1pb.MetaType_VIEW:              true,
	v1pb.MetaType_MATERIALIZED_VIEW: true,
}

// qualitySignalNotMonitored answers that nothing watches the object.
func qualitySignalNotMonitored(object resolvedObject) qualitySignalUnmonitoredResult {
	return qualitySignalUnmonitoredResult{
		GUID:      object.GUID,
		MetaType:  object.MetaType.String(),
		Name:      object.Name,
		Monitored: false,
	}
}

// qualityTargetSummaryOf finds the one summary a single-key selection answered
// with. A selection omits the keys it has no live rule for, so an empty answer is
// the only legitimate miss; anything else is not this object's signal and is
// treated as such rather than reported as a different table's health.
func qualityTargetSummaryOf(summaries []*v1pb.QualityTargetSummary, guid string, metaType v1pb.MetaType) *v1pb.QualityTargetSummary {
	for _, summary := range summaries {
		if summary.GetTarget().GetMetaGuid() == guid && summary.GetTarget().GetMetaType() == metaType {
			return summary
		}
	}
	return nil
}

// qualitySignalName prefers the name the caller's reference resolved to, and
// falls back to the path snapshot stored with the target, which is what answers
// when the caller addressed the object by GUID.
func qualitySignalName(resolved string, summary *v1pb.QualityTargetSummary) string {
	if resolved != "" {
		return resolved
	}
	return summary.GetTarget().GetPath().GetObject()
}

// qualitySignalStatus renders the worst latest result. UNSPECIFIED means no check
// of the target has run yet, and it is omitted rather than reported as a status a
// model would have to learn to ignore.
func qualitySignalStatus(status v1pb.QualityStatus) string {
	if status == v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED {
		return ""
	}
	return status.String()
}

// qualitySignalTime renders a timestamp the way the rest of this surface does; an
// unset one is omitted.
func qualitySignalTime(timestamp *timestamppb.Timestamp) string {
	if timestamp == nil || timestamp.AsTime().Unix() <= 0 {
		return ""
	}
	return timestamp.AsTime().UTC().Format(time.RFC3339)
}

// --- quality read models ---

// qualityListParent names the current workspace, the way every quality RPC does.
const qualityListParent = "workspaces/-"

// qualityTargetKey identifies one target: a GUID alone does not identify a
// registry row across object types.
type qualityTargetKey struct {
	guid     string
	metaType v1pb.MetaType
}

// qualityTargetRef is the target identity a quality row carries. Path is the
// target's stored path snapshot, absent when the target no longer has a live
// check.
type qualityTargetRef struct {
	GUID     string          `json:"guid"`
	MetaType string          `json:"metaType,omitempty"`
	Path     json.RawMessage `json:"path,omitempty"`
}

// qualityTargetRefOf builds a row's target from the API's target message and a
// path the caller rendered.
func qualityTargetRefOf(target *v1pb.QualityTarget, path json.RawMessage) qualityTargetRef {
	ref := qualityTargetRef{Path: path}
	if target == nil {
		return ref
	}
	ref.GUID = target.GetMetaGuid()
	ref.MetaType = qualityEnumName(target.GetMetaType())
	return ref
}

// qualityTargetPath renders the path snapshot stored with a resource; an unset
// one stays absent.
func qualityTargetPath(target *v1pb.QualityTarget) (json.RawMessage, error) {
	path := target.GetPath()
	if path == nil {
		return nil, nil
	}
	return protoJSON(path)
}

// qualityEnumName renders an enum for the wire and omits the unspecified value: a
// model should not have to learn to ignore it.
func qualityEnumName(value interface{ String() string }) string {
	name := value.String()
	if name == "" || name == "UNSPECIFIED" || strings.HasSuffix(name, "_UNSPECIFIED") {
		return ""
	}
	return name
}

// qualityEnum reads an enum argument, accepting both the short name a model
// writes (OPEN, FAIL, SCHEDULED) and the protobuf name an earlier result
// rendered (QUALITY_ISSUE_STATUS_OPEN).
func qualityEnum[E ~int32](value, prefix string, names map[string]int32) (E, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	if trimmed == "" {
		return 0, nil
	}
	number, ok := names[trimmed]
	if !ok {
		number, ok = names[prefix+trimmed]
	}
	if !ok {
		return 0, invalidArgument("unknown value %q; accepted values are %s", value,
			strings.Join(qualityEnumValues(prefix, names), ", "))
	}
	return E(number), nil
}

// qualityEnumValues lists an enum's short names for an error message, without the
// unspecified value nobody should ask for.
func qualityEnumValues(prefix string, names map[string]int32) []string {
	values := make([]string, 0, len(names))
	for name := range names {
		if short := strings.TrimPrefix(name, prefix); short != name && short != "UNSPECIFIED" {
			values = append(values, short)
		}
	}
	slices.Sort(values)
	return values
}

// objectRefIsZero reports that a listing tool was given no object filter.
func objectRefIsZero(ref objectRef) bool {
	return ref == objectRef{}
}

// requireQualityListTarget refuses a listing object that no check can watch:
// issues and executions belong to check targets, and an empty page would read as
// "this object has none". A bare guid is allowed, because it leaves the type
// unknown and the RPC filters by GUID only.
func requireQualityListTarget(object resolvedObject, subject string) error {
	if object.MetaType == v1pb.MetaType_UNSPECIFIED || qualitySignalMetaTypes[object.MetaType] {
		return nil
	}
	return newToolError(codeInvalidArgument,
		subject+" belong to quality check targets: a table, external table, view or materialized view",
		"address one of those, or pass the target's guid")
}

// --- list_quality_issues ---

type listQualityIssuesArgs struct {
	Object    objectRef `json:"object"`
	Rule      string    `json:"rule"`
	Status    string    `json:"status"`
	Severity  string    `json:"severity"`
	PageSize  int32     `json:"page_size"`
	PageToken string    `json:"page_token"`
}

// qualityIssueRow is one problem-ledger row. It deliberately carries no rule
// detail: get_quality_target and the execution tools answer that.
type qualityIssueRow struct {
	Name               string           `json:"name"`
	Rule               string           `json:"rule"`
	Target             qualityTargetRef `json:"target"`
	Status             string           `json:"status,omitempty"`
	Severity           string           `json:"severity,omitempty"`
	FirstSeenTime      string           `json:"firstSeenTime,omitempty"`
	LastSeenTime       string           `json:"lastSeenTime,omitempty"`
	FirstSeenExecution string           `json:"firstSeenExecution,omitempty"`
	LastSeenExecution  string           `json:"lastSeenExecution,omitempty"`
	OccurrenceCount    int32            `json:"occurrenceCount"`
	AcknowledgedBy     string           `json:"acknowledgedBy,omitempty"`
	AcknowledgedTime   string           `json:"acknowledgedTime,omitempty"`
	ResolvedTime       string           `json:"resolvedTime,omitempty"`
	ResolutionReason   string           `json:"resolutionReason,omitempty"`
	ResolutionNote     string           `json:"resolutionNote,omitempty"`
	MutedUntil         string           `json:"mutedUntil,omitempty"`
}

type listQualityIssuesResult struct {
	Issues        []qualityIssueRow `json:"issues"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
}

func (s *Server) runListQualityIssues(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args listQualityIssuesArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	status, err := qualityEnum[v1pb.QualityIssueStatus](args.Status, "QUALITY_ISSUE_STATUS_", v1pb.QualityIssueStatus_value)
	if err != nil {
		return nil, err
	}
	severity, err := qualityEnum[v1pb.QualitySeverity](args.Severity, "QUALITY_SEVERITY_", v1pb.QualitySeverity_value)
	if err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	request := &v1pb.ListQualityIssuesRequest{
		Parent:    qualityListParent,
		PageSize:  pageSize,
		PageToken: args.PageToken,
		Status:    status,
		Severity:  severity,
		Rule:      strings.TrimSpace(args.Rule),
	}
	if !objectRefIsZero(args.Object) {
		object, err := s.resolveObject(ctx, args.Object, v1pb.MetaType_UNSPECIFIED)
		if err != nil {
			return nil, err
		}
		if err := requireQualityListTarget(object, "quality issues"); err != nil {
			return nil, err
		}
		request.TargetGuid = object.GUID
	}
	response, err := s.config.Quality.ListQualityIssues(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	issues := response.Msg.GetQualityIssues()
	paths := s.qualityIssueTargetPaths(ctx, issues)
	rows := qualityIssueRows(issues, func(target *v1pb.QualityTarget) json.RawMessage {
		return paths[qualityTargetKey{guid: target.GetMetaGuid(), metaType: target.GetMetaType()}]
	})
	return listQualityIssuesResult{Issues: rows, NextPageToken: response.Msg.GetNextPageToken()}, nil
}

// qualityIssueRows projects a page of issues. pathOf supplies each row's target
// path, which the API does not carry on an issue: it is enriched from the target
// directory, or taken from the target being opened.
func qualityIssueRows(issues []*v1pb.QualityIssue, pathOf func(*v1pb.QualityTarget) json.RawMessage) []qualityIssueRow {
	rows := make([]qualityIssueRow, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		target := issue.GetTarget()
		rows = append(rows, qualityIssueRow{
			Name:               issue.GetName(),
			Rule:               issue.GetRule(),
			Target:             qualityTargetRefOf(target, pathOf(target)),
			Status:             qualityEnumName(issue.GetStatus()),
			Severity:           qualityEnumName(issue.GetSeverity()),
			FirstSeenTime:      qualitySignalTime(issue.GetFirstSeenTime()),
			LastSeenTime:       qualitySignalTime(issue.GetLastSeenTime()),
			FirstSeenExecution: issue.GetFirstSeenExecution(),
			LastSeenExecution:  issue.GetLastSeenExecution(),
			OccurrenceCount:    issue.GetOccurrenceCount(),
			AcknowledgedBy:     issue.GetAcknowledgedBy(),
			AcknowledgedTime:   qualitySignalTime(issue.GetAcknowledgedTime()),
			ResolvedTime:       qualitySignalTime(issue.GetResolvedTime()),
			ResolutionReason:   qualityEnumName(issue.GetResolutionReason()),
			ResolutionNote:     issue.GetResolutionNote(),
			MutedUntil:         qualitySignalTime(issue.GetMutedUntil()),
		})
	}
	return rows
}

// qualityIssueTargetPaths reads the stored path snapshot of every target a page of
// issues names, in one exact-selection call. A page holds at most 200 issues, and
// a selection accepts at most 200 keys. It is best effort: an enrichment failure
// leaves the paths out rather than losing the listing.
func (s *Server) qualityIssueTargetPaths(ctx context.Context, issues []*v1pb.QualityIssue) map[qualityTargetKey]json.RawMessage {
	paths := map[qualityTargetKey]json.RawMessage{}
	keys := qualityIssueTargetKeys(issues)
	if len(keys) == 0 {
		return paths
	}
	response, err := s.config.Quality.ListQualityTargets(ctx, connect.NewRequest(&v1pb.ListQualityTargetsRequest{
		Parent:  qualityListParent,
		Targets: keys,
	}))
	if err != nil {
		return paths
	}
	for _, summary := range response.Msg.GetQualityTargets() {
		path := summary.GetTarget().GetPath()
		if path == nil {
			continue
		}
		encoded, err := protoJSON(path)
		if err != nil {
			continue
		}
		paths[qualityTargetKey{
			guid:     summary.GetTarget().GetMetaGuid(),
			metaType: summary.GetTarget().GetMetaType(),
		}] = encoded
	}
	return paths
}

// qualityIssueTargetKeys is the distinct set of targets a page of issues names,
// limited to the types a check can watch because a selection rejects any other.
func qualityIssueTargetKeys(issues []*v1pb.QualityIssue) []*v1pb.QualityTargetKey {
	keys := make([]*v1pb.QualityTargetKey, 0, len(issues))
	seen := make(map[qualityTargetKey]bool, len(issues))
	for _, issue := range issues {
		target := issue.GetTarget()
		if target == nil || target.GetMetaGuid() == "" || !qualitySignalMetaTypes[target.GetMetaType()] {
			continue
		}
		key := qualityTargetKey{guid: target.GetMetaGuid(), metaType: target.GetMetaType()}
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, &v1pb.QualityTargetKey{TargetGuid: key.guid, MetaType: key.metaType})
	}
	return keys
}

// --- get_quality_target ---

type getQualityTargetArgs struct {
	Object   objectRef `json:"object"`
	MetaType string    `json:"meta_type"`
}

// qualityTargetInfo names the target a workspace read was opened on.
type qualityTargetInfo struct {
	GUID     string `json:"guid"`
	MetaType string `json:"metaType"`
	Name     string `json:"name,omitempty"`
}

// getQualityTargetResult is one target's workspace: the header, every live check
// and the unresolved problems. Rule history and the target's column list stay out:
// the execution tools and get_metadata answer those.
type getQualityTargetResult struct {
	Monitored bool `json:"monitored"`
	// AllPaused says every check on the target is paused: the checks exist but
	// nothing is being verified, so the summary's verdict is history rather than
	// a current health claim.
	AllPaused      bool              `json:"allPaused"`
	Target         qualityTargetInfo `json:"target"`
	Summary        json.RawMessage   `json:"summary,omitempty"`
	Rules          []qualityRuleRow  `json:"rules"`
	OpenIssues     []qualityIssueRow `json:"openIssues"`
	OrphanedReason string            `json:"orphanedReason,omitempty"`
}

// qualityRuleRow is one check as this surface shows it: its definition, state and
// latest result, without the rule's target, description, timestamps or history.
type qualityRuleRow struct {
	Name                string          `json:"name"`
	Title               string          `json:"title"`
	Dimension           string          `json:"dimension,omitempty"`
	RuleType            string          `json:"ruleType,omitempty"`
	Config              json.RawMessage `json:"config,omitempty"`
	Severity            string          `json:"severity,omitempty"`
	State               string          `json:"state,omitempty"`
	Owner               string          `json:"owner,omitempty"`
	Revision            int32           `json:"revision"`
	FrequencyTier       string          `json:"frequencyTier,omitempty"`
	Tags                []string        `json:"tags"`
	TargetSchemaChanged bool            `json:"targetSchemaChanged"`
	OrphanedReason      string          `json:"orphanedReason,omitempty"`
	MuteUntil           string          `json:"muteUntil,omitempty"`
	LastResult          json.RawMessage `json:"lastResult,omitempty"`
}

func (s *Server) runGetQualityTarget(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args getQualityTargetArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		return nil, newToolError(codeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
	}
	object, err := s.resolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	if !qualitySignalMetaTypes[object.MetaType] {
		// A rule can only watch a table or view; the quality RPC would refuse the
		// key, so "nothing watches this" is the truthful answer.
		return qualitySignalNotMonitored(object), nil
	}
	response, err := s.config.Quality.GetQualityTarget(ctx, connect.NewRequest(&v1pb.GetQualityTargetRequest{
		Parent:     qualityListParent,
		TargetGuid: object.GUID,
		MetaType:   object.MetaType,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	summary := response.Msg.GetSummary()
	allPaused := qualityTargetAllPaused(summary)
	var encodedSummary, targetPath json.RawMessage
	if summary != nil {
		// A paused check's last verdict is history, not a current claim: clear it
		// from a copy of the summary, the way get_quality_signal omits it.
		rendered := summary
		if allPaused {
			cloned := &v1pb.QualityTargetSummary{}
			proto.Merge(cloned, summary)
			cloned.WorstStatus = v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED
			rendered = cloned
		}
		encodedSummary, err = protoJSON(rendered)
		if err != nil {
			return nil, err
		}
		targetPath, err = qualityTargetPath(summary.GetTarget())
		if err != nil {
			return nil, err
		}
	}
	rules, err := qualityRuleRows(response.Msg.GetQualityRules())
	if err != nil {
		return nil, err
	}
	// Every open issue belongs to this target, so the workspace's path fills them
	// all, the same shape list_quality_issues returns.
	openIssues := qualityIssueRows(response.Msg.GetOpenIssues(), func(*v1pb.QualityTarget) json.RawMessage {
		return targetPath
	})
	return getQualityTargetResult{
		Monitored: summary.GetRuleCount() > 0,
		AllPaused: allPaused,
		Target: qualityTargetInfo{
			GUID:     object.GUID,
			MetaType: object.MetaType.String(),
			Name:     qualitySignalName(object.Name, summary),
		},
		Summary:        encodedSummary,
		Rules:          rules,
		OpenIssues:     openIssues,
		OrphanedReason: response.Msg.GetOrphanedReason(),
	}, nil
}

// qualityTargetAllPaused reports a target whose checks all exist but are paused:
// the target is monitored in the sense that checks are defined, and nothing is
// being verified.
func qualityTargetAllPaused(summary *v1pb.QualityTargetSummary) bool {
	return summary.GetRuleCount() > 0 && summary.GetEnabledRuleCount() == 0
}

// qualityStatusJudged reports whether this verdict carries a measurement. Only
// PASS, WARN and FAIL are judged outcomes; ERROR, SKIPPED and NOT_EVALUATED have
// no observation.
func qualityStatusJudged(status v1pb.QualityStatus) bool {
	switch status {
	case v1pb.QualityStatus_QUALITY_STATUS_PASS,
		v1pb.QualityStatus_QUALITY_STATUS_WARN,
		v1pb.QualityStatus_QUALITY_STATUS_FAIL:
		return true
	case v1pb.QualityStatus_QUALITY_STATUS_UNSPECIFIED,
		v1pb.QualityStatus_QUALITY_STATUS_ERROR,
		v1pb.QualityStatus_QUALITY_STATUS_SKIPPED,
		v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED:
		return false
	default:
		return false
	}
}

// qualityRuleRows projects a target's checks.
func qualityRuleRows(rules []*v1pb.QualityRule) ([]qualityRuleRow, error) {
	rows := make([]qualityRuleRow, 0, len(rules))
	for _, rule := range rules {
		if rule == nil {
			continue
		}
		row := qualityRuleRow{
			Name:                rule.GetName(),
			Title:               rule.GetTitle(),
			Dimension:           qualityEnumName(rule.GetDimension()),
			RuleType:            qualityEnumName(rule.GetRuleType()),
			Severity:            qualityEnumName(rule.GetSeverity()),
			State:               qualityEnumName(rule.GetState()),
			Owner:               rule.GetOwner(),
			Revision:            rule.GetRevision(),
			FrequencyTier:       qualityEnumName(rule.GetFrequencyTier()),
			Tags:                stringsOrEmpty(rule.GetTags()),
			TargetSchemaChanged: rule.GetTargetSchemaChanged(),
			OrphanedReason:      rule.GetOrphanedReason(),
			MuteUntil:           qualitySignalTime(rule.GetMuteUntil()),
		}
		if config := rule.GetConfig(); config != nil {
			encoded, err := protoJSON(config)
			if err != nil {
				return nil, err
			}
			row.Config = encoded
		}
		if lastResult := rule.GetLastResult(); lastResult != nil {
			encoded, err := protoJSON(lastResult)
			if err != nil {
				return nil, err
			}
			row.LastResult = encoded
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// --- list_quality_executions ---

type listQualityExecutionsArgs struct {
	Object    objectRef `json:"object"`
	Rule      string    `json:"rule"`
	State     string    `json:"state"`
	Outcome   string    `json:"outcome"`
	Trigger   string    `json:"trigger"`
	PageSize  int32     `json:"page_size"`
	PageToken string    `json:"page_token"`
}

// qualityExecutionRow is one run as a listing shows it: the verdict and the flags
// that say how far to trust it. The evidence itself stays in get_quality_execution:
// failed sample rows and executed SQL are one run's worth of data, not a page's.
type qualityExecutionRow struct {
	Name      string           `json:"name"`
	Rule      string           `json:"rule"`
	Target    qualityTargetRef `json:"target"`
	Instance  string           `json:"instance,omitempty"`
	Run       string           `json:"run,omitempty"`
	Trigger   string           `json:"trigger,omitempty"`
	State     string           `json:"state,omitempty"`
	Outcome   string           `json:"outcome,omitempty"`
	Attempt   int32            `json:"attempt"`
	Initiator string           `json:"initiator,omitempty"`
	// ObservedValue is absent when the run produced no measurement: only a judged
	// verdict (PASS, WARN, FAIL) carries one, and a present 0 is a real
	// observation.
	ObservedValue *float64        `json:"observedValue,omitempty"`
	ObservedKind  string          `json:"observedKind,omitempty"`
	Thresholds    json.RawMessage `json:"thresholds,omitempty"`
	FailedRows    int64           `json:"failedRows"`
	TotalRows     int64           `json:"totalRows"`
	Sampled       bool            `json:"sampled"`
	SampleRows    int64           `json:"sampleRows"`
	Truncated     bool            `json:"truncated"`
	ErrorCode     string          `json:"errorCode,omitempty"`
	ErrorMessage  string          `json:"errorMessage,omitempty"`
	Duration      json.RawMessage `json:"duration,omitempty"`
	StartTime     string          `json:"startTime,omitempty"`
	FinishTime    string          `json:"finishTime,omitempty"`
	BaselineValue *float64        `json:"baselineValue,omitempty"`
	BaselineTime  string          `json:"baselineTime,omitempty"`
}

type listQualityExecutionsResult struct {
	Executions    []qualityExecutionRow `json:"executions"`
	NextPageToken string                `json:"nextPageToken,omitempty"`
}

func (s *Server) runListQualityExecutions(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args listQualityExecutionsArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	state, err := qualityEnum[v1pb.QualityExecutionState](args.State, "QUALITY_EXECUTION_STATE_", v1pb.QualityExecutionState_value)
	if err != nil {
		return nil, err
	}
	outcome, err := qualityEnum[v1pb.QualityStatus](args.Outcome, "QUALITY_STATUS_", v1pb.QualityStatus_value)
	if err != nil {
		return nil, err
	}
	trigger, err := qualityEnum[v1pb.QualityExecutionTrigger](args.Trigger, "QUALITY_EXECUTION_TRIGGER_", v1pb.QualityExecutionTrigger_value)
	if err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	request := &v1pb.ListQualityExecutionsRequest{
		Parent:    qualityListParent,
		PageSize:  pageSize,
		PageToken: args.PageToken,
		State:     state,
		Outcome:   outcome,
		Trigger:   trigger,
		Rule:      strings.TrimSpace(args.Rule),
	}
	if !objectRefIsZero(args.Object) {
		object, err := s.resolveObject(ctx, args.Object, v1pb.MetaType_UNSPECIFIED)
		if err != nil {
			return nil, err
		}
		if err := requireQualityListTarget(object, "quality executions"); err != nil {
			return nil, err
		}
		request.TargetGuid = object.GUID
	}
	response, err := s.config.Quality.ListQualityExecutions(ctx, connect.NewRequest(request))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	executions := response.Msg.GetQualityExecutions()
	result := listQualityExecutionsResult{
		Executions:    make([]qualityExecutionRow, 0, len(executions)),
		NextPageToken: response.Msg.GetNextPageToken(),
	}
	for _, execution := range executions {
		if execution == nil {
			continue
		}
		row, err := qualityExecutionRowOf(execution)
		if err != nil {
			return nil, err
		}
		result.Executions = append(result.Executions, row)
	}
	return result, nil
}

// qualityExecutionRowOf projects one run.
func qualityExecutionRowOf(execution *v1pb.QualityExecution) (qualityExecutionRow, error) {
	path, err := qualityTargetPath(execution.GetTarget())
	if err != nil {
		return qualityExecutionRow{}, err
	}
	row := qualityExecutionRow{
		Name:          execution.GetName(),
		Rule:          execution.GetRule(),
		Target:        qualityTargetRefOf(execution.GetTarget(), path),
		Instance:      execution.GetInstance(),
		Run:           execution.GetRun(),
		Trigger:       qualityEnumName(execution.GetTrigger()),
		State:         qualityEnumName(execution.GetState()),
		Outcome:       qualityEnumName(execution.GetOutcome()),
		Attempt:       execution.GetAttempt(),
		Initiator:     execution.GetInitiator(),
		ObservedKind:  qualityEnumName(execution.GetObservedKind()),
		FailedRows:    execution.GetFailedRows(),
		TotalRows:     execution.GetTotalRows(),
		Sampled:       execution.GetSampled(),
		SampleRows:    execution.GetSampleRows(),
		Truncated:     execution.GetTruncated(),
		ErrorCode:     execution.GetErrorCode(),
		ErrorMessage:  execution.GetErrorMessage(),
		StartTime:     qualitySignalTime(execution.GetStartTime()),
		FinishTime:    qualitySignalTime(execution.GetFinishTime()),
		BaselineValue: execution.BaselineValue,
		BaselineTime:  qualitySignalTime(execution.GetBaselineTime()),
	}
	// Only a judged verdict records a measurement; for ERROR, SKIPPED and
	// NOT_EVALUATED the API's non-optional field is 0, which would read as an
	// observed zero.
	if qualityStatusJudged(execution.GetOutcome()) {
		observed := execution.GetObservedValue()
		row.ObservedValue = &observed
	}
	if thresholds := execution.GetThresholds(); thresholds != nil {
		row.Thresholds, err = protoJSON(thresholds)
		if err != nil {
			return qualityExecutionRow{}, err
		}
	}
	if duration := execution.GetDuration(); duration != nil {
		row.Duration, err = protoJSON(duration)
		if err != nil {
			return qualityExecutionRow{}, err
		}
	}
	return row, nil
}

// --- get_quality_execution ---

type getQualityExecutionArgs struct {
	Execution string `json:"execution"`
}

func (s *Server) runGetQualityExecution(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args getQualityExecutionArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Execution) == "" {
		return nil, invalidArgument("execution is required; list_quality_executions returns one per run")
	}
	response, err := s.config.Quality.GetQualityExecution(ctx, connect.NewRequest(&v1pb.GetQualityExecutionRequest{
		Name: args.Execution,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	// The whole execution is the answer: this is the evidence tool, so nothing is
	// projected away.
	return protoJSON(response.Msg)
}

// --- get_quality_impact ---

type getQualityImpactArgs struct {
	Object      objectRef `json:"object"`
	MetaType    string    `json:"meta_type"`
	Depth       int32     `json:"depth"`
	WindowHours int32     `json:"window_hours"`
}

func (s *Server) runGetQualityImpact(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args getQualityImpactArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	metaType, err := parseMetaType(args.MetaType)
	if err != nil {
		return nil, err
	}
	if metaType == v1pb.MetaType_UNSPECIFIED && args.Object.GUID != "" {
		return nil, newToolError(codeInvalidArgument, "meta_type is required when the reference is a bare guid",
			"list_metadata and search_metadata return metaType with every object, or address the object by name instead")
	}
	object, err := s.resolveObject(ctx, args.Object, metaType)
	if err != nil {
		return nil, err
	}
	if !qualitySignalMetaTypes[object.MetaType] {
		// Only the four object types a check can watch carry an impact: a walk from
		// a reported statement or a schema has no assets to answer with.
		return nil, newToolError(codeInvalidArgument,
			"impact can only be read for a table, external table, view or materialized view",
			"list_metadata and search_metadata return metaType with every object")
	}
	response, err := s.config.Quality.GetQualityTargetImpact(ctx, connect.NewRequest(&v1pb.GetQualityTargetImpactRequest{
		Parent: qualityListParent,
		Target: &v1pb.QualityTargetKey{
			TargetGuid: object.GUID,
			MetaType:   object.MetaType,
		},
		Depth:       args.Depth,
		WindowHours: args.WindowHours,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	// The whole answer is evidence: the model decides what matters, so nothing is
	// projected away.
	return protoJSON(response.Msg)
}

// --- get_quality_consumption_coverage ---

type getQualityConsumptionCoverageArgs struct {
	PageSize int32 `json:"page_size"`
}

func (s *Server) runGetQualityConsumptionCoverage(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args getQualityConsumptionCoverageArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	response, err := s.config.Quality.GetQualityConsumptionCoverage(ctx, connect.NewRequest(&v1pb.GetQualityConsumptionCoverageRequest{
		Parent:   qualityListParent,
		PageSize: pageSize,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	return protoJSON(response.Msg)
}

// --- list_quality_targets ---

type listQualityTargetsArgs struct {
	Search       string `json:"search"`
	WorstStatus  string `json:"worst_status"`
	HasOpenIssue bool   `json:"has_open_issue"`
	PageSize     int32  `json:"page_size"`
	PageToken    string `json:"page_token"`
}

// qualityTargetRow is one row of the target directory.
type qualityTargetRow struct {
	GUID             string          `json:"guid"`
	MetaType         string          `json:"metaType"`
	Path             json.RawMessage `json:"path,omitempty"`
	RuleCount        int32           `json:"ruleCount"`
	EnabledRuleCount int32           `json:"enabledRuleCount"`
	PausedRuleCount  int32           `json:"pausedRuleCount"`
	// AllPaused says every check is paused: the target's worst latest result is
	// history, so WorstStatus stays absent rather than reading as a current claim.
	AllPaused            bool            `json:"allPaused"`
	ColumnCount          int32           `json:"columnCount"`
	OpenIssueCount       int32           `json:"openIssueCount"`
	PendingDraftCount    int32           `json:"pendingDraftCount"`
	PendingLabelCount    int32           `json:"pendingLabelCount"`
	WorstStatus          string          `json:"worstStatus,omitempty"`
	LastRunTime          string          `json:"lastRunTime,omitempty"`
	SchemaChanged        bool            `json:"schemaChanged"`
	Orphaned             bool            `json:"orphaned"`
	QualityScore         *float64        `json:"qualityScore,omitempty"`
	ScoredRuleCount      int32           `json:"scoredRuleCount"`
	RecentPassRate       *float64        `json:"recentPassRate,omitempty"`
	RecentExecutionCount int32           `json:"recentExecutionCount"`
	CoveredColumnCount   int32           `json:"coveredColumnCount"`
	TotalColumnCount     int32           `json:"totalColumnCount"`
	LatestRun            json.RawMessage `json:"latestRun,omitempty"`
	RunCount             int32           `json:"runCount"`
	Schedule             json.RawMessage `json:"schedule,omitempty"`
}

type listQualityTargetsResult struct {
	Targets       []qualityTargetRow `json:"targets"`
	NextPageToken string             `json:"nextPageToken,omitempty"`
}

func (s *Server) runListQualityTargets(ctx context.Context, _ *store.UserMessage, raw json.RawMessage) (any, error) {
	var args listQualityTargetsArgs
	if err := decodeArguments(raw, &args); err != nil {
		return nil, err
	}
	worstStatus, err := qualityEnum[v1pb.QualityStatus](args.WorstStatus, "QUALITY_STATUS_", v1pb.QualityStatus_value)
	if err != nil {
		return nil, err
	}
	pageSize, err := normalizePageSize(args.PageSize)
	if err != nil {
		return nil, err
	}
	response, err := s.config.Quality.ListQualityTargets(ctx, connect.NewRequest(&v1pb.ListQualityTargetsRequest{
		Parent:       qualityListParent,
		PageSize:     pageSize,
		PageToken:    args.PageToken,
		Search:       strings.TrimSpace(args.Search),
		WorstStatus:  worstStatus,
		HasOpenIssue: args.HasOpenIssue,
	}))
	if err != nil {
		return nil, toolErrorFromRPC(err)
	}
	summaries := response.Msg.GetQualityTargets()
	result := listQualityTargetsResult{
		Targets:       make([]qualityTargetRow, 0, len(summaries)),
		NextPageToken: response.Msg.GetNextPageToken(),
	}
	for _, summary := range summaries {
		if summary == nil {
			continue
		}
		row, err := qualityTargetRowOf(summary)
		if err != nil {
			return nil, err
		}
		result.Targets = append(result.Targets, row)
	}
	return result, nil
}

// qualityTargetRowOf projects one directory row.
func qualityTargetRowOf(summary *v1pb.QualityTargetSummary) (qualityTargetRow, error) {
	path, err := qualityTargetPath(summary.GetTarget())
	if err != nil {
		return qualityTargetRow{}, err
	}
	allPaused := qualityTargetAllPaused(summary)
	row := qualityTargetRow{
		GUID:                 summary.GetTarget().GetMetaGuid(),
		MetaType:             qualityEnumName(summary.GetTarget().GetMetaType()),
		Path:                 path,
		RuleCount:            summary.GetRuleCount(),
		EnabledRuleCount:     summary.GetEnabledRuleCount(),
		PausedRuleCount:      summary.GetPausedRuleCount(),
		AllPaused:            allPaused,
		ColumnCount:          summary.GetColumnCount(),
		OpenIssueCount:       summary.GetOpenIssueCount(),
		PendingDraftCount:    summary.GetPendingDraftCount(),
		PendingLabelCount:    summary.GetPendingLabelCount(),
		LastRunTime:          qualitySignalTime(summary.GetLastRunTime()),
		SchemaChanged:        summary.GetSchemaChanged(),
		Orphaned:             summary.GetOrphaned(),
		QualityScore:         summary.QualityScore,
		ScoredRuleCount:      summary.GetScoredRuleCount(),
		RecentPassRate:       summary.RecentPassRate,
		RecentExecutionCount: summary.GetRecentExecutionCount(),
		CoveredColumnCount:   summary.GetCoveredColumnCount(),
		TotalColumnCount:     summary.GetTotalColumnCount(),
		RunCount:             summary.GetRunCount(),
	}
	// A paused target's last verdict is history, so it is omitted the way
	// get_quality_signal omits it.
	if !allPaused {
		row.WorstStatus = qualitySignalStatus(summary.GetWorstStatus())
	}
	if latestRun := summary.GetLatestRun(); latestRun != nil {
		row.LatestRun, err = protoJSON(latestRun)
		if err != nil {
			return qualityTargetRow{}, err
		}
	}
	if schedule := summary.GetSchedule(); schedule != nil {
		row.Schedule, err = protoJSON(schedule)
		if err != nil {
			return qualityTargetRow{}, err
		}
	}
	return row, nil
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
		Warnings: stringsOrEmpty(response.Msg.GetWarnings()),
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
			Warnings:    stringsOrEmpty(analyzed.GetWarnings()),
		}
		if args.Depth > 0 {
			graphs, warning, err := s.expandTargets(ctx, analyzed, args.Depth)
			if err != nil {
				return nil, err
			}
			if warning != "" {
				scopeResult.Warnings = append(scopeResult.Warnings, warning)
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

// maxExpandedTargets bounds the graph walks one analysis may trigger: a statement
// can name thousands of targets and every walk is its own query.
const maxExpandedTargets = 20

// expandTargets walks the graph from each real target the statement produced. A
// target the registry has no entry for cannot be expanded, and that is not a reason
// to lose the analysis; neither is a statement with more targets than the budget,
// which comes back with the first ones and a warning saying so.
func (s *Server) expandTargets(ctx context.Context, analyzed *v1pb.AnalyzeSQLResult, depth int32) ([]*v1pb.GetLineageGraphResponse, string, error) {
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
		response, err := s.config.Lineage.GetLineageGraph(ctx, connect.NewRequest(&v1pb.GetLineageGraphRequest{
			Guid:  target,
			Depth: depth,
		}))
		if err != nil {
			if connect.CodeOf(err) == connect.CodeNotFound {
				continue
			}
			return nil, "", toolErrorFromRPC(err)
		}
		graphs = append(graphs, response.Msg)
	}
	return graphs, "", nil
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
		Permissions: stringsOrEmpty(response.Msg.GetPermissions()),
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

func boolProperty(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func pageSizeProperty() map[string]any {
	return map[string]any{"type": "integer", "description": fmt.Sprintf("rows to return (default %d, max %d)", defaultPageSize, maxPageSize)}
}

func pageTokenProperty() map[string]any {
	return map[string]any{"type": "string", "description": "continuation token from a previous call"}
}
