package llm

import (
	"context"
	"testing"

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

func profileWithModel(name, model string) *store.LLMProfileMessage {
	return &store.LLMProfileMessage{
		Metadata: &storepb.LlmProviderProfile{
			Name:  name,
			Title: name,
			Models: []*storepb.LlmProviderModel{
				{Name: model, Enabled: true},
			},
		},
	}
}

// The resolved config carries each model's own capacities: zero resolves to the
// server defaults and an explicit pair is kept.
func TestRegistryResolvesPerModelCapacities(t *testing.T) {
	t.Parallel()

	fake := &fakeProfileStore{pages: map[int][]*store.LLMProfileMessage{
		0: {
			{
				Metadata: &storepb.LlmProviderProfile{
					Name:  "multi",
					Title: "multi",
					Models: []*storepb.LlmProviderModel{
						{Name: "m1", Enabled: true},
						{Name: "m2", Enabled: true, ContextWindow: 131072, MaxOutputTokens: 8192},
						{Name: "m3", Enabled: false, ContextWindow: 65536, MaxOutputTokens: 4096},
					},
				},
			},
		},
	}}
	registry := &Registry{store: fake}

	configs, err := registry.ListEnabled(context.Background())
	require.NoError(t, err)
	require.Len(t, configs, 2)
	require.Equal(t, "m1", configs[0].ModelName)
	require.Equal(t, DefaultContextWindow, configs[0].ContextWindow)
	require.Equal(t, DefaultMaxOutputTokens, configs[0].MaxOutputTokens)
	require.Equal(t, "m2", configs[1].ModelName)
	require.Equal(t, 131072, configs[1].ContextWindow)
	require.Equal(t, 8192, configs[1].MaxOutputTokens)
}

func TestRegistryListEnabledCachesUntilInvalidated(t *testing.T) {
	t.Parallel()

	fake := &fakeProfileStore{pages: map[int][]*store.LLMProfileMessage{
		0: {profileWithModel("openai", "gpt-4")},
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
		fullPage = append(fullPage, profileWithModel("p", "m"))
	}
	fake := &fakeProfileStore{pages: map[int][]*store.LLMProfileMessage{
		0:                fullPage,
		registryPageSize: {profileWithModel("tail", "m")},
	}}
	registry := &Registry{store: fake}

	configs, err := registry.ListEnabled(context.Background())
	require.NoError(t, err)
	require.Len(t, configs, registryPageSize+1, "profiles past the first page must not be dropped")
	require.Equal(t, 2, fake.calls)
}
