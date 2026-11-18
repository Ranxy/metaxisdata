// Package tool defines the model-callable tool contract and the registry a
// product resolves its tool set from.
//
// A tool is one capability a model may call: a name, a description, a JSON
// Schema for its arguments, the execution body, and the metadata authorization
// and the runtime need to treat it correctly. Domain capability packages
// register tools here; the transports (the MCP server and the built-in agent
// loop) consume the same definitions through their own adapters.
//
// The package deliberately knows nothing about any domain: it has no store, no
// protobuf and no quality knowledge. A tool's Execute body owns that, and a
// product's spec decides which tools an agent sees.
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Ranxy/metaxisdata/backend/common/permission"
	"github.com/Ranxy/metaxisdata/backend/store"
)

// Scope says who may consume a tool.
type Scope int

const (
	// ScopeCatalog is a tool both front ends may consume: the MCP server reads
	// the read-only subset, and any product may put it in a spec.
	ScopeCatalog Scope = iota
	// ScopeProductOnly is a tool that only exists inside one agent product (wake
	// control and bookkeeping). It is never exposed over MCP.
	ScopeProductOnly
)

// AuthzMode says what authorizes one tool call.
type AuthzMode int

const (
	// AuthzPermission is a tool gated by an IAM permission. It typically wraps an
	// RPC; an in-process product tool may declare the nearest-analogue permission
	// instead (the DQ agent does), in which case the declaration states the
	// capability the product's role must hold rather than the exact RPC that runs.
	AuthzPermission AuthzMode = iota
	// AuthzInternal is agent bookkeeping or wake control. It is scoped by session
	// ownership, not by workspace permission, and may only be registered as a
	// product-only tool.
	AuthzInternal
)

// SideEffect classifies what a tool changes.
type SideEffect int

const (
	// SideEffectNone reads.
	SideEffectNone SideEffect = iota
	// SideEffectPropose writes a proposal or evidence without changing a verdict
	// or publishing anything.
	SideEffectPropose
	// SideEffectPublish changes a verdict, publishes, or administers. No agent
	// product may expose one in this build.
	SideEffectPublish
)

// Meta is a tool's policy and runtime declaration. A tool whose metadata cannot
// be validated is refused at registration, which is a boot error for a product.
type Meta struct {
	// Group names the capability family ("metadata", "lineage", "quality",
	// "agent"), for metrics and review.
	Group string
	// Scope decides whether the tool may enter the shared catalog.
	Scope Scope
	// Authz decides what authorizes a call.
	Authz AuthzMode
	// ReadOnly is true only for a tool that changes nothing; only these enter the
	// MCP read-only surface.
	ReadOnly bool
	// SideEffect classifies a non-read tool.
	SideEffect SideEffect
	// Permission is the IAM permission of the RPC the tool wraps. Required for
	// AuthzPermission.
	Permission string
	// Timeout bounds one execution. Zero inherits the caller's deadline.
	Timeout time.Duration
	// MaxResultBytes bounds the model-visible result. Zero means no cap.
	MaxResultBytes int
	// ParallelSafe reports that concurrent execution with other parallel-safe
	// calls is safe. Only a pure read may set it.
	ParallelSafe bool
}

// Tool is one model-callable capability.
type Tool struct {
	// Name is the model-facing function name.
	Name string
	// Description tells the model when to call it.
	Description string
	// Parameters is the JSON Schema of the arguments, as a JSON-marshalable
	// value. It is the single source of the tool's schema.
	Parameters any
	// Meta is the policy and runtime declaration.
	Meta Meta
	// Execute runs one call. It returns the canonical result; a returned error is
	// normalized into an error result, so an ordinary failure never ends a wake.
	Execute func(ctx context.Context, call Call, inv *Invocation) (Result, error)
}

// Call is one model-requested tool invocation with its arguments already
// isolated from the rest of the turn.
type Call struct {
	// ID identifies the call inside one model turn.
	ID string
	// Name is the tool name the model requested.
	Name string
	// Arguments is the raw argument JSON.
	Arguments json.RawMessage
}

// Invocation carries what a tool may read about its caller and its runtime. A
// tool never reads ambient process state.
type Invocation struct {
	// Principal is the caller's own identity: a user for MCP, the agent's
	// service-account principal for the built-in agent. It is never the person
	// who woke the agent.
	Principal *store.UserMessage
	// Workspace is the workspace the call runs in.
	Workspace string
	// Logger is the structured logger of the owning product.
	Logger *slog.Logger
	// Now returns the current time; nil means time.Now. A product may pin it for
	// tests and deterministic budgets.
	Now func() time.Time
	// Checker enforces a tool's IAM permission against Principal. A transport that
	// already ran the ACL interceptor may leave it nil; a product that calls tools
	// in-process (the built-in agent) must set it, because the interceptor does not
	// run on that path.
	Checker PermissionChecker
	// Observer records one execution's outcome, duration and result size. A product
	// wires it to its metrics; nil records nothing.
	Observer Observer
	// Audit records the call in the deployment's ledger. A product wires it to the
	// same ledger the RPC path writes; nil records nothing.
	Audit AuditSink
}

