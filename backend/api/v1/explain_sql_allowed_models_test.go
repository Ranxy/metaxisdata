package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestProviderModelRefs covers the conversion the RPC applies before the
// component resolves the workspace allowlist: a model name with a slash stays
// one string rather than being split into a path, and a bare profile reference
// is compared through the component's profile id.
func TestProviderModelRefs(t *testing.T) {
	t.Parallel()

	refs := providerModelRefs([]*storepb.LlmProviderModelRef{
		{ProviderProfile: "llm-provider-profiles/main", Model: "fast"},
		nil,
		{ProviderProfile: " llm-provider-profiles/other ", Model: " anthropic/claude-sonnet "},
	})
	require.True(t, llm.ModelAllowed("llm-provider-profiles/main", "fast", refs))
	require.True(t, llm.ModelAllowed("main", "fast", refs))
	require.True(t, llm.ModelAllowed("llm-provider-profiles/other", "anthropic/claude-sonnet", refs),
		"a model name with a slash has to survive as one string")
	require.False(t, llm.ModelAllowed("llm-provider-profiles/main", "anthropic/claude-sonnet", refs))
	require.False(t, llm.ModelAllowed("llm-provider-profiles/other", "fast", refs))

	require.Empty(t, providerModelRefs(nil))
}
