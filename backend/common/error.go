//nolint:revive
package common

import (
	"errors"
	"fmt"
	"io"

	pkgerrors "github.com/pkg/errors"
)

// Code is the error code.
type Code int

// Application error codes.
const (
	// 0 ~ 99 general error.
	Ok             Code = 0
	Internal       Code = 1
	NotAuthorized  Code = 2
	Invalid        Code = 3
	NotFound       Code = 4
	Conflict       Code = 5
	NotImplemented Code = 6
	SizeExceeded   Code = 7

	// 101 ~ 199 db error.
	DBExecutionError Code = 102
)

// Error represents an application-specific error. Application errors can be
// unwrapped by the caller to extract out the code & message.
//
// Any non-application error (such as a disk error) should be reported as an
// Internal error and the human user should only see "Internal error" as the
// message. These low-level internal error details should only be logged and
// reported to the operator of the application (not the end user).
type Error struct {
	// Machine-readable error code.
	Code Code

	// Embedded error.
	Err error
}

// Error implements the error interface. Not used by the application otherwise.
func (e *Error) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("common: error code %d", e.Code)
	}
	return e.Err.Error()
}

// Unwrap exposes the wrapped error so errors.Is/errors.As and the API boundary
// can inspect the cause behind an application code.
func (e *Error) Unwrap() error {
	return e.Err
}

// Format forwards formatting to the wrapped cause so %+v keeps the stack
// pkg/errors captured. fmt only consults the cause's Formatter through the
// outermost error, so without this an application error flattened to a message
// and lost the location of the failure.
func (e *Error) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if formatter, ok := e.Err.(fmt.Formatter); ok {
			formatter.Format(s, verb)
			return
		}
		_, _ = io.WriteString(s, e.Error())
	case 's':
		_, _ = io.WriteString(s, e.Error())
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", e.Error())
	default:
		// fmt consults Formatter for every verb; keep the message visible for
		// the rare ones (%#v in a test failure, %x) instead of printing nothing.
		_, _ = io.WriteString(s, e.Error())
	}
}

// ErrorCode unwraps an application error and returns its code.
// Non-application errors always return EINTERNAL.
func ErrorCode(err error) Code {
	if err == nil {
		return Ok
	} else if e, ok := errors.AsType[*Error](err); ok {
		return e.Code
	}
	return Internal
}

// Errorf is a helper function to create an Error with given code and formatted message.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{
		Code: code,
		Err:  pkgerrors.Errorf(format, args...),
	}
}

// FormatDBErrorEmptyRowWithQuery formats database error that query returns empty row.
func FormatDBErrorEmptyRowWithQuery(query string) error {
	return Errorf(DBExecutionError, "query %q returned empty row", query)
}
