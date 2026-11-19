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

func TestOf(t *testing.T) {
	t.Parallel()

	require.Equal(t, Chinese, Of(&store.UserMessage{Profile: &storepb.UserProfile{Language: "zh-CN"}}))
	require.Equal(t, English, Of(&store.UserMessage{Profile: &storepb.UserProfile{Language: "en-US"}}))
	require.Equal(t, English, Of(nil))
	require.Equal(t, English, Of(&store.UserMessage{}))
}

func TestLocalizedServerProse(t *testing.T) {
	t.Parallel()

	require.Contains(t, AttentionTitle(Chinese, store.QualityAgentAttentionKindProposalPending), "草案")
	require.Contains(t, AttentionTitle(English, store.QualityAgentAttentionKindProposalPending), "drafts")
	require.Contains(t, BlockedQuestion(Chinese, "model failed"), "model failed")
	require.Contains(t, ProposalQuestion(Chinese, ProposalCounts{Remaining: 3, Adopted: 1, Skipped: 1, Failed: 1, Failure: "boom"}), "3")
	require.Contains(t, ProposalQuestion(Chinese, ProposalCounts{Remaining: 3, Adopted: 1, Skipped: 1, Failed: 1, Failure: "boom"}), "失败")
	require.Contains(t, ProposalQuestion(English, ProposalCounts{Remaining: 3, Adopted: 1, Skipped: 1, Failed: 1, Failure: "boom"}), "failed to write")
	require.Contains(t, ReportQuestion(English, 2, 1), "2")
	require.Contains(t, ScopeSummary(Chinese, 10, 1, 2, 3, 4, 5), "范围")
	require.Contains(t, ReportReady(Chinese, "2026-11-01", "2026-11-07"), "巡检报告")
}

func TestForAgentEdgeCases(t *testing.T) {
	t.Parallel()

	owner := &store.UserMessage{Profile: &storepb.UserProfile{Language: "zh-CN"}}
	require.Equal(t, Chinese, ForAgent(nil, owner))
	require.Equal(t, Chinese, ForAgent(&store.QualityAgentMessage{Language: "  zh-CN  "}, nil))
	require.Equal(t, Chinese, ForAgent(&store.QualityAgentMessage{Language: "  "}, owner))
	require.True(t, Supported(" zh-CN "))
}
