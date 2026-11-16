package agentrunner

import (
	"github.com/Ranxy/metaxisdata/backend/component/agent"
	"github.com/Ranxy/metaxisdata/backend/component/agent/transcript"
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// BuildConversation rebuilds the model input from the stored transcript: the
// session's system message, the rolling compaction summary (which replaces the
// messages it covers), then every later message except engine notes. It repairs a
// dangling tool call left by a crash, so the provider always receives a legal
// message sequence.
func BuildConversation(
	session *store.QualityAgentSessionMessage,
	system *store.QualityAgentTranscriptMessage,
	messages []*store.QualityAgentTranscriptMessage,
) []agent.Message {
	systemText := ""
	if system != nil {
		systemText = system.Content
	}
	summary := ""
	if session != nil {
		summary = session.Summary
	}
	return transcript.BuildConversation(systemText, summary, transcriptEntries(messages))
}

// toolCallsToLLM maps a transcript's tool calls back to the provider shape.
func toolCallsToLLM(list *storepb.QualityToolCallList) []llm.ToolCall {
	if len(list.GetToolCalls()) == 0 {
		return nil
	}
	calls := make([]llm.ToolCall, 0, len(list.GetToolCalls()))
	for _, call := range list.GetToolCalls() {
		var converted llm.ToolCall
		converted.ID = call.GetId()
		converted.Type = "function"
		converted.Function.Name = call.GetName()
		converted.Function.Arguments = call.GetArguments()
		calls = append(calls, converted)
	}
	return calls
}

// EstimateTokens approximates the token size of a conversation.
func EstimateTokens(messages []agent.Message) int {
	return transcript.EstimateTokens(messages)
}
