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

	// Done signals the agent loop has finished.
	Done bool

	// Error is a fatal error that stopped the loop.
	Error error

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
	AgentEventTurnEnd   AgentEventType = "turn_end"
	AgentEventAgentEnd  AgentEventType = "agent_end"
	AgentEventError     AgentEventType = "error"
)

// AgentHooks provides optional callbacks to customize the agent loop behavior.
type AgentHooks struct {
	// BeforeToolCall is invoked before executing a tool. Return a non-empty
	// blockReason to skip execution and inject an error result instead.
	BeforeToolCall func(toolCall ToolCall) (blockReason string)

	// AfterToolCall is invoked after tool execution, before the result
	// is appended to the transcript. It can transform the result.
	AfterToolCall func(toolCall ToolCall, result string, execErr error) (finalResult string)
}

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

	// MaxTurns limits the number of LLM calls (default 6).
	MaxTurns int

	// Hooks for customization.
	Hooks AgentHooks

	// DebugLogger, when set, logs every LLM request/response.
	DebugLogger DebugLogger
}
