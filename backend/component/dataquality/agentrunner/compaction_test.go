package agentrunner

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func compactionSession() *store.QualityAgentSessionMessage {
	session := testSession()
	session.ContextTokens = 10_000
	return session
}

func fillerMessages(count int) []*store.QualityAgentTranscriptMessage {
	messages := make([]*store.QualityAgentTranscriptMessage, 0, count)
	for index := 0; index < count; index++ {
		role := storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER
		if index%2 == 1 {
			role = storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT
		}
		messages = append(messages, &store.QualityAgentTranscriptMessage{
			Seq:     int64(index + 1),
			Role:    role,
			Content: strings.Repeat("a data quality observation. ", 200),
		})
	}
	return messages
}

// Over the trigger, the oldest region is summarized into one checkpoint message and the
// session's watermark moves to the last summarized message.
func TestMaybeCompactSummarizesTheOldRegion(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{
		{content: "## Session Objective and Target\n- target-1\n\n## Next Step\n- (none)"},
	}), resolveToModel, nil)
	session := compactionSession()
	messages := fillerMessages(12)

	tokens, compacted, err := engine.maybeCompact(context.Background(), session, 3, llm.ResolvedConfig{ModelName: "test-model"}, messages, 2_000, false)
	require.NoError(t, err)
	require.True(t, compacted)
	require.Positive(t, tokens)
	require.NotEmpty(t, session.Summary)
	require.Greater(t, session.SummaryThroughSeq, int64(0))
	require.Less(t, session.SummaryThroughSeq, int64(12))

	var compaction *store.QualityAgentTranscriptMessage
	for _, message := range stores.messages {
		if message.Role == storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_COMPACTION {
			compaction = message
		}
	}
	require.NotNil(t, compaction)
	require.Equal(t, session.Summary, compaction.Content)
}

// Under the trigger nothing is summarized and no model call is made.
func TestMaybeCompactSkipsUnderTheTrigger(t *testing.T) {
	t.Parallel()

	stores := newFakeStore()
	engine := NewModelEngine(stores, scriptedChat([]scriptedTurn{{content: "unused"}}), resolveToModel, nil)
	session := compactionSession()
	session.ContextTokens = 100

	_, compacted, err := engine.maybeCompact(context.Background(), session, 1, llm.ResolvedConfig{ModelName: "test-model"}, fillerMessages(2), 10_000, false)
	require.NoError(t, err)
	require.False(t, compacted)
	require.Empty(t, session.Summary)
	require.Empty(t, stores.messages)
}

// The cut must not split an assistant tool call from its result.
func TestBalancedCutKeepsToolPairs(t *testing.T) {
	t.Parallel()

	messages := []*store.QualityAgentTranscriptMessage{
		{Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER},
		{Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
			ToolCalls: &storepb.QualityToolCallList{ToolCalls: []*storepb.QualityToolCall{{Id: "call_1", Name: "lookup"}}}},
		{Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL, ToolCallID: "call_1"},
		{Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER},
	}
	require.Equal(t, 1, balancedCut(messages, 2))
	require.Equal(t, 3, balancedCut(messages, 3))
}

// Pruning keeps the head and tail of an old oversized tool result and marks the cut.
func TestPruneToolResultsKeepsTheEnds(t *testing.T) {
	t.Parallel()

	content := strings.Repeat("头部内容", 2000) + "TAIL-MARKER"
	messages := []llm.AgentMessage{
		{Role: "toolResult", Content: content},
		{Role: "toolResult", Content: "recent result"},
	}
	pruned := pruneToolResults(messages, 1)
	require.Contains(t, pruned[0].Content, prunedMarkerForTest())
	require.True(t, strings.HasSuffix(pruned[0].Content, "TAIL-MARKER"))
	require.Equal(t, "recent result", pruned[1].Content)
}

// prunedMarkerForTest keeps the marker expectation in one place.
func prunedMarkerForTest() string {
	return "[... tool result pruned ...]"
}
