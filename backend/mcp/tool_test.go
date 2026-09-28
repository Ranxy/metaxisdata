package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeReaders implements all four reader interfaces, with one function per
// method: a test states exactly what a tool may see, and any call it did not
// expect fails the test instead of quietly succeeding.
type fakeReaders struct {
	t *testing.T

	listInstances   func(*v1pb.ListInstancesRequest) (*v1pb.ListInstancesResponse, error)
	listDatabases   func(*v1pb.ListDatabasesRequest) (*v1pb.ListDatabasesResponse, error)
	listMetadata    func(*v1pb.ListMetadataRequest) (*v1pb.MetadataResponse, error)
	getMetadata     func(*v1pb.GetMetadataRequest) (*v1pb.GetMetadataResponse, error)
	searchMetadata  func(*v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error)
	getSchemaString func(*v1pb.GetSchemaStringRequest) (*v1pb.MetadataSchemaString, error)
	analyzeSQL      func(*v1pb.AnalyzeSQLRequest) (*v1pb.AnalyzeSQLResponse, error)
	getLineageGraph func(*v1pb.GetLineageGraphRequest) (*v1pb.GetLineageGraphResponse, error)
	getCurrentUser  func(*emptypb.Empty) (*v1pb.User, error)
}

func (f *fakeReaders) unexpected(method string) error {
	f.t.Helper()
	f.t.Fatalf("%s was called, and this test did not expect it", method)
	return nil
}

