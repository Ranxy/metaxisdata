package mcp

import (
	"encoding/json"
	"errors"

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
	// codeUnsupported is the deployment's answer when the question is
	// answerable in principle but not by this one: an engine with no lineage
	// analyzer, a resource that has not been synced. It is neither a bad argument
	// nor a transient fault, and telling a model to retry or to fix its arguments
	// would send it in a circle.
	codeUnsupported = "unsupported"
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

func internalFailure(err error) *toolError {
	return newToolError(codeInternal, err.Error(), "")
}

// permissionDenied reports a caller missing the tool's declared permission.
func permissionDenied(permission string) *toolError {
	return newToolError(codePermissionDenied, "the caller does not hold "+permission,
		"a workspace administrator can grant it; another tool may answer part of the question")
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
