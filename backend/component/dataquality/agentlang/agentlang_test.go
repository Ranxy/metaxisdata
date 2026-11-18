package agentlang

import (
	"testing"

	"github.com/stretchr/testify/require"

	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func TestOf(t *testing.T) {
	t.Parallel()

	require.Equal(t, Chinese, Of(&store.UserMessage{Profile: &storepb.UserProfile{Language: "zh-CN"}}))
	require.Equal(t, English, Of(&store.UserMessage{Profile: &storepb.UserProfile{Language: "en-US"}}))
	require.Equal(t, English, Of(nil))
}

func TestLocalizedAttentionText(t *testing.T) {
	t.Parallel()

	require.Contains(t, AttentionTitle(Chinese, store.QualityAgentAttentionKindProposalPending), "草案")
	require.Contains(t, AttentionTitle(English, store.QualityAgentAttentionKindProposalPending), "drafts")
	require.Contains(t, BlockedQuestion(Chinese, "model failed"), "model failed")
	require.Contains(t, ProposalQuestion(Chinese, 3), "3")
	require.Contains(t, ReportQuestion(English, 2, 1), "2")
	require.Contains(t, ScopeSummary(Chinese, 10, 1, 2, 3, 4, 5), "范围")
}
