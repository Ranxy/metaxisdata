// Package llm provides LLM agent capabilities.
package llm

// AgentMessage is the internal message type used throughout the agent loop.
// It is richer than the raw LLM Message and is converted to Message at the LLM boundary.
type AgentMessage struct {
	Role       string     // "system" | "user" | "assistant" | "toolResult"
	Content    string     // text content (for user/assistant/toolResult)
	ToolCalls  []ToolCall // for assistant role
	ToolCallID string     // for toolResult role
	ToolName   string     // for toolResult role
}

// ConvertToLlm transforms AgentMessage[] to LLM-compatible Message[].
// This is the boundary between internal transcript and model input.
func ConvertToLlm(msgs []AgentMessage) []Message {
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "system":
			out = append(out, Message{Role: "system", Content: m.Content})
		case "user":
			out = append(out, Message{Role: "user", Content: m.Content})
		case "assistant":
			if len(m.ToolCalls) > 0 {
				out = append(out, Message{Role: "assistant", ToolCalls: m.ToolCalls})
			} else {
				out = append(out, Message{Role: "assistant", Content: m.Content})
			}
		case "toolResult":
			out = append(out, Message{
				Role:       "tool",
				ToolCallID: m.ToolCallID,
				Content:    m.Content,
			})
		default:
		}
	}
	return out
}
