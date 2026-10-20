package llm

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

// configTestConfigs mirrors what the registry resolves: a profile is named after
// its store row, "llm-provider-profiles/{id}", prefix included.
func configTestConfigs() []ResolvedConfig {
	return []ResolvedConfig{
		{ProfileName: "llm-provider-profiles/openai", ModelName: "gpt-4"},
		{ProfileName: "llm-provider-profiles/local", ModelName: "llama"},
	}
}

func TestResolveConfig(t *testing.T) {
	t.Parallel()

	allowed := []string{"llm-provider-profiles/local"}
	configs := configTestConfigs()

	resolved, err := ResolveConfig(configs, allowed, "llm-provider-profiles/local")
	require.NoError(t, err)
	require.Equal(t, "llama", resolved.ModelName)

	// A bare id from an older client resolves to the same profile.
	resolved, err = ResolveConfig(configs, allowed, "local")
	require.NoError(t, err)
	require.Equal(t, "llama", resolved.ModelName)

	// Requested, allowed, but not one of the enabled profiles.
	resolved, err = ResolveConfig(configs, nil, "llm-provider-profiles/gone")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// Requested but outside the workspace allowlist.
	resolved, err = ResolveConfig(configs, []string{"llm-provider-profiles/openai"}, "llm-provider-profiles/local")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// No request: the first allowed profile is the default.
	resolved, err = ResolveConfig(configs, []string{"llm-provider-profiles/local"}, "")
	require.NoError(t, err)
	require.Equal(t, "llama", resolved.ModelName)

	resolved, err = ResolveConfig(configs, nil, "")
	require.NoError(t, err)
	require.Equal(t, "gpt-4", resolved.ModelName)

	// No request and an allowlist that matches nothing.
	resolved, err = ResolveConfig(configs, []string{"llm-provider-profiles/gone"}, "")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func TestFindConfig(t *testing.T) {
	t.Parallel()

	configs := configTestConfigs()

	found := FindConfig(configs, "llm-provider-profiles/local")
	require.NotNil(t, found)
	require.Equal(t, "llama", found.ModelName)

	found = FindConfig(configs, "local")
	require.NotNil(t, found)
	require.Equal(t, "llama", found.ModelName)

	require.Nil(t, FindConfig(configs, "llm-provider-profiles/gone"))
	require.Nil(t, FindConfig(nil, "local"))
}

func TestAllowedConfigs(t *testing.T) {
	t.Parallel()

	configs := configTestConfigs()

	require.Equal(t, configs, AllowedConfigs(configs, nil), "an empty allowlist allows everything")
	require.Equal(t, configs, AllowedConfigs(configs, []string{}), "an empty allowlist allows everything")

	filtered := AllowedConfigs(configs, []string{"llm-provider-profiles/local"})
	require.Len(t, filtered, 1)
	require.Equal(t, "llm-provider-profiles/local", filtered[0].ProfileName)

	require.Empty(t, AllowedConfigs(configs, []string{"llm-provider-profiles/gone"}))

	// The bare id is accepted too, so an older setting value keeps working.
	require.Len(t, AllowedConfigs(configs, []string{"openai"}), 1)
}

func TestProfileAllowed(t *testing.T) {
	t.Parallel()

	require.True(t, ProfileAllowed("anything", nil))
	require.True(t, ProfileAllowed("openai", []string{"llm-provider-profiles/openai"}))
	require.True(t, ProfileAllowed("llm-provider-profiles/openai", []string{"openai"}))
	require.False(t, ProfileAllowed("openai", []string{"llm-provider-profiles/local"}))
}

func TestProfileID(t *testing.T) {
	t.Parallel()

	require.Equal(t, "local", ProfileID("llm-provider-profiles/local"))
	require.Equal(t, "local", ProfileID("  llm-provider-profiles/local  "))
	require.Equal(t, "local", ProfileID("local"))
	require.Empty(t, ProfileID(""))
	// Only the exact resource prefix is stripped; a prefix in a title-like value
	// stays part of the id.
	require.Equal(t, "other/local", ProfileID("other/local"))
}
