package common

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorUnwrapAndNilSafeMessage(t *testing.T) {
	t.Parallel()

	cause := errors.New("relation does not exist")
	wrapped := &Error{Code: DBExecutionError, Err: cause}

	// The API boundary maps errors by walking the chain, so the wrapped error
	// has to be reachable.
	require.ErrorIs(t, wrapped, cause)
	require.Equal(t, DBExecutionError, ErrorCode(wrapped))
	require.Equal(t, "relation does not exist", wrapped.Error())

	// A code-only Error used to panic on Error(); it is reachable because
	// common.Error is also constructed by hand.
	codeOnly := &Error{Code: NotFound}
	require.NotPanics(t, func() { _ = codeOnly.Error() })
	require.Equal(t, NotFound, ErrorCode(codeOnly))
	require.NoError(t, codeOnly.Unwrap())
}

func TestErrorFormatHandlesVerbs(t *testing.T) {
	t.Parallel()

	stacked := Errorf(NotFound, "missing")
	plain := &Error{Code: Invalid, Err: errors.New("bad input")}
	codeOnly := &Error{Code: Internal}

	require.Equal(t, "missing", fmt.Sprintf("%v", stacked))
	require.Equal(t, "missing", fmt.Sprintf("%s", stacked))
	require.Equal(t, `"missing"`, fmt.Sprintf("%q", stacked))
	// fmt only reaches the cause's Formatter through the outermost error, so
	// %+v has to keep the location pkg/errors captured rather than flatten.
	require.Contains(t, fmt.Sprintf("%+v", stacked), "error_test.go:")

	// A cause without a stack still prints its message.
	require.Equal(t, "bad input", fmt.Sprintf("%v", plain))
	require.Equal(t, "bad input", fmt.Sprintf("%s", plain))

	// A code-only error falls back to its message instead of panicking.
	require.Equal(t, "common: error code 1", fmt.Sprintf("%v", codeOnly))

	// fmt consults Formatter for every verb; an unhandled one must not print "".
	require.NotEmpty(t, fmt.Sprintf("%#v", plain))
}

func TestErrorCodeDefaultsToInternal(t *testing.T) {
	t.Parallel()

	require.Equal(t, Ok, ErrorCode(nil))
	require.Equal(t, Internal, ErrorCode(errors.New("plain")))
	require.Equal(t, NotFound, ErrorCode(Errorf(NotFound, "missing")))
	// The chain is inspected even when several errors are joined.
	require.Equal(t, Conflict, ErrorCode(errors.Join(Errorf(Conflict, "duplicate"), errors.New("insert"))))
	require.Equal(t, Internal, ErrorCode(&Error{Code: Internal}))
}
