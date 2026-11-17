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

// NewError builds one failure.
func NewError(code, message, hint string) *Error {
	return &Error{Code: code, Message: message, Hint: hint}
}

// InvalidArgument reports a malformed reference.
func InvalidArgument(format string, args ...any) *Error {
	return NewError(CodeInvalidArgument, fmt.Sprintf(format, args...), "")
}

// InvalidArgumentHint reports a malformed reference together with the next step
// the caller should take. The hint is part of the tool envelope, so a name-path
// failure must carry it on every surface.
func InvalidArgumentHint(message, hint string) *Error {
	return NewError(CodeInvalidArgument, message, hint)
}

// internalFailure reports a failure the tool cannot classify further.
func internalFailure(err error) *Error {
	return NewError(CodeInternal, err.Error(), "")
}

// notFound reports an unknown reference together with what the registry does have,
// so the next call can be right instead of being a guess.
func notFound(kind, value string, candidates []Candidate) *Error {
	failure := NewError(CodeNotFound, fmt.Sprintf("no %s matches %q", kind, value),
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

// FromRPC maps a ConnectRPC failure onto the tool vocabulary. The resolvers call
// the same read services the API exposes, so every failure a caller could see
// there arrives here too and has to keep its meaning.
func FromRPC(err error) *Error {
	if err == nil {
		return nil
	}
	var failure *Error
	if errors.As(err, &failure) {
		return failure
	}
	switch connect.CodeOf(err) {
	case connect.CodeNotFound:
		return NewError(CodeNotFound, err.Error(), "search or list first to get a reference that exists")
	case connect.CodeInvalidArgument:
		return NewError(CodeInvalidArgument, err.Error(), "the server refused the arguments; the message says which")
	case connect.CodeFailedPrecondition:
		return NewError(CodeUnsupported, err.Error(), "this deployment cannot answer that as asked; the message says why")
	case connect.CodePermissionDenied:
		return NewError(CodePermissionDenied, err.Error(), "")
	case connect.CodeUnauthenticated:
		return NewError(CodeUnauthenticated, err.Error(), "the client's authorization is gone: authorize it again")
	case connect.CodeResourceExhausted:
		return NewError(CodeResourceExhausted, err.Error(), "back off before retrying")
	case connect.CodeUnavailable:
		return NewError(CodeUnavailable, err.Error(), "the server is not reachable right now; retrying may work")
	case connect.CodeDeadlineExceeded, connect.CodeCanceled:
		return NewError(CodeTimeout, err.Error(), "retry, or ask for less at once")
	default:
		return NewError(CodeInternal, err.Error(), "")
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
