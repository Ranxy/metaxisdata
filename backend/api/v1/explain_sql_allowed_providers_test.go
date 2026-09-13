package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
)

func TestFilterAllowedLLMConfigs(t *testing.T) {
	t.Parallel()

	configs := []llm.ResolvedConfig{
		{ProfileName: "openai", ModelName: "gpt-4"},
		{ProfileName: "local", ModelName: "llama"},
	}

	require.Equal(t, configs, filterAllowedLLMConfigs(configs, nil), "an empty allowlist allows everything")

	filtered := filterAllowedLLMConfigs(configs, []string{"llm-provider-profiles/local"})
	require.Len(t, filtered, 1)
	require.Equal(t, "local", filtered[0].ProfileName)

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
