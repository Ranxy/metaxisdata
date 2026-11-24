package agentrunner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/component/agent"
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

// The rebuilt model input is always legal for the provider: an unanswered call gets an
// interrupted result, an answered call keeps the stored result, and a result whose call
// is gone is dropped.
func TestBuildConversationKeepsTheProviderConversationLegal(t *testing.T) {
	t.Parallel()

	call := func(id string) *storepb.QualityToolCallList {
		return &storepb.QualityToolCallList{ToolCalls: []*storepb.QualityToolCall{{Id: id, Name: "lookup"}}}
	}
	tests := []struct {
		name  string
		input []*store.QualityAgentTranscriptMessage
		check func(*testing.T, []agent.Message)
	}{
		{
			name: "an unanswered call gets an interrupted result",
			input: []*store.QualityAgentTranscriptMessage{
				{Seq: 1, Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT, ToolCalls: call("call_1")},
				transcriptMessage(2, storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER),
			},
			check: func(t *testing.T, conversation []agent.Message) {
				require.Len(t, conversation, 3)
				require.Equal(t, "assistant", conversation[0].Role)
				require.Equal(t, "toolResult", conversation[1].Role)
				require.Equal(t, "call_1", conversation[1].ToolCallID)
				require.Contains(t, conversation[1].Content, "restarted")
				require.Equal(t, "user", conversation[2].Role)
			},
		},
		{
			name: "an answered call keeps the stored result",
			input: []*store.QualityAgentTranscriptMessage{
				{Seq: 1, Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT, ToolCalls: call("call_1")},
				{Seq: 2, Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL, ToolCallID: "call_1", Content: "the result"},
			},
			check: func(t *testing.T, conversation []agent.Message) {
				require.Len(t, conversation, 2)
				require.Equal(t, "the result", conversation[1].Content)
			},
		},
		{
			name: "an orphan result is dropped",
			input: []*store.QualityAgentTranscriptMessage{
				{
					Seq: 1, Role: storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL,
					ToolCallID: "call_orphan", ToolName: "lookup", Content: "orphan result",
				},
				transcriptMessage(2, storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER),
			},
			check: func(t *testing.T, conversation []agent.Message) {
				require.Len(t, conversation, 1)
				require.Equal(t, "user", conversation[0].Role)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.check(t, BuildConversation(nil, nil, test.input))
		})
	}
}
