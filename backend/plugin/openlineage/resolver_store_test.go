package openlineage

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeResolutionStore answers the resolver from memory and counts the lookups it
// was asked for. A page resolves thousands of datasets and the lookups behind one
// namespace are queries, so "how many lookups did one resolution pass cost" is the
// invariant these tests pin.
type fakeResolutionStore struct {
	mappings      map[string]*store.NamespaceMappingMessage
	instances     []*store.InstanceMessage
	instancesByID map[string]*store.InstanceMessage

	mappingCalls      int
	instanceCalls     int
	instanceListCalls int
	externalCalls     int
}

func (f *fakeResolutionStore) GetNamespaceMapping(_ context.Context, find *store.FindNamespaceMappingMessage) (*store.NamespaceMappingMessage, error) {
	f.mappingCalls++
	if find.Namespace == nil {
		return nil, nil
	}
	return f.mappings[*find.Namespace], nil
}

func (f *fakeResolutionStore) GetInstance(_ context.Context, find *store.FindInstanceMessage) (*store.InstanceMessage, error) {
	f.instanceCalls++
	if find.ResourceID == nil {
		return nil, nil
	}
	return f.instancesByID[*find.ResourceID], nil
}

func (f *fakeResolutionStore) ListInstances(_ context.Context, _ *store.FindInstanceMessage) ([]*store.InstanceMessage, error) {
	f.instanceListCalls++
	return f.instances, nil
}

func (f *fakeResolutionStore) GetOrCreateExternalDataset(context.Context, string, string, string) (*store.ExternalDatasetMessage, error) {
	f.externalCalls++
	return &store.ExternalDatasetMessage{}, nil
}

func fakeInstance(engine storepb.Engine, resourceID, host, port, database string) *store.InstanceMessage {
	return &store.InstanceMessage{
		ResourceID: resourceID,
		Metadata: &storepb.Instance{
			Engine:      engine,
			DataSources: []*storepb.DataSource{{Host: host, Port: port, Database: database}},
		},
	}
}

// One request resolves every dataset of a page, and the datasets of a page share a
// handful of namespaces: the mapping and the instance list are looked up once per
// namespace, not once per dataset. Without this a page of 5000 rows issued 5000
// queries before it could render.
func TestResolveDatasetPreviewLooksUpOneNamespaceOnce(t *testing.T) {
	t.Parallel()

	instance := fakeInstance(storepb.Engine_POSTGRES, "inst-a", "warehouse", "5432", "app")
	fake := &fakeResolutionStore{
		mappings: map[string]*store.NamespaceMappingMessage{
			"postgres://warehouse:5432/app": {ID: 1, Namespace: "postgres://warehouse:5432/app", InstanceResourceID: "inst-a"},
		},
		instancesByID: map[string]*store.InstanceMessage{"inst-a": instance},
	}
	resolver := NewRequestScopedResolver(fake)

	for index := range 200 {
		datasetName := fmt.Sprintf("public.table_%03d", index)
		resolved, err := resolver.ResolveDatasetPreview(context.Background(), "postgres://warehouse:5432/app", datasetName)
		require.NoError(t, err)
		require.True(t, resolved.Internal)
		require.Equal(t, buildGUID("inst-a", storepb.Engine_POSTGRES, "app", datasetName), resolved.GUID)
	}

	require.Equal(t, 1, fake.mappingCalls, "one namespace costs one mapping lookup")
	require.Equal(t, 1, fake.instanceCalls, "and one instance lookup")
	require.Zero(t, fake.instanceListCalls, "a mapped namespace never lists the instances")

	// A second namespace is a second lookup; the memo is per namespace, not global.
	resolved, err := resolver.ResolveDatasetPreview(context.Background(), "postgres://other:5432/app", "public.table_001")
	require.NoError(t, err)
	require.False(t, resolved.Internal, "a namespace no instance answers stays external")
	require.Equal(t, 2, fake.mappingCalls)
}

// A namespace that names no instance is matched by the host and port it carries,
// and the instance list behind that match is looked up once for the whole page.
func TestResolveDatasetPreviewMatchesInstancesOncePerRequest(t *testing.T) {
	t.Parallel()

	fake := &fakeResolutionStore{
		instances: []*store.InstanceMessage{
			fakeInstance(storepb.Engine_POSTGRES, "inst-b", "warehouse", "5432", "app"),
		},
	}
	resolver := NewRequestScopedResolver(fake)

	for index := range 50 {
		resolved, err := resolver.ResolveDatasetPreview(context.Background(), "postgres://warehouse:5432/app", fmt.Sprintf("public.table_%02d", index))
		require.NoError(t, err)
		require.True(t, resolved.Internal)
	}

	require.Equal(t, 1, fake.mappingCalls)
	require.Equal(t, 1, fake.instanceListCalls)
}

// Ingestion resolves datasets over hours with one resolver, so its answers must
// not be memoized: a namespace mapping or an instance registered in between has to
// take effect on the next event.
func TestResolverOutsideARequestMemoizesNothing(t *testing.T) {
	t.Parallel()

	instance := fakeInstance(storepb.Engine_POSTGRES, "inst-a", "warehouse", "5432", "app")
	fake := &fakeResolutionStore{
		mappings:      map[string]*store.NamespaceMappingMessage{},
		instancesByID: map[string]*store.InstanceMessage{"inst-a": instance},
		instances:     []*store.InstanceMessage{instance},
	}
	resolver := NewResolver(fake, nil)

	for range 3 {
		_, err := resolver.ResolveDatasetPreview(context.Background(), "postgres://warehouse:5432/app", "public.orders")
		require.NoError(t, err)
	}

	// A memoized lookup would have answered the three resolutions with one query;
	// the lookup is repeated so a mapping or instance registered in between is
	// visible to the next event.
	require.Greater(t, fake.mappingCalls, 3, "ingestion must re-read the namespace per resolution")
	require.GreaterOrEqual(t, fake.instanceListCalls, 3)
}
