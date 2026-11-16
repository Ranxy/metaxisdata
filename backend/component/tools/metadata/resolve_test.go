package metadata

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
)

// fakeReaders answers the three read calls the resolver uses, and fails a test
// that reaches a call it did not set up.
type fakeReaders struct {
	t *testing.T

	instances []*v1pb.Instance
	databases []*v1pb.Database
	results   []*v1pb.SearchMetadataResult
	schemas   []*v1pb.StoredMetadata
}

func (f *fakeReaders) ListInstances(context.Context, *connect.Request[v1pb.ListInstancesRequest]) (*connect.Response[v1pb.ListInstancesResponse], error) {
	return connect.NewResponse(&v1pb.ListInstancesResponse{Instances: f.instances}), nil
}

func (f *fakeReaders) ListDatabases(context.Context, *connect.Request[v1pb.ListDatabasesRequest]) (*connect.Response[v1pb.ListDatabasesResponse], error) {
	return connect.NewResponse(&v1pb.ListDatabasesResponse{Databases: f.databases}), nil
}

func (f *fakeReaders) ListMetadata(context.Context, *connect.Request[v1pb.ListMetadataRequest]) (*connect.Response[v1pb.MetadataResponse], error) {
	return connect.NewResponse(&v1pb.MetadataResponse{
		TypesStoredMetadata: []*v1pb.MetadataResponse_Metadata{
			{MetaType: v1pb.MetaType_SCHEMA, List: f.schemas},
		},
	}), nil
}

func (f *fakeReaders) SearchMetadata(context.Context, *connect.Request[v1pb.SearchMetadataRequest]) (*connect.Response[v1pb.SearchMetadataResponse], error) {
	return connect.NewResponse(&v1pb.SearchMetadataResponse{Results: f.results}), nil
}

func (*fakeReaders) GetMetadata(context.Context, *connect.Request[v1pb.GetMetadataRequest]) (*connect.Response[v1pb.GetMetadataResponse], error) {
	return connect.NewResponse(&v1pb.GetMetadataResponse{}), nil
}

func (*fakeReaders) GetSchemaString(context.Context, *connect.Request[v1pb.GetSchemaStringRequest]) (*connect.Response[v1pb.MetadataSchemaString], error) {
	return connect.NewResponse(&v1pb.MetadataSchemaString{}), nil
}

func TestResolveObjectPassesAGUIDThrough(t *testing.T) {
	t.Parallel()

	// No readers are configured: a GUID reference must not read anything.
	resolver := NewResolver(&fakeReaders{t: t}, &fakeReaders{t: t})
	resolved, err := resolver.ResolveObject(context.Background(), ObjectRef{GUID: "1;shop;orders"}, v1pb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, "1;shop;orders", resolved.GUID)
	require.Equal(t, v1pb.MetaType_TABLE, resolved.MetaType)
}

func TestResolveObjectNeedsANameAndAScope(t *testing.T) {
	t.Parallel()

	resolver := NewResolver(&fakeReaders{t: t}, &fakeReaders{t: t})
	_, err := resolver.ResolveObject(context.Background(), ObjectRef{Instance: "prod"}, v1pb.MetaType_TABLE)
	require.Error(t, err)
	failure := AsError(err)
	require.NotNil(t, failure)
	require.Equal(t, CodeInvalidArgument, failure.Code)

	_, err = resolver.ResolveObject(context.Background(), ObjectRef{Instance: "prod", Database: "shop"}, v1pb.MetaType_TABLE)
	require.Equal(t, CodeInvalidArgument, AsError(err).Code)
}

func TestResolveInstanceMatchesATitleAndReportsCandidates(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{t: t, instances: []*v1pb.Instance{
		{Name: "instances/1", Title: "prod-mysql"},
		{Name: "instances/2", Title: "staging-pg"},
	}}
	resolver := NewResolver(readers, readers)

	name, err := resolver.ResolveInstance(context.Background(), "prod-mysql")
	require.NoError(t, err)
	require.Equal(t, "instances/1", name)

	_, err = resolver.ResolveInstance(context.Background(), "missing")
	failure := AsError(err)
	require.NotNil(t, failure)
	require.Equal(t, CodeNotFound, failure.Code)
	details, ok := failure.Details.(map[string]any)
	require.True(t, ok)
	require.Len(t, details["candidates"], 2)
}

func TestResolveDatabaseRejectsAGUIDFromAnotherInstance(t *testing.T) {
	t.Parallel()

	resolver := NewResolver(&fakeReaders{t: t}, &fakeReaders{t: t})
	_, err := resolver.ResolveDatabase(context.Background(), "instances/1", "2;shop")
	require.Equal(t, CodeInvalidArgument, AsError(err).Code)
}