// AuditEntry is one tool call as the ledger records it.
type AuditEntry struct {
	// Tool is the model-facing tool name.
	Tool string
	// Principal is the caller the call ran as.
	Principal *store.UserMessage
	// Workspace is the workspace the call ran in.
	Workspace string
	// Arguments is the raw argument JSON.
	Arguments json.RawMessage
	// Failure is set when the call failed or was refused.
	Failure *Error
	// Duration is how long the call took.
	Duration time.Duration
}

// AuditSink records tool calls. A write or proposal is audited here, which is the
// ledger entry the in-process path would otherwise not get.
type AuditSink interface {
	RecordToolCall(ctx context.Context, entry AuditEntry)
}

// Observer records one tool execution. The product captures the labels it owns
// (its product name); the pipeline supplies the tool, outcome and measurement.
type Observer interface {
	ObserveToolCall(tool, outcome string, duration time.Duration, resultBytes int)
}

// PermissionChecker resolves whether a principal holds a workspace permission.
// *iam.Manager implements it, and the interface lets a test decide the answer.
type PermissionChecker interface {
	CheckPermission(ctx context.Context, perm permission.Permission, principal *store.UserMessage) (bool, error)
}

// EvidenceRef is one citable reference a tool result carries. It is a reference,
// never free text: a product records it verbatim.
type EvidenceRef struct {
	Kind      string
	Reference string
	Note      string
}

// Result is a tool's canonical output.
type Result struct {
	// Value is the structured value. It is rendered to Content when Content is
	// empty.
	Value any
	// Content is the model-visible text. Empty means the compact JSON of Value.
	Content string
	// IsError marks a business failure the model may retry or rephrase. The Go
	// error path of Execute is normalized into this shape.
	IsError bool
	// Error describes a business failure.
	Error *Error
	// Truncated reports that Content was cut to Meta.MaxResultBytes.
	Truncated bool
	// Evidence lists the references this result establishes.
	Evidence []EvidenceRef
	// Meta is presentation metadata for a UI card; it never reaches the model.
	Meta map[string]any
}

// ErrorCode is the stable machine-readable classification of a tool failure.
type ErrorCode string

// The error vocabulary mirrors the MCP tool error contract so a model meets the
// same words on both surfaces.
const (
	CodeUnknownTool       ErrorCode = "unknown_tool"
	CodeInvalidArgument   ErrorCode = "invalid_argument"
	CodeNotFound          ErrorCode = "not_found"
	CodeAmbiguous         ErrorCode = "ambiguous"
	CodePermissionDenied  ErrorCode = "permission_denied"
	CodeUnauthenticated   ErrorCode = "unauthenticated"
	CodeResourceExhausted ErrorCode = "resource_exhausted"
	CodeTimeout           ErrorCode = "tool_timeout"
	CodeUnavailable       ErrorCode = "unavailable"
	CodeInternal          ErrorCode = "internal"
)

// Error is a structured tool failure.
type Error struct {
	// Code is the stable classifier.
	Code ErrorCode
	// Message is the sentence the model reads.
	Message string
	// Hint says what to do next (rephrase, pick a candidate, back off).
	Hint string
	// Candidates lists the names a failed resolution could have meant.
	Candidates []string
	// Retryable reports whether the same call may succeed later.
	Retryable bool
}

// Error implements error. It returns only the message because the audit row and
// the UI show that; the model-facing rendering goes through ModelContent.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// ModelContent is what a model reads for this failure: the message plus the hint,
// the candidates a reference could have meant, and whether a retry may work. It is
// the in-process counterpart of the MCP error envelope.
func (e *Error) ModelContent() string {
	if e == nil {
		return ""
	}
	parts := []string{e.Message}
	if e.Hint != "" {
		parts = append(parts, "hint: "+e.Hint)
	}
	if len(e.Candidates) > 0 {
		parts = append(parts, "candidates: "+strings.Join(e.Candidates, ", "))
	}
	if e.Retryable {
		parts = append(parts, "retryable: yes")
	}
	return strings.Join(parts, "\n")
}

// NewError builds a tool error.
func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

// ErrorOf normalizes any error into a structured tool error. A plain error is an
// internal failure whose message is kept, because the model may still act on it.
func ErrorOf(err error) *Error {
	if err == nil {
		return nil
	}
	var toolErr *Error
	if errors.As(err, &toolErr) {
		return toolErr
	}
	return &Error{Code: CodeInternal, Message: err.Error()}
}

// Schema is the model-facing declaration of one tool.
type Schema struct {
	// Name is the function name.
	Name string
	// Description tells the model when to call it.
	Description string
	// Parameters is the argument JSON Schema.
	Parameters any
}
