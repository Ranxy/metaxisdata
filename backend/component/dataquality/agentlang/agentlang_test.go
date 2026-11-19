package agentlang

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestSupportedAndNormalize(t *testing.T) {
	t.Parallel()

	require.True(t, Supported(""))
	require.True(t, Supported("en-US"))
	require.True(t, Supported("zh-CN"))
	require.False(t, Supported("fr-FR"))
	require.Equal(t, Chinese, Normalize("zh-TW"))
	require.Equal(t, English, Normalize("fr-FR"))
	require.Equal(t, "Simplified Chinese (zh-CN)", Name("zh-CN"))
	require.Equal(t, "English (en-US)", Name("en-US"))
}

func TestForAgentPrefersTheExplicitAgentLanguage(t *testing.T) {
	t.Parallel()

	owner := &store.UserMessage{Profile: &storepb.UserProfile{Language: "zh-CN"}}
	require.Equal(t, Chinese, ForAgent(&store.QualityAgentMessage{Language: "zh-CN"}, owner))
	require.Equal(t, English, ForAgent(&store.QualityAgentMessage{Language: "en-US"}, owner))
	require.Equal(t, Chinese, ForAgent(&store.QualityAgentMessage{}, owner))
	require.Equal(t, English, ForAgent(&store.QualityAgentMessage{}, nil))
}
