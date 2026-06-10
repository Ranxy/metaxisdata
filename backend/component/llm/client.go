// Package llm provides LLM client capabilities.
package llm

import "time"

const httpClientTimeout = 5 * time.Minute

// Message represents a chat message sent to the LLM API.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// ToolCall is a function call requested by the LLM.
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ToolDef defines a tool available to the LLM.
type ToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

// ToolResult is the result of executing a tool call.
type ToolResult struct {
	ToolCallID string
	Content    string
}

// ToolExecutor executes tool calls and returns results.
type ToolExecutor func(toolCall ToolCall) ([]ToolResult, error)

// DebugLogger receives full request/response bodies for debugging.
type DebugLogger func(reqBody string, respBody string)

// ---- internal types used by agent.go ----

// chatRequest is the JSON body sent to the chat completion API.
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Tools    []ToolDef `json:"tools,omitempty"`
}

// streamToolCall is a tool call fragment from the SSE stream.
type streamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// chatStreamChunk is one SSE data event.
type chatStreamChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []streamToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}
