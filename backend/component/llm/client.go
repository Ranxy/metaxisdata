// Package llm provides LLM client capabilities.
package llm

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

func (t *ToolCall) GetName() string {
	if t == nil {
		return ""
	}
	return t.Function.Name
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

// Usage is the token usage one turn reported. It is what the provider returned,
// not a billing statement: a provider that does not report usage leaves the
// zero value, and every field stays 0. The numbers are comparable across turns
// because they are the provider's own counts.
type Usage struct {
	// PromptTokens is what the provider counted for the request, input included.
	PromptTokens int64
	// CompletionTokens is what it counted for the generated answer.
	CompletionTokens int64
	// TotalTokens is the provider's total; when it omits it, add() derives it.
	TotalTokens int64
}

// add accumulates one turn's usage into a running total. A missing total is
// derived from its parts, so a provider that reports only the two components
// still yields a usable sum.
func (u *Usage) add(other *Usage) {
	if u == nil || other == nil {
		return
	}
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
	if other.TotalTokens > 0 {
		u.TotalTokens += other.TotalTokens
	} else {
		u.TotalTokens += other.PromptTokens + other.CompletionTokens
	}
}

// ResponseFormatJSONObject is the OpenAI response_format type that asks for a
// JSON object. It is the only format this build requests: a JSON schema would
// have to be provider-specific, and the caller validates the decoded value
// anyway.
const ResponseFormatJSONObject = "json_object"

// ResponseFormat is the OpenAI-compatible response_format object.
type ResponseFormat struct {
	Type string `json:"type"`
}

// JSONObjectResponseFormat returns the response_format that asks for a JSON
// object, or nil for a caller that wants the provider's default.
func JSONObjectResponseFormat() *ResponseFormat {
	return &ResponseFormat{Type: ResponseFormatJSONObject}
}

// ---- internal types used by agent.go ----

// chatRequest is the JSON body sent to the chat completion API.
//
// The optional fields are omitted when unset, so a caller that does not ask for
// them sends exactly the request it sent before they existed.
type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Stream         bool            `json:"stream"`
	Tools          []ToolDef       `json:"tools,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
	// StreamOptions is set only when the caller wants usage reported in the
	// stream; OpenAI withholds it otherwise.
	StreamOptions *streamOptions `json:"stream_options,omitempty"`
}

// streamOptions asks the provider to include the trailing usage chunk.
type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatUsage is the provider's usage object.
type chatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// usage converts the wire form into the exported Usage, and nil for a provider
// that reported none.
func (u *chatUsage) usage() *Usage {
	if u == nil {
		return nil
	}
	return &Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}
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

// chatStreamChunk is one SSE data event. The usage object arrives in a chunk of
// its own after the final choice, so a parser that stops at finish_reason never
// sees it.
type chatStreamChunk struct {
	Choices []struct {
		Index int `json:"index"`
		Delta struct {
			Content   string           `json:"content"`
			ToolCalls []streamToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *chatUsage `json:"usage,omitempty"`
}

// chatCompletionResponse is a provider that ignored stream=true and answered
// with one non-streaming completion object. Some OpenAI-compatible gateways do
// this, and the streaming-only parser used to report the whole answer as a
// dropped stream.
type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content   string           `json:"content"`
			ToolCalls []streamToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *chatUsage `json:"usage,omitempty"`
}
