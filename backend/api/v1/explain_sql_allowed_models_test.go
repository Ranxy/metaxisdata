package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
)

// TestProviderModelRefs covers the conversion the RPC applies before it can reach
// the provider: the stored allowlist becomes the component's exact pair, and a
// model name with a slash stays one string rather than being split into a path.
func TestProviderModelRefs(t *testing.T) {
	t.Parallel()

	refs := providerModelRefs([]*storepb.LlmProviderModelRef{
		{ProviderProfile: "llm-provider-profiles/main", Model: "fast"},
		nil,
		{ProviderProfile: " llm-provider-profiles/other ", Model: " anthropic/claude-sonnet "},
	})
	require.Equal(t, []llm.ModelRef{
		{Profile: "llm-provider-profiles/main", Model: "fast"},
		{Profile: "llm-provider-profiles/other", Model: "anthropic/claude-sonnet"},
	}, refs)

	require.Empty(t, providerModelRefs(nil))
}
