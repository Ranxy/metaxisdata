package agent

import "github.com/Ranxy/metaxisdata/backend/component/llm"

// Message is the internal message type used throughout the agent loop.
// It is richer than the raw LLM Message and is converted to Message at the LLM boundary.
type Message struct {
	Role    string // "system" | "user" | "assistant" | "toolResult"
	Content string // text content (for user/assistant/toolResult)
	// Reasoning is display-only model reasoning. ConvertToLlm deliberately does
	// not send it back: reasoning models reject their own reasoning in the
	// conversation, and it is not part of the answer contract.
	Reasoning  string
	ToolCalls  []llm.ToolCall // for assistant role
	ToolCallID string         // for toolResult role
	ToolName   string         // for toolResult role
}

// ConvertToLlm transforms Message[] to LLM-compatible Message[].
// This is the boundary between internal transcript and model input.
func ConvertToLlm(msgs []Message) []llm.Message {
	out := make([]llm.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			out = append(out, llm.Message{Role: "system", Content: m.Content})
		case "user":
			out = append(out, llm.Message{Role: "user", Content: m.Content})
		case "assistant":
			// Keep the assistant's text alongside its tool calls. Sending only
			// the calls dropped the model's reasoning/preface from the
			// transcript, so the next turn could not see what it had said.
			out = append(out, llm.Message{Role: "assistant", Content: m.Content, ToolCalls: m.ToolCalls})
		case "toolResult":
			out = append(out, llm.Message{
				Role:       "tool",
				ToolCallID: m.ToolCallID,
				Content:    m.Content,
			})
		default:
		}
	}
	return out
}
