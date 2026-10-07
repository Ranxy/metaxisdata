package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
)

// enabledConfigs mirrors what the registry resolves: a profile is named after
// its store row, "llm-provider-profiles/{id}", prefix included.
func enabledConfigs() []llm.ResolvedConfig {
	return []llm.ResolvedConfig{
		{ProfileName: "llm-provider-profiles/openai", ModelName: "gpt-4"},
		{ProfileName: "llm-provider-profiles/local", ModelName: "llama"},
	}
}

func TestFindLLMConfig(t *testing.T) {
	t.Parallel()

	configs := enabledConfigs()

	// The request carries the resource name the picker listed, so the config's
	// name must be normalized before comparing.
	found := findLLMConfig(configs, "llm-provider-profiles/local")
	require.NotNil(t, found)
	require.Equal(t, "llama", found.ModelName)

	// A bare id from an older client resolves to the same profile.
	found = findLLMConfig(configs, "local")
	require.NotNil(t, found)
	require.Equal(t, "llama", found.ModelName)

	require.Nil(t, findLLMConfig(configs, "llm-provider-profiles/gone"))
}

func TestFilterAllowedLLMConfigs(t *testing.T) {
	t.Parallel()

	configs := enabledConfigs()

	require.Equal(t, configs, filterAllowedLLMConfigs(configs, nil), "an empty allowlist allows everything")

	filtered := filterAllowedLLMConfigs(configs, []string{"llm-provider-profiles/local"})
	require.Len(t, filtered, 1)
	require.Equal(t, "llm-provider-profiles/local", filtered[0].ProfileName)

	require.Empty(t, filterAllowedLLMConfigs(configs, []string{"llm-provider-profiles/gone"}))

	// The bare id is accepted too, so an older setting value keeps working.
	require.Len(t, filterAllowedLLMConfigs(configs, []string{"openai"}), 1)
}

func TestIsLLMProfileAllowed(t *testing.T) {
	t.Parallel()

	require.True(t, isLLMProfileAllowed("anything", nil))
	require.True(t, isLLMProfileAllowed("openai", []string{"llm-provider-profiles/openai"}))
	require.True(t, isLLMProfileAllowed("llm-provider-profiles/openai", []string{"openai"}))
	require.False(t, isLLMProfileAllowed("openai", []string{"llm-provider-profiles/local"}))
}
