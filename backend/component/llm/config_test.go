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

// modelTestConfigs mirrors a profile that enables two models at different prices.
func modelTestConfigs() []ResolvedConfig {
	return []ResolvedConfig{
		{ProfileName: "llm-provider-profiles/main", ModelName: "fast"},
		{ProfileName: "llm-provider-profiles/main", ModelName: "reasoner"},
		{ProfileName: "llm-provider-profiles/other", ModelName: "reasoner"},
	}
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

func TestAllowedModelConfigs(t *testing.T) {
	t.Parallel()

	configs := modelTestConfigs()

	require.Equal(t, configs, AllowedModelConfigs(configs, nil), "an empty allowlist allows everything")
	require.Equal(t, configs, AllowedModelConfigs(configs, []ModelRef{}), "an empty allowlist allows everything")

	filtered := AllowedModelConfigs(configs, []ModelRef{{Profile: "llm-provider-profiles/main", Model: "fast"}})
	require.Len(t, filtered, 1)
	require.Equal(t, "fast", filtered[0].ModelName)

	// The same model under another profile is a different pair.
	filtered = AllowedModelConfigs(configs, []ModelRef{{Profile: "llm-provider-profiles/other", Model: "reasoner"}})
	require.Len(t, filtered, 1)
	require.Equal(t, "llm-provider-profiles/other", filtered[0].ProfileName)

	// A bare profile id in the allowlist is accepted too.
	filtered = AllowedModelConfigs(configs, []ModelRef{{Profile: "main", Model: "reasoner"}})
	require.Len(t, filtered, 1)
	require.Equal(t, "reasoner", filtered[0].ModelName)

	require.Empty(t, AllowedModelConfigs(configs, []ModelRef{{Profile: "llm-provider-profiles/gone", Model: "fast"}}))

	// A model id may contain a slash; it is compared as one exact string.
	slashConfigs := []ResolvedConfig{{ProfileName: "llm-provider-profiles/openrouter", ModelName: "anthropic/claude-sonnet"}}
	filtered = AllowedModelConfigs(slashConfigs, []ModelRef{{Profile: "llm-provider-profiles/openrouter", Model: "anthropic/claude-sonnet"}})
	require.Len(t, filtered, 1)
}

func TestModelAllowed(t *testing.T) {
	t.Parallel()

	allowed := []ModelRef{
		{Profile: "llm-provider-profiles/main", Model: "fast"},
		{Profile: "llm-provider-profiles/other", Model: "anthropic/claude-sonnet"},
	}

	require.True(t, ModelAllowed("anything", "anything", nil), "an empty allowlist allows everything")
	require.True(t, ModelAllowed("llm-provider-profiles/main", "fast", allowed))
	require.True(t, ModelAllowed("main", "fast", allowed))
	require.True(t, ModelAllowed(" other ", " anthropic/claude-sonnet ", allowed))

	require.False(t, ModelAllowed("llm-provider-profiles/main", "reasoner", allowed))
	require.False(t, ModelAllowed("llm-provider-profiles/other", "fast", allowed))
	require.False(t, ModelAllowed("", "fast", allowed))
	require.False(t, ModelAllowed("main", "", allowed))
}

func TestResolveAllowedModelConfig(t *testing.T) {
	t.Parallel()

	configs := modelTestConfigs()
	allowed := []ModelRef{
		{Profile: "llm-provider-profiles/main", Model: "fast"},
		{Profile: "llm-provider-profiles/other", Model: "reasoner"},
	}

	// A requested pair is used exactly: no inference from the profile.
	resolved, err := ResolveAllowedModelConfig(configs, nil, "llm-provider-profiles/main", "reasoner")
	require.NoError(t, err)
	require.Equal(t, "reasoner", resolved.ModelName)

	// A bare profile id from a client resolves to the same pair.
	resolved, err = ResolveAllowedModelConfig(configs, allowed, "main", "fast")
	require.NoError(t, err)
	require.Equal(t, "fast", resolved.ModelName)

	// Half a pair is invalid: the model is never inferred.
	resolved, err = ResolveAllowedModelConfig(configs, allowed, "llm-provider-profiles/main", "")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	resolved, err = ResolveAllowedModelConfig(configs, allowed, "", "fast")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Requested but outside the workspace allowlist.
	resolved, err = ResolveAllowedModelConfig(configs, allowed, "llm-provider-profiles/main", "reasoner")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// Allowed but not currently enabled.
	resolved, err = ResolveAllowedModelConfig(configs, []ModelRef{{Profile: "llm-provider-profiles/main", Model: "gone"}}, "llm-provider-profiles/main", "gone")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// No request: the first allowed pair is the default.
	resolved, err = ResolveAllowedModelConfig(configs, allowed, "", "")
	require.NoError(t, err)
	require.Equal(t, "fast", resolved.ModelName)

	resolved, err = ResolveAllowedModelConfig(configs, nil, "", "")
	require.NoError(t, err)
	require.Equal(t, "fast", resolved.ModelName)

	// No request and an allowlist that matches nothing enabled.
	resolved, err = ResolveAllowedModelConfig(configs, []ModelRef{{Profile: "llm-provider-profiles/main", Model: "gone"}}, "", "")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

// A feature whose own settings name the pair resolves it without a second,
// workspace-wide restriction: nothing here can reject a pair the registry enables.
func TestResolveAnalysisConfig(t *testing.T) {
	t.Parallel()

	configs := modelTestConfigs()

	resolved, err := ResolveAnalysisConfig(configs, "llm-provider-profiles/main", "reasoner")
	require.NoError(t, err)
	require.Equal(t, "reasoner", resolved.ModelName)

	// Both empty: the first enabled pair is the default.
	resolved, err = ResolveAnalysisConfig(configs, "", "")
	require.NoError(t, err)
	require.Equal(t, "fast", resolved.ModelName)

	// Half a pair is invalid rather than inferred.
	resolved, err = ResolveAnalysisConfig(configs, "llm-provider-profiles/main", "")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	resolved, err = ResolveAnalysisConfig(configs, "", "reasoner")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// A named pair the registry does not enable has no fallback.
	resolved, err = ResolveAnalysisConfig(configs, "llm-provider-profiles/gone", "fast")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// No enabled pair at all.
	resolved, err = ResolveAnalysisConfig(nil, "", "")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
}

func TestResolveModelConfig(t *testing.T) {
	t.Parallel()

	configs := modelTestConfigs()

	resolved, err := ResolveModelConfig(configs, "llm-provider-profiles/main", "reasoner")
	require.NoError(t, err)
	require.Equal(t, "reasoner", resolved.ModelName)

	// Both halves are required: there is no "first enabled model" default.
	resolved, err = ResolveModelConfig(configs, "llm-provider-profiles/main", "")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	resolved, err = ResolveModelConfig(configs, "", "reasoner")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	// The profile exists but does not enable that model.
	resolved, err = ResolveModelConfig(configs, "llm-provider-profiles/other", "fast")
	require.Error(t, err)
	require.Nil(t, resolved)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	// A model name is exact, not a prefix.
	resolved, err = ResolveModelConfig(configs, "llm-provider-profiles/main", "fast-mini")
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
