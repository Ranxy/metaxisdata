package metadata

import (
	"errors"
	"fmt"

	"connectrpc.com/connect"
)

// The error vocabulary mirrors the MCP tool contract, so the same failure reads
// the same on both surfaces: a model can act on candidates or a hint instead of
// spending another round trip on prose.
const (
	// CodeInvalidArgument reports a malformed or contradictory reference.
	CodeInvalidArgument = "invalid_argument"
	// CodeNotFound reports a reference that matches nothing.
	CodeNotFound = "not_found"
	// CodeAmbiguous reports a name several objects answer to.
	CodeAmbiguous = "ambiguous"
	// CodeScopeRequired reports an analysis request with no scope.
	CodeScopeRequired = "scope_required"
	// CodePermissionDenied reports a missing workspace permission.
	CodePermissionDenied = "permission_denied"
	// CodeUnauthenticated reports a call with no verified identity.
	CodeUnauthenticated = "unauthenticated"
	// CodeResourceExhausted reports a budget that ran out.
	CodeResourceExhausted = "resource_exhausted"
	// CodeUnavailable reports a dependency that could not answer.
	CodeUnavailable = "unavailable"
	// CodeTimeout reports an expired call.
	CodeTimeout = "timeout"
	// CodeInternal reports an unexpected failure.
	CodeInternal = "internal"
	// CodeUnsupported reports a question this deployment cannot answer at all.
	CodeUnsupported = "unsupported"
)

// Error is the machine-readable failure a metadata tool reports. Its JSON shape is
// the tool contract's error envelope.
type Error struct {
	// Code is the stable classifier.
	Code string `json:"code"`
	// Message is the sentence the model reads.
	Message string `json:"message"`
	// Hint says what to do next.
	Hint string `json:"hint,omitempty"`
	// Details carries candidates or other structured context.
	Details any `json:"details,omitempty"`
}

// Error implements error.
func (e *Error) Error() string { return e.Message }

// newError builds one failure.
func newError(code, message, hint string) *Error {
	return &Error{Code: code, Message: message, Hint: hint}
}

// invalidArgument reports a malformed reference.
func invalidArgument(message string) *Error {
	return newError(CodeInvalidArgument, message, "")
}

// notFound reports an unknown reference together with what the registry does have,
// so the next call can be right instead of being a guess.
func notFound(kind, value string, candidates []Candidate) *Error {
	failure := newError(CodeNotFound, fmt.Sprintf("no %s matches %q", kind, value),
		"the candidates below are what the registry has: use one of them, or list/search first")
	if len(candidates) > 0 {
		failure.Details = map[string]any{"candidates": candidates}
	}
	return failure
}

// ambiguous reports a name several objects answer to.
func ambiguous(kind, value string, candidates []Candidate) *Error {
	return &Error{
		Code:    CodeAmbiguous,
		Message: fmt.Sprintf("%q matches %d %ss", value, len(candidates), kind),
		Hint:    "pass a more specific reference, or the guid of the one you mean",
		Details: map[string]any{"candidates": candidates},
	}
}

// fromRPC maps a ConnectRPC failure onto the tool vocabulary. The resolvers call
// the same read services the API exposes, so every failure a caller could see
// there arrives here too and has to keep its meaning.
func fromRPC(err error) *Error {
	if err == nil {
		return nil
	}
	var failure *Error
	if errors.As(err, &failure) {
		return failure
	}
	switch connect.CodeOf(err) {
	case connect.CodeNotFound:
		return newError(CodeNotFound, err.Error(), "search or list first to get a reference that exists")
	case connect.CodeInvalidArgument:
		return newError(CodeInvalidArgument, err.Error(), "the server refused the arguments; the message says which")
	case connect.CodeFailedPrecondition:
		return newError(CodeUnsupported, err.Error(), "this deployment cannot answer that as asked; the message says why")
	case connect.CodePermissionDenied:
		return newError(CodePermissionDenied, err.Error(), "")
	case connect.CodeUnauthenticated:
		return newError(CodeUnauthenticated, err.Error(), "the client's authorization is gone: authorize it again")
	case connect.CodeResourceExhausted:
		return newError(CodeResourceExhausted, err.Error(), "back off before retrying")
	case connect.CodeUnavailable:
		return newError(CodeUnavailable, err.Error(), "the server is not reachable right now; retrying may work")
	case connect.CodeDeadlineExceeded, connect.CodeCanceled:
		return newError(CodeTimeout, err.Error(), "retry, or ask for less at once")
	default:
		return newError(CodeInternal, err.Error(), "")
	}
}

// AsError returns err as a metadata tool error, or nil when it is something else.
func AsError(err error) *Error {
	var failure *Error
	if errors.As(err, &failure) {
		return failure
	}
	return nil
}
