package v1

import (
	"testing"

	"connectrpc.com/connect"
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

// TestResolveLLMConfig covers the branch the RPC takes before it can reach the
// provider: the picker's resource name must resolve, and each rejection has to
// come back with its own code.
func TestResolveLLMConfig(t *testing.T) {
	t.Parallel()

	allowed := []string{"llm-provider-profiles/local"}
	configs := enabledConfigs()

	resolved, err := resolveLLMConfig(configs, allowed, "llm-provider-profiles/local")
	require.NoError(t, err)
	require.Equal(t, "llama", resolved.ModelName)

	// A bare id from an older client resolves to the same profile.
	resolved, err = resolveLLMConfig(configs, allowed, "local")
	require.NoError(t, err)
	require.Equal(t, "llama", resolved.ModelName)

	// Requested, allowed, but not one of the enabled profiles.
	resolved, err = resolveLLMConfig(configs, nil, "llm-provider-profiles/gone")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// Requested but outside the workspace allowlist.
	resolved, err = resolveLLMConfig(configs, []string{"llm-provider-profiles/openai"}, "llm-provider-profiles/local")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// No request: the first allowed profile is the default.
	resolved, err = resolveLLMConfig(configs, []string{"llm-provider-profiles/local"}, "")
	require.NoError(t, err)
	require.Equal(t, "llama", resolved.ModelName)

	resolved, err = resolveLLMConfig(configs, nil, "")
	require.NoError(t, err)
	require.Equal(t, "gpt-4", resolved.ModelName)

	// No request and an allowlist that matches nothing.
	resolved, err = resolveLLMConfig(configs, []string{"llm-provider-profiles/gone"}, "")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
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
