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
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeReaders implements all five reader interfaces, with one function per
// method: a test states exactly what a tool may see, and any call it did not
// expect fails the test instead of quietly succeeding.
type fakeReaders struct {
	t *testing.T

	listInstances         func(*v1pb.ListInstancesRequest) (*v1pb.ListInstancesResponse, error)
	listDatabases         func(*v1pb.ListDatabasesRequest) (*v1pb.ListDatabasesResponse, error)
	listMetadata          func(*v1pb.ListMetadataRequest) (*v1pb.MetadataResponse, error)
	getMetadata           func(*v1pb.GetMetadataRequest) (*v1pb.GetMetadataResponse, error)
	searchMetadata        func(*v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error)
	getSchemaString       func(*v1pb.GetSchemaStringRequest) (*v1pb.MetadataSchemaString, error)
	analyzeSQL            func(*v1pb.AnalyzeSQLRequest) (*v1pb.AnalyzeSQLResponse, error)
	getLineageGraph       func(*v1pb.GetLineageGraphRequest) (*v1pb.GetLineageGraphResponse, error)
	getCurrentUser        func(*emptypb.Empty) (*v1pb.User, error)
	listQualityTargets    func(*v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error)
	listQualityIssues     func(*v1pb.ListQualityIssuesRequest) (*v1pb.ListQualityIssuesResponse, error)
	getQualityTarget      func(*v1pb.GetQualityTargetRequest) (*v1pb.GetQualityTargetResponse, error)
	listQualityExecutions func(*v1pb.ListQualityExecutionsRequest) (*v1pb.ListQualityExecutionsResponse, error)
	getQualityExecution   func(*v1pb.GetQualityExecutionRequest) (*v1pb.QualityExecution, error)
	getQualityImpact      func(*v1pb.GetQualityTargetImpactRequest) (*v1pb.GetQualityTargetImpactResponse, error)
	getQualityCoverage    func(*v1pb.GetQualityConsumptionCoverageRequest) (*v1pb.GetQualityConsumptionCoverageResponse, error)
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

func (f *fakeReaders) ListQualityTargets(_ context.Context, request *connect.Request[v1pb.ListQualityTargetsRequest]) (*connect.Response[v1pb.ListQualityTargetsResponse], error) {
	if f.listQualityTargets == nil {
		return nil, f.unexpected("ListQualityTargets")
	}
	response, err := f.listQualityTargets(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) ListQualityIssues(_ context.Context, request *connect.Request[v1pb.ListQualityIssuesRequest]) (*connect.Response[v1pb.ListQualityIssuesResponse], error) {
	if f.listQualityIssues == nil {
		return nil, f.unexpected("ListQualityIssues")
	}
	response, err := f.listQualityIssues(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) GetQualityTarget(_ context.Context, request *connect.Request[v1pb.GetQualityTargetRequest]) (*connect.Response[v1pb.GetQualityTargetResponse], error) {
	if f.getQualityTarget == nil {
		return nil, f.unexpected("GetQualityTarget")
	}
	response, err := f.getQualityTarget(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) ListQualityExecutions(_ context.Context, request *connect.Request[v1pb.ListQualityExecutionsRequest]) (*connect.Response[v1pb.ListQualityExecutionsResponse], error) {
	if f.listQualityExecutions == nil {
		return nil, f.unexpected("ListQualityExecutions")
	}
	response, err := f.listQualityExecutions(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) GetQualityExecution(_ context.Context, request *connect.Request[v1pb.GetQualityExecutionRequest]) (*connect.Response[v1pb.QualityExecution], error) {
	if f.getQualityExecution == nil {
		return nil, f.unexpected("GetQualityExecution")
	}
	response, err := f.getQualityExecution(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) GetQualityTargetImpact(_ context.Context, request *connect.Request[v1pb.GetQualityTargetImpactRequest]) (*connect.Response[v1pb.GetQualityTargetImpactResponse], error) {
	if f.getQualityImpact == nil {
		return nil, f.unexpected("GetQualityTargetImpact")
	}
	response, err := f.getQualityImpact(request.Msg)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(response), nil
}

func (f *fakeReaders) GetQualityConsumptionCoverage(_ context.Context, request *connect.Request[v1pb.GetQualityConsumptionCoverageRequest]) (*connect.Response[v1pb.GetQualityConsumptionCoverageResponse], error) {
	if f.getQualityCoverage == nil {
		return nil, f.unexpected("GetQualityConsumptionCoverage")
	}
	response, err := f.getQualityCoverage(request.Msg)
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
		Quality:    readers,
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

// asObject reads a nested object out of a payload, failing the test rather than
// panicking when the shape is not the one it expected.
func asObject(t *testing.T, value any) map[string]any {
	t.Helper()
	object, ok := value.(map[string]any)
	require.True(t, ok, "value is not an object: %T", value)
	return object
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

func storedColumn(guid, name string) *v1pb.StoredMetadata {
	return &v1pb.StoredMetadata{
		Guid: guid,
		Type: &v1pb.StoredMetadata_ColumnMetadata{ColumnMetadata: &v1pb.ColumnMetadata{Name: name}},
	}
}

// foundObject is one search hit of an arbitrary type; the result carries the same
// guid the stored payload does, which is what the read RPC returns.
func foundObject(stored *v1pb.StoredMetadata, metaType v1pb.MetaType) *v1pb.SearchMetadataResult {
	return &v1pb.SearchMetadataResult{Guid: stored.GetGuid(), MetaType: metaType, Metadata: stored}
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

// --- quality signals ---

func TestGetQualitySignalReadsOneMonitoredTarget(t *testing.T) {
	t.Parallel()

	score, passRate := 87.5, 0.75
	var seen *v1pb.ListQualityTargetsRequest
	readers := &fakeReaders{
		listInstances: oneInstance(),
		listDatabases: oneDatabase(),
		searchMetadata: func(*v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
			return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
				foundTable(storedTable("1;shop;;orders", "orders")),
			}}, nil
		},
		listQualityTargets: func(request *v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error) {
			seen = request
			return &v1pb.ListQualityTargetsResponse{QualityTargets: []*v1pb.QualityTargetSummary{{
				Target: &v1pb.QualityTarget{
					MetaGuid: "1;shop;;orders",
					MetaType: v1pb.MetaType_TABLE,
					Path:     &v1pb.QualityTargetPath{Instance: "prod-mysql", Database: "shop", Object: "orders"},
				},
				EnabledRuleCount:     4,
				WorstStatus:          v1pb.QualityStatus_QUALITY_STATUS_WARN,
				LastRunTime:          timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
				QualityScore:         &score,
				RecentPassRate:       &passRate,
				RecentExecutionCount: 6,
				CoveredColumnCount:   2,
				TotalColumnCount:     5,
				OpenIssueCount:       1,
				SchemaChanged:        true,
			}}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_signal", map[string]any{
		"object": map[string]string{"instance": "prod-mysql", "database": "shop", "name": "orders"},
	})
	require.NoError(t, err)

	// The selection is exact and unpaged: one key, no filters, no page size.
	require.Equal(t, "workspaces/-", seen.GetParent())
	require.Len(t, seen.GetTargets(), 1)
	require.Equal(t, "1;shop;;orders", seen.GetTargets()[0].GetTargetGuid())
	require.Equal(t, v1pb.MetaType_TABLE, seen.GetTargets()[0].GetMetaType())
	require.Zero(t, seen.GetPageSize())
	require.Zero(t, seen.GetPageToken())

	payload := asMap(t, value)
	require.Equal(t, true, payload["monitored"])
	require.Equal(t, "TABLE", payload["metaType"])
	require.Equal(t, "1;shop;;orders", payload["guid"])
	require.Equal(t, "orders", payload["name"])
	require.Equal(t, "QUALITY_STATUS_WARN", payload["worstStatus"])
	require.Equal(t, "2026-01-02T03:04:05Z", payload["lastRunTime"])
	require.Equal(t, 87.5, payload["qualityScore"])
	require.Equal(t, 0.75, payload["recentPassRate"])
	require.Equal(t, float64(6), payload["recentExecutionCount"])
	require.Equal(t, float64(2), payload["coveredColumnCount"])
	require.Equal(t, float64(5), payload["totalColumnCount"])
	require.Equal(t, float64(1), payload["openIssueCount"])
	require.Equal(t, float64(4), payload["enabledRuleCount"])
	require.Equal(t, true, payload["schemaChanged"])
	require.Equal(t, false, payload["orphaned"])
}

// TestGetQualitySignalReportsAnUnmonitoredObject pins the distinction the tool
// exists to preserve: a target the server omits from a selection has no live
// rule, which is an answer — not monitored — and not a missing object.
func TestGetQualitySignalReportsAnUnmonitoredObject(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listQualityTargets: func(*v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error) {
			return &v1pb.ListQualityTargetsResponse{}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_signal", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"meta_type": "TABLE",
	})
	require.NoError(t, err, "no live rule is not a not_found")

	payload := asMap(t, value)
	require.Equal(t, false, payload["monitored"])
	require.Equal(t, "1;shop;;orders", payload["guid"])
	require.Equal(t, "TABLE", payload["metaType"])
	require.NotContains(t, payload, "worstStatus")
	require.NotContains(t, payload, "qualityScore")
	require.NotContains(t, payload, "enabledRuleCount", "an unmonitored object has no signal to report")
}

// TestGetQualitySignalOmitsWhatHasNotBeenJudged covers a monitored target whose
// checks have not run yet: the judged fields are absent rather than reported as
// zero, which would read as a measured result.
func TestGetQualitySignalOmitsWhatHasNotBeenJudged(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listQualityTargets: func(*v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error) {
			return &v1pb.ListQualityTargetsResponse{QualityTargets: []*v1pb.QualityTargetSummary{{
				Target:           &v1pb.QualityTarget{MetaGuid: "1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
				EnabledRuleCount: 1,
			}}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_signal", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"meta_type": "TABLE",
	})
	require.NoError(t, err)

	payload := asMap(t, value)
	require.Equal(t, true, payload["monitored"])
	require.Equal(t, float64(1), payload["enabledRuleCount"])
	require.NotContains(t, payload, "worstStatus")
	require.NotContains(t, payload, "lastRunTime")
	require.NotContains(t, payload, "qualityScore")
	require.NotContains(t, payload, "recentPassRate")
}

func TestGetQualitySignalNeedsAMetaTypeForABareGUID(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	_, err := callTool(t, server, "get_quality_signal", map[string]any{
		"object": map[string]string{"guid": "1;shop;;orders"},
	})
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
}

// TestGetQualitySignalReportsAllPaused covers a target whose checks all exist but
// are paused. The aggregate still carries the paused checks' last verdict, which is
// history rather than a health claim: the answer names the state and omits the
// verdict, so a model cannot read the paused table as healthy.
func TestGetQualitySignalReportsAllPaused(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listQualityTargets: func(*v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error) {
			return &v1pb.ListQualityTargetsResponse{QualityTargets: []*v1pb.QualityTargetSummary{{
				Target:           &v1pb.QualityTarget{MetaGuid: "1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
				EnabledRuleCount: 0,
				PausedRuleCount:  2,
				// The last verdict of the paused checks; still not a current claim.
				WorstStatus:        v1pb.QualityStatus_QUALITY_STATUS_PASS,
				CoveredColumnCount: 0,
				TotalColumnCount:   3,
			}}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_signal", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"meta_type": "TABLE",
	})
	require.NoError(t, err)

	payload := asMap(t, value)
	require.Equal(t, true, payload["monitored"])
	require.Equal(t, true, payload["allPaused"])
	require.Equal(t, float64(0), payload["enabledRuleCount"])
	require.NotContains(t, payload, "worstStatus", "a paused check's last verdict is not a current health claim")
	require.Equal(t, float64(3), payload["totalColumnCount"])
	require.Equal(t, float64(0), payload["coveredColumnCount"], "a paused check covers no column")
}

// TestGetQualitySignalAnswersUnmonitoredForAnUnwatchableType covers the object
// types no rule can watch: the quality RPC refuses such a key, so the tool answers
// the truthful signal — nothing monitors it — instead of asking.
func TestGetQualitySignalAnswersUnmonitoredForAnUnwatchableType(t *testing.T) {
	t.Parallel()

	// No quality reader is configured: any RPC call at all fails the test.
	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_signal", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;amount"},
		"meta_type": "COLUMN",
	})
	require.NoError(t, err)

	payload := asMap(t, value)
	require.Equal(t, false, payload["monitored"])
	require.Equal(t, "COLUMN", payload["metaType"])
	require.NotContains(t, payload, "enabledRuleCount")
}

// --- quality read models ---

// TestListQualityIssuesEnrichesTargetPaths pins the one enrichment call the
// listing makes: the API's issue rows carry only the target's GUID and type, so
// the target path is read from the directory in a single selection.
func TestListQualityIssuesEnrichesTargetPaths(t *testing.T) {
	t.Parallel()

	var seenIssues *v1pb.ListQualityIssuesRequest
	var seenTargets *v1pb.ListQualityTargetsRequest
	readers := &fakeReaders{
		listQualityIssues: func(request *v1pb.ListQualityIssuesRequest) (*v1pb.ListQualityIssuesResponse, error) {
			seenIssues = request
			return &v1pb.ListQualityIssuesResponse{QualityIssues: []*v1pb.QualityIssue{
				{
					Name:            "workspaces/1/qualityIssues/1",
					Rule:            "workspaces/1/qualityRules/9",
					Target:          &v1pb.QualityTarget{MetaGuid: "1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
					Status:          v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN,
					Severity:        v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
					FirstSeenTime:   timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
					OccurrenceCount: 3,
				},
				{
					Name:   "workspaces/1/qualityIssues/2",
					Target: &v1pb.QualityTarget{MetaGuid: "1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
					Status: v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_RESOLVED,
				},
				{
					Name:   "workspaces/1/qualityIssues/3",
					Target: &v1pb.QualityTarget{MetaGuid: "1;shop;;gone", MetaType: v1pb.MetaType_VIEW},
				},
			}}, nil
		},
		listQualityTargets: func(request *v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error) {
			seenTargets = request
			return &v1pb.ListQualityTargetsResponse{QualityTargets: []*v1pb.QualityTargetSummary{{
				Target: &v1pb.QualityTarget{
					MetaGuid: "1;shop;;orders",
					MetaType: v1pb.MetaType_TABLE,
					Path:     &v1pb.QualityTargetPath{Instance: "prod-mysql", Database: "shop", Object: "orders"},
				},
			}}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "list_quality_issues", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"status":    "OPEN",
		"severity":  "QUALITY_SEVERITY_ERROR",
		"page_size": 500,
	})
	require.NoError(t, err)

	// Both enum spellings are accepted, and the page size is explicit and clamped
	// rather than left to the RPC's larger default.
	require.Equal(t, "workspaces/-", seenIssues.GetParent())
	require.Equal(t, "1;shop;;orders", seenIssues.GetTargetGuid())
	require.Equal(t, v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_OPEN, seenIssues.GetStatus())
	require.Equal(t, v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR, seenIssues.GetSeverity())
	require.Equal(t, int32(maxPageSize), seenIssues.GetPageSize())

	// One selection call, deduplicated, and only the two watchable targets.
	require.Equal(t, "workspaces/-", seenTargets.GetParent())
	require.Len(t, seenTargets.GetTargets(), 2)
	require.Zero(t, seenTargets.GetPageSize())

	issues := listOf(t, asMap(t, value), "issues")
	require.Len(t, issues, 3)
	first := asObject(t, issues[0])
	require.Equal(t, "QUALITY_ISSUE_STATUS_OPEN", first["status"])
	require.Equal(t, "QUALITY_SEVERITY_ERROR", first["severity"])
	require.Equal(t, "2026-01-02T03:04:05Z", first["firstSeenTime"])
	require.Equal(t, float64(3), first["occurrenceCount"])
	target := asObject(t, first["target"])
	require.Equal(t, "1;shop;;orders", target["guid"])
	require.Equal(t, "TABLE", target["metaType"])
	require.NotNil(t, target["path"], "the path is what the enrichment adds")

	// The same target twice is one key, so both rows get the same path.
	second := asObject(t, asObject(t, issues[1])["target"])
	require.NotNil(t, second["path"])

	// A target the directory omits has no path, and the listing still answers.
	third := asObject(t, asObject(t, issues[2])["target"])
	require.NotContains(t, third, "path")
}

// TestListQualityIssuesToleratesAnEnrichmentFailure is the behaviour that matters
// most about the enrichment: it is decoration, not the listing.
func TestListQualityIssuesToleratesAnEnrichmentFailure(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listQualityIssues: func(*v1pb.ListQualityIssuesRequest) (*v1pb.ListQualityIssuesResponse, error) {
			return &v1pb.ListQualityIssuesResponse{QualityIssues: []*v1pb.QualityIssue{{
				Name:   "workspaces/1/qualityIssues/1",
				Target: &v1pb.QualityTarget{MetaGuid: "1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
			}}}, nil
		},
		listQualityTargets: func(*v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error) {
			return nil, connect.NewError(connect.CodeInternal, errors.New("the directory is unavailable"))
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "list_quality_issues", map[string]any{})
	require.NoError(t, err)

	issue := firstOf(t, asMap(t, value), "issues")
	require.NotContains(t, asObject(t, issue["target"]), "path")
}

func TestGetQualityTargetProjectsACheck(t *testing.T) {
	t.Parallel()

	score := 50.0
	var seen *v1pb.GetQualityTargetRequest
	readers := &fakeReaders{
		getQualityTarget: func(request *v1pb.GetQualityTargetRequest) (*v1pb.GetQualityTargetResponse, error) {
			seen = request
			return &v1pb.GetQualityTargetResponse{
				Summary: &v1pb.QualityTargetSummary{
					Target: &v1pb.QualityTarget{
						MetaGuid: "1;shop;;orders",
						MetaType: v1pb.MetaType_TABLE,
						Path:     &v1pb.QualityTargetPath{Instance: "prod-mysql", Database: "shop", Object: "orders"},
					},
					RuleCount:        1,
					EnabledRuleCount: 1,
					WorstStatus:      v1pb.QualityStatus_QUALITY_STATUS_FAIL,
					QualityScore:     &score,
				},
				QualityRules: []*v1pb.QualityRule{{
					Name:                "workspaces/1/qualityRules/9",
					Title:               "row count",
					Description:         "not part of this projection",
					Dimension:           v1pb.QualityDimension_QUALITY_DIMENSION_COMPLETENESS,
					RuleType:            v1pb.QualityRuleType_QUALITY_RULE_TYPE_ROW_COUNT,
					Config:              &v1pb.QualityRuleConfig{},
					Severity:            v1pb.QualitySeverity_QUALITY_SEVERITY_ERROR,
					State:               v1pb.QualityRuleState_QUALITY_RULE_STATE_ENABLED,
					Revision:            2,
					Tags:                []string{"tier-1"},
					TargetSchemaChanged: true,
					MuteUntil:           timestamppb.New(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)),
					LastResult: &v1pb.QualityExecutionSummary{
						Status:     v1pb.QualityStatus_QUALITY_STATUS_FAIL,
						FailedRows: 5,
					},
				}},
				OpenIssues: []*v1pb.QualityIssue{{
					Name:     "workspaces/1/qualityIssues/1",
					Target:   &v1pb.QualityTarget{MetaGuid: "1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
					Status:   v1pb.QualityIssueStatus_QUALITY_ISSUE_STATUS_ACKNOWLEDGED,
					Severity: v1pb.QualitySeverity_QUALITY_SEVERITY_WARNING,
				}},
			}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_target", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"meta_type": "TABLE",
	})
	require.NoError(t, err)

	require.Equal(t, "workspaces/-", seen.GetParent())
	require.Equal(t, "1;shop;;orders", seen.GetTargetGuid())
	require.Equal(t, v1pb.MetaType_TABLE, seen.GetMetaType())

	payload := asMap(t, value)
	require.Equal(t, true, payload["monitored"])
	require.Equal(t, false, payload["allPaused"], "an enabled check is being verified")
	target := asObject(t, payload["target"])
	require.Equal(t, "1;shop;;orders", target["guid"])
	require.Equal(t, "orders", target["name"], "a guid reference takes its name from the stored path")
	require.Equal(t, "QUALITY_STATUS_FAIL", asObject(t, payload["summary"])["worstStatus"])

	rule := firstOf(t, payload, "rules")
	require.Equal(t, "row count", rule["title"])
	require.Equal(t, "QUALITY_RULE_TYPE_ROW_COUNT", rule["ruleType"])
	require.Equal(t, "QUALITY_DIMENSION_COMPLETENESS", rule["dimension"])
	require.Equal(t, "QUALITY_RULE_STATE_ENABLED", rule["state"])
	require.Equal(t, "QUALITY_SEVERITY_ERROR", rule["severity"])
	require.Equal(t, float64(2), rule["revision"])
	require.Equal(t, "2026-02-01T00:00:00Z", rule["muteUntil"])
	require.Equal(t, []any{"tier-1"}, rule["tags"])
	require.Equal(t, true, rule["targetSchemaChanged"])
	require.Equal(t, "QUALITY_STATUS_FAIL", asObject(t, rule["lastResult"])["status"])
	require.NotContains(t, rule, "description", "the rule's description is not part of this projection")
	require.NotContains(t, rule, "target", "the rule's own target would duplicate the answer's")
	require.NotContains(t, rule, "createTime")
	require.NotContains(t, rule, "updateTime")

	issue := firstOf(t, payload, "openIssues")
	require.Equal(t, "QUALITY_ISSUE_STATUS_ACKNOWLEDGED", issue["status"])
	require.NotNil(t, asObject(t, issue["target"])["path"], "an open issue names its table from the workspace path")
}

func TestGetQualityTargetRequiresATypeAndSkipsUnwatchableObjects(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	_, err := callTool(t, server, "get_quality_target", map[string]any{
		"object": map[string]string{"guid": "1;shop;;orders"},
	})
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)

	// No quality reader is configured: an unwatchable object must not reach the RPC.
	value, err := callTool(t, server, "get_quality_target", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;amount"},
		"meta_type": "COLUMN",
	})
	require.NoError(t, err)
	payload := asMap(t, value)
	require.Equal(t, false, payload["monitored"])
	require.NotContains(t, payload, "rules")
}

func TestGetQualityTargetReportsATargetWithNoLiveCheck(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		getQualityTarget: func(*v1pb.GetQualityTargetRequest) (*v1pb.GetQualityTargetResponse, error) {
			return &v1pb.GetQualityTargetResponse{Summary: &v1pb.QualityTargetSummary{
				Target: &v1pb.QualityTarget{MetaGuid: "1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
			}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_target", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"meta_type": "TABLE",
	})
	require.NoError(t, err)

	payload := asMap(t, value)
	require.Equal(t, false, payload["monitored"])
	require.Empty(t, listOf(t, payload, "rules"))
}

func TestListQualityExecutionsProjectsRows(t *testing.T) {
	t.Parallel()

	var seen *v1pb.ListQualityExecutionsRequest
	baseline := 100.0
	readers := &fakeReaders{
		listQualityExecutions: func(request *v1pb.ListQualityExecutionsRequest) (*v1pb.ListQualityExecutionsResponse, error) {
			seen = request
			return &v1pb.ListQualityExecutionsResponse{QualityExecutions: []*v1pb.QualityExecution{{
				Name: "workspaces/1/qualityExecutions/5",
				Rule: "workspaces/1/qualityRules/9",
				Target: &v1pb.QualityTarget{
					MetaGuid: "1;shop;;orders",
					MetaType: v1pb.MetaType_TABLE,
					Path:     &v1pb.QualityTargetPath{Instance: "prod-mysql", Database: "shop", Object: "orders"},
				},
				Instance:      "instances/1",
				Run:           "workspaces/1/qualityRuns/2",
				State:         v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
				Outcome:       v1pb.QualityStatus_QUALITY_STATUS_FAIL,
				Trigger:       v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED,
				Attempt:       2,
				ObservedValue: 7,
				ObservedKind:  v1pb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
				Thresholds:    &v1pb.QualityThresholds{Comparison: v1pb.QualityComparison_QUALITY_COMPARISON_GREATER_THAN},
				FailedRows:    7,
				TotalRows:     100,
				Sampled:       true,
				SampleRows:    5000,
				Truncated:     true,
				Samples: []*v1pb.FailureSample{{
					Columns: []*v1pb.ColumnValue{{Column: "amount", Value: "42"}},
				}},
				ExecutedSql:   "select count(*) from orders",
				Duration:      durationpb.New(2 * time.Second),
				StartTime:     timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
				FinishTime:    timestamppb.New(time.Date(2026, 1, 2, 3, 4, 7, 0, time.UTC)),
				BaselineValue: &baseline,
				BaselineTime:  timestamppb.New(time.Date(2026, 1, 1, 3, 4, 5, 0, time.UTC)),
			}}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "list_quality_executions", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"rule":      "workspaces/1/qualityRules/9",
		"state":     "RUNNING",
		"outcome":   "FAIL",
		"trigger":   "SCHEDULED",
		"page_size": 10,
	})
	require.NoError(t, err)

	require.Equal(t, "workspaces/-", seen.GetParent())
	require.Equal(t, "1;shop;;orders", seen.GetTargetGuid())
	require.Equal(t, "workspaces/1/qualityRules/9", seen.GetRule())
	require.Equal(t, v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_RUNNING, seen.GetState())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_FAIL, seen.GetOutcome())
	require.Equal(t, v1pb.QualityExecutionTrigger_QUALITY_EXECUTION_TRIGGER_SCHEDULED, seen.GetTrigger())
	require.Equal(t, int32(10), seen.GetPageSize())

	row := firstOf(t, asMap(t, value), "executions")
	require.Equal(t, "QUALITY_EXECUTION_STATE_COMPLETED", row["state"])
	require.Equal(t, "QUALITY_STATUS_FAIL", row["outcome"])
	require.Equal(t, "QUALITY_EXECUTION_TRIGGER_SCHEDULED", row["trigger"])
	require.Equal(t, "instances/1", row["instance"])
	require.Equal(t, "workspaces/1/qualityRuns/2", row["run"])
	require.Equal(t, float64(7), row["observedValue"])
	require.Equal(t, "QUALITY_OBSERVED_KIND_COUNT", row["observedKind"])
	require.Equal(t, float64(7), row["failedRows"])
	require.Equal(t, float64(100), row["totalRows"])
	require.Equal(t, true, row["sampled"])
	require.Equal(t, float64(5000), row["sampleRows"])
	require.Equal(t, true, row["truncated"])
	require.Equal(t, float64(100), row["baselineValue"])
	require.Equal(t, "2026-01-01T03:04:05Z", row["baselineTime"])
	require.Equal(t, "2026-01-02T03:04:05Z", row["startTime"])
	require.Equal(t, "2026-01-02T03:04:07Z", row["finishTime"])
	require.NotNil(t, asObject(t, row["target"])["path"])
	require.NotNil(t, row["thresholds"])
	require.NotNil(t, row["duration"])
	// The listing is a projection: the evidence stays in get_quality_execution.
	require.NotContains(t, row, "samples")
	require.NotContains(t, row, "executedSql")
	require.NotContains(t, row, "ruleRevision")
}

func TestGetQualityExecutionReturnsItsWholeEvidence(t *testing.T) {
	t.Parallel()

	var seen *v1pb.GetQualityExecutionRequest
	readers := &fakeReaders{
		getQualityExecution: func(request *v1pb.GetQualityExecutionRequest) (*v1pb.QualityExecution, error) {
			seen = request
			return &v1pb.QualityExecution{
				Name:        request.GetName(),
				ExecutedSql: "select count(*) from orders",
				Samples: []*v1pb.FailureSample{{
					Columns: []*v1pb.ColumnValue{{Column: "amount", Value: "42"}},
				}},
			}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_execution", map[string]any{
		"execution": "workspaces/1/qualityExecutions/5",
	})
	require.NoError(t, err)
	require.Equal(t, "workspaces/1/qualityExecutions/5", seen.GetName())

	payload := asMap(t, value)
	require.Equal(t, "select count(*) from orders", payload["executedSql"])
	require.Equal(t, "42", firstOf(t, asObject(t, listOf(t, payload, "samples")[0]), "columns")["value"])

	_, err = callTool(t, server, "get_quality_execution", map[string]any{})
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
}

// TestGetQualityExecutionPinsZeroValueOmission pins the documented contract that
// the whole-execution protoJSON omits zero-valued fields (the default marshal
// options, not EmitUnpopulated), so a future marshal change cannot silently
// invalidate the guide and docs that promise it.
func TestGetQualityExecutionPinsZeroValueOmission(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		getQualityExecution: func(request *v1pb.GetQualityExecutionRequest) (*v1pb.QualityExecution, error) {
			return &v1pb.QualityExecution{
				Name:          request.GetName(),
				State:         v1pb.QualityExecutionState_QUALITY_EXECUTION_STATE_COMPLETED,
				Outcome:       v1pb.QualityStatus_QUALITY_STATUS_PASS,
				Attempt:       1,
				ObservedValue: 0,
				FailedRows:    0,
				TotalRows:     0,
				Sampled:       false,
				SampleRows:    0,
				Truncated:     false,
			}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_execution", map[string]any{
		"execution": "workspaces/1/qualityExecutions/7",
	})
	require.NoError(t, err)

	payload := asMap(t, value)
	require.Equal(t, "workspaces/1/qualityExecutions/7", payload["name"])
	// A non-zero field still travels, so the omissions below are not vacuous.
	require.Equal(t, float64(1), payload["attempt"])
	for _, omitted := range []string{"observedValue", "failedRows", "sampled", "truncated"} {
		require.NotContains(t, payload, omitted, "%s is a zero scalar and must be omitted", omitted)
	}
}

func TestListQualityTargetsProjectsTheDirectory(t *testing.T) {
	t.Parallel()

	score, passRate := 91.0, 0.8
	var seen *v1pb.ListQualityTargetsRequest
	readers := &fakeReaders{
		listQualityTargets: func(request *v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error) {
			seen = request
			return &v1pb.ListQualityTargetsResponse{QualityTargets: []*v1pb.QualityTargetSummary{{
				Target: &v1pb.QualityTarget{
					MetaGuid: "1;shop;;orders",
					MetaType: v1pb.MetaType_TABLE,
					Path:     &v1pb.QualityTargetPath{Instance: "prod-mysql", Database: "shop", Object: "orders"},
				},
				RuleCount:            3,
				EnabledRuleCount:     2,
				PausedRuleCount:      1,
				ColumnCount:          2,
				OpenIssueCount:       1,
				PendingDraftCount:    2,
				PendingLabelCount:    3,
				WorstStatus:          v1pb.QualityStatus_QUALITY_STATUS_WARN,
				LastRunTime:          timestamppb.New(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)),
				SchemaChanged:        true,
				QualityScore:         &score,
				ScoredRuleCount:      2,
				RecentPassRate:       &passRate,
				RecentExecutionCount: 5,
				CoveredColumnCount:   2,
				TotalColumnCount:     4,
				LatestRun:            &v1pb.QualityRun{Name: "workspaces/1/qualityRuns/2"},
				RunCount:             4,
				Schedule:             &v1pb.QualityTargetSchedule{Cron: "0 2 * * *", Timezone: "UTC"},
			}}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "list_quality_targets", map[string]any{
		"search":         "order",
		"worst_status":   "QUALITY_STATUS_WARN",
		"has_open_issue": true,
		"page_size":      10,
	})
	require.NoError(t, err)

	require.Equal(t, "workspaces/-", seen.GetParent())
	require.Equal(t, "order", seen.GetSearch())
	require.Equal(t, v1pb.QualityStatus_QUALITY_STATUS_WARN, seen.GetWorstStatus())
	require.True(t, seen.GetHasOpenIssue())
	require.Equal(t, int32(10), seen.GetPageSize())

	row := firstOf(t, asMap(t, value), "targets")
	require.Equal(t, "1;shop;;orders", row["guid"])
	require.Equal(t, "TABLE", row["metaType"])
	require.NotNil(t, row["path"])
	require.Equal(t, float64(3), row["ruleCount"])
	require.Equal(t, float64(2), row["enabledRuleCount"])
	require.Equal(t, float64(1), row["pausedRuleCount"])
	require.Equal(t, float64(2), row["columnCount"])
	require.Equal(t, float64(1), row["openIssueCount"])
	require.Equal(t, false, row["allPaused"], "an enabled check is being verified")
	require.Equal(t, "QUALITY_STATUS_WARN", row["worstStatus"])
	require.Equal(t, "2026-01-02T03:04:05Z", row["lastRunTime"])
	require.Equal(t, true, row["schemaChanged"])
	require.Equal(t, float64(91), row["qualityScore"])
	require.Equal(t, 0.8, row["recentPassRate"])
	require.Equal(t, float64(5), row["recentExecutionCount"])
	require.Equal(t, float64(2), row["coveredColumnCount"])
	require.Equal(t, float64(4), row["totalColumnCount"])
	require.Equal(t, float64(4), row["runCount"])
	require.NotNil(t, row["latestRun"])
	require.NotNil(t, row["schedule"])
}

// TestQualityFiltersListAcceptedEnumValues keeps a bad enum a self-correcting
// error: the message names what the tool does accept.
func TestQualityFiltersListAcceptedEnumValues(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	cases := []struct {
		tool   string
		args   map[string]any
		values []string
	}{
		{tool: "list_quality_issues", args: map[string]any{"status": "PENDING"}, values: []string{"ACKNOWLEDGED", "OPEN", "RESOLVED"}},
		{tool: "list_quality_issues", args: map[string]any{"severity": "BLOCKER"}, values: []string{"CRITICAL", "ERROR", "WARNING"}},
		{tool: "list_quality_executions", args: map[string]any{"state": "DONE"}, values: []string{"ABANDONED", "COMPLETED", "PENDING", "RUNNING"}},
		{tool: "list_quality_executions", args: map[string]any{"outcome": "BAD"}, values: []string{"ERROR", "FAIL", "NOT_EVALUATED", "PASS", "SKIPPED", "WARN"}},
		{tool: "list_quality_executions", args: map[string]any{"trigger": "CRON"}, values: []string{"MANUAL", "SCHEDULED"}},
		{tool: "list_quality_targets", args: map[string]any{"worst_status": "BAD"}, values: []string{"ERROR", "FAIL", "NOT_EVALUATED", "PASS", "SKIPPED", "WARN"}},
	}
	for _, testCase := range cases {
		_, err := callTool(t, server, testCase.tool, testCase.args)
		failure := failureOf(t, err)
		require.Equal(t, codeInvalidArgument, failure.Code)
		for _, value := range testCase.values {
			require.Contains(t, failure.Message, value)
		}
	}
}

// TestListQualityExecutionsOmitsAbsentObservations pins the zero-vs-absent
// distinction: the API's non-optional observed_value is 0 when the runner recorded
// no measurement, which must not read as an observed zero.
func TestListQualityExecutionsOmitsAbsentObservations(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listQualityExecutions: func(*v1pb.ListQualityExecutionsRequest) (*v1pb.ListQualityExecutionsResponse, error) {
			return &v1pb.ListQualityExecutionsResponse{QualityExecutions: []*v1pb.QualityExecution{
				// ERROR can still carry a kind: the observation was attempted and
				// failed, so the row must not look like a measured zero.
				{
					Name:         "workspaces/1/qualityExecutions/1",
					Outcome:      v1pb.QualityStatus_QUALITY_STATUS_ERROR,
					ObservedKind: v1pb.QualityObservedKind_QUALITY_OBSERVED_KIND_COUNT,
				},
				// NOT_EVALUATED has neither a value nor a unit.
				{
					Name:    "workspaces/1/qualityExecutions/2",
					Outcome: v1pb.QualityStatus_QUALITY_STATUS_NOT_EVALUATED,
				},
				// A judged zero is a real observation and must travel.
				{
					Name:          "workspaces/1/qualityExecutions/3",
					Outcome:       v1pb.QualityStatus_QUALITY_STATUS_PASS,
					ObservedValue: 0,
				},
			}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "list_quality_executions", map[string]any{})
	require.NoError(t, err)

	rows := listOf(t, asMap(t, value), "executions")
	require.Len(t, rows, 3)
	errorRow := asObject(t, rows[0])
	require.NotContains(t, errorRow, "observedValue", "an ERROR row has no measurement")
	require.Equal(t, "QUALITY_OBSERVED_KIND_COUNT", errorRow["observedKind"])
	notEvaluatedRow := asObject(t, rows[1])
	require.NotContains(t, notEvaluatedRow, "observedValue")
	passRow := asObject(t, rows[2])
	require.Contains(t, passRow, "observedValue")
	require.Equal(t, float64(0), passRow["observedValue"], "a judged zero is a real observation")
}

// TestGetQualityTargetReportsAnAllPausedTarget keeps a paused target's last
// verdict from reading as a current health claim, the way get_quality_signal does.
func TestGetQualityTargetReportsAnAllPausedTarget(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		getQualityTarget: func(*v1pb.GetQualityTargetRequest) (*v1pb.GetQualityTargetResponse, error) {
			return &v1pb.GetQualityTargetResponse{
				Summary: &v1pb.QualityTargetSummary{
					Target: &v1pb.QualityTarget{
						MetaGuid: "1;shop;;orders",
						MetaType: v1pb.MetaType_TABLE,
						Path:     &v1pb.QualityTargetPath{Instance: "prod-mysql", Database: "shop", Object: "orders"},
					},
					RuleCount:       2,
					PausedRuleCount: 2,
					WorstStatus:     v1pb.QualityStatus_QUALITY_STATUS_FAIL,
				},
				QualityRules: []*v1pb.QualityRule{{
					Name:  "workspaces/1/qualityRules/9",
					State: v1pb.QualityRuleState_QUALITY_RULE_STATE_PAUSED,
				}},
			}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_target", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"meta_type": "TABLE",
	})
	require.NoError(t, err)

	payload := asMap(t, value)
	require.Equal(t, true, payload["monitored"], "a paused check is still a defined check")
	require.Equal(t, true, payload["allPaused"])
	summary := asObject(t, payload["summary"])
	require.NotContains(t, summary, "worstStatus", "a paused check's last verdict is history")
	// protoJSON omits a zero scalar, so "no enabled check" is an absent field.
	require.NotContains(t, summary, "enabledRuleCount")
}

// TestListQualityTargetsMarksAnAllPausedRow is the directory half of the same
// rule: the flag says why worstStatus is absent.
func TestListQualityTargetsMarksAnAllPausedRow(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		listQualityTargets: func(*v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error) {
			return &v1pb.ListQualityTargetsResponse{QualityTargets: []*v1pb.QualityTargetSummary{
				{
					Target:          &v1pb.QualityTarget{MetaGuid: "1;shop;;paused", MetaType: v1pb.MetaType_TABLE},
					RuleCount:       2,
					PausedRuleCount: 2,
					WorstStatus:     v1pb.QualityStatus_QUALITY_STATUS_FAIL,
				},
				{
					Target:           &v1pb.QualityTarget{MetaGuid: "1;shop;;watched", MetaType: v1pb.MetaType_TABLE},
					RuleCount:        1,
					EnabledRuleCount: 1,
					WorstStatus:      v1pb.QualityStatus_QUALITY_STATUS_WARN,
				},
			}}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "list_quality_targets", map[string]any{})
	require.NoError(t, err)

	rows := listOf(t, asMap(t, value), "targets")
	require.Len(t, rows, 2)
	paused := asObject(t, rows[0])
	require.Equal(t, true, paused["allPaused"])
	require.NotContains(t, paused, "worstStatus", "a paused target's last verdict is history")
	watched := asObject(t, rows[1])
	require.Equal(t, false, watched["allPaused"])
	require.Equal(t, "QUALITY_STATUS_WARN", watched["worstStatus"])
}

// TestQualityListingRefusesAnUnwatchableObject covers the listing filters: a
// schema or column has no checks, so an empty page would be indistinguishable
// from "nothing is wrong". A bare guid still passes, because it carries no type.
func TestQualityListingRefusesAnUnwatchableObject(t *testing.T) {
	t.Parallel()

	columnReaders := func() *fakeReaders {
		return &fakeReaders{
			listInstances: oneInstance(),
			listDatabases: oneDatabase(),
			searchMetadata: func(*v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
				return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
					foundObject(storedColumn("1;shop;;orders;amount", "amount"), v1pb.MetaType_COLUMN),
				}}, nil
			},
		}
	}
	object := map[string]string{"instance": "prod-mysql", "database": "shop", "name": "amount"}
	for _, tool := range []string{"list_quality_issues", "list_quality_executions"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()
			server := newTestServer(t, columnReaders(), &fakeChecker{allow: true})
			_, err := callTool(t, server, tool, map[string]any{"object": object})
			failure := failureOf(t, err)
			require.Equal(t, codeInvalidArgument, failure.Code)
			require.NotEmpty(t, failure.Hint)
			require.Contains(t, failure.Message, "table")
			require.Contains(t, failure.Message, "view")
		})
	}

	t.Run("a bare guid still passes", func(t *testing.T) {
		t.Parallel()
		// No resolution reader is configured: a guid must not trigger one.
		var seen *v1pb.ListQualityIssuesRequest
		readers := &fakeReaders{
			listQualityIssues: func(request *v1pb.ListQualityIssuesRequest) (*v1pb.ListQualityIssuesResponse, error) {
				seen = request
				return &v1pb.ListQualityIssuesResponse{}, nil
			},
		}
		server := newTestServer(t, readers, &fakeChecker{allow: true})
		value, err := callTool(t, server, "list_quality_issues", map[string]any{
			"object": map[string]string{"guid": "1;shop;;orders"},
		})
		require.NoError(t, err)
		require.Equal(t, "1;shop;;orders", seen.GetTargetGuid())
		require.Empty(t, listOf(t, asMap(t, value), "issues"))
	})
}

// TestListQualityTargetsTreatsFalseHasOpenIssueAsNoFilter pins that the boolean
// cannot ask for targets without issues: false is the proto default, no filter.
func TestListQualityTargetsTreatsFalseHasOpenIssueAsNoFilter(t *testing.T) {
	t.Parallel()

	var seen *v1pb.ListQualityTargetsRequest
	readers := &fakeReaders{
		listQualityTargets: func(request *v1pb.ListQualityTargetsRequest) (*v1pb.ListQualityTargetsResponse, error) {
			seen = request
			return &v1pb.ListQualityTargetsResponse{}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "list_quality_targets", map[string]any{"has_open_issue": false})
	require.NoError(t, err)
	require.False(t, seen.GetHasOpenIssue())
	require.Empty(t, seen.GetSearch())
	require.Empty(t, listOf(t, asMap(t, value), "targets"))
}

// TestQualityListingPassesAResolvedNamePathTargetGUID covers the resolver branch
// the listings take when the object is addressed by name rather than by guid.
func TestQualityListingPassesAResolvedNamePathTargetGUID(t *testing.T) {
	t.Parallel()

	tableSearch := func(*v1pb.SearchMetadataRequest) (*v1pb.SearchMetadataResponse, error) {
		return &v1pb.SearchMetadataResponse{Results: []*v1pb.SearchMetadataResult{
			foundTable(storedTable("1;shop;;orders", "orders")),
		}}, nil
	}
	object := map[string]string{"instance": "prod-mysql", "database": "shop", "name": "orders"}

	t.Run("issues", func(t *testing.T) {
		t.Parallel()
		var seen *v1pb.ListQualityIssuesRequest
		readers := &fakeReaders{
			listInstances:  oneInstance(),
			listDatabases:  oneDatabase(),
			searchMetadata: tableSearch,
			listQualityIssues: func(request *v1pb.ListQualityIssuesRequest) (*v1pb.ListQualityIssuesResponse, error) {
				seen = request
				return &v1pb.ListQualityIssuesResponse{}, nil
			},
		}
		server := newTestServer(t, readers, &fakeChecker{allow: true})
		_, err := callTool(t, server, "list_quality_issues", map[string]any{"object": object})
		require.NoError(t, err)
		require.Equal(t, "1;shop;;orders", seen.GetTargetGuid())
	})

	t.Run("executions", func(t *testing.T) {
		t.Parallel()
		var seen *v1pb.ListQualityExecutionsRequest
		readers := &fakeReaders{
			listInstances:  oneInstance(),
			listDatabases:  oneDatabase(),
			searchMetadata: tableSearch,
			listQualityExecutions: func(request *v1pb.ListQualityExecutionsRequest) (*v1pb.ListQualityExecutionsResponse, error) {
				seen = request
				return &v1pb.ListQualityExecutionsResponse{}, nil
			},
		}
		server := newTestServer(t, readers, &fakeChecker{allow: true})
		_, err := callTool(t, server, "list_quality_executions", map[string]any{"object": object})
		require.NoError(t, err)
		require.Equal(t, "1;shop;;orders", seen.GetTargetGuid())
	})
}

// TestGetQualityTargetCarriesAnOrphanedReason covers the readable-but-orphaned
// state: the RPC answers 200 with the reason and the rules, which the tool keeps.
func TestGetQualityTargetCarriesAnOrphanedReason(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		getQualityTarget: func(*v1pb.GetQualityTargetRequest) (*v1pb.GetQualityTargetResponse, error) {
			return &v1pb.GetQualityTargetResponse{
				Summary: &v1pb.QualityTargetSummary{
					Target:           &v1pb.QualityTarget{MetaGuid: "1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
					RuleCount:        1,
					EnabledRuleCount: 1,
				},
				QualityRules:   []*v1pb.QualityRule{{Name: "workspaces/1/qualityRules/9"}},
				OrphanedReason: "the target table no longer resolves",
			}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_target", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"meta_type": "TABLE",
	})
	require.NoError(t, err)

	payload := asMap(t, value)
	require.Equal(t, "the target table no longer resolves", payload["orphanedReason"])
	require.Equal(t, true, payload["monitored"])
	require.Len(t, listOf(t, payload, "rules"), 1)
}

// TestGetQualityTargetMapsANotFound covers an unreadable target: the handler's
// NotFound is the tool's not_found, not an empty workspace.
func TestGetQualityTargetMapsANotFound(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{
		getQualityTarget: func(*v1pb.GetQualityTargetRequest) (*v1pb.GetQualityTargetResponse, error) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("no quality rules and no registry row"))
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	_, err := callTool(t, server, "get_quality_target", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;orders"},
		"meta_type": "TABLE",
	})
	require.Equal(t, codeNotFound, failureOf(t, err).Code)
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

// The MCP endpoint is a remote entry point a model drives: every call reads the
// registry and writes a permanent row. A principal's calls are budgeted, and a
// call refused by the budget is a tool failure like any other, so it is recorded
// too — as is an unauthenticated one, which has no principal to count.
func TestDispatchBudgetsToolCallsPerPrincipal(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{listInstances: oneInstance()}, &fakeChecker{allow: true})
	limiter := &fakeCallLimiter{limit: 1}
	server.config.CallLimiter = limiter
	definition := toolByName(t, server, "list_instances")

	rows := 0
	server.audit = func(context.Context, *mcpsdk.CallToolRequest, toolDefinition, *store.UserMessage, error, time.Time) {
		rows++
	}

	result, err := server.dispatch(context.Background(), requestFor(t, testUser(), definition.Name, `{"page_size": 1}`), definition)
	require.NoError(t, err)
	require.False(t, result.IsError)

	_, err = server.dispatch(context.Background(), requestFor(t, testUser(), definition.Name, `{}`), definition)
	require.Equal(t, codeResourceExhausted, failureOf(t, err).Code)
	require.Equal(t, []string{"user:7", "user:7"}, limiter.keys)
	require.Equal(t, 2, rows, "a call the budget refused is still recorded")

	_, err = server.dispatch(context.Background(),
		&mcpsdk.CallToolRequest{Params: &mcpsdk.CallToolParamsRaw{Name: definition.Name, Arguments: json.RawMessage(`{}`)}},
		definition)
	require.Equal(t, codeUnauthenticated, failureOf(t, err).Code)
	require.Len(t, limiter.keys, 2, "an unauthenticated call has no principal to count against")
}

// TestDispatchAuditsARefusedCall is the invariant the refusal paths used to skip:
// a call the caller is not allowed to make is the one most worth recording, so the
// row is written before the refusal is returned.
func TestDispatchAuditsARefusedCall(t *testing.T) {
	t.Parallel()

	denied := &fakeChecker{allow: false}
	server := newTestServer(t, &fakeReaders{listInstances: oneInstance()}, denied)
	definition := toolByName(t, server, "list_instances")

	type recorded struct {
		failure error
		user    *store.UserMessage
	}
	var rows []recorded
	server.audit = func(_ context.Context, _ *mcpsdk.CallToolRequest, _ toolDefinition, user *store.UserMessage, failure error, _ time.Time) {
		rows = append(rows, recorded{failure: failure, user: user})
	}

	_, err := server.dispatch(context.Background(), requestFor(t, testUser(), definition.Name, `{}`), definition)
	require.Equal(t, codePermissionDenied, failureOf(t, err).Code)

	require.Len(t, rows, 1, "a refused call must leave a row")
	require.Equal(t, codePermissionDenied, failureOf(t, rows[0].failure).Code)
	require.Equal(t, 7, rows[0].user.ID, "the row names who was refused")

	// A call refused before the identity was resolved still leaves a row, with no
	// actor: the endpoint was reached, and that is the fact worth keeping.
	rows = nil
	result, err := server.dispatch(context.Background(),
		&mcpsdk.CallToolRequest{Params: &mcpsdk.CallToolParamsRaw{Name: definition.Name, Arguments: json.RawMessage(`{}`)}},
		definition)
	require.Nil(t, result)
	require.Equal(t, codeUnauthenticated, failureOf(t, err).Code)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0].user)
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

// fakeCallLimiter spends a fixed budget and remembers who was counted.
type fakeCallLimiter struct {
	limit int
	keys  []string
}

func (l *fakeCallLimiter) Allow(key string, _ time.Time) bool {
	l.keys = append(l.keys, key)
	return len(l.keys) <= l.limit
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
		connect.CodeNotFound:           codeNotFound,
		connect.CodePermissionDenied:   codePermissionDenied,
		connect.CodeInvalidArgument:    codeInvalidArgument,
		connect.CodeFailedPrecondition: codeUnsupported,
		connect.CodeDeadlineExceeded:   codeTimeout,
		connect.CodeUnknown:            codeInternal,
	}
	for code, want := range cases {
		require.Equal(t, want, toolErrorFromRPC(connect.NewError(code, errors.New("x"))).Code, "code %v", code)
	}
	require.Equal(t, codeUnsupported, toolErrorFromRPC(connect.NewError(connect.CodeFailedPrecondition, errors.New("no analyzer"))).Code,
		"a capability the deployment lacks is not a bad argument")
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

func TestGetQualityImpactPassesTheWalkThrough(t *testing.T) {
	t.Parallel()

	var seen *v1pb.GetQualityTargetImpactRequest
	readers := &fakeReaders{
		getQualityImpact: func(request *v1pb.GetQualityTargetImpactRequest) (*v1pb.GetQualityTargetImpactResponse, error) {
			seen = request
			return &v1pb.GetQualityTargetImpactResponse{
				DownstreamAssets: []*v1pb.QualityImpactAsset{{
					Target:   &v1pb.QualityTarget{MetaGuid: "1;shop;;order_summary", MetaType: v1pb.MetaType_VIEW},
					Distance: 1,
				}},
				RootCauses: []*v1pb.QualityRootCauseCandidate{{
					Target:                &v1pb.QualityTarget{MetaGuid: "1;shop;;raw_orders", MetaType: v1pb.MetaType_TABLE},
					Distance:              -1,
					FailingRuleCount:      2,
					FailingExecutionCount: 3,
					RecentFailingExecutions: []string{
						"workspaces/1/qualityExecutions/7",
					},
				}},
				DepthReached: 1,
				WindowStart:  timestamppb.New(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
				WindowEnd:    timestamppb.New(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)),
			}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_impact", map[string]any{
		"object":       map[string]string{"guid": "1;shop;;orders"},
		"meta_type":    "TABLE",
		"depth":        2,
		"window_hours": 48,
	})
	require.NoError(t, err)

	require.Equal(t, "workspaces/-", seen.GetParent())
	require.Equal(t, "1;shop;;orders", seen.GetTarget().GetTargetGuid())
	require.Equal(t, v1pb.MetaType_TABLE, seen.GetTarget().GetMetaType())
	require.Equal(t, int32(2), seen.GetDepth())
	require.Equal(t, int32(48), seen.GetWindowHours())

	payload := asMap(t, value)
	require.Equal(t, float64(1), payload["depthReached"])
	require.Equal(t, "2026-11-01T00:00:00Z", payload["windowStart"])
	asset := firstOf(t, payload, "downstreamAssets")
	require.Equal(t, float64(1), asset["distance"])
	require.Equal(t, "1;shop;;order_summary", asObject(t, asset["target"])["metaGuid"])
	cause := firstOf(t, payload, "rootCauses")
	require.Equal(t, float64(-1), cause["distance"], "an upstream candidate keeps its negative distance")
	require.Equal(t, float64(2), cause["failingRuleCount"])
	require.Equal(t, float64(3), cause["failingExecutionCount"])
	require.Equal(t, []any{"workspaces/1/qualityExecutions/7"}, cause["recentFailingExecutions"])
}

// Impact is a quality read of a table or view; a column or a reported statement has
// no assets to answer with, and the tool says so before reaching the RPC.
func TestGetQualityImpactRefusesAnObjectWithoutAssets(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeReaders{}, &fakeChecker{allow: true})
	_, err := callTool(t, server, "get_quality_impact", map[string]any{
		"object":    map[string]string{"guid": "1;shop;;amount"},
		"meta_type": "COLUMN",
	})
	require.Equal(t, codeInvalidArgument, failureOf(t, err).Code)
}

func TestGetQualityConsumptionCoverageProjectsCounts(t *testing.T) {
	t.Parallel()

	var seen *v1pb.GetQualityConsumptionCoverageRequest
	readers := &fakeReaders{
		getQualityCoverage: func(request *v1pb.GetQualityConsumptionCoverageRequest) (*v1pb.GetQualityConsumptionCoverageResponse, error) {
			seen = request
			return &v1pb.GetQualityConsumptionCoverageResponse{
				ConsumedAssetCount:  7,
				MonitoredAssetCount: 5,
				GapCount:            2,
				Gaps: []*v1pb.QualityConsumptionGap{{
					Target:               &v1pb.QualityTarget{MetaGuid: "1;shop;;orders", MetaType: v1pb.MetaType_TABLE},
					DownstreamAssetCount: 3,
				}},
			}, nil
		},
	}
	server := newTestServer(t, readers, &fakeChecker{allow: true})
	value, err := callTool(t, server, "get_quality_consumption_coverage", map[string]any{
		"page_size": 5,
	})
	require.NoError(t, err)

	require.Equal(t, "workspaces/-", seen.GetParent())
	require.Equal(t, int32(5), seen.GetPageSize())

	payload := asMap(t, value)
	require.Equal(t, float64(7), payload["consumedAssetCount"])
	require.Equal(t, float64(5), payload["monitoredAssetCount"])
	require.Equal(t, float64(2), payload["gapCount"])
	gap := firstOf(t, payload, "gaps")
	require.Equal(t, "1;shop;;orders", asObject(t, gap["target"])["metaGuid"])
	require.Equal(t, float64(3), gap["downstreamAssetCount"])
}
