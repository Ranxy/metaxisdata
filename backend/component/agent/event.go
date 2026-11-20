package agent

import (
	"github.com/Ranxy/metaxisdata/backend/component/llm"
	"github.com/Ranxy/metaxisdata/backend/metrics"
)

// Event is emitted by the agent loop to report progress.
type Event struct {
	// Type identifies the event kind.
	Type EventType

	// Content is a text delta from the LLM stream.
	Content string

	// ToolCall contains info about a tool being executed (tool_start / tool_end).
	ToolCall *llm.ToolCall

	// ToolResult is the result of a completed tool call (tool_end).
	ToolResult string

	// Assistant is the completed assistant message of one turn
	// (EventAssistant). It is what a session recorder persists; the Content
	// deltas are only the streaming view of the same text.
	Assistant *Message

	// ToolError is set when a tool execution fails.
	ToolError string

	// Error is a fatal error that stopped the loop.
	Error error

	// Usage is the token usage a turn reported, on EventUsage; on
	// EventEnd it is the sum over every turn. Nil means the provider
	// reported nothing.
	Usage *llm.Usage

	// Turn is the current turn number (1-based).
	Turn int
}

// EventType identifies the kind of agent event.
type EventType string

const (
	EventTurnStart EventType = "turn_start"
	EventContent   EventType = "content"
	EventToolStart EventType = "tool_start"
	EventToolEnd   EventType = "tool_end"
	// EventAssistant reports one completed assistant message (text plus any
	// tool calls), so a recorder does not have to reconstruct it from deltas.
	EventAssistant EventType = "assistant"
	// EventUsage reports the token usage one turn's provider response
	// carried. It is not a verdict and not a bill; a provider that omits usage
	// emits no such event.
	EventUsage EventType = "usage"
	EventEnd   EventType = "agent_end"
	EventError EventType = "error"
)

// Config configures a single run of the agent loop.
type Config struct {
	// Provider configuration for LLM calls.
	Provider llm.ResolvedConfig

	// Feature labels this caller's model calls in metrics (for example
	// "explain_sql" or "dq_explain_result"). The agent loop records one LLM call
	// per invocation, so this is the only place a caller labels its calls.
	Feature string

	// Metrics records the invocation's call, duration and token outcome. A nil
	// value disables instrumentation.
	Metrics *metrics.Metrics

	// History is the conversation to continue, in order. When non-empty it
	// replaces SystemPrompt/UserPrompt; a long-lived agent session rebuilds it from
	// the stored transcript so each wake continues the same conversation. A
	// single-shot caller keeps using the two prompt fields.
	History []Message

	// SystemPrompt is prepended to the conversation.
	SystemPrompt string

	// UserPrompt is the initial user message.
	UserPrompt string

	// Tools available to the LLM.
	Tools []llm.ToolDef

	// Executor runs tool calls.
	Executor ToolExecutor

	// ParallelSafe reports whether a tool call may run concurrently with the next
	// safe call in the same model batch. Nil keeps execution serial.
	ParallelSafe func(toolName string) bool

	// MaxParallelToolCalls bounds how many parallel-safe calls run at once. A
	// value below 2 keeps execution serial.
	MaxParallelToolCalls int

	// MaxTurns limits the number of LLM calls (default DefaultMaxTurns).
	MaxTurns int

	// MaxConversationBytes limits the conversation kept in memory (default
	// DefaultMaxConversationBytes).
	MaxConversationBytes int

	// Temperature is the sampling temperature. Nil leaves the provider's default
	// in place; 0 is a request for deterministic sampling and is sent.
	Temperature *float64

	// MaxTokens bounds the generated answer. 0 falls back to the resolved
	// model budget (Provider.MaxOutputTokens); when both are 0 the request omits
	// the field and the provider's default applies.
	MaxTokens int

	// ResponseFormat asks the provider for a structured response. Nil leaves the
	// provider's default in place; llm.JSONObjectResponseFormat() asks for a JSON
	// object.
	ResponseFormat *llm.ResponseFormat

	// IncludeUsage asks the provider to report token usage in the stream. It is
	// opt-in because it adds a request field some OpenAI-compatible gateways do
	// not accept, and a provider that refuses the whole request is worse than
	// missing usage numbers.
	IncludeUsage bool

	// DebugLogger, when set, logs every LLM request/response.
	DebugLogger llm.DebugLogger
}