func (f *fakeReaders) ListInstances(_ context.Context, request *connect.Request[v1pb.ListInstancesRequest]) (*connect.Response[v1pb.ListInstancesResponse], error) {
	if f.listInstances == nil {
		return nil, f.unexpected("ListInstances")
	}
	response, err := f.listInstances(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) ListDatabases(_ context.Context, request *connect.Request[v1pb.ListDatabasesRequest]) (*connect.Response[v1pb.ListDatabasesResponse], error) {
	if f.listDatabases == nil {
		return nil, f.unexpected("ListDatabases")
	}
	response, err := f.listDatabases(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) ListMetadata(_ context.Context, request *connect.Request[v1pb.ListMetadataRequest]) (*connect.Response[v1pb.MetadataResponse], error) {
	if f.listMetadata == nil {
		return nil, f.unexpected("ListMetadata")
	}
	response, err := f.listMetadata(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) GetMetadata(_ context.Context, request *connect.Request[v1pb.GetMetadataRequest]) (*connect.Response[v1pb.GetMetadataResponse], error) {
	if f.getMetadata == nil {
		return nil, f.unexpected("GetMetadata")
	}
	response, err := f.getMetadata(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) SearchMetadata(_ context.Context, request *connect.Request[v1pb.SearchMetadataRequest]) (*connect.Response[v1pb.SearchMetadataResponse], error) {
	if f.searchMetadata == nil {
		return nil, f.unexpected("SearchMetadata")
	}
	response, err := f.searchMetadata(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) GetSchemaString(_ context.Context, request *connect.Request[v1pb.GetSchemaStringRequest]) (*connect.Response[v1pb.MetadataSchemaString], error) {
	if f.getSchemaString == nil {
		return nil, f.unexpected("GetSchemaString")
	}
	response, err := f.getSchemaString(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) AnalyzeSQL(_ context.Context, request *connect.Request[v1pb.AnalyzeSQLRequest]) (*connect.Response[v1pb.AnalyzeSQLResponse], error) {
	if f.analyzeSQL == nil {
		return nil, f.unexpected("AnalyzeSQL")
	}
	response, err := f.analyzeSQL(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) GetLineageGraph(_ context.Context, request *connect.Request[v1pb.GetLineageGraphRequest]) (*connect.Response[v1pb.GetLineageGraphResponse], error) {
	if f.getLineageGraph == nil {
		return nil, f.unexpected("GetLineageGraph")
	}
	response, err := f.getLineageGraph(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) GetCurrentUser(_ context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[v1pb.User], error) {
	if f.getCurrentUser == nil {
		return nil, f.unexpected("GetCurrentUser")
	}
	response, err := f.getCurrentUser(&emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

type fakeChecker struct {
	allow bool
	err   error
	asked []string
}

func (c *fakeChecker) CheckPermission(_ context.Context, asked permission.Permission, _ *store.UserMessage) (bool, error) {
	c.asked = append(c.asked, asked)
	return c.allow, c.err
}

func newTestServer(t *testing.T, readers *fakeReaders, checker PermissionChecker) *Server {
	t.Helper()
	readers.t = t
	return NewServer(Config{
		Instances:  readers,
		Databases:  readers,
		Lineage:    readers,
		Principals: readers,
		Checker:    checker,
		Now:        func() time.Time { return time.Unix(0, 0) },
	})
}

func toolByName(t *testing.T, server *Server, name string) toolDefinition {
	t.Helper()
	for _, definition := range server.toolDefinitions() {
		if definition.Name == name {
			return definition
		}
	}
	t.Fatalf("tool %q is not defined", name)
	return toolDefinition{}
}

func callTool(t *testing.T, server *Server, name string, args map[string]any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	return toolByName(t, server, name).Run(context.Background(), testUser(), raw)
}

func testUser() *store.UserMessage {
	return &store.UserMessage{ID: 7, Email: "user@example.com"}
}

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	payload, err := json.Marshal(value)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(payload, &parsed))
	return parsed
}

func failureOf(t *testing.T, err error) *toolError {
	t.Helper()
	require.Error(t, err)
	var failure *toolError
	require.True(t, errors.As(err, &failure), "expected a tool error, got %v", err)
	return failure
}

func detailsOf(t *testing.T, failure *toolError) map[string]any {
	t.Helper()
	require.NotNil(t, failure.Details)
	payload, err := json.Marshal(failure.Details)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(payload, &parsed))
	return parsed
}

// listOf reads a list out of a tool's payload, failing the test rather than
// panicking when the shape is not the one the test expected.
func listOf(t *testing.T, payload map[string]any, key string) []any {
	t.Helper()
	entries, ok := payload[key].([]any)
	require.True(t, ok, "%q is not a list", key)
	return entries
}

// firstOf reads the first entry of such a list.
func firstOf(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	entries := listOf(t, payload, key)
	require.NotEmpty(t, entries, "%q is empty", key)
	entry, ok := entries[0].(map[string]any)
	require.True(t, ok, "the first %q is not an object", key)
	return entry
}

// --- fixtures ---

func storedTable(guid, name string) *v1pb.StoredMetadata {
	return &v1pb.StoredMetadata{
		Guid: guid,
		Type: &v1pb.StoredMetadata_TableMetadata{TableMetadata: &v1pb.TableMetadata{
			Name:    name,
			Columns: []*v1pb.ColumnMetadata{{Name: "amount", Type: "bigint"}},
		}},
	}
}

func storedSchema(guid, name string) *v1pb.StoredMetadata {
	return &v1pb.StoredMetadata{
		Guid: guid,
		Type: &v1pb.StoredMetadata_SchemaMetadata{SchemaMetadata: &v1pb.SchemaMetadata{Name: name}},
	}
}

// foundTable is one search hit for a table, which is what every resolver test
// looks for. The result carries the same guid the stored payload does, which is
// what the read RPC returns.
func foundTable(stored *v1pb.StoredMetadata) *v1pb.SearchMetadataResult {
	return &v1pb.SearchMetadataResult{Guid: stored.GetGuid(), MetaType: v1pb.MetaType_TABLE, Metadata: stored}
}

func oneInstance() func(*v1pb.ListInstancesRequest) (*v1pb.ListInstancesResponse, error) {
	return func(*v1pb.ListInstancesRequest) (*v1pb.ListInstancesResponse, error) {
		return &v1pb.ListInstancesResponse{Instances: []*v1pb.Instance{
			{Name: "instances/1", Title: "prod-mysql", Engine: v1pb.Engine_MYSQL, Environment: "prod"},
		}}, nil
	}
}

func oneDatabase() func(*v1pb.ListDatabasesRequest) (*v1pb.ListDatabasesResponse, error) {
	return func(*v1pb.ListDatabasesRequest) (*v1pb.ListDatabasesResponse, error) {
		return &v1pb.ListDatabasesResponse{Databases: []*v1pb.Database{
			{Name: "instances/1/databases/shop", Guid: "1;shop", EffectiveEnvironment: "prod"},
		}}, nil
	}
}

// --- addressing ---

func TestResolveObjectPrefersAGUID(t *testing.T) {
	t.Parallel()

	// No reader is configured: any call at all fails the test.
	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	object, err := server.resolveObject(context.Background(), objectRef{GUID: "1;shop;;orders", Name: "ignored"}, v1pb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, "1;shop;;orders", object.GUID)
}

func TestResolveObjectResolvesANamePath(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listInstances: oneInstance(),
		listDatabases: oneDatabase(),
		searchMetadata: func(request *v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
			require.Equal(t, "orders", request.GetSearchStr())
			require.Equal(t, "1;shop", request.GetParentGuidPrefix())
			return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
				foundTable(storedTable("1;shop;;orders", "orders")),
			}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	object, err := server.resolveObject(context.Background(), objectRef{Instance: "prod-mysql", Database: "shop", Name: "orders"}, v1pb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, "1;shop;;orders", object.GUID)
	require.Equal(t, v1pb.MetaType_TABLE, object.MetaType)
	require.Equal(t, "orders", object.Name)
}

func TestResolveObjectReportsCandidates(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listInstances: oneInstance(),
		listDatabases: oneDatabase(),
		searchMetadata: func(*v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
			// A near miss: the search found something, the name did not match.
			return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
				foundTable(storedTable("1;shop;;order_items", "order_items")),
			}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	_, err := server.resolveObject(context.Background(), objectRef{Instance: "prod-mysql", Database: "shop", Name: "orders"}, v1pb.MetaType_UNSPECIFIED)

	failure := failureOf(t, err)
	require.Equal(t, codeNotFound, failure.Code)
	require.Len(t, listOf(t, detailsOf(t, failure), "candidates"), 1)
	require.Equal(t, "1;shop;;order_items", firstOf(t, detailsOf(t, failure), "candidates")["guid"])
}

func TestResolveObjectReportsAmbiguity(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listInstances: oneInstance(),
		listDatabases: oneDatabase(),
		// Two schemas hold a table with the same name, and the caller named no
		// schema: guessing one would answer about the wrong object.
		searchMetadata: func(*v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
			return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
				foundTable(storedTable("1;shop;public;orders", "orders")),
				foundTable(storedTable("1;shop;sales;orders", "orders")),
			}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	_, err := server.resolveObject(context.Background(), objectRef{Instance: "prod-mysql", Database: "shop", Name: "orders"}, v1pb.MetaType_UNSPECIFIED)

	failure := failureOf(t, err)
	require.Equal(t, codeAmbiguous, failure.Code)
	require.Len(t, listOf(t, detailsOf(t, failure), "candidates"), 2)
}

func TestResolveObjectKeepsASiblingDatabaseOut(t *testing.T) {
	t.Parallel()

	// The search filter is a string prefix, so "1;shop" also matches
	// "1;shop_archive"; the resolver requires the parent to be a whole segment.
	readers := &fakeReaders{
		listInstances: oneInstance(),
		listDatabases: oneDatabase(),
		searchMetadata: func(*v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
			return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
				foundTable(storedTable("1;shop_archive;;orders", "orders")),
			}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	_, err := server.resolveObject(context.Background(), objectRef{Instance: "prod-mysql", Database: "shop", Name: "orders"}, v1pb.MetaType_UNSPECIFIED)
	require.Equal(t, codeNotFound, failureOf(t, err).Code)
}

func TestResolveObjectRejectsAnIncompleteReference(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	cases := map[string]objectRef{
		"no name":     {Instance: "prod-mysql", Database: "shop"},
		"no instance": {Database: "shop", Name: "orders"},
		"no database": {Instance: "prod-mysql", Name: "orders"},
	}
	for name, ref := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := server.resolveObject(context.Background(), ref, v1pb.MetaType_UNSPECIFIED)
			require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
		})
	}
}

// TestResolveObjectResolvesASchema covers the engines that have a named schema
// level: the schema is looked up under the database first, and the object search
// is scoped to it.
func TestResolveObjectResolvesASchema(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listInstances: oneInstance(),
		listDatabases: oneDatabase(),
		listMetadata: func(request *v1pb.ListMetadataRequest) (*v1pb.MetadataResponse, error) {
			require.Equal(t, "1;shop", request.GetParentGuid())
			require.Equal(t, v1pb.MetaType_SCHEMA, request.GetMetaType())
			return &v1pb.MetadataResponse{TypesStoredMetadata: []*v1pb.MetadataResponse_Metadata{{
				MetaType: v1pb.MetaType_SCHEMA,
				List:     []*v1pb.StoredMetadata{storedSchema("1;shop;public", "public")},
			}}}, nil
		},
		searchMetadata: func(request *v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
			require.Equal(t, "1;shop;public", request.GetParentGuidPrefix())
			return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
				foundTable(storedTable("1;shop;public;orders", "orders")),
			}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	object, err := server.resolveObject(context.Background(), objectRef{
		Instance: "prod-mysql", Database: "shop", Schema: "public", Name: "orders",
	}, v1pb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, "1;shop;public;orders", object.GUID)
}

// --- scopes ---

func TestResolveScopeNeedsBothFields(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	_, err := server.resolveScope(context.Background(), analysisScope{Instance: "prod-mysql"})
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
}

func TestAnalyzeSQLNeedsAScopeAndListsWhatExists(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{listInstances: oneInstance(), listDatabases: oneDatabase()}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	_, err := callTool(t, server, "analyze_sql", map[string]any{"sql": "select 1"})

	failure := failureOf(t, err)
	require.Equal(t, codeScopeRequired, failure.Code)
	require.NotEmpty(t, failure.Hint)
	require.Len(t, listOf(t, detailsOf(t, failure), "databases"), 1)
	require.Equal(t, "shop", firstOf(t, detailsOf(t, failure), "databases")["database"])
	require.Equal(t, "prod-mysql", firstOf(t, detailsOf(t, failure), "databases")["instance"])
}

func TestAnalyzeSQLRejectsBadArguments(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	cases := []map[string]any{
		{"sql": ""},
		{"sql": "select 1", "depth": 11, "scopes": []map[string]string{{"instance": "i", "database": "d"}}},
		{"sql": "select 1", "scopes": make([]map[string]string, maxAnalyzeSQLScopes+1)},
	}
	for _, args := range cases {
		_, err := callTool(t, server, "analyze_sql", args)
		require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
	}
}

// --- listings ---

func TestListMetadataProjectsRowsAndClampsPageSize(t *testing.T) {
	t.Parallel()

	var seen *v1pb.ListMetadataRequest
	readers := &fakeReaders{
		listMetadata: func(request *v1pb.ListMetadataRequest) (*v1pb.MetadataResponse, error) {
			seen = request
			return &v1pb.MetadataResponse{TypesStoredMetadata: []*v1pb.MetadataResponse_Metadata{{
				MetaType: v1pb.MetaType_TABLE,
				List:     []*v1pb.StoredMetadata{storedTable("1;shop;;orders", "orders")},
			}}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := toolByName(t, server, "list_metadata").Run(context.Background(), testUser(),
		json.RawMessage(`{"parent":{"guid":"1;shop"},"page_size":9000}`))
	require.NoError(t, err)
	require.Equal(t, int32(maxPageSize), seen.GetPageSize(), "a larger page is clamped, not refused")

	payload, err := json.Marshal(value)
	require.NoError(t, err)
	require.Contains(t, string(payload), `"guid":"1;shop;;orders"`)
	require.Contains(t, string(payload), `"parentGuid":"1;shop"`)
	require.NotContains(t, string(payload), "amount", "a listing is a projection: the columns must not travel with it")
	require.NotContains(t, string(payload), "bigint")
}

func TestListMetadataRefusesAnUntypedPageToken(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	_, err := toolByName(t, server, "list_metadata").Run(context.Background(), testUser(),
		json.RawMessage(`{"parent":{"guid":"1;shop"},"page_token":"next"}`))
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
}

func TestSearchMetadataRefusesAPartialScope(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	_, err := callTool(t, server, "search_metadata", map[string]any{"keyword": "orders", "instance": "prod-mysql"})
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
}

func TestSearchMetadataSearchesEverythingWithoutAScope(t *testing.T) {
	t.Parallel()

	var seen *v1pb.SearchMetadataRequest
	readers := &fakeReaders{
		searchMetadata: func(request *v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
			seen = request
			return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
				foundTable(storedTable("1;shop;;orders", "orders")),
			}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "search_metadata", map[string]any{"keyword": "orders"})
	require.NoError(t, err)
	require.Nil(t, seen.ParentGuidPrefix, "no scope means the whole workspace")
	require.Equal(t, "1;shop;;orders", firstOf(t, asMap(t, value), "objects")["guid"])
}

// --- lineage ---

func TestAnalyzeSQLSeparatesTemporaryRelations(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listInstances: oneInstance(),
		listDatabases: oneDatabase(),
		analyzeSQL: func(request *v1pb.AnalyzeSQLRequest) (*v1pb.AnalyzeSQLResponse, error) {
			require.Len(t, request.GetScopes(), 1)
			require.Equal(t, "1;shop", request.GetScopes()[0].GetGuid())
			require.Equal(t, "shop", request.GetScopes()[0].GetName())
			return &v1pb.AnalyzeSQLResponse{Results: []*v1pb.AnalyzeSQLResult{{
				ScopeName: "shop",
				ScopeGuid: "1;shop",
				Relations: []*v1pb.AnalyzeSQLRelation{
					{SourceGuid: "1;shop;;orders", SourceColumn: "amount", TargetGuid: "1;shop;;daily", TargetColumn: "amount"},
					{SourceGuid: "1;shop;;orders", SourceColumn: "amount", TargetColumn: "amount_out", IsTemp: true},
				},
			}}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "analyze_sql", map[string]any{
		"sql":    "insert into daily (amount) select amount from orders",
		"scopes": []map[string]string{{"instance": "prod-mysql", "database": "shop"}},
	})
	require.NoError(t, err)

	scope := firstOf(t, asMap(t, value), "results")
	require.Len(t, listOf(t, scope, "relations"), 1)
	require.Len(t, listOf(t, scope, "temporaryRelations"), 1, "the temporary relation is the answer for a statement that writes nowhere")
	require.Equal(t, "amount_out", firstOf(t, scope, "temporaryRelations")["targetColumn"])
}

func TestAnalyzeSQLExpandsTargetsAndSkipsMissingOnes(t *testing.T) {
	t.Parallel()

	expanded := 0
	readers := &fakeReaders{
		listInstances: oneInstance(),
		listDatabases: oneDatabase(),
		analyzeSQL: func(*v1pb.AnalyzeSQLRequest) (*v1pb.AnalyzeSQLResponse, error) {
			return &v1pb.AnalyzeSQLResponse{Results: []*v1pb.AnalyzeSQLResult{{
				ScopeGuid: "1;shop",
				Relations: []*v1pb.AnalyzeSQLRelation{
					{TargetGuid: "1;shop;;daily"},
					{TargetGuid: "1;shop;;gone"},
				},
			}}}, nil
		},
		getLineageGraph: func(request *v1pb.GetLineageGraphRequest) (*v1pb.GetLineageGraphResponse, error) {
			expanded++
			require.Equal(t, int32(2), request.GetDepth())
			if request.GetGuid() == "1;shop;;gone" {
				return nil, connect.NewError(connect.CodeNotFound, errors.New("not in the registry"))
			}
			return &v1pb.GetLineageGraphResponse{RootGuid: request.GetGuid()}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "analyze_sql", map[string]any{
		"sql":    "insert into daily select * from orders",
		"scopes": []map[string]string{{"instance": "prod-mysql", "database": "shop"}},
		"depth":  2,
	})
	require.NoError(t, err)
	require.Equal(t, 2, expanded)
	scope := firstOf(t, asMap(t, value), "results")
	require.Len(t, listOf(t, scope, "graphs"), 1, "a target the registry cannot expand is skipped, not fatal")
}

func TestGetLineageGraphFiltersByColumn(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listInstances: oneInstance(),
		listDatabases: oneDatabase(),
		searchMetadata: func(*v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
			return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
				foundTable(storedTable("1;shop;;orders", "orders")),
			}}, nil
		},
		getLineageGraph: func(*v1pb.GetLineageGraphRequest) (*v1pb.GetLineageGraphResponse, error) {
			return &v1pb.GetLineageGraphResponse{
				RootGuid: "1;shop;;orders",
				Nodes: []*v1pb.LineageNode{
					{Guid: "1;shop;;orders"},
					{Guid: "1;shop;;daily"},
					{Guid: "1;shop;;other"},
				},
				Edges: []*v1pb.LineageRelation{
					{SourceGuid: "1;shop;;orders", SourceColumn: "amount", TargetGuid: "1;shop;;daily", TargetColumn: "amount"},
					{SourceGuid: "1;shop;;orders", SourceColumn: "id", TargetGuid: "1;shop;;other", TargetColumn: "id"},
				},
			}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_lineage_graph", map[string]any{
		"object": map[string]string{"instance": "prod-mysql", "database": "shop", "name": "orders"},
		"column": "amount",
	})
	require.NoError(t, err)

	graph := asMap(t, value)
	require.Len(t, listOf(t, graph, "edges"), 1)
	require.Len(t, listOf(t, graph, "nodes"), 2, "the nodes the surviving edge connects, and the root")
}

// --- dispatch ---

func TestDispatchRequiresIdentityAndPermission(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{listInstances: oneInstance()}
	checker := &fakeChecker{allow: true}
	server := newTestServer(t, readers, checker)
	definition := toolByName(t, server, "list_instances")

	t.Run("no verified identity", func(t *testing.T) {
		t.Parallel()
		request := &mcpsdk.CallToolRequest{Params: &mcpsdk.CallToolParamsRaw{Name: definition.Name, Arguments: json.RawMessage(`{}`)}}
		_, err := server.dispatch(context.Background(), request, definition)
		require.Equal(t, codeUnauthenticated, failureOf(t, err).Code)
	})

	t.Run("the permission is refused", func(t *testing.T) {
		t.Parallel()
		denied := &fakeChecker{allow: false}
		denyingServer := newTestServer(t, &fakeReaders{listInstances: oneInstance()}, denied)
		_, err := denyingServer.dispatch(context.Background(), requestFor(t, testUser(), definition.Name, `{}`), definition)
		require.Equal(t, codePermissionDenied, failureOf(t, err).Code)
		require.Equal(t, []string{permission.InstancesList}, denied.asked, "the tool's declared permission is what is checked")
	})
}

func TestDispatchReturnsOnlyStructuredContentAndRendersErrorsInBand(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{listInstances: oneInstance()}
	checker := &fakeChecker{allow: true}
	server := newTestServer(t, readers, checker)
	definition := toolByName(t, server, "list_instances")

	result, err := server.dispatch(context.Background(), requestFor(t, testUser(), definition.Name, `{}`), definition)
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Empty(t, result.Content, "structured content only: the SDK's text copy would double every result")
	require.NotNil(t, result.StructuredContent)
}

func TestErrorResultCarriesTheEnvelope(t *testing.T) {
	t.Parallel()

	result := errorResult(newToolError(codeNotFound, "no object matches", "search first"))
	require.True(t, result.IsError)
	require.Len(t, result.Content, 1)
	text := result.Content[0].(*mcpsdk.TextContent).Text
	require.Contains(t, text, codeNotFound)
	require.Contains(t, text, "search first")
}

func requestFor(t *testing.T, user *store.UserMessage, name, args string) *mcpsdk.CallToolRequest {
	t.Helper()
	return &mcpsdk.CallToolRequest{
		Params: &mcpsdk.CallToolParamsRaw{Name: name, Arguments: json.RawMessage(args)},
		Extra: &mcpsdk.RequestExtra{TokenInfo: &sdkauth.TokenInfo{
			UserID: "7",
			Extra:  map[string]any{userExtraKey: user},
		}},
	}
}

// --- small helpers ---

func TestToolErrorFromRPCMapsCodes(t *testing.T) {
	t.Parallel()

	cases := map[connect.Code]string{
		connect.CodeNotFound:         codeNotFound,
		connect.CodePermissionDenied: codePermissionDenied,
		connect.CodeInvalidArgument:  codeInvalidArgument,
		connect.CodeDeadlineExceeded: codeTimeout,
		connect.CodeUnknown:          codeInternal,
	}
	for code, want := range cases {
		require.Equal(t, want, toolErrorFromRPC(connect.NewError(code, errors.New("x"))).Code, "code %v", code)
	}
	require.Nil(t, toolErrorFromRPC(nil))
}

func TestNormalizePageSize(t *testing.T) {
	t.Parallel()

	size, err := normalizePageSize(0)
	require.NoError(t, err)
	require.Equal(t, int32(defaultPageSize), size)

	size, err = normalizePageSize(5000)
	require.NoError(t, err)
	require.Equal(t, int32(maxPageSize), size)

	size, err = normalizePageSize(50)
	require.NoError(t, err)
	require.Equal(t, int32(50), size)

	_, err = normalizePageSize(-1)
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
}

func TestParseMetaTypeAndDirection(t *testing.T) {
	t.Parallel()

	metaType, err := parseMetaType("table")
	require.NoError(t, err)
	require.Equal(t, v1pb.MetaType_TABLE, metaType)
	_, err = parseMetaType("nonsense")
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)

	require.Equal(t, v1pb.LineageType_SOURCE, mustDirection(t, "up"))
	require.Equal(t, v1pb.LineageType_TARGET, mustDirection(t, "down"))
	require.Equal(t, v1pb.LineageType_LINEAGE_TYPE_UNSPECIFIED, mustDirection(t, ""))
	_, err = parseDirection("sideways")
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
}

func mustDirection(t *testing.T, value string) v1pb.LineageType {
	t.Helper()
	direction, err := parseDirection(value)
	require.NoError(t, err)
	return direction
}
