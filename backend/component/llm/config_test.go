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

// modelTestConfigs mirrors a profile that enables two models at different prices.
func modelTestConfigs() []ResolvedConfig {
	return []ResolvedConfig{
		{ProfileName: "llm-provider-profiles/main", ModelName: "fast"},
		{ProfileName: "llm-provider-profiles/main", ModelName: "reasoner"},
		{ProfileName: "llm-provider-profiles/other", ModelName: "reasoner"},
	}
}

func TestFindModelConfig(t *testing.T) {
	t.Parallel()

	configs := modelTestConfigs()

	found := FindModelConfig(configs, "llm-provider-profiles/main", "reasoner")
	require.NotNil(t, found)
	require.Equal(t, "reasoner", found.ModelName)
	require.Equal(t, "llm-provider-profiles/main", found.ProfileName)

	// A bare profile id resolves; the model stays exact.
	found = FindModelConfig(configs, "main", "fast")
	require.NotNil(t, found)
	require.Equal(t, "fast", found.ModelName)

	// Both sides are trimmed: a profile may hold the name with padding.
	found = FindModelConfig([]ResolvedConfig{{ProfileName: "llm-provider-profiles/main", ModelName: " spaced "}}, " main ", " spaced ")
	require.NotNil(t, found)
	require.Equal(t, " spaced ", found.ModelName)

	// The same model on another profile is a different config.
	found = FindModelConfig(configs, "other", "reasoner")
	require.NotNil(t, found)
	require.Equal(t, "llm-provider-profiles/other", found.ProfileName)

	require.Nil(t, FindModelConfig(configs, "main", "reasoner-pro"))
	require.Nil(t, FindModelConfig(configs, "main", ""))
	require.Nil(t, FindModelConfig(configs, "", "fast"))
	require.Nil(t, FindModelConfig(nil, "main", "fast"))
}

func TestResolveModelConfig(t *testing.T) {
	t.Parallel()

	configs := modelTestConfigs()

	resolved, err := ResolveModelConfig(configs, nil, "llm-provider-profiles/main", "reasoner")
	require.NoError(t, err)
	require.Equal(t, "reasoner", resolved.ModelName)

	// Both halves are required: there is no "first enabled model" default.
	resolved, err = ResolveModelConfig(configs, nil, "llm-provider-profiles/main", "")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	resolved, err = ResolveModelConfig(configs, nil, "", "reasoner")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// The profile exists but does not enable that model.
	resolved, err = ResolveModelConfig(configs, nil, "llm-provider-profiles/other", "fast")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// The pair exists but the workspace allowlist rejects the profile.
	resolved, err = ResolveModelConfig(configs, []string{"llm-provider-profiles/other"}, "llm-provider-profiles/main", "fast")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// A model name is exact, not a prefix.
	resolved, err = ResolveModelConfig(configs, nil, "llm-provider-profiles/main", "fast-mini")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestNormalizeProfileName(t *testing.T) {
	t.Parallel()

	name, err := NormalizeProfileName("llm-provider-profiles/main")
	require.NoError(t, err)
	require.Equal(t, "llm-provider-profiles/main", name)

	// A bare id is accepted and canonicalized, because older settings may hold one.
	name, err = NormalizeProfileName(" main ")
	require.NoError(t, err)
	require.Equal(t, "llm-provider-profiles/main", name)

	_, err = NormalizeProfileName("")
	require.Error(t, err)

	_, err = NormalizeProfileName("   ")
	require.Error(t, err)

	_, err = NormalizeProfileName("other/main")
	require.Error(t, err)

	_, err = NormalizeProfileName("llm-provider-profiles/")
	require.Error(t, err)
}
