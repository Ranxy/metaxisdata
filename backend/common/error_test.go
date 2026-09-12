package common

import (
	"errors"
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

func TestErrorCodeDefaultsToInternal(t *testing.T) {
	t.Parallel()

	require.Equal(t, Ok, ErrorCode(nil))
	require.Equal(t, Internal, ErrorCode(errors.New("plain")))
	require.Equal(t, NotFound, ErrorCode(Errorf(NotFound, "missing")))
	// The chain is inspected even when several errors are joined.
	require.Equal(t, Conflict, ErrorCode(errors.Join(Errorf(Conflict, "duplicate"), errors.New("insert"))))
	require.Equal(t, Internal, ErrorCode(&Error{Code: Internal}))
}