func TestSearchObjectRequiresAnExactChildOfTheParent(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{t: t, results: []*v1pb.SearchMetadataResult{
		{Guid: "1;shop;orders", MetaType: v1pb.MetaType_TABLE, Metadata: &v1pb.StoredMetadata{
			Type: &v1pb.StoredMetadata_TableMetadata{TableMetadata: &v1pb.TableMetadata{Name: "orders"}},
		}},
		{Guid: "1;shop_archive;orders", MetaType: v1pb.MetaType_TABLE, Metadata: &v1pb.StoredMetadata{
			Type: &v1pb.StoredMetadata_TableMetadata{TableMetadata: &v1pb.TableMetadata{Name: "orders"}},
		}},
	}}
	resolver := NewResolver(readers, readers)

	resolved, err := resolver.SearchObject(context.Background(), "1;shop", "orders", v1pb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, "1;shop;orders", resolved.GUID)

	_, err = resolver.SearchObject(context.Background(), "1;shop", "invoices", v1pb.MetaType_TABLE)
	require.Equal(t, CodeNotFound, AsError(err).Code)
}

func TestResolveScopeNeedsBothFieldsAndResolvesTheDatabase(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{t: t,
		instances: []*v1pb.Instance{{Name: "instances/1", Title: "prod"}},
		databases: []*v1pb.Database{{Name: "instances/1/databases/shop", Guid: "1;shop"}},
	}
	resolver := NewResolver(readers, readers)

	_, err := resolver.ResolveScope(context.Background(), AnalysisScope{Instance: "prod"})
	require.Equal(t, CodeInvalidArgument, AsError(err).Code)

	guid, err := resolver.ResolveScope(context.Background(), AnalysisScope{Instance: "prod", Database: "shop"})
	require.NoError(t, err)
	require.Equal(t, "1;shop", guid)
}

func TestScopeRequiredCarriesTheAvailableDatabases(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{t: t,
		instances: []*v1pb.Instance{{Name: "instances/1", Title: "prod"}},
		databases: []*v1pb.Database{{Name: "instances/1/databases/shop", Guid: "1;shop"}},
	}
	resolver := NewResolver(readers, readers)

	failure := resolver.ScopeRequired(context.Background())
	require.Equal(t, CodeScopeRequired, failure.Code)
	details, ok := failure.Details.(map[string]any)
	require.True(t, ok)
	require.Len(t, details["databases"], 1)
}

// Every name-path refusal carries the hint that tells the caller how to fix the
// call; the tool envelope would be incomplete without it.
func TestNamePathRefusalsCarryTheirHint(t *testing.T) {
	t.Parallel()

	resolver := NewResolver(&fakeReaders{t: t}, &fakeReaders{t: t})
	cases := []struct {
		name string
		call func() error
		hint string
	}{
		{
			name: "object name",
			call: func() error {
				_, err := resolver.ResolveObject(context.Background(), ObjectRef{Instance: "prod", Database: "shop"}, v1pb.MetaType_TABLE)
				return err
			},
			hint: "address the object as {instance, database, schema?, name}, or pass a guid from an earlier result",
		},
		{
			name: "object scope",
			call: func() error {
				_, err := resolver.ResolveObject(context.Background(), ObjectRef{Name: "orders"}, v1pb.MetaType_TABLE)
				return err
			},
			hint: "an object's name is unique only inside one database",
		},
		{
			name: "container",
			call: func() error {
				_, err := resolver.ResolveContainer(context.Background(), ObjectRef{})
				return err
			},
			hint: "address it as {instance, database, schema?}, or pass a guid from an earlier result",
		},
		{
			name: "scope",
			call: func() error {
				_, err := resolver.ResolveScope(context.Background(), AnalysisScope{Instance: "prod"})
				return err
			},
			hint: "a statement's unqualified names cannot say which database they belong to",
		},
		{
			name: "cross-instance guid",
			call: func() error {
				_, err := resolver.ResolveDatabase(context.Background(), "instances/1", "2;shop")
				return err
			},
			hint: "pass a database name, or a guid from list_databases of the instance you named",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			failure := AsError(testCase.call())
			require.NotNil(t, failure)
			require.Equal(t, testCase.hint, failure.Hint)
		})
	}
}

func TestNormalizePageSizeClampsTheListingBudget(t *testing.T) {
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
	require.Equal(t, CodeInvalidArgument, AsError(err).Code)
}

