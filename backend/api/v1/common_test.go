package v1

import (
	"context"
	"errors"
	"testing"

	"connectrpc.com/connect"
	pkgerrors "github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/Ranxy/metaxisdata/backend/common"
	storepb "github.com/Ranxy/metaxisdata/backend/generated-go/store"
	v1pb "github.com/Ranxy/metaxisdata/backend/generated-go/v1"
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
		{"a pkg/errors wrap does not hide the code", connect.NewError(connect.CodeInternal, pkgerrors.Wrap(common.Errorf(common.NotFound, "no such mapping"), "failed to delete namespace mapping")), connect.CodeNotFound},
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

// The store and v1 engine enums are value-compatible; every value a driver can
// connect to must round-trip, and anything else must collapse to UNSPECIFIED in
// both directions instead of leaking a driver-less engine to the client.
func TestConvertEngineRoundTrip(t *testing.T) {
	t.Parallel()

	for _, engine := range []storepb.Engine{
		storepb.Engine_MYSQL,
		storepb.Engine_POSTGRES,
		storepb.Engine_TIDB,
		storepb.Engine_MARIADB,
		storepb.Engine_OCEANBASE,
	} {
		require.Equal(t, engine, convertEngine(convertToEngine(engine)))
	}

	require.Equal(t, v1pb.Engine_ENGINE_UNSPECIFIED, convertToEngine(storepb.Engine(99)))
	require.Equal(t, storepb.Engine_ENGINE_UNSPECIFIED, convertEngine(v1pb.Engine(99)))
	require.Equal(t, v1pb.Engine_ENGINE_UNSPECIFIED, convertToEngine(storepb.Engine_ENGINE_UNSPECIFIED))
	require.Equal(t, storepb.Engine_ENGINE_UNSPECIFIED, convertEngine(v1pb.Engine_ENGINE_UNSPECIFIED))
}
