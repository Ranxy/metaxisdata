package agentrunner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

func transcriptMessage(seq int64, role storepb.QualityAgentMessageRole) *store.QualityAgentTranscriptMessage {
	return &store.QualityAgentTranscriptMessage{
		Seq:     seq,
		Role:    role,
		Content: role.String(),
	}
}

// The context starts with the system message, injects the compaction checkpoint in
// place of the messages it covers, and never sends an engine note to the model.
func TestBuildConversationOrdersTheModelInput(t *testing.T) {
	t.Parallel()

	session := &store.QualityAgentSessionMessage{Summary: "earlier work", SummaryThroughSeq: 2}
	system := &store.QualityAgentTranscriptMessage{Content: "you are the agent"}
	messages := []*store.QualityAgentTranscriptMessage{
		transcriptMessage(3, storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER),
		transcriptMessage(4, storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE),
		{
			Seq:     5,
			Role:    storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
			Content: "calling a tool",
			ToolCalls: &storepb.QualityToolCallList{ToolCalls: []*storepb.QualityToolCall{
				{Id: "call_1", Name: "get_target_summary", Arguments: "{}"},
			}},
		},
		{
			Seq: 6, Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL,
			Content: "the summary", ToolCallID: "call_1", ToolName: "get_target_summary",
		},
		transcriptMessage(7, storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER),
	}

	conversation := BuildConversation(session, system, messages)
	require.Len(t, conversation, 6)
	require.Equal(t, "system", conversation[0].Role)
	require.Equal(t, "you are the agent", conversation[0].Content)
	require.Equal(t, "user", conversation[1].Role)
	require.Contains(t, conversation[1].Content, "earlier work")
	require.Contains(t, conversation[1].Content, "<compacted-summary>")
	require.Equal(t, "user", conversation[2].Role)
	require.Equal(t, "assistant", conversation[3].Role)
	require.Len(t, conversation[3].ToolCalls, 1)
	require.Equal(t, "toolResult", conversation[4].Role)
	require.Equal(t, "call_1", conversation[4].ToolCallID)
	require.Equal(t, "user", conversation[5].Role)
}

// A crash between the assistant message and the tool execution leaves an unanswered
// call; the context repairs it so the provider accepts the conversation.
func TestBuildConversationRepairsDanglingToolCalls(t *testing.T) {
	t.Parallel()

	conversation := BuildConversation(nil, nil, []*store.QualityAgentTranscriptMessage{
		{
			Seq: 1, Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
			ToolCalls: &storepb.QualityToolCallList{ToolCalls: []*storepb.QualityToolCall{{Id: "call_1", Name: "lookup"}}},
		},
		transcriptMessage(2, storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER),
	})

	require.Len(t, conversation, 3)
	require.Equal(t, "assistant", conversation[0].Role)
	require.Equal(t, "toolResult", conversation[1].Role)
	require.Equal(t, "call_1", conversation[1].ToolCallID)
	require.Contains(t, conversation[1].Content, "restarted")
	require.Equal(t, "user", conversation[2].Role)
}

// A stored tool result that answers the call is kept, not duplicated by the repair.
func TestBuildConversationKeepsAnsweredToolCalls(t *testing.T) {
	t.Parallel()

	conversation := BuildConversation(nil, nil, []*store.QualityAgentTranscriptMessage{
		{
			Seq: 1, Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT,
			ToolCalls: &storepb.QualityToolCallList{ToolCalls: []*storepb.QualityToolCall{{Id: "call_1", Name: "lookup"}}},
		},
		{
			Seq: 2, Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL,
			ToolCallID: "call_1", Content: "the result",
		},
	})

	require.Len(t, conversation, 2)
	require.Equal(t, "the result", conversation[1].Content)
}

func TestEstimateTokensCountsATextConversation(t *testing.T) {
	t.Parallel()

	estimate := EstimateTokens([]llm.AgentMessage{
		{Role: "system", Content: "you are the agent"},
		{Role: "user", Content: "a short sentence"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_1"}}},
	})
	require.Positive(t, estimate)
	require.Zero(t, EstimateTokens(nil))
}
