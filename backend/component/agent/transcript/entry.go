// Package transcript rebuilds a model conversation from a stored, append-only
// transcript and compacts it when it outgrows its budget.
//
// It is mechanism, not a product: it knows message roles, tool calls and token
// estimates, but not what a session means, where it is stored, or which model
// reads it. A product adapter maps its own stored rows into Entry values and
// keeps the resulting checkpoint; this package only decides what the model should
// see and where a compaction cut is legal.
package transcript

import "github.com/Ranxy/metaxisdata/backend/component/llm"

// Role is one stored message's role, reduced to what rebuilding the conversation
// needs.
type Role int

const (
	// RoleUser is a user message: a wake, an event or a person's question.
	RoleUser Role = iota
	// RoleAssistant is a completed model message, tool calls included.
	RoleAssistant
	// RoleTool is one tool result answering an assistant tool call.
	RoleTool
	// RoleSystem is the session's system message. The conversation builder
	// prepends the live system prompt instead of replaying a stored copy.
	RoleSystem
	// RoleEngine is a runner note for the reader; it is never model context.
	RoleEngine
	// RoleCompaction is a checkpoint marker row; the summary lives on the session.
	RoleCompaction
)

// Entry is one stored transcript message, independent of the store's concrete
// row type.
type Entry struct {
	// Seq is the append-only sequence number; a compaction records the sequence
	// it covers.
	Seq int64
	// Role classifies the message.
	Role Role
	// Content is the text.
	Content string
	// ToolCalls are the assistant message's requested calls.
	ToolCalls []llm.ToolCall
	// ToolCallID ties a tool result to its call.
	ToolCallID string
	// ToolName is the called tool's name, for diagnostics and pairing.
	ToolName string
}
