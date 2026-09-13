package llm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// fakeProfileStore serves one page per offset key so the registry's page walk
// can be exercised without PostgreSQL.
type fakeProfileStore struct {
	pages map[int][]*store.LLMProfileMessage
	calls int
}

func (f *fakeProfileStore) ListLLMProfiles(_ context.Context, find *store.FindLLMProfileMessage) ([]*store.LLMProfileMessage, error) {
	f.calls++
	offset := 0
	if find.Offset != nil {
		offset = *find.Offset
	}
	return f.pages[offset], nil
}

func profileWithModel(name, model string, enabled bool) *store.LLMProfileMessage {
	return &store.LLMProfileMessage{
		Metadata: &storepb.LlmProviderProfile{
			Name:  name,
			Title: name,
			Models: []*storepb.LlmProviderModel{
				{Name: model, Enabled: enabled},
			},
		},
	}
}

func TestRegistryListEnabledCachesUntilInvalidated(t *testing.T) {
	t.Parallel()

	fake := &fakeProfileStore{pages: map[int][]*store.LLMProfileMessage{
		0: {profileWithModel("openai", "gpt-4", true)},
	}}
	registry := &Registry{store: fake}

	configs, err := registry.ListEnabled(context.Background())
	require.NoError(t, err)
	require.Len(t, configs, 1)
	require.Equal(t, "openai", configs[0].ProfileName)

	// A second call inside the TTL must not hit the store.
	_, err = registry.ListEnabled(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, fake.calls)

	registry.Invalidate()
	_, err = registry.ListEnabled(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, fake.calls, "Invalidate must force a reload")
}

func TestRegistryListEnabledWalksEveryPage(t *testing.T) {
	t.Parallel()

	fullPage := make([]*store.LLMProfileMessage, 0, registryPageSize)
	for i := 0; i < registryPageSize; i++ {
		fullPage = append(fullPage, profileWithModel("p", "m", true))
	}
	fake := &fakeProfileStore{pages: map[int][]*store.LLMProfileMessage{
		0:                fullPage,
		registryPageSize: {profileWithModel("tail", "m", true)},
	}}
	registry := &Registry{store: fake}

	configs, err := registry.ListEnabled(context.Background())
	require.NoError(t, err)
	require.Len(t, configs, registryPageSize+1, "profiles past the first page must not be dropped")
	require.Equal(t, 2, fake.calls)
}

func TestRegistryListEnabledSkipsDisabledModels(t *testing.T) {
	t.Parallel()

	fake := &fakeProfileStore{pages: map[int][]*store.LLMProfileMessage{
		0: {
			profileWithModel("openai", "gpt-4", true),
			profileWithModel("local", "llama", false),
		},
	}}
	registry := &Registry{store: fake}

	configs, err := registry.ListEnabled(context.Background())
	require.NoError(t, err)
	require.Len(t, configs, 1)
	require.Equal(t, "openai", configs[0].ProfileName)
}

// registryCacheTTL is part of the contract the ExplainSQL cache key relies on;
// keep it in one place and fail loudly if it is ever shortened to zero.
func TestRegistryCacheTTLIsSet(t *testing.T) {
	t.Parallel()

	require.Positive(t, registryCacheTTL)
	require.LessOrEqual(t, registryCacheTTL, time.Minute)
}
