package agentrunner

import (
	"unicode/utf8"

	"github.com/Ranxy/metaxisdata/backend/component/llm"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// checkpointPreamble frames the compaction summary as established background, so the
// model continues from the messages after it instead of acknowledging the checkpoint.
const checkpointPreamble = "This is an automatically generated checkpoint condensing an " +
	"earlier span of this conversation to free up context. Treat the captured context as " +
	"established background and build on it without restating it. Continue directly from " +
	"the messages that follow."

// BuildConversation rebuilds the model input from the stored transcript: the session's
// system message, the rolling compaction summary (which replaces the messages it
// covers), then every later message except engine notes. It repairs a dangling tool
// call left by a crash, so the provider always receives a legal message sequence.
func BuildConversation(
	session *store.QualityAgentSessionMessage,
	system *store.QualityAgentTranscriptMessage,
	messages []*store.QualityAgentTranscriptMessage,
) []llm.AgentMessage {
	if session == nil {
		session = &store.QualityAgentSessionMessage{}
	}
	conversation := make([]llm.AgentMessage, 0, len(messages)+2)
	if system != nil && system.Content != "" {
		conversation = append(conversation, llm.AgentMessage{Role: "system", Content: system.Content})
	}
	if session.Summary != "" {
		conversation = append(conversation, llm.AgentMessage{
			Role: "user",
			Content: checkpointPreamble + "\n\n<compacted-summary>\n" + session.Summary +
				"\n</compacted-summary>",
		})
	}
	for _, message := range messages {
		switch message.Role {
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_SYSTEM:
			// The system message is prepended above; a historical copy is not repeated.
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_USER:
			conversation = append(conversation, llm.AgentMessage{Role: "user", Content: message.Content})
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ASSISTANT:
			conversation = append(conversation, llm.AgentMessage{
				Role:      "assistant",
				Content:   message.Content,
				ToolCalls: toolCallsToLLM(message.ToolCalls),
			})
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_TOOL:
			conversation = append(conversation, llm.AgentMessage{
				Role:       "toolResult",
				ToolCallID: message.ToolCallID,
				ToolName:   message.ToolName,
				Content:    message.Content,
			})
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_ENGINE:
			// Runner/UI notes are not evidence and never return to the model.
		case storepb.QualityAgentMessageRole_QUALITY_AGENT_MESSAGE_ROLE_COMPACTION:
			// The summary field carries the checkpoint; the row is for the reader.
		default:
		}
	}
	return repairToolCalls(conversation)
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

// repairToolCalls inserts a synthetic result for every assistant tool call that has no
// stored result, which is what a crash between the assistant message and the tool
// execution leaves behind. A provider rejects a conversation with an unanswered call.
func repairToolCalls(messages []llm.AgentMessage) []llm.AgentMessage {
	repaired := make([]llm.AgentMessage, 0, len(messages))
	pending := make([]string, 0, 2)
	flush := func() {
		for _, id := range pending {
			repaired = append(repaired, llm.AgentMessage{
				Role:       "toolResult",
				ToolCallID: id,
				Content:    "the server restarted before this tool ran; its result is unknown",
			})
		}
		pending = pending[:0]
	}
	for _, message := range messages {
		switch message.Role {
		case "assistant":
			flush()
			repaired = append(repaired, message)
			for _, call := range message.ToolCalls {
				pending = append(pending, call.ID)
			}
		case "toolResult":
			// A result whose call is gone is an orphan from a corrupt or half-written
			// transcript; forwarding it would make the provider reject the whole
			// conversation, so it is dropped.
			matched := false
			for i, id := range pending {
				if id == message.ToolCallID {
					pending = append(pending[:i], pending[i+1:]...)
					matched = true
					break
				}
			}
			if matched {
				repaired = append(repaired, message)
			}
		default:
			flush()
			repaired = append(repaired, message)
		}
	}
	flush()
	return repaired
}

// EstimateTokens approximates the token size of a conversation. A provider's real
// prompt_tokens is always preferred when a wake reported one; this estimate only sizes
// messages added since, and it leans high (bytes/4 and about three quarters of a rune
// per token) so a Chinese-heavy conversation is not undercounted.
func EstimateTokens(messages []llm.AgentMessage) int {
	total := 0
	for _, message := range messages {
		total += estimateTextTokens(message.Content)
		total += estimateTextTokens(message.ToolCallID)
		total += estimateTextTokens(message.ToolName)
		for _, call := range message.ToolCalls {
			total += estimateTextTokens(call.Function.Name)
			total += estimateTextTokens(call.Function.Arguments)
		}
	}
	return total
}

// estimateTextTokens approximates one string's token count.
func estimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	byBytes := (len(text) + 3) / 4
	byRunes := (utf8.RuneCountInString(text)*3 + 3) / 4
	if byBytes > byRunes {
		return byBytes
	}
	return byRunes
}
