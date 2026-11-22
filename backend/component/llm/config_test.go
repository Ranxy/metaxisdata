package llm

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
)

// modelTestConfigs mirrors a profile that enables two models at different prices.
func modelTestConfigs() []ResolvedConfig {
	return []ResolvedConfig{
		{ProfileName: "llm-provider-profiles/main", ModelName: "fast"},
		{ProfileName: "llm-provider-profiles/main", ModelName: "reasoner"},
		{ProfileName: "llm-provider-profiles/other", ModelName: "reasoner"},
	}
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

	// A padded profile and model resolve like their trimmed form.
	resolved, err = ResolveModelConfig(configs, " main ", " fast ")
	require.NoError(t, err)
	require.Equal(t, "fast", resolved.ModelName)

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
