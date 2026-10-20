package llm

// AgentEvent is emitted by the agent loop to report progress.
type AgentEvent struct {
	// Type identifies the event kind.
	Type AgentEventType

	// Content is a text delta from the LLM stream.
	Content string

	// ToolCall contains info about a tool being executed (tool_start / tool_end).
	ToolCall *ToolCall

	// ToolResult is the result of a completed tool call (tool_end).
	ToolResult string

	// ToolError is set when a tool execution fails.
	ToolError string

	// Error is a fatal error that stopped the loop.
	Error error

	// Usage is the token usage a turn reported, on AgentEventUsage; on
	// AgentEventAgentEnd it is the sum over every turn. Nil means the provider
	// reported nothing.
	Usage *Usage

	// Turn is the current turn number (1-based).
	Turn int
}

// AgentEventType identifies the kind of agent event.
type AgentEventType string

const (
	AgentEventTurnStart AgentEventType = "turn_start"
	AgentEventContent   AgentEventType = "content"
	AgentEventToolStart AgentEventType = "tool_start"
	AgentEventToolEnd   AgentEventType = "tool_end"
	// AgentEventUsage reports the token usage one turn's provider response
	// carried. It is not a verdict and not a bill; a provider that omits usage
	// emits no such event.
	AgentEventUsage    AgentEventType = "usage"
	AgentEventAgentEnd AgentEventType = "agent_end"
	AgentEventError    AgentEventType = "error"
)

// AgentConfig configures a single run of the agent loop.
type AgentConfig struct {
	// Provider configuration for LLM calls.
	Provider ResolvedConfig

	// SystemPrompt is prepended to the conversation.
	SystemPrompt string

	// UserPrompt is the initial user message.
	UserPrompt string

	// Tools available to the LLM.
	Tools []ToolDef

	// Executor runs tool calls.
	Executor ToolExecutor

	// MaxTurns limits the number of LLM calls (default DefaultMaxTurns).
	MaxTurns int

	// MaxConversationBytes limits the conversation kept in memory (default
	// DefaultMaxConversationBytes).
	MaxConversationBytes int

	// Temperature is the sampling temperature. Nil leaves the provider's default
	// in place; 0 is a request for deterministic sampling and is sent.
	Temperature *float64

	// MaxTokens bounds the generated answer. 0 leaves the provider's default in
	// place.
	MaxTokens int

	// ResponseFormat asks the provider for a structured response. Nil leaves the
	// provider's default in place; JSONObjectResponseFormat() asks for a JSON
	// object.
	ResponseFormat *ResponseFormat

	// IncludeUsage asks the provider to report token usage in the stream. It is
	// opt-in because it adds a request field some OpenAI-compatible gateways do
	// not accept, and a provider that refuses the whole request is worse than
	// missing usage numbers.
	IncludeUsage bool

	// DebugLogger, when set, logs every LLM request/response.
	DebugLogger DebugLogger
}