func TestParseMetaTypeReadsNamesAndRejectsNonsense(t *testing.T) {
	t.Parallel()

	metaType, err := parseMetaType("table")
	require.NoError(t, err)
	require.Equal(t, v1pb.MetaType_TABLE, metaType)

	_, err = parseMetaType("nonsense")
	require.Equal(t, CodeInvalidArgument, AsError(err).Code)
}

func TestFromRPCMapsTheConnectCodes(t *testing.T) {
	t.Parallel()

	cases := map[connect.Code]string{
		connect.CodeNotFound:           CodeNotFound,
		connect.CodePermissionDenied:   CodePermissionDenied,
		connect.CodeInvalidArgument:    CodeInvalidArgument,
		connect.CodeFailedPrecondition: CodeUnsupported,
		connect.CodeDeadlineExceeded:   CodeTimeout,
		connect.CodeUnknown:            CodeInternal,
	}
	for code, want := range cases {
		failure := FromRPC(connect.NewError(code, errors.New("x")))
		require.Equal(t, want, failure.Code, "code %v", code)
	}
	require.Nil(t, FromRPC(nil))
}

func TestResolveObjectResolvesANamePath(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{t: t,
		instances: []*v1pb.Instance{{Name: "instances/1", Title: "prod"}},
		databases: []*v1pb.Database{{Name: "instances/1/databases/shop", Guid: "1;shop"}},
		results: []*v1pb.SearchMetadataResult{{
			Guid:     "1;shop;;orders",
			MetaType: v1pb.MetaType_TABLE,
			Metadata: &v1pb.StoredMetadata{Type: &v1pb.StoredMetadata_TableMetadata{
				TableMetadata: &v1pb.TableMetadata{Name: "orders"},
			}},
		}},
	}
	resolver := NewResolver(readers, readers)

	resolved, err := resolver.ResolveObject(context.Background(),
		ObjectRef{Instance: "prod", Database: "shop", Name: "orders"}, v1pb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, "1;shop;;orders", resolved.GUID)
	require.Equal(t, "orders", resolved.Name)
}

func TestSearchObjectReportsAmbiguityWithCandidates(t *testing.T) {
	t.Parallel()

	object := func(guid string) *v1pb.SearchMetadataResult {
		return &v1pb.SearchMetadataResult{
			Guid:     guid,
			MetaType: v1pb.MetaType_TABLE,
			Metadata: &v1pb.StoredMetadata{Type: &v1pb.StoredMetadata_TableMetadata{
				TableMetadata: &v1pb.TableMetadata{Name: "orders"},
			}},
		}
	}
	readers := &fakeReaders{t: t, results: []*v1pb.SearchMetadataResult{
		object("1;shop;;orders_a"),
		object("1;shop;;orders_b"),
	}}
	resolver := NewResolver(readers, readers)

	_, err := resolver.SearchObject(context.Background(), "1;shop", "orders", v1pb.MetaType_TABLE)
	failure := AsError(err)
	require.NotNil(t, failure)
	require.Equal(t, CodeAmbiguous, failure.Code)
	details, ok := failure.Details.(map[string]any)
	require.True(t, ok)
	require.Len(t, details["candidates"], 2)
}

func TestResolveObjectResolvesASchemaSegment(t *testing.T) {
	t.Parallel()

	readers := &fakeReaders{t: t,
		instances: []*v1pb.Instance{{Name: "instances/1", Title: "prod"}},
		databases: []*v1pb.Database{{Name: "instances/1/databases/shop", Guid: "1;shop"}},
		schemas: []*v1pb.StoredMetadata{{
			Guid: "1;shop;;public",
			Type: &v1pb.StoredMetadata_SchemaMetadata{SchemaMetadata: &v1pb.SchemaMetadata{Name: "public"}},
		}},
		results: []*v1pb.SearchMetadataResult{{
			Guid:     "1;shop;;public;orders",
			MetaType: v1pb.MetaType_TABLE,
			Metadata: &v1pb.StoredMetadata{Type: &v1pb.StoredMetadata_TableMetadata{
				TableMetadata: &v1pb.TableMetadata{Name: "orders"},
			}},
		}},
	}
	resolver := NewResolver(readers, readers)

	resolved, err := resolver.ResolveObject(context.Background(),
		ObjectRef{Instance: "prod", Database: "shop", Schema: "public", Name: "orders"}, v1pb.MetaType_TABLE)
	require.NoError(t, err)
	require.Equal(t, "1;shop;;public;orders", resolved.GUID)
}
