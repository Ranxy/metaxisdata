package mcp

import (
	"encoding/json"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool error codes. They follow the CLI's vocabulary, so the failure playbook
// written for it keeps applying, plus "ambiguous", which name addressing needs
// and the CLI — which only ever took a GUID — never had to answer.
const (
	codeInvalidArgument   = "invalid_argument"
	codeNotFound          = "not_found"
	codeAmbiguous         = "ambiguous"
	codeScopeRequired     = "scope_required"
	codePermissionDenied  = "permission_denied"
	codeUnauthenticated   = "unauthenticated"
	codeResourceExhausted = "resource_exhausted"
	codeUnavailable       = "unavailable"
	codeTimeout           = "timeout"
	codeInternal          = "internal"
)

// toolError is the machine-readable failure a tool reports. It mirrors the CLI's
// error envelope and adds details: a model can act on candidates or a hint
// without spending another round trip, which prose in a message would not give it.
type toolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Details any    `json:"details,omitempty"`
}

func (e *toolError) Error() string { return e.Message }

func newToolError(code, message, hint string) *toolError {
	return &toolError{Code: code, Message: message, Hint: hint}
}

func invalidArgument(format string, args ...any) *toolError {
	return newToolError(codeInvalidArgument, fmt.Sprintf(format, args...), "")
}

// notFound reports an unknown reference together with what the registry does
// have, so the next call can be right instead of being a guess.
func notFound(kind, value string, candidates []candidate) *toolError {
	failure := newToolError(codeNotFound, fmt.Sprintf("no %s matches %q", kind, value),
		"the candidates below are what the registry has: use one of them, or list/search first")
	if len(candidates) > 0 {
		failure.Details = map[string]any{"candidates": candidates}
	}
	return failure
}

// ambiguous reports a name several objects answer to. Guessing one of them would
// silently answer about the wrong object, so it refuses and shows the choices.
func ambiguous(kind, value string, candidates []candidate) *toolError {
	return &toolError{
		Code:    codeAmbiguous,
		Message: fmt.Sprintf("%q matches %d %ss", value, len(candidates), kind),
		Hint:    "pass a more specific reference, or the guid of the one you mean",
		Details: map[string]any{"candidates": candidates},
	}
}

func permissionDenied(permission string) *toolError {
	return newToolError(codePermissionDenied, "the caller does not hold "+permission,
		"a workspace administrator can grant it; another tool may answer part of the question")
}

// toolErrorFromRPC maps a ConnectRPC failure onto the tool vocabulary. The read
// services are the same ones the ConnectRPC API exposes, so every failure a
// caller could see there arrives here too, and it has to keep its meaning.
func toolErrorFromRPC(err error) *toolError {
	if err == nil {
		return nil
	}
	var failure *toolError
	if errors.As(err, &failure) {
		return failure
	}
	switch connect.CodeOf(err) {
	case connect.CodeNotFound:
		return newToolError(codeNotFound, err.Error(), "search or list first to get a reference that exists")
	case connect.CodeInvalidArgument, connect.CodeFailedPrecondition:
		return newToolError(codeInvalidArgument, err.Error(), "fix the arguments and retry")
	case connect.CodePermissionDenied:
		return newToolError(codePermissionDenied, err.Error(), "")
	case connect.CodeUnauthenticated:
		return newToolError(codeUnauthenticated, err.Error(), "the client's authorization is gone: authorize it again")
	case connect.CodeResourceExhausted:
		return newToolError(codeResourceExhausted, err.Error(), "back off before retrying")
	case connect.CodeUnavailable:
		return newToolError(codeUnavailable, err.Error(), "the server is not reachable right now; retrying may work")
	case connect.CodeDeadlineExceeded, connect.CodeCanceled:
		return newToolError(codeTimeout, err.Error(), "retry, or ask for less at once")
	default:
		return newToolError(codeInternal, err.Error(), "")
	}
}

func internalFailure(err error) *toolError {
	return newToolError(codeInternal, err.Error(), "")
}

// errNoPermissionChecker means the server was built without an authorization
// checker, so a tool that requires a permission cannot be allowed to run.
var errNoPermissionChecker = errors.New("no permission checker is configured")

// errorResult renders a failure in band: the MCP runtime reports it as a normal
// result with isError set, which is what lets the model see what went wrong and
// correct itself. A protocol-level error would not reach it.
func errorResult(err error) *mcpsdk.CallToolResult {
	var failure *toolError
	if !errors.As(err, &failure) {
		failure = internalFailure(err)
	}
	payload, marshalErr := json.Marshal(failure)
	if marshalErr != nil {
		payload = []byte(`{"code":"internal","message":"the tool failed and its error could not be encoded"}`)
	}
	return &mcpsdk.CallToolResult{
		IsError: true,
		// The text copy is deliberate here: the envelope is small and a model
		// reads it directly, unlike a listing where a copy would double the cost.
		Content:           []mcpsdk.Content{&mcpsdk.TextContent{Text: string(payload)}},
		StructuredContent: json.RawMessage(payload),
	}
}
