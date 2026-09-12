package v1

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
)

// Store and service code returns common.Code values; the interceptor is the
// only place that turns them into status codes. Without it a missing row or a
// duplicate key reached the client as CodeInternal.
func TestMapCommonError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want connect.Code
	}{
		{"nil stays nil", nil, 0},
		{"bare error is internal", errors.New("boom"), connect.CodeInternal},
		{"not found", common.Errorf(common.NotFound, "no such user"), connect.CodeNotFound},
		{"conflict", common.Errorf(common.Conflict, "duplicate email"), connect.CodeAlreadyExists},
		{"invalid", common.Errorf(common.Invalid, "bad id"), connect.CodeInvalidArgument},
		{"not authorized", common.Errorf(common.NotAuthorized, "nope"), connect.CodePermissionDenied},
		{"not implemented", common.Errorf(common.NotImplemented, "later"), connect.CodeUnimplemented},
		{"size exceeded", common.Errorf(common.SizeExceeded, "too big"), connect.CodeResourceExhausted},
		{"db error is internal", common.Errorf(common.DBExecutionError, "42P01"), connect.CodeInternal},
		{"wrapped store conflict keeps its code", errors.Join(common.Errorf(common.Conflict, "duplicate email"), errors.New("insert")), connect.CodeAlreadyExists},
		{"handler status wins", connect.NewError(connect.CodePermissionDenied, common.Errorf(common.NotFound, "x")), connect.CodePermissionDenied},
		{"handler already_exists wins", connect.NewError(connect.CodeAlreadyExists, common.Errorf(common.Conflict, "x")), connect.CodeAlreadyExists},
		{"internal is upgraded from the chain", connect.NewError(connect.CodeInternal, common.Errorf(common.NotFound, "x")), connect.CodeNotFound},
		{"internal without a common code stays internal", connect.NewError(connect.CodeInternal, errors.New("boom")), connect.CodeInternal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mapCommonError(tc.err)
			if tc.err == nil {
				require.NoError(t, got)
				return
			}
			require.Error(t, got)
			require.Equal(t, tc.want, connect.CodeOf(got))
		})
	}
}

// The interceptor must apply the mapping to whatever the handler returns.
func TestErrorMappingInterceptorWrapUnary(t *testing.T) {
	t.Parallel()

	interceptor := NewErrorMappingInterceptor()
	handler := interceptor.WrapUnary(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, common.Errorf(common.Conflict, "duplicate email")
	})

	_, err := handler(context.Background(), connect.NewRequest(&struct{}{}))
	require.Error(t, err)
	require.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))
}
