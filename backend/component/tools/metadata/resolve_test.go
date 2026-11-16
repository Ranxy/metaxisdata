package metadata

import (
	"context"
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
