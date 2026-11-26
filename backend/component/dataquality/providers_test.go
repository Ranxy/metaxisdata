package dataquality

import (
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func providerTestConfigs() []llm.ResolvedConfig {
	return []llm.ResolvedConfig{
		{ProfileName: "llm-provider-profiles/main", ModelName: "fast"},
		{ProfileName: "llm-provider-profiles/main", ModelName: "reasoner"},
		{ProfileName: "llm-provider-profiles/other", ModelName: "reasoner"},
	}
}

func providerTestAgent(profile, model string) *store.QualityAgentMessage {
	return &store.QualityAgentMessage{ProviderProfile: profile, ProviderModel: model}
}

// An agent's selection is exact and has no allowlist input: the workspace
// allowlist restricts Explain SQL only, so any profile the registry currently
// enables can be pinned. These cases pin the "no fallback" rule instead.
func TestDecideAgentConfig(t *testing.T) {
	t.Parallel()

	configs := providerTestConfigs()

	t.Run("exact pair", func(t *testing.T) {
		t.Parallel()
		config, err := decideAgentConfig(configs, providerTestAgent("llm-provider-profiles/main", "reasoner"), true)
		require.NoError(t, err)
		require.Equal(t, "reasoner", config.ModelName)
		require.Equal(t, "llm-provider-profiles/main", config.ProfileName)
	})

	t.Run("the workspace analysis switch gates model use", func(t *testing.T) {
		t.Parallel()
		config, err := decideAgentConfig(configs, providerTestAgent("llm-provider-profiles/main", "fast"), false)
		require.Error(t, err)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		require.Empty(t, config)
	})

	t.Run("no agent and no pair have no model", func(t *testing.T) {
		t.Parallel()
		_, err := decideAgentConfig(configs, nil, true)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

		_, err = decideAgentConfig(configs, providerTestAgent("", ""), true)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("a disabled or unknown model is not replaced", func(t *testing.T) {
		t.Parallel()
		// The profile exists and enables another model, but not this one.
		_, err := decideAgentConfig(configs, providerTestAgent("llm-provider-profiles/other", "fast"), true)
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

		// The profile is gone entirely.
		_, err = decideAgentConfig(configs, providerTestAgent("llm-provider-profiles/gone", "fast"), true)
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("no enabled config leaves the agent without a model", func(t *testing.T) {
		t.Parallel()
		// The registry enables nothing, so even a named pair cannot resolve; there is
		// no "first enabled model" fallback for an agent.
		_, err := decideAgentConfig(nil, providerTestAgent("llm-provider-profiles/main", "fast"), true)
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})
}

// A quality agent needs a model that can hold a conversation and compact it: the output
// budget must clear the summary floor, and the compaction budget must leave room for the
// fixed overhead every request carries besides the transcript (the system prompt and the
// tool schemas), which is why the floor is a budget and not merely a positive number.
func TestValidateAgentModel(t *testing.T) {
	t.Parallel()

	valid := llm.ResolvedConfig{ModelName: "m", ContextWindow: 131072, MaxOutputTokens: 8192}
	require.NoError(t, validateAgentModel(valid, "m"))

	smallOutput := llm.ResolvedConfig{ModelName: "m", ContextWindow: 131072, MaxOutputTokens: MinAgentModelOutputTokens - 1}
	err := validateAgentModel(smallOutput, "m")
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

	// 65,536 - 24,576 - 8,192 is exactly the floor: the smallest pair an agent accepts.
	atFloor := llm.ResolvedConfig{ModelName: "m", ContextWindow: 65536, MaxOutputTokens: 24576}
	require.NoError(t, validateAgentModel(atFloor, "m"))

	belowFloor := llm.ResolvedConfig{ModelName: "m", ContextWindow: 65536, MaxOutputTokens: 32768}
	err = validateAgentModel(belowFloor, "m")
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Contains(t, err.Error(), "room to compact")

	noRoom := llm.ResolvedConfig{ModelName: "m", ContextWindow: 32768, MaxOutputTokens: 32767}
	err = validateAgentModel(noRoom, "m")
	require.Error(t, err)
	require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
	require.Contains(t, err.Error(), "compact")
}
